package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	tea "charm.land/bubbletea/v2"
	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/fini-net/gh-observer/internal/timing"
	"github.com/mattn/go-runewidth"
)

// View renders the repo-watch state: a repo header, a summary line, PR groups
// (each with its checks), standalone branch-run groups, the rate-limit
// indicator, an optional non-fatal fetch-error status line, and the quit hint.
//
// Repo mode is persistent: transient fetch errors (e.g. 504 Gateway Timeout)
// do NOT replace the screen. The last good state stays on screen and the
// error is surfaced as a red status line so the user knows something is wrong
// while polling continues and self-heals when the API returns.
func (m RepoModel) View() tea.View {
	var b strings.Builder

	utcTime := time.Now().UTC().Format("15:04:05 UTC")
	timeSinceUpdate := time.Since(m.lastUpdate)

	repoHeader := m.styles.Header.Render(fmt.Sprintf("%s/%s", m.owner, m.repo))
	prCount := len(m.prs)
	branchRunCount := len(m.standaloneRuns)

	summaryParts := []string{
		fmt.Sprintf("%d active PR%s", prCount, pluralS(prCount)),
	}
	if branchRunCount > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d branch run%s", branchRunCount, pluralS(branchRunCount)))
	}
	summaryParts = append(summaryParts, fmt.Sprintf("Updated %s ago", timing.FormatDuration(timeSinceUpdate)))
	summaryLine := strings.Join(summaryParts, "  •  ")

	fmt.Fprintf(&b, "%s %s\n", repoHeader, utcTime)
	fmt.Fprintf(&b, "%s\n", summaryLine)
	b.WriteString("\n")

	if prCount == 0 && branchRunCount == 0 {
		fmt.Fprintf(&b, "%s ", m.spinner.View())
		b.WriteString(m.styles.Running.Render("No active PRs or branch runs..."))
		b.WriteString("\n\n")
	} else {
		for _, prNum := range m.sortedPRNumbers() {
			m.renderPRGroup(&b, prNum, m.prs[prNum])
		}
		if branchRunCount > 0 {
			m.renderStandaloneRunsSection(&b)
		}
	}

	// Two-tier rate-limit indicator: red under minRateLimitForFetch, yellow
	// under rateWarningThreshold. Only render once we've actually received a
	// response — before that, rateLimitRemaining is the Go zero value (0) and
	// showing "[Rate limit: 0 remaining]" in red would be misleading.
	if m.fetchReceived {
		if m.rateLimitRemaining < minRateLimitForFetch {
			b.WriteString(m.styles.Failure.Render(fmt.Sprintf("  [Rate limit: %d remaining]", m.rateLimitRemaining)))
			b.WriteString("\n")
		} else if m.rateLimitRemaining < rateWarningThreshold {
			b.WriteString(m.styles.Running.Render(fmt.Sprintf("  [Rate limit: %d remaining]", m.rateLimitRemaining)))
			b.WriteString("\n")
		}
	}

	// Non-fatal fetch error status line. The last good state remains on screen
	// above this line; polling continues and the line clears on next success.
	if m.fetchErr != nil {
		errText := truncateFetchError(m.fetchErr.Error(), 80)
		age := timing.FormatDuration(time.Since(m.fetchErrAt))
		b.WriteString(m.styles.Failure.Render(fmt.Sprintf("  [Fetch error: %s — %s ago]", errText, age)))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	if !m.quitting {
		b.WriteString("Press q to quit\n")
	}

	return tea.NewView(b.String())
}

// truncateFetchError shortens an error message to fit within maxWidth terminal
// display cells, appending an ellipsis if truncation occurs. GitHub 504 errors
// embed a large HTML body that would otherwise span many terminal lines.
func truncateFetchError(s string, maxWidth int) string {
	if runewidth.StringWidth(s) <= maxWidth {
		return s
	}
	return runewidth.Truncate(s, maxWidth, "…")
}

// renderPRGroup renders a single PR's header and its (already fade-filtered) checks.
func (m RepoModel) renderPRGroup(b *strings.Builder, prNum int, prData PRViewData) {
	prHeader := m.styles.Header.Render(fmt.Sprintf("PR #%d: %s", prNum, prData.Title))
	b.WriteString(prHeader)
	b.WriteString("\n")

	if len(prData.CheckRuns) == 0 {
		b.WriteString("  No checks\n\n")
		return
	}

	widths := CalculateColumnWidths(prData.CheckRuns, prData.HeadCommitTime, nil)

	for _, check := range prData.CheckRuns {
		line := m.renderRepoCheckRun(check, prData.HeadCommitTime, widths)
		b.WriteString(line)
	}

	b.WriteString("\n")
}

