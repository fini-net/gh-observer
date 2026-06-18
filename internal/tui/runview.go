package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/google/go-github/v88/github"
	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/fini-net/gh-observer/internal/timing"
	"github.com/mattn/go-runewidth"
)

// View renders the current state for run-watching mode.
func (m RunModel) View() tea.View {
	if m.err != nil {
		return tea.NewView(m.styles.Error.Render(fmt.Sprintf("Error: %v\n", m.err)))
	}

	var b strings.Builder

	if m.runInfoLoaded {
		header := m.styles.Header.Render(fmt.Sprintf("%s/%s: %s", m.owner, m.repo, m.runInfo.DisplayTitle))
		utcTime := time.Now().UTC().Format("15:04:05 UTC")
		timeSinceUpdate := time.Since(m.lastUpdate)

		var updatedLine string
		if m.runInfo.HeadCommitTime != nil && !m.runInfo.HeadCommitTime.IsZero() {
			timeSincePush := time.Since(m.runInfo.HeadCommitTime.Time)
			updatedLine = fmt.Sprintf("Updated %s ago  •  Pushed %s ago",
				timing.FormatDuration(timeSinceUpdate),
				timing.FormatDuration(timeSincePush))
		} else if m.runInfo.CreatedAt != nil && !m.runInfo.CreatedAt.IsZero() {
			timeSinceCreate := time.Since(m.runInfo.CreatedAt.Time)
			updatedLine = fmt.Sprintf("Updated %s ago  •  Created %s ago",
				timing.FormatDuration(timeSinceUpdate),
				timing.FormatDuration(timeSinceCreate))
		} else {
			updatedLine = fmt.Sprintf("Updated %s ago", timing.FormatDuration(timeSinceUpdate))
		}

		if !m.noAvg {
			isFetching := m.avgFetchPending || len(m.pendingWorkflowFetch) > 0
			if isFetching {
				elapsed := time.Since(m.avgFetchStartTime)
				updatedLine += m.styles.Running.Render(fmt.Sprintf("  •  Fetching historical averages... (%s)", timing.FormatDuration(elapsed)))
			} else if m.avgFetchErr != nil {
				updatedLine += m.styles.Queued.Render("  •  Historical averages unavailable")
			} else if m.avgFetchLastDuration > 0 {
				wfCount := len(m.fetchedWorkflowIDs)
				updatedLine += m.styles.Info.Render(fmt.Sprintf("  •  Historical averages ready (%d workflows, %s)", wfCount, timing.FormatDuration(m.avgFetchLastDuration)))
			}
		}

		fmt.Fprintf(&b, "%s %s\n", header, utcTime)
		fmt.Fprintf(&b, "%s\n", updatedLine)
		b.WriteString("\n")
	}

	if len(m.jobs) == 0 {
		return tea.NewView(b.String() + m.renderRunStartupPhase())
	}

	widths := CalculateRunColumnWidths(m.jobs, m.jobAverages)

	headerName, headerDuration, headerAvg := FormatRunHeaderColumns(widths)
	b.WriteString(m.styles.Header.Render(fmt.Sprintf("  %s  %s  %s\n", headerName, headerDuration, headerAvg)))
	b.WriteString("\n")

	for _, job := range m.jobs {
		jobLine := m.renderRunJob(job, widths)
		b.WriteString(jobLine)
	}

	b.WriteString("\n")

	if m.rateLimitRemaining < minRateLimitForFetch {
		b.WriteString(m.styles.Failure.Render(fmt.Sprintf("  [Rate limit: %d remaining]", m.rateLimitRemaining)))
	} else if m.rateLimitRemaining < rateWarningThreshold {
		b.WriteString(m.styles.Running.Render(fmt.Sprintf("  [Rate limit: %d remaining]", m.rateLimitRemaining)))
	}

	b.WriteString("\n")

	if !m.quitting {
		b.WriteString("\nPress q to quit\n")
	}

	return tea.NewView(b.String())
}

