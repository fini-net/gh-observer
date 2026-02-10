package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/fini-net/gh-observer/internal/timing"
	"github.com/google/go-github/v58/github"
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

// renderCheckRuns displays all check runs with status and timing
func (m Model) renderCheckRuns() string {
	var b strings.Builder

	b.WriteString(m.styles.Header.Render("Checks:\n"))

	for _, check := range m.checkRuns {
		b.WriteString(m.renderCheckRun(check))
	}

	return b.String()
}

// renderCheckRun displays a single check run
func (m Model) renderCheckRun(check *github.CheckRun) string {
	status := check.GetStatus()
	conclusion := check.GetConclusion()
	name := check.GetName()

	var icon, timeText string
	var style = m.styles.Queued

	switch status {
	case "completed":
		duration := timing.FinalDuration(check)
		timeText = fmt.Sprintf("[completed in %s]", timing.FormatDuration(duration))

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
		runtime := timing.Runtime(check)
		timeText = fmt.Sprintf("[running: %s]", timing.FormatDuration(runtime))

	case "queued":
		icon = "⏸️"
		style = m.styles.Queued
		if check.CheckSuite != nil && check.CheckSuite.CreatedAt != nil {
			timeText = fmt.Sprintf("[queued: %s]", timing.FormatDuration(time.Since(check.CheckSuite.CreatedAt.Time)))
		} else {
			timeText = "[queued]"
		}

	default:
		icon = "?"
		style = m.styles.Queued
		timeText = fmt.Sprintf("[%s]", status)
	}

	// Queue latency (only show for completed/running)
	var queueInfo string
	if status != "queued" {
		queueLatency := timing.QueueLatency(check)
		if queueLatency > 0 {
			queueInfo = fmt.Sprintf("  (queued: %s)", timing.FormatDuration(queueLatency))
		}
	}

	return fmt.Sprintf("  %s %-30s %s%s\n",
		style.Render(icon),
		name,
		style.Render(timeText),
		queueInfo,
	)
}
