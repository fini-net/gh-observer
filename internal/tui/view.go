package tui

import (
	"fmt"
	"strings"
	"time"

	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/fini-net/gh-observer/internal/timing"
)

// View renders the current state
func (m Model) View() string {
	if m.err != nil {
		return m.styles.Error.Render(fmt.Sprintf("Error: %v\n", m.err))
	}

	var b strings.Builder

	// Header
	if m.prTitle != "" {
		// Render PR info (bold and underlined)
		prInfo := m.styles.Header.Render(fmt.Sprintf("PR #%d: %s", m.prNumber, m.prTitle))

		// Get current UTC time (not bold)
		utcTime := time.Now().UTC().Format("15:04:05 UTC")

		// Get time since last update
		timeSinceUpdate := time.Since(m.lastUpdate)

		// Get time since last push
		var updatedLine string
		if !m.headCommitTime.IsZero() {
			timeSincePush := time.Since(m.headCommitTime)
			updatedLine = fmt.Sprintf("Updated %s ago  •  Pushed %s ago",
				timing.FormatDuration(timeSinceUpdate),
				timing.FormatDuration(timeSincePush))
		} else {
			updatedLine = fmt.Sprintf("Updated %s ago", timing.FormatDuration(timeSinceUpdate))
		}

		// PR info and UTC time on first line
		b.WriteString(fmt.Sprintf("%s %s\n", prInfo, utcTime))
		// Last updated on second line
		b.WriteString(fmt.Sprintf("%s\n", updatedLine))
		b.WriteString("\n")
	}

	// Startup phase handling
	if len(m.checkRuns) == 0 {
		return b.String() + m.renderStartupPhase()
	}

	// Calculate column widths once
	widths := CalculateColumnWidths(m.checkRuns, m.headCommitTime, m.enableLinks)

	// Render column headers with matching alignment
	headerQueue, headerName, headerDuration := FormatHeaderColumns(widths)
	b.WriteString(m.styles.Header.Render(fmt.Sprintf("%s   %s  %s\n", headerQueue, headerName, headerDuration)))
	b.WriteString("\n")

	// Render each check with aligned columns and error boxes
	for _, check := range m.checkRuns {
		checkLine := m.renderCheckRun(check, widths)
		b.WriteString(checkLine)

		// Render error annotations box for failed jobs
		if (check.Conclusion == "failure" || check.Conclusion == "timed_out") && len(check.Annotations) > 0 {
			b.WriteString(m.renderErrorBox(check, widths))
		}
	}

	// Footer
	b.WriteString("\n")

	if m.rateLimitRemaining < 100 {
		b.WriteString(m.styles.Running.Render(fmt.Sprintf("  [Rate limit: %d remaining]", m.rateLimitRemaining)))
	}

	b.WriteString("\n")

	// Only show quit message if not quitting
	if !m.quitting {
		b.WriteString("\nPress q to quit\n")
	}

	return b.String()
}

// renderErrorBox displays error annotations for failed checks
func (m Model) renderErrorBox(check ghclient.CheckRunInfo, widths ColumnWidths) string {
	var b strings.Builder

	for _, ann := range check.Annotations {
		// Format the error message
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

		// Add file path if available
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

	// Add spacing if we rendered any errors
	if b.Len() > 0 {
		b.WriteString("\n")
	}

	return b.String()
}

// renderCheckRun displays a single check run with aligned columns
func (m Model) renderCheckRun(check ghclient.CheckRunInfo, widths ColumnWidths) string {
	status := check.Status
	conclusion := check.Conclusion

	// Format name with clickable link
	name := FormatCheckNameWithLink(check, widths.NameWidth, m.enableLinks)

	// Get column data (plain text)
	queueText := FormatQueueLatency(check, m.headCommitTime)
	durationText := FormatDuration(check)

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

	// Build columns with explicit padding using strings.Repeat
	// This avoids fmt.Sprintf format specifier issues with ANSI codes
	queueCol, nameCol, durationCol := FormatAlignedColumns(queueText, name, durationText, widths)

	// Apply styling to icon and duration
	styledIcon := style.Render(icon)
	styledDuration := style.Render(durationCol)

	// Apply styling to name only if it failed
	styledName := nameCol
	if conclusion == "failure" || conclusion == "timed_out" {
		styledName = style.Render(nameCol)
	}

	// Assemble line: [queue][1 space][icon][1 space][name][2 spaces][duration][newline]
	return queueCol + " " + styledIcon + " " + styledName + "  " + styledDuration + "\n"
}

// renderStartupPhase shows helpful message during GitHub Actions startup delay
func (m Model) renderStartupPhase() string {
	sinceStart := time.Since(m.startTime)

	var b strings.Builder

	if sinceStart < 2*time.Minute {
		b.WriteString(fmt.Sprintf("%s ", m.spinner.View()))
		b.WriteString(m.styles.Running.Render(fmt.Sprintf("Startup Phase (%s elapsed):\n", timing.FormatDuration(sinceStart))))
		b.WriteString("  ⏳ Waiting for Actions to start...\n")
		b.WriteString("  💡 GitHub typically takes 30-90s to queue jobs after PR creation\n")
	} else if sinceStart < 3*time.Minute {
		b.WriteString(fmt.Sprintf("%s ", m.spinner.View()))
		b.WriteString(m.styles.Running.Render(fmt.Sprintf("Still waiting (%s elapsed)...\n", timing.FormatDuration(sinceStart))))
		b.WriteString("  ⏳ Checks may be delayed or not configured for this PR\n")
	} else {
		b.WriteString(m.styles.Queued.Render("No checks found.\n"))
		b.WriteString("  This PR may not have workflows configured, or they may have been skipped.\n")
	}

	return b.String()
}
