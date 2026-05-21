package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/fini-net/gh-observer/internal/timing"
)

// View renders the current state for repo-watching mode.
func (m RepoModel) View() tea.View {
	if m.err != nil {
		return tea.NewView(m.styles.Error.Render(fmt.Sprintf("Error: %v\n", m.err)))
	}

	var b strings.Builder

	utcTime := time.Now().UTC().Format("15:04:05 UTC")
	timeSinceUpdate := time.Since(m.lastUpdate)

	repoHeader := m.styles.Header.Render(fmt.Sprintf("%s/%s", m.owner, m.repo))
	prCount := len(m.prs)
	summaryLine := fmt.Sprintf("%d active PR%s  •  Updated %s ago",
		prCount, pluralS(prCount), timing.FormatDuration(timeSinceUpdate))

	fmt.Fprintf(&b, "%s %s\n", repoHeader, utcTime)
	fmt.Fprintf(&b, "%s\n", summaryLine)
	b.WriteString("\n")

	if prCount == 0 {
		fmt.Fprintf(&b, "%s ", m.spinner.View())
		b.WriteString(m.styles.Running.Render("No active PRs with checks running...\n"))
		b.WriteString("\n")
	} else {
		prNums := m.sortedPRNumbers()
		for _, prNum := range prNums {
			prData := m.prs[prNum]
			m.renderPRGroup(&b, prNum, prData)
		}
	}

	if m.rateLimitRemaining < minRateLimitForFetch {
		b.WriteString(m.styles.Running.Render(fmt.Sprintf("  [Rate limit: %d remaining]", m.rateLimitRemaining)))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	if !m.quitting {
		b.WriteString("Press q to quit\n")
	}

	return tea.NewView(b.String())
}

// renderPRGroup renders a single PR group with header and checks.
func (m RepoModel) renderPRGroup(b *strings.Builder, prNum int, prData PRViewData) {
	prHeader := m.styles.Header.Render(fmt.Sprintf("PR #%d: %s", prNum, prData.Title))
	b.WriteString(prHeader)
	b.WriteString("\n")

	if len(prData.CheckRuns) == 0 {
		b.WriteString("  No checks\n")
		b.WriteString("\n")
		return
	}

	widths := CalculateColumnWidths(prData.CheckRuns, prData.HeadCommitTime, nil)

	for _, check := range prData.CheckRuns {
		line := m.renderRepoCheckRun(check, prData.HeadCommitTime, widths)
		b.WriteString(line)
	}

	b.WriteString("\n")
}

// renderRepoCheckRun renders a single check run in repo mode.
func (m RepoModel) renderRepoCheckRun(check ghclient.CheckRunInfo, headCommitTime time.Time, widths ColumnWidths) string {
	status := check.Status
	conclusion := check.Conclusion

	nameCol := BuildNameColumn(check, widths, m.enableLinks)
	queueText := FormatQueueLatency(check, headCommitTime)
	durationText := FormatDuration(check)
	avgText := FormatAvg(check, nil)

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

	queueCol, _, durationCol, avgCol := FormatAlignedColumns(queueText, FormatCheckNameWithTruncate(check, widths.NameWidth), durationText, avgText, widths)

	styledIcon := style.Render(icon)
	styledDuration := style.Render(durationCol)
	styledAvg := style.Render(avgCol)

	styledName := nameCol
	if conclusion == "failure" || conclusion == "timed_out" {
		styledName = style.Render(nameCol)
	}

	return "  " + queueCol + " " + styledIcon + " " + styledName + "  " + styledDuration + "  " + styledAvg + "\n"
}

// pluralS returns "s" if n != 1, otherwise "".
func pluralS(n int) string {
	if n != 1 {
		return "s"
	}
	return ""
}