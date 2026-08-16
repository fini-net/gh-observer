package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/fini-net/gh-observer/internal/timing"
	"github.com/mattn/go-runewidth"
)

// View renders the current state
func (m Model) View() tea.View {
	if m.err != nil {
		return tea.NewView(m.styles.Error.Render(fmt.Sprintf("Error: %v\n", m.err)))
	}

	var b strings.Builder

	if m.prTitle != "" {
		prInfo := m.styles.Header.Render(fmt.Sprintf("PR #%d: %s", m.prNumber, m.prTitle))
		utcTime := time.Now().UTC().Format("15:04:05 UTC")
		timeSinceUpdate := time.Since(m.lastUpdate)

		var updatedLine string
		if !m.headCommitTime.IsZero() {
			timeSincePush := time.Since(m.headCommitTime)
			updatedLine = fmt.Sprintf("Updated %s ago  •  Pushed %s ago",
				timing.FormatDuration(timeSinceUpdate),
				timing.FormatDuration(timeSincePush))
		} else {
			updatedLine = fmt.Sprintf("Updated %s ago", timing.FormatDuration(timeSinceUpdate))
		}

		// Add historical averages status
		if !m.noAvg {
			isFetching := m.avgFetchPending || len(m.pendingWorkflowFetch) > 0
			if isFetching {
				// Fetch in progress - show elapsed time
				elapsed := time.Since(m.avgFetchStartTime)
				updatedLine += m.styles.Running.Render(fmt.Sprintf("  •  Fetching historical averages... (%s)", timing.FormatDuration(elapsed)))
			} else if m.avgFetchErr != nil {
				// Fetch failed
				updatedLine += m.styles.Queued.Render("  •  Historical averages unavailable")
			} else if m.avgFetchLastDuration > 0 {
				// Fetch succeeded - show workflow count and last fetch duration
				wfCount := len(m.fetchedWorkflowIDs)
				updatedLine += m.styles.Info.Render(fmt.Sprintf("  •  Historical averages ready (%d workflows, %s)", wfCount, timing.FormatDuration(m.avgFetchLastDuration)))
			}
		}

		fmt.Fprintf(&b, "%s %s\n", prInfo, utcTime)
		fmt.Fprintf(&b, "%s\n", updatedLine)

		// Copilot review status line (issue #409)
		if m.waitForCopilot {
			b.WriteString(m.renderCopilotStatusLine())
		}

		b.WriteString("\n")
	}

	if len(m.checkRuns) == 0 {
		return tea.NewView(b.String() + m.renderStartupPhase())
	}

	widths := CalculateColumnWidths(m.checkRuns, m.headCommitTime, m.jobAverages)

	headerQueue, headerName, headerDuration, headerAvg := FormatHeaderColumns(widths)
	b.WriteString(m.styles.Header.Render(fmt.Sprintf("%s   %s  %s  %s\n", headerQueue, headerName, headerDuration, headerAvg)))
	b.WriteString("\n")

	for _, check := range m.checkRuns {
		checkLine := m.renderCheckRun(check, widths)
		b.WriteString(checkLine)

		if check.Summary != "" && (check.Conclusion == "failure" || check.Conclusion == "timed_out") {
			b.WriteString(m.renderSummary(check, widths))
		}

		if (check.Conclusion == "failure" || check.Conclusion == "timed_out") && len(check.Annotations) > 0 {
			b.WriteString(m.renderErrorBox(check, widths))
		}
	}

	// Copilot review row (issue #409) — no queue/duration/avg columns since
	// reviews have no startedAt/completedAt timestamps. Rendered as a simple
	// icon + label line aligned under the check table's icon column.
	if m.waitForCopilot && m.copilotReviewComplete && m.copilotState != "" {
		b.WriteString(m.renderCopilotReviewRow(widths))
	}

	b.WriteString("\n")

	if allChecksComplete(m.checkRuns) && !canTrustCompletion(&m) {
		b.WriteString(m.styles.Queued.Render("  ⏳ Waiting for more checks to appear...\n"))
		if m.expectedCheckCount > 0 {
			fmt.Fprintf(&b, m.styles.Queued.Render("  Seen %d of ~%d expected checks (%d%% threshold: %d%%)\n"),
				len(m.checkRuns), m.expectedCheckCount,
				int(minCheckAppearanceRatio*100),
				int(float64(len(m.checkRuns))/float64(m.expectedCheckCount)*100))
		} else if m.noAvg {
			b.WriteString(m.styles.Queued.Render("  Waiting for all seen checks to finish...\n"))
		} else {
			elapsed := time.Since(m.firstCheckSeenAt)
			remaining := startupGracePeriod - elapsed
			if remaining > 0 {
				fmt.Fprintf(&b, m.styles.Queued.Render("  Grace period: %s remaining\n"),
					timing.FormatDuration(remaining))
			}
		}
		b.WriteString("\n")
	}

	// Two-tier rate-limit indicator: red under minRateLimitForFetch, yellow
	// under rateWarningThreshold. Only render once we've actually received a
	// response — before that, rateLimitRemaining is the Go zero value (0) and
	// showing "[Rate limit: 0 remaining]" in red would be misleading.
	if m.fetchReceived {
		if m.rateLimitRemaining < minRateLimitForFetch {
			b.WriteString(m.styles.Failure.Render(fmt.Sprintf("  [Rate limit: %d remaining]", m.rateLimitRemaining)))
		} else if m.rateLimitRemaining < rateWarningThreshold {
			b.WriteString(m.styles.Running.Render(fmt.Sprintf("  [Rate limit: %d remaining]", m.rateLimitRemaining)))
		}
	}

	b.WriteString("\n")

	if !m.quitting {
		b.WriteString("\nPress q to quit\n")
	}

	return tea.NewView(b.String())
}