// renderRunStartupPhase shows a message while loading run info.
func (m RunModel) renderRunStartupPhase() string {
	sinceStart := time.Since(m.startTime)

	var b strings.Builder

	if sinceStart < slowJobThreshold {
		fmt.Fprintf(&b, "%s ", m.spinner.View())
		b.WriteString(m.styles.Running.Render(fmt.Sprintf("Loading run info (%s elapsed)...\n", timing.FormatDuration(sinceStart))))
	} else {
		b.WriteString(m.styles.Queued.Render("No jobs found.\n"))
		b.WriteString("  This run may not have jobs configured, or they may not have started yet.\n")
	}

	return b.String()
}

// renderRunJob displays a single job with aligned columns.
func (m RunModel) renderRunJob(job ghclient.WorkflowJobInfo, widths RunColumnWidths) string {
	status := job.Status
	conclusion := job.Conclusion

	nameCol := BuildRunJobNameColumn(job, widths, m.enableLinks)

	durationText := FormatRunJobDuration(job)
	avgText := FormatRunJobAvg(job, m.jobAverages)

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
	case "queued", "waiting":
		style = m.styles.Queued
	}

	styledIcon := style.Render(icon)
	styledDuration := style.Render(durationText)
	styledAvg := style.Render(avgText)

	styledName := nameCol
	if conclusion == "failure" || conclusion == "timed_out" {
		styledName = style.Render(nameCol)
	}

	return styledIcon + " " + styledName + "  " + styledDuration + "  " + styledAvg + "\n"
}

// RunColumnWidths holds pre-calculated column widths for run mode rendering.
type RunColumnWidths struct {
	NameWidth     int
	DurationWidth int
	AvgWidth      int
}

// CalculateRunColumnWidths scans all jobs and determines max width for each column.
func CalculateRunColumnWidths(jobs []ghclient.WorkflowJobInfo, jobAverages map[string]time.Duration) RunColumnWidths {
	const (
		minNameWidth = 20
		maxNameWidth = 60
		minTimeWidth = 5
	)

	widths := RunColumnWidths{
		NameWidth:     minNameWidth,
		DurationWidth: minTimeWidth,
		AvgWidth:      minTimeWidth,
	}

	for _, job := range jobs {
		name := FormatRunJobName(job)
		nameLen := runewidth.StringWidth(name)
		if nameLen > widths.NameWidth && nameLen <= maxNameWidth {
			widths.NameWidth = nameLen
		} else if nameLen > maxNameWidth {
			widths.NameWidth = maxNameWidth
		}

		durationText := FormatRunJobDuration(job)
		if runewidth.StringWidth(durationText) > widths.DurationWidth {
			widths.DurationWidth = runewidth.StringWidth(durationText)
		}

		avgText := FormatRunJobAvg(job, jobAverages)
		if runewidth.StringWidth(avgText) > widths.AvgWidth {
			widths.AvgWidth = runewidth.StringWidth(avgText)
		}
	}

	return widths
}

// FormatRunHeaderColumns formats the column headers for run mode.
func FormatRunHeaderColumns(widths RunColumnWidths) (string, string, string) {
	namePad := max(widths.NameWidth-12, 0)
	headerName := "Workflow/Job" + strings.Repeat(" ", namePad)

	durationPad := max(widths.DurationWidth-7, 0)
	headerDuration := strings.Repeat(" ", durationPad) + "ThisRun"

	avgPad := max(widths.AvgWidth-7, 0)
	headerAvg := strings.Repeat(" ", avgPad) + "HistAvg"

	return headerName, headerDuration, headerAvg
}

// FormatRunJobName formats the job name as "Workflow / Job", or just "Job".
func FormatRunJobName(job ghclient.WorkflowJobInfo) string {
	if job.WorkflowName != "" {
		return fmt.Sprintf("%s / %s", job.WorkflowName, job.Name)
	}
	return job.Name
}

