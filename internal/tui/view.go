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

		b.WriteString("\n")
	}

	if len(m.checkRuns) == 0 {
		return tea.NewView(b.String() + m.renderStartupPhase())
	}

	widths := CalculateColumnWidths(m.checkRuns, m.headCommitTime, m.jobAverages)

	// Compute the synthetic Copilot review row once and reuse it for both
	// column-width widening and the row render. Calling buildCopilotCheckRun
	// twice would take two time.Now() snapshots, and the queued/in_progress
	// branch inside it (gated on time.Until(copilotPollStartTime)) could
	// evaluate differently between the two calls if the countdown crosses
	// zero mid-render — leading the widths calc and the rendered row to
	// disagree on status.
	//
	// This call site is intentionally below the len(m.checkRuns)==0 early
	// return above: during the Actions startup phase the Copilot row is not
	// rendered (see the comment by renderCopilotStatusLine below for why
	// that tradeoff is acceptable). Hoisting it above the early return would
	// resurface the row during startup but would also require re-evaluating
	// the column-width path for an empty checkRuns slice.
	copilotRow := m.buildCopilotCheckRun()

	// Ensure the column geometry accommodates the synthetic Copilot review
	// row when present, so the row never truncates its own name or the
	// countdown/elapsed text in the duration column.
	if copilotRow != nil {
		widths = widenForCopilotRow(widths, *copilotRow, m.copilotPollStartTime)
	}

	headerQueue, headerName, headerDuration, headerAvg := FormatHeaderColumns(widths)
	b.WriteString(m.styles.Header.Render(fmt.Sprintf("%s   %s  %s  %s\n", headerQueue, headerName, headerDuration, headerAvg)))
	b.WriteString("\n")

	for _, check := range m.checkRuns {
		checkLine := m.renderCheckRun(check, widths)
		b.WriteString(checkLine)

	// Render the summary line for failed checks. (Synthetic Copilot
	// review rows never appear in m.checkRuns — they are rendered at the
	// separate copilotRow site below — so the summary path for them is
	// also reached there, not here.)
	if check.Summary != "" && (check.Conclusion == "failure" || check.Conclusion == "timed_out") {
		b.WriteString(m.renderSummary(check, widths))
	}

		if (check.Conclusion == "failure" || check.Conclusion == "timed_out") && len(check.Annotations) > 0 {
			b.WriteString(m.renderErrorBox(check, widths))
		}
	}

	// Copilot review row (issue #409). The row is display-only: it never
	// enters m.checkRuns, so allChecksComplete, SortCheckRuns, and
	// determineExitCode are unaffected. It is always rendered last,
	// regardless of state, to keep its position predictable.
	if copilotRow != nil {
		b.WriteString(m.renderCheckRun(*copilotRow, widths))
		// Surface the stale-review Summary below the synthetic row,
		// mirroring the failed-check summary render inside the loop
		// above. The stale case is the only Copilot state that sets
		// Summary today, but gating on Summary != "" keeps this future-
		// proof for other review states that may carry guidance.
		if copilotRow.Summary != "" {
			b.WriteString(m.renderSummary(*copilotRow, widths))
		}
	}

	// Copilot review status line (issue #409). Rendered below the Copilot
	// table row, not in the header, so the spinner/countdown line sits
	// visually next to the row it describes.
	//
	// Startup-phase tradeoff: this call site is below the
	// len(m.checkRuns)==0 early return above, so during the Actions startup
	// phase (before any check appears, typically 30-90s after PR creation)
	// NEITHER the synthetic Copilot row NOR this status line is rendered.
	// That reopens a narrow window that an earlier revision (commit dd23bdd)
	// had closed for the status line. We accept this because:
	//   - copilotWaitStartTime is armed in PRInfoMsg, which fires before
	//     any check appears, so the suppression window is bounded by how
	//     long Actions takes to surface its first check, not by Copilot.
	//   - For that window the only information lost is the initial-delay
	//     countdown ("Copilot review queued, polling in 15s…"), which is
	//     itself bounded by copilot_initial_delay (default 15s) and is
	//     low-value: the user already knows from the PR-title block that a
	//     watch is running, and the row would only show "⏸ in 15s" before
	//     transitioning to "◐ in progress…".
	//   - Polling is armed in PRInfoMsg independently of this render path,
	//     so suppression here is display-only and does not delay the first
	//     Copilot fetch.
	// If user feedback changes the calculus, surface the status line from
	// renderStartupPhase() (which IS reached during the startup phase) rather
	// than hoisting this call above the early return, so the table geometry
	// path is not perturbed for an empty checkRuns slice.
	// renderCopilotStatusLine self-gates via copilotWaitStartTime and
	// returns "" for terminal non-stale states.
	if m.waitForCopilot {
		b.WriteString(m.renderCopilotStatusLine())
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

// renderCheckRun displays a single check run with aligned columns.
//
// For regular check runs (Kind == "") the four columns are populated from
// timing data as before. For Copilot review rows (Kind == "review") the
// queue and avg columns are left blank (reviews have no queue latency or
// historical averages), the icon and style come from the review state, and
// the duration column either shows a countdown (while queued in the
// initial-delay window), an elapsed runtime (while pending), or "-"
// (once complete — reviews expose no timestamps for FinalDuration).
func (m Model) renderCheckRun(check ghclient.CheckRunInfo, widths ColumnWidths) string {
	if check.Kind == "review" {
		return m.renderCopilotReviewCheckRun(check, widths)
	}
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

// renderCopilotReviewCheckRun renders a synthetic Copilot review row using
// the same column geometry as renderCheckRun but with review-specific
// icon, style, and blank queue/avg columns.
func (m Model) renderCopilotReviewCheckRun(check ghclient.CheckRunInfo, widths ColumnWidths) string {
	icon := GetCopilotReviewIcon(check.ReviewState)

	style := m.styles.Queued
	switch check.ReviewState {
	case "approved":
		style = m.styles.Success
	case "changes_requested":
		style = m.styles.Failure
	case "commented":
		style = m.styles.Info
	case "dismissed":
		style = m.styles.Queued
	case "pending", "stale":
		style = m.styles.Running
	}

	nameCol := BuildNameColumn(check, widths, false)

	// Duration column: countdown while in the initial-delay window, elapsed
	// runtime while in_progress (StartedAt was set to copilotPollStartTime
	// by buildCopilotCheckRun so FormatDuration falls through to Runtime),
	// or "-" once completed (timestamps are nil). Right-align to the
	// column width, mirroring FormatAlignedColumns' duration padding
	// without computing and discarding the queue/name/avg columns.
	//
	// copilotDurationText is shared with widenForCopilotRow so the width
	// calc and the rendered row agree on the exact string — including the
	// "in Xs" countdown that FormatDuration alone does not synthesize.
	durationText := copilotDurationText(check, m.copilotPollStartTime)
	durationPad := max(widths.DurationWidth-runewidth.StringWidth(durationText), 0)
	durationCol := strings.Repeat(" ", durationPad) + durationText

	// Reviews carry no queue latency or historical average: blank both
	// columns to keep the row aligned with the table geometry.
	queueCol := strings.Repeat(" ", widths.QueueWidth)
	avgCol := strings.Repeat(" ", widths.AvgWidth)

	styledIcon := style.Render(icon)
	styledDuration := style.Render(durationCol)
	styledName := style.Render(nameCol)

	return queueCol + " " + styledIcon + " " + styledName + "  " + styledDuration + "  " + avgCol + "\n"
}

// renderCopilotStatusLine renders the Copilot review status below the
// Copilot table row: a spinner + countdown/elapsed line while pending.
// Returns "" when the review is in a terminal state (the table row
// above conveys that state on its own, and stale-state guidance is
// surfaced via the row's Summary line through renderSummary) or when
// the gate has not yet been armed.
func (m Model) renderCopilotStatusLine() string {
	if m.copilotWaitStartTime.IsZero() {
		return ""
	}
	if m.copilotPending && !m.copilotStale {
		remaining := time.Until(m.copilotPollStartTime)
		if remaining > 0 {
			// Still inside the initial-delay window (copilotPollStartTime
			// is set to PRInfoMsg-time + copilotInitialDelay). Polling
			// hasn't started yet, so show a countdown rather than "0s".
			return fmt.Sprintf("%s %s\n", m.spinner.View(),
				m.styles.Running.Render(fmt.Sprintf("Copilot review queued, polling in %s…", timing.FormatDuration(remaining))))
		}
		elapsed := time.Since(m.copilotPollStartTime)
		return fmt.Sprintf("%s %s\n", m.spinner.View(),
			m.styles.Running.Render(fmt.Sprintf("Copilot review in progress… (%s elapsed)", timing.FormatDuration(elapsed))))
	}
	return ""
}

// shortHeadSHA returns the first 7 characters of a git SHA, matching GitHub's
// short-SHA display convention. Returns "" when the SHA is empty.
func shortHeadSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// buildCopilotCheckRun synthesizes a display-only CheckRunInfo representing
// the current Copilot review state for the PR's HEAD commit. Returns nil
// when no row should be rendered:
//   - waitForCopilot disabled
//   - review never armed (copilotWaitStartTime is zero)
//   - not-requested with no stale review to surface
//
// The returned row never enters m.checkRuns; it is rendered separately as
// the last row of the table and is excluded from SortCheckRuns,
// allChecksComplete, and determineExitCode. Timing fields are populated
// only to drive the duration-column display:
//   - queued:   StartedAt/CompletedAt nil (FormatDuration falls to "-")
//   - in_progress: StartedAt = copilotPollStartTime (Runtime yields elapsed)
//   - completed: StartedAt/CompletedAt nil (FormatDuration returns "-")
func (m Model) buildCopilotCheckRun() *ghclient.CheckRunInfo {
	if !m.waitForCopilot {
		return nil
	}
	// Only render once the Copilot gate has been armed by PRInfoMsg.
	if m.copilotWaitStartTime.IsZero() {
		return nil
	}

	const (
		kind       = "review"
		workflow   = "Copilot"
		name       = "Review"
		detailsURL = ""
	)

	row := &ghclient.CheckRunInfo{
		Kind:         kind,
		WorkflowName: workflow,
		Name:         name,
		DetailsURL:   detailsURL,
	}

	switch {
	case m.copilotStale:
		row.Status = "completed"
		row.ReviewState = "stale"
		// Surface the stale-state guidance as a Summary line below the
		// row (rendered via renderSummary). GetCopilotReviewIcon gives
		// "stale" its own "⚠" icon, but the short SHA + actionable hint
		// are too long for the name column, so they live here — this
		// restores the information the pre-refactor status line carried.
		row.Summary = fmt.Sprintf("Copilot review is stale (HEAD is %s) — refresh or re-request", shortHeadSHA(m.headSHA))
	case m.copilotPending && !m.copilotReviewComplete:
		// Pending: either still inside the initial-delay window (queued,
		// countdown shown in duration column) or actively polling
		// (in_progress, elapsed shown).
		if !m.copilotPollStartTime.IsZero() && time.Until(m.copilotPollStartTime) > 0 {
			row.Status = "queued"
			row.ReviewState = "pending"
		} else {
			row.Status = "in_progress"
			row.ReviewState = "pending"
			start := m.copilotPollStartTime
			row.StartedAt = &start
		}
	default:
		// Review reached a terminal state (approved/commented/dismissed/
		// changes_requested) or copilotReviewComplete was set with an empty
		// copilotState (e.g. two-consecutive-not-requested). Suppress the
		// row entirely if there's nothing to show.
		if !m.copilotReviewComplete || m.copilotState == "" {
			return nil
		}
		row.Status = "completed"
		row.ReviewState = m.copilotState
	}
	return row
}

// copilotDurationText returns the string that renderCopilotReviewCheckRun
// will display in the duration column for a synthetic Copilot review row:
//
//   - queued + inside the initial-delay window: "in <remaining>" countdown
//     (e.g. "in 15s"), where remaining = time.Until(copilotPollStartTime)
//   - queued + delay elapsed, or in_progress: the elapsed-runtime text
//     FormatDuration derives from StartedAt (set to copilotPollStartTime
//     by buildCopilotCheckRun), or "-" if StartedAt is nil
//   - completed (or any other status): "-" — reviews expose no timestamps
//     for FinalDuration
//
// widenForCopilotRow calls this same helper so the width calc and the
// rendered row agree on the exact string, including the "in <remaining>"
// countdown that FormatDuration(check) alone does not synthesize (it
// returns "-" for queued rows). Without this shared path, a configured
// copilot_initial_delay large enough to produce a wider countdown than
// any other row's duration text (e.g. "in 1m 30s") would overflow the
// pre-computed DurationWidth and misalign the Copilot row against the
// header for the duration of the countdown.
func copilotDurationText(check ghclient.CheckRunInfo, copilotPollStartTime time.Time) string {
	if check.Status == "queued" && !copilotPollStartTime.IsZero() {
		remaining := time.Until(copilotPollStartTime)
		if remaining > 0 {
			return "in " + timing.FormatDuration(remaining)
		}
	}
	return FormatDuration(check)
}

// widenForCopilotRow grows column widths to fit the synthetic Copilot row,
// mirroring the per-column logic in CalculateColumnWidths but without
// touching the queue column (which is blank for reviews) and capping the
// name column at maxCheckNameWidth. This keeps the header row and check rows
// aligned when the Copilot row would otherwise exceed the geometry derived
// from m.checkRuns alone.
func widenForCopilotRow(widths ColumnWidths, row ghclient.CheckRunInfo, copilotPollStartTime time.Time) ColumnWidths {
	name := FormatCheckName(row)
	nameLen := runewidth.StringWidth(name)
	if nameLen > widths.NameWidth {
		if nameLen <= maxCheckNameWidth {
			widths.NameWidth = nameLen
		} else {
			widths.NameWidth = maxCheckNameWidth
		}
	}
	// Duration column may show "in 15s" style countdown text while queued
	// (synthesized by copilotDurationText, NOT by FormatDuration which
	// returns "-" for queued rows), or the elapsed runtime while
	// in_progress. Use the shared helper so the width calc sees the same
	// string the row will render, including the countdown prefix.
	durationText := copilotDurationText(row, copilotPollStartTime)
	if runewidth.StringWidth(durationText) > widths.DurationWidth {
		widths.DurationWidth = runewidth.StringWidth(durationText)
	}
	return widths
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
