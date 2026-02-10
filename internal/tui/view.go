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
	if m.quitting {
		return ""
	}

	if m.err != nil {
		return m.styles.Error.Render(fmt.Sprintf("Error: %v\n", m.err))
	}

	var b strings.Builder

	// Header
	if m.prTitle != "" {
		b.WriteString(m.styles.Header.Render(fmt.Sprintf("PR #%d: %s\n", m.prNumber, m.prTitle)))
		b.WriteString("\n")
	}

	// Startup phase handling
	if len(m.checkRuns) == 0 {
		return b.String() + m.renderStartupPhase()
	}

	// Render check runs
	b.WriteString(m.renderCheckRuns())

	// Footer
	b.WriteString("\n")
	timeSinceUpdate := time.Since(m.lastUpdate)
	b.WriteString(m.styles.Info.Render(fmt.Sprintf("Last updated: %s ago", timing.FormatDuration(timeSinceUpdate))))

	if m.rateLimitRemaining < 100 {
		b.WriteString(m.styles.Running.Render(fmt.Sprintf("  [Rate limit: %d remaining]", m.rateLimitRemaining)))
	}

	b.WriteString("\n\n")
	b.WriteString("Press q to quit\n")

	return b.String()
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

// formatQueueLatency returns the queue time text or placeholder
func (m Model) formatQueueLatency(check ghclient.CheckRunInfo) string {
	if check.Status == "queued" {
		// For queued checks, show time since commit
		if !m.headCommitTime.IsZero() {
			return timing.FormatDuration(time.Since(m.headCommitTime))
		}
		return "-"
	}

	// For non-queued checks, show actual queue latency
	queueLatency := timing.QueueLatency(m.headCommitTime, check)
	if queueLatency > 0 {
		return timing.FormatDuration(queueLatency)
	}
	return "-"
}

// formatDuration returns the duration/runtime text or placeholder
func (m Model) formatDuration(check ghclient.CheckRunInfo) string {
	switch check.Status {
	case "completed":
		duration := timing.FinalDuration(check)
		if duration > 0 {
			return timing.FormatDuration(duration)
		}
		return "-"
	case "in_progress":
		runtime := timing.Runtime(check)
		if runtime > 0 {
			return timing.FormatDuration(runtime)
		}
		return "-"
	default:
		// For queued or unknown status
		return "-"
	}
}

// calculateColumnWidths scans all check runs and determines max width for each column
func (m Model) calculateColumnWidths() ColumnWidths {
	const (
		minNameWidth = 20
		maxNameWidth = 60
		minTimeWidth = 5 // "1m 2s"
	)

	widths := ColumnWidths{
		QueueWidth:    minTimeWidth,
		NameWidth:     minNameWidth,
		DurationWidth: minTimeWidth,
	}

	for _, check := range m.checkRuns {
		// Measure queue latency text
		queueText := m.formatQueueLatency(check)
		if len(queueText) > widths.QueueWidth {
			widths.QueueWidth = len(queueText)
		}

		// Measure name (Workflow / Job format)
		name := check.Name
		if check.WorkflowName != "" {
			name = fmt.Sprintf("%s / %s", check.WorkflowName, check.Name)
		}
		nameLen := len(name)
		if nameLen > widths.NameWidth && nameLen <= maxNameWidth {
			widths.NameWidth = nameLen
		} else if nameLen > maxNameWidth {
			widths.NameWidth = maxNameWidth
		}

		// Measure duration text
		durationText := m.formatDuration(check)
		if len(durationText) > widths.DurationWidth {
			widths.DurationWidth = len(durationText)
		}
	}

	return widths
}

// renderCheckRuns displays all check runs with status and timing
func (m Model) renderCheckRuns() string {
	var b strings.Builder

	b.WriteString(m.styles.Header.Render("Checks:\n"))

	// Calculate column widths once
	widths := m.calculateColumnWidths()

	// Render each check with aligned columns
	for _, check := range m.checkRuns {
		b.WriteString(m.renderCheckRun(check, widths))
	}

	return b.String()
}

// renderCheckRun displays a single check run with aligned columns
func (m Model) renderCheckRun(check ghclient.CheckRunInfo, widths ColumnWidths) string {
	status := check.Status
	conclusion := check.Conclusion

	// Format name as "Workflow / Job" or just "Job"
	name := check.Name
	if check.WorkflowName != "" {
		name = fmt.Sprintf("%s / %s", check.WorkflowName, check.Name)
	}

	// Truncate name if needed (with ellipsis)
	if len(name) > widths.NameWidth {
		name = name[:widths.NameWidth-1] + "…"
	}

	// Get column data (plain text)
	queueText := m.formatQueueLatency(check)
	durationText := m.formatDuration(check)

	// Determine icon and style
	var icon string
	var style = m.styles.Queued

	switch status {
	case "completed":
		switch conclusion {
		case "success":
			icon = "✓"
			style = m.styles.Success
		case "failure":
			icon = "✗"
			style = m.styles.Failure
		case "cancelled":
			icon = "⊗"
			style = m.styles.Queued
		case "skipped":
			icon = "⊘"
			style = m.styles.Queued
		case "timed_out":
			icon = "⏱"
			style = m.styles.Failure
		case "action_required":
			icon = "!"
			style = m.styles.Running
		default:
			icon = "?"
			style = m.styles.Queued
		}
	case "in_progress":
		icon = "⏳"
		style = m.styles.Running
	case "queued":
		icon = "⏸️"
		style = m.styles.Queued
	default:
		icon = "?"
		style = m.styles.Queued
	}

	// Build columns with explicit padding using strings.Repeat
	// This avoids fmt.Sprintf format specifier issues with ANSI codes

	// Right-align queue time
	queuePadding := widths.QueueWidth - len(queueText)
	if queuePadding < 0 {
		queuePadding = 0
	}
	queueCol := strings.Repeat(" ", queuePadding) + queueText

	// Left-align name (already correct length due to truncation logic above)
	namePadding := widths.NameWidth - len(name)
	if namePadding < 0 {
		namePadding = 0
	}
	nameCol := name + strings.Repeat(" ", namePadding)

	// Right-align duration
	durationPadding := widths.DurationWidth - len(durationText)
	if durationPadding < 0 {
		durationPadding = 0
	}
	durationCol := strings.Repeat(" ", durationPadding) + durationText

	// Apply styling to icon and duration
	styledIcon := style.Render(icon)
	styledDuration := style.Render(durationCol)

	// Assemble line: [2 spaces][queue][2 spaces][icon][2 spaces][name][2 spaces][duration][newline]
	return "  " + queueCol + "  " + styledIcon + "  " + nameCol + "  " + styledDuration + "\n"
}
