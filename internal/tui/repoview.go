package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/fini-net/gh-observer/internal/timing"
)

// View renders the current state for repo-watch mode.
func (m RepoWatchModel) View() tea.View {
	if m.err != nil {
		return tea.NewView(m.styles.Error.Render(fmt.Sprintf("Error: %v\n", m.err)))
	}

	var b strings.Builder

	header := m.styles.Header.Render(fmt.Sprintf("%s/%s — workflow runs", m.owner, m.repo))
	utcTime := time.Now().UTC().Format("15:04:05 UTC")
	fmt.Fprintf(&b, "%s %s\n", header, utcTime)

	timeSinceUpdate := time.Since(m.lastUpdate)
	fmt.Fprintf(&b, "Updated %s ago\n", timing.FormatDuration(timeSinceUpdate))
	b.WriteString("\n")

	if len(m.runs) == 0 {
		fmt.Fprintf(&b, "%s No workflow runs found\n", m.spinner.View())
		b.WriteString("  Waiting for runs to appear...\n")
	} else {
		fmt.Fprintf(&b, "  %-40s  %-10s  %-10s  %s\n", "Workflow", "Status", "Duration", "Branch")
		b.WriteString("\n")

		for _, run := range m.runs {
			icon := runIcon(run.Status, run.Conclusion)
			name := run.DisplayTitle
			if len(name) > 40 {
				name = name[:37] + "..."
			}

			status := run.Status
			if status == "completed" {
				status = run.Conclusion
				if status == "" {
					status = "completed"
				}
			}

			var duration string
			if run.RunStartedAt != nil && !run.RunStartedAt.IsZero() {
				if run.Status == "completed" && run.UpdatedAt != nil && !run.UpdatedAt.IsZero() {
					duration = timing.FormatDuration(run.UpdatedAt.Time.Sub(run.RunStartedAt.Time))
				} else {
					duration = timing.FormatDuration(time.Since(run.RunStartedAt.Time))
				}
			} else if run.CreatedAt != nil && !run.CreatedAt.IsZero() {
				duration = timing.FormatDuration(time.Since(run.CreatedAt.Time))
			}

			branch := run.HeadBranch
			if branch == "" {
				branch = "-"
			}

			var style = m.styles.Queued
			switch run.Status {
			case "completed":
				switch run.Conclusion {
				case "success":
					style = m.styles.Success
				case "failure", "timed_out":
					style = m.styles.Failure
				case "cancelled", "skipped":
					style = m.styles.Queued
				default:
					style = m.styles.Queued
				}
			case "in_progress", "waiting":
				style = m.styles.Running
			case "queued":
				style = m.styles.Queued
			}

			styledIcon := style.Render(icon)
			styledStatus := style.Render(status)
			styledDuration := style.Render(duration)

			fmt.Fprintf(&b, "%s %-40s  %-10s  %-10s  %s\n", styledIcon, name, styledStatus, styledDuration, branch)
		}
	}

	b.WriteString("\n")

	allComplete := ghclient.AllRunsComplete(m.runs)
	if allComplete && len(m.runs) > 0 && m.persist {
		exitLabel := "all runs passed"
		if m.exitCode != 0 {
			exitLabel = "runs failed"
		}
		b.WriteString(m.styles.Info.Render(fmt.Sprintf("  Watching (%s) — persist mode\n", exitLabel)))
		b.WriteString("\n")
	}

	if m.rateLimitRemaining < minRateLimitForFetch {
		b.WriteString(m.styles.Running.Render(fmt.Sprintf("  [Rate limit: %d remaining]", m.rateLimitRemaining)))
		b.WriteString("\n")
	}

	if !m.quitting {
		if m.persist {
			b.WriteString("\nPress q to quit (persist mode)\n")
		} else {
			b.WriteString("\nPress q to quit\n")
		}
	}

	return tea.NewView(b.String())
}

func runIcon(status, conclusion string) string {
	switch status {
	case "completed":
		switch conclusion {
		case "success":
			return "✓"
		case "failure", "timed_out":
			return "✗"
		case "cancelled":
			return "⊘"
		case "skipped":
			return "⏭"
		default:
			return "·"
		}
	case "in_progress":
		return "●"
	case "queued", "waiting":
		return "○"
	default:
		return "?"
	}
}