// renderRepoCheckRun renders one PR check row, indented two spaces under the PR header.
// Reuses display.go helpers (no HistAvg column — jobAverages is nil in repo mode).
func (m RepoModel) renderRepoCheckRun(check ghclient.CheckRunInfo, headCommitTime time.Time, widths ColumnWidths) string {
	nameCol := BuildNameColumn(check, widths, m.enableLinks)
	queueText := FormatQueueLatency(check, headCommitTime)
	durationText := FormatDuration(check)
	avgText := FormatAvg(check, nil)

	icon := GetCheckIcon(check.Status, check.Conclusion)
	style := styleForCheck(check.Status, check.Conclusion, m.styles)

	queueCol, _, durationCol, avgCol := FormatAlignedColumns(queueText, FormatCheckNameWithTruncate(check, widths.NameWidth), durationText, avgText, widths)

	styledIcon := style.Render(icon)
	styledDuration := style.Render(durationCol)
	styledAvg := style.Render(avgCol)

	styledName := nameCol
	if check.Conclusion == "failure" || check.Conclusion == "timed_out" {
		styledName = style.Render(nameCol)
	}

	return "  " + queueCol + " " + styledIcon + " " + styledName + "  " + styledDuration + "  " + styledAvg + "\n"
}

// renderStandaloneRunsSection renders standalone (non-PR) workflow runs grouped by branch.
func (m RepoModel) renderStandaloneRunsSection(b *strings.Builder) {
	branchGroups := m.groupBranchRunsByBranch()
	for _, branch := range sortedBranchNames(branchGroups) {
		m.renderBranchGroup(b, branch, branchGroups[branch])
	}
}

// renderBranchGroup renders a "Branch: name" header followed by its runs and their jobs.
func (m RepoModel) renderBranchGroup(b *strings.Builder, branch string, runs []ghclient.BranchRunData) {
	b.WriteString(m.styles.Header.Render(fmt.Sprintf("Branch: %s", branch)))
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

// renderBranchRunHeader renders the run-level row (icon + title + event + duration).
func (m RepoModel) renderBranchRunHeader(b *strings.Builder, run ghclient.BranchRunData) {
	icon := GetCheckIcon(run.Status, run.Conclusion)
	style := styleForCheck(run.Status, run.Conclusion, m.styles)

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

// renderBranchRunJob renders a single job row under a branch run header.
func (m RepoModel) renderBranchRunJob(job ghclient.CheckRunInfo, widths branchRunColumnWidths) string {
	icon := GetCheckIcon(job.Status, job.Conclusion)
	style := styleForCheck(job.Status, job.Conclusion, m.styles)

	nameText := formatBranchJobNameTruncate(job, widths.nameWidth)
	namePad := max(widths.nameWidth-runewidth.StringWidth(nameText), 0)
	nameCol := nameText + strings.Repeat(" ", namePad)

	durationText := formatBranchJobDuration(job)
	durationPad := max(widths.durationWidth-runewidth.StringWidth(durationText), 0)
	durationCol := strings.Repeat(" ", durationPad) + durationText

	styledIcon := style.Render(icon)
	styledDuration := style.Render(durationCol)

	styledName := nameCol
	if job.Conclusion == "failure" || job.Conclusion == "timed_out" {
		styledName = style.Render(nameCol)
	}

	return "    " + styledIcon + " " + styledName + "  " + styledDuration + "\n"
}

// styleForCheck returns the lipgloss style for a status/conclusion pair.
func styleForCheck(status, conclusion string, styles Styles) lipgloss.Style {
	switch status {
	case "completed":
		switch conclusion {
		case "success":
			return styles.Success
		case "failure", "timed_out":
			return styles.Failure
		case "action_required":
			return styles.Running
		default:
			return styles.Queued
		}
	case "in_progress":
		return styles.Running
	case "queued", "waiting":
		return styles.Queued
	default:
		return styles.Queued
	}
}

// formatBranchRunDuration returns a display duration for a standalone run header.
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

// formatBranchJobName formats a job name as "Workflow / Job" or "Job".
func formatBranchJobName(job ghclient.CheckRunInfo) string {
	if job.WorkflowName != "" {
		return fmt.Sprintf("%s / %s", job.WorkflowName, job.Name)
	}
	return job.Name
}

// formatBranchJobNameTruncate truncates a job name to maxWidth with an ellipsis.
func formatBranchJobNameTruncate(job ghclient.CheckRunInfo, maxWidth int) string {
	name := formatBranchJobName(job)
	if runewidth.StringWidth(name) <= maxWidth {
		return name
	}
	return runewidth.Truncate(name, maxWidth, "…")
}

// formatBranchJobDuration returns a display duration for a single job.
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

// branchRunColumnWidths holds widths for the two-column branch-run job layout.
type branchRunColumnWidths struct {
	nameWidth     int
	durationWidth int
}

// calculateBranchRunColumnWidths scans jobs to size the name and duration columns.
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

// groupBranchRunsByBranch groups standalone runs by their HeadBranch.
func (m RepoModel) groupBranchRunsByBranch() map[string][]ghclient.BranchRunData {
	groups := make(map[string][]ghclient.BranchRunData)
	for _, run := range m.standaloneRuns {
		branch := run.HeadBranch
		if branch == "" {
			branch = "(unknown)"
		}
		groups[branch] = append(groups[branch], run)
	}
	return groups
}

// sortedBranchNames returns branch names sorted alphabetically for stable rendering.
func sortedBranchNames(groups map[string][]ghclient.BranchRunData) []string {
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// pluralS returns "s" when n != 1, else "".
func pluralS(n int) string {
	if n != 1 {
		return "s"
	}
	return ""
}