// renderErrorBox displays error annotations for failed checks
func (m Model) renderErrorBox(check ghclient.CheckRunInfo, widths ColumnWidths) string {
	var b strings.Builder

	for _, ann := range check.Annotations {
		var errorMsg string
		if ann.Message != "" {
			errorMsg = ann.Message
			if ann.Title != "" {
				errorMsg = ann.Title + ": " + errorMsg
			}
		} else if ann.Title != "" {
			errorMsg = ann.Title
		} else {
			continue
		}

		if ann.Path != "" {
			if ann.StartLine > 0 {
				errorMsg = fmt.Sprintf("%s:%d - %s", ann.Path, ann.StartLine, errorMsg)
			} else {
				errorMsg = fmt.Sprintf("%s - %s", ann.Path, errorMsg)
			}
		}
		b.WriteString("  ")
		b.WriteString(m.styles.ErrorBox.Render(errorMsg))
		b.WriteString("\n")
	}

	if b.Len() > 0 {
		b.WriteString("\n")
	}

	return b.String()
}

// renderDescription displays check description as a dimmed line below the check
func (m Model) renderSummary(check ghclient.CheckRunInfo, widths ColumnWidths) string {
	if check.Summary == "" {
		return ""
	}
	indent := widths.QueueWidth + 3
	return fmt.Sprintf("%s%s\n", strings.Repeat(" ", indent), m.styles.Description.Render(check.Summary))
}

// renderCheckRun displays a single check run with aligned columns
func (m Model) renderCheckRun(check ghclient.CheckRunInfo, widths ColumnWidths) string {
	status := check.Status
	conclusion := check.Conclusion

	nameCol := BuildNameColumn(check, widths, m.enableLinks)

	// Get column data (plain text)
	queueText := FormatQueueLatency(check, m.headCommitTime)
	durationText := FormatDuration(check)
	avgText := FormatAvg(check, m.jobAverages)

	// Determine icon and style
	icon := GetCheckIcon(status, conclusion)
	var style = m.styles.Queued

	switch status {
	case "completed":
		switch conclusion {
		case "success":
			style = m.styles.Success
		case "failure", "timed_out":
			style = m.styles.Failure
		case "cancelled", "skipped":
			style = m.styles.Queued
		case "action_required":
			style = m.styles.Running
		default:
			style = m.styles.Queued
		}
	case "in_progress":
		style = m.styles.Running
	case "queued":
		style = m.styles.Queued
	}

	// Compute queue, duration, and avg columns; discard the name return value since
	// nameCol was already built correctly by BuildNameColumn above.
	queueCol, _, durationCol, avgCol := FormatAlignedColumns(queueText, FormatCheckNameWithTruncate(check, widths.NameWidth), durationText, avgText, widths)

	// Apply styling to icon, duration, and avg
	styledIcon := style.Render(icon)
	styledDuration := style.Render(durationCol)
	styledAvg := style.Render(avgCol)

	// Apply styling to name only if it failed
	styledName := nameCol
	if conclusion == "failure" || conclusion == "timed_out" {
		styledName = style.Render(nameCol)
	}

	// Assemble line: [queue][1 space][icon][1 space][name][2 spaces][duration][2 spaces][avg][newline]
	return queueCol + " " + styledIcon + " " + styledName + "  " + styledDuration + "  " + styledAvg + "\n"
}