// FormatRunJobNameWithTruncate formats the job name and truncates if needed.
func FormatRunJobNameWithTruncate(job ghclient.WorkflowJobInfo, maxWidth int) string {
	name := FormatRunJobName(job)
	if runewidth.StringWidth(name) <= maxWidth {
		return name
	}

	prefix := ""
	if job.WorkflowName != "" {
		prefix = job.WorkflowName + " / "
	}

	if prefix != "" {
		prefixWidth := runewidth.StringWidth(prefix)
		if prefixWidth >= maxWidth {
			return runewidth.Truncate(name, maxWidth, "…")
		}
		remainingWidth := maxWidth - prefixWidth
		jobName := job.Name
		if runewidth.StringWidth(jobName) <= remainingWidth {
			return prefix + jobName
		}
		return prefix + runewidth.Truncate(jobName, remainingWidth, "…")
	}

	return runewidth.Truncate(name, maxWidth, "…")
}

// BuildRunJobNameColumn returns a left-aligned name column for a job.
func BuildRunJobNameColumn(job ghclient.WorkflowJobInfo, widths RunColumnWidths, enableLinks bool) string {
	name := FormatRunJobNameWithTruncate(job, widths.NameWidth)
	paddingLen := max(widths.NameWidth-runewidth.StringWidth(name), 0)
	padding := strings.Repeat(" ", paddingLen)
	if enableLinks && job.HTMLURL != "" {
		return FormatLink(job.HTMLURL, name) + padding
	}
	return name + padding
}

// FormatRunJobDuration returns the duration/runtime text for a job.
func FormatRunJobDuration(job ghclient.WorkflowJobInfo) string {
	switch job.Status {
	case "completed":
		duration := timing.RunJobDuration(timestampToTimePtr(job.StartedAt), timestampToTimePtr(job.CompletedAt))
		if duration > 0 {
			return timing.FormatDuration(duration)
		}
		return "-"
	case "in_progress":
		runtime := timing.RunJobRuntime(timestampToTimePtr(job.StartedAt))
		if runtime > 0 {
			return timing.FormatDuration(runtime)
		}
		return "-"
	default:
		return "-"
	}
}

// FormatRunJobAvg returns the historical average duration for a job, or "--" if unavailable.
func FormatRunJobAvg(job ghclient.WorkflowJobInfo, jobAverages map[string]time.Duration) string {
	if jobAverages == nil {
		return "--"
	}
	avg, ok := jobAverages[job.Name]
	if !ok {
		return "--"
	}
	return timing.FormatDuration(avg)
}

// SortRunJobs sorts jobs with the same criteria as SortCheckRuns.
func SortRunJobs(jobs []ghclient.WorkflowJobInfo) {
	sort.Slice(jobs, func(i, j int) bool {
		di := runSortKeyDuration(jobs[i])
		dj := runSortKeyDuration(jobs[j])
		if di != dj {
			return di < dj
		}
		si := statusPriority(jobs[i].Status)
		sj := statusPriority(jobs[j].Status)
		if si != sj {
			return si < sj
		}
		return FormatRunJobName(jobs[i]) < FormatRunJobName(jobs[j])
	})
}

// runSortKeyDuration returns a duration for sorting jobs.
func runSortKeyDuration(job ghclient.WorkflowJobInfo) time.Duration {
	switch job.Status {
	case "completed":
		d := timing.RunJobDuration(timestampToTimePtr(job.StartedAt), timestampToTimePtr(job.CompletedAt))
		if d > 0 {
			return d
		}
		return 0
	case "in_progress":
		d := timing.RunJobRuntime(timestampToTimePtr(job.StartedAt))
		if d > 0 {
			return d
		}
		return 0
	default:
		return time.Duration(1 << 62)
	}
}

// timestampToTimePtr converts a *github.Timestamp to *time.Time.
func timestampToTimePtr(ts *github.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}
	t := ts.Time
	return &t
}