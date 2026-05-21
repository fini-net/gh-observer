package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/fini-net/gh-observer/internal/timing"
	"github.com/mattn/go-runewidth"
)

func (m RepoModel) View() tea.View {
	if m.err != nil {
		return tea.NewView(m.styles.Error.Render(fmt.Sprintf("Error: %v\n", m.err)))
	}

	var b strings.Builder

	utcTime := time.Now().UTC().Format("15:04:05 UTC")
	timeSinceUpdate := time.Since(m.lastUpdate)

	repoHeader := m.styles.Header.Render(fmt.Sprintf("%s/%s", m.owner, m.repo))
	prCount := len(m.prs)
	summaryParts := []string{}
	summaryParts = append(summaryParts, fmt.Sprintf("%d active PR%s", prCount, pluralS(prCount)))
	if len(m.standaloneRuns) > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d branch run%s", len(m.standaloneRuns), pluralS(len(m.standaloneRuns))))
	}
	summaryLine := fmt.Sprintf("%s  •  Updated %s ago",
		strings.Join(summaryParts, "  •  "), timing.FormatDuration(timeSinceUpdate))

	fmt.Fprintf(&b, "%s %s\n", repoHeader, utcTime)
	fmt.Fprintf(&b, "%s\n", summaryLine)
	b.WriteString("\n")

	if prCount == 0 && len(m.standaloneRuns) == 0 {
		fmt.Fprintf(&b, "%s ", m.spinner.View())
		b.WriteString(m.styles.Running.Render("No active PRs or branch runs...\n"))
		b.WriteString("\n")
	} else {
		prNums := m.sortedPRNumbers()
		for _, prNum := range prNums {
			prData := m.prs[prNum]
			m.renderPRGroup(&b, prNum, prData)
		}

		if len(m.standaloneRuns) > 0 {
			m.renderBranchRunsSection(&b)
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

func (m RepoModel) renderBranchRunsSection(b *strings.Builder) {
	branchGroups := m.groupBranchRunsByBranch()

	for _, branch := range sortedBranchNames(branchGroups) {
		runs := branchGroups[branch]
		m.renderBranchGroup(b, branch, runs)
	}
}

func (m RepoModel) renderBranchGroup(b *strings.Builder, branch string, runs []ghclient.BranchRunData) {
	header := branch
	b.WriteString(m.styles.Header.Render(fmt.Sprintf("Branch: %s", header)))
	b.WriteString("\n")

	for _, run := range runs {
		m.renderBranchRunHeader(b, run)

		if len(run.Jobs) > 0 {
			widths := calculateBranchRunColumnWidths(run.Jobs)
			for _, job := range run.Jobs {
				line := m.renderBranchRunJob(job, widths)
				b.WriteString(line)
			}
		} else if run.Status != "completed" {
			b.WriteString("    ")
			b.WriteString(m.styles.Queued.Render("Waiting for jobs...\n"))
		}
	}

	b.WriteString("\n")
}

func (m RepoModel) renderBranchRunHeader(b *strings.Builder, run ghclient.BranchRunData) {
	icon := GetCheckIcon(run.Status, run.Conclusion)

	var style = m.styles.Queued
	switch run.Status {
	case "completed":
		switch run.Conclusion {
		case "success":
			style = m.styles.Success
		case "failure", "timed_out":
			style = m.styles.Failure
		default:
			style = m.styles.Queued
		}
	case "in_progress":
		style = m.styles.Running
	case "queued", "waiting":
		style = m.styles.Queued
	}

	eventAnnotation := ""
	if run.Event != "" && run.Event != "push" {
		eventAnnotation = fmt.Sprintf(" (%s)", run.Event)
	}

	durationText := formatBranchRunDuration(run)
	title := run.DisplayTitle
	if run.WorkflowName != "" && title == "" {
		title = run.WorkflowName
	}

	styledIcon := style.Render(icon)
	styledTitle := style.Render(fmt.Sprintf("%s%s", title, eventAnnotation))
	styledDuration := style.Render(durationText)

	fmt.Fprintf(b, "  %s %s  %s\n", styledIcon, styledTitle, styledDuration)
}

func (m RepoModel) renderBranchRunJob(job ghclient.CheckRunInfo, widths branchRunColumnWidths) string {
	status := job.Status
	conclusion := job.Conclusion

	icon := GetCheckIcon(status, conclusion)

	var style = m.styles.Queued
	switch status {
	case "completed":
		switch conclusion {
		case "success":
			style = m.styles.Success
		case "failure", "timed_out":
			style = m.styles.Failure
		default:
			style = m.styles.Queued
		}
	case "in_progress":
		style = m.styles.Running
	case "queued", "waiting":
		style = m.styles.Queued
	}

	nameText := formatBranchJobNameTruncate(job, widths.nameWidth)
	namePad := max(widths.nameWidth-runewidth.StringWidth(nameText), 0)
	nameCol := nameText + strings.Repeat(" ", namePad)

	durationText := formatBranchJobDuration(job)
	durationPad := max(widths.durationWidth-runewidth.StringWidth(durationText), 0)
	durationCol := strings.Repeat(" ", durationPad) + durationText

	styledIcon := style.Render(icon)
	styledDuration := style.Render(durationCol)

	styledName := nameCol
	if conclusion == "failure" || conclusion == "timed_out" {
		styledName = style.Render(nameCol)
	}

	return "    " + styledIcon + " " + styledName + "  " + styledDuration + "\n"
}

func formatBranchRunDuration(run ghclient.BranchRunData) string {
	if !run.RunStartedAt.IsZero() {
		switch run.Status {
		case "in_progress", "queued", "waiting":
			runtime := time.Since(run.RunStartedAt)
			if runtime > 0 {
				return timing.FormatDuration(runtime)
			}
		}
	}
	return "-"
}

func formatBranchJobName(job ghclient.CheckRunInfo) string {
	if job.WorkflowName != "" {
		return fmt.Sprintf("%s / %s", job.WorkflowName, job.Name)
	}
	return job.Name
}

func formatBranchJobNameTruncate(job ghclient.CheckRunInfo, maxWidth int) string {
	name := formatBranchJobName(job)
	if runewidth.StringWidth(name) <= maxWidth {
		return name
	}
	return runewidth.Truncate(name, maxWidth, "…")
}

func formatBranchJobDuration(job ghclient.CheckRunInfo) string {
	switch job.Status {
	case "completed":
		if job.StartedAt != nil && job.CompletedAt != nil {
			d := job.CompletedAt.Sub(*job.StartedAt)
			if d > 0 {
				return timing.FormatDuration(d)
			}
		}
		return "-"
	case "in_progress":
		if job.StartedAt != nil {
			runtime := time.Since(*job.StartedAt)
			if runtime > 0 {
				return timing.FormatDuration(runtime)
			}
		}
		return "-"
	default:
		return "-"
	}
}

type branchRunColumnWidths struct {
	nameWidth     int
	durationWidth int
}

func calculateBranchRunColumnWidths(jobs []ghclient.CheckRunInfo) branchRunColumnWidths {
	const (
		minNameWidth     = 20
		maxNameWidth     = 60
		minDurationWidth = 5
	)

	widths := branchRunColumnWidths{
		nameWidth:     minNameWidth,
		durationWidth: minDurationWidth,
	}

	for _, job := range jobs {
		name := formatBranchJobName(job)
		nameLen := runewidth.StringWidth(name)
		if nameLen > widths.nameWidth && nameLen <= maxNameWidth {
			widths.nameWidth = nameLen
		} else if nameLen > maxNameWidth {
			widths.nameWidth = maxNameWidth
		}

		durationText := formatBranchJobDuration(job)
		if runewidth.StringWidth(durationText) > widths.durationWidth {
			widths.durationWidth = runewidth.StringWidth(durationText)
		}
	}

	return widths
}

func (m RepoModel) groupBranchRunsByBranch() map[string][]ghclient.BranchRunData {
	groups := make(map[string][]ghclient.BranchRunData)
	for _, run := range m.standaloneRuns {
		branch := run.HeadBranch
		if branch == "" {
			branch = m.defaultBranch
		}
		groups[branch] = append(groups[branch], run)
	}
	return groups
}

func sortedBranchNames(groups map[string][]ghclient.BranchRunData) []string {
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func pluralS(n int) string {
	if n != 1 {
		return "s"
	}
	return ""
}