// renderCopilotStatusLine renders the Copilot review status under the PR
// header: a spinner + elapsed line while pending, or a yellow stale warning
// (issue #409).
func (m Model) renderCopilotStatusLine() string {
	if m.copilotPending && !m.copilotStale {
		remaining := time.Until(m.copilotPollStartTime)
		if remaining > 0 {
			// Still inside the initial-delay window (copilotPollStartTime is
			// set to PRInfoMsg-time + copilotInitialDelay). Polling hasn't
			// started yet, so show a countdown rather than "0s elapsed".
			return fmt.Sprintf("%s %s\n", m.spinner.View(),
				m.styles.Running.Render(fmt.Sprintf("Copilot review queued, polling in %s…", timing.FormatDuration(remaining))))
		}
		elapsed := time.Since(m.copilotPollStartTime)
		return fmt.Sprintf("%s %s\n", m.spinner.View(),
			m.styles.Running.Render(fmt.Sprintf("Copilot review in progress… (%s elapsed)", timing.FormatDuration(elapsed))))
	}
	if m.copilotStale {
		shortHead := m.headSHA
		if len(shortHead) > 7 {
			shortHead = shortHead[:7]
		}
		return m.styles.Running.Render(
			fmt.Sprintf("⚠ Copilot review is stale (HEAD is %s) — refresh or re-request\n", shortHead))
	}
	return ""
}

// renderCopilotReviewRow renders the completed Copilot review as a row in the
// check table with an icon by state, but no queue-latency or runtime columns
// (reviews have no startedAt/completedAt — issue #409).
func (m Model) renderCopilotReviewRow(widths ColumnWidths) string {
	icon := GetCopilotReviewIcon(m.copilotState)

	var style = m.styles.Queued
	switch m.copilotState {
	case "approved":
		style = m.styles.Success
	case "changes_requested":
		style = m.styles.Failure
	case "commented":
		style = m.styles.Info
	case "dismissed":
		style = m.styles.Queued
	}

	// Pad the queue column with spaces (reviews have no queue latency) and
	// render the name as "Copilot / <state>" to match the Workflow/Job format.
	queueCol := strings.Repeat(" ", widths.QueueWidth)
	nameCol := fmt.Sprintf("Copilot / %s", m.copilotState)

	// Apply same width-based truncation as check names
	if runewidth.StringWidth(nameCol) > widths.NameWidth {
		nameCol = runewidth.Truncate(nameCol, widths.NameWidth, "…")
	}
	namePadding := max(widths.NameWidth-runewidth.StringWidth(nameCol), 0)
	paddedName := nameCol + strings.Repeat(" ", namePadding)

	// No duration or avg columns — pad with spaces to align with the table
	durPad := strings.Repeat(" ", widths.DurationWidth)
	avgPad := strings.Repeat(" ", widths.AvgWidth)

	return queueCol + " " + style.Render(icon) + " " + style.Render(paddedName) + "  " + durPad + "  " + avgPad + "\n"
}

// renderStartupPhase shows helpful message during GitHub Actions startup delay
func (m Model) renderStartupPhase() string {
	sinceStart := time.Since(m.startTime)

	var b strings.Builder

	if sinceStart < slowJobThreshold {
		fmt.Fprintf(&b, "%s ", m.spinner.View())
		b.WriteString(m.styles.Running.Render(fmt.Sprintf("Startup Phase (%s elapsed):\n", timing.FormatDuration(sinceStart))))
		b.WriteString("  ⏳ Waiting for Actions to start...\n")
		b.WriteString("  💡 GitHub typically takes 30-90s to queue jobs after PR creation\n")
	} else if sinceStart < verySlowJobThreshold {
		fmt.Fprintf(&b, "%s ", m.spinner.View())
		b.WriteString(m.styles.Running.Render(fmt.Sprintf("Still waiting (%s elapsed)...\n", timing.FormatDuration(sinceStart))))
		b.WriteString("  ⏳ Checks may be delayed or not configured for this PR\n")
	} else {
		b.WriteString(m.styles.Queued.Render("No checks found.\n"))
		b.WriteString("  This PR may not have workflows configured, or they may have been skipped.\n")
	}

	return b.String()
}
