package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/fini-net/gh-observer/internal/timing"
	"github.com/muesli/termenv"
)

// FormatQueueLatency returns the queue time text or placeholder
func FormatQueueLatency(check ghclient.CheckRunInfo, headCommitTime time.Time) string {
	if check.Status == "queued" {
		if !headCommitTime.IsZero() {
			return timing.FormatDuration(time.Since(headCommitTime))
		}
		return "-"
	}

	queueLatency := timing.QueueLatency(headCommitTime, check)
	if queueLatency > 0 {
		return timing.FormatDuration(queueLatency)
	}
	return "-"
}

// FormatDuration returns the duration/runtime text or placeholder
func FormatDuration(check ghclient.CheckRunInfo) string {
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
		return "-"
	}
}

// GetCheckIcon returns the appropriate icon for a check run based on status and conclusion
func GetCheckIcon(status, conclusion string) string {
	switch status {
	case "completed":
		switch conclusion {
		case "success":
			return "✓"
		case "failure":
			return "✗"
		case "cancelled":
			return "⊗"
		case "skipped":
			return "⊘"
		case "timed_out":
			return "⏱"
		case "action_required":
			return "!"
		default:
			return "?"
		}
	case "in_progress":
		return "◐"
	case "queued":
		return "⏸"
	default:
		return "?"
	}
}

// FormatCheckName formats the check name as "Workflow / Job" or just "Job"
func FormatCheckName(check ghclient.CheckRunInfo) string {
	if check.WorkflowName != "" {
		return fmt.Sprintf("%s / %s", check.WorkflowName, check.Name)
	}
	return check.Name
}

// FormatCheckNameWithTruncate formats the check name and truncates if needed
func FormatCheckNameWithTruncate(check ghclient.CheckRunInfo, maxWidth int) string {
	name := FormatCheckName(check)
	if len(name) <= maxWidth {
		return name
	}

	ellipsis := "…"

	// If there's a workflow name, try to preserve "Workflow / " structure
	if check.WorkflowName != "" {
		prefix := check.WorkflowName + " / "
		prefixLen := len(prefix)

		// If even the prefix alone exceeds maxWidth, truncate the whole string
		if prefixLen >= maxWidth {
			if maxWidth <= 1 {
				return ellipsis[:maxWidth]
			}
			return name[:maxWidth-1] + ellipsis
		}

		// Truncate just the job name part, leaving room for ellipsis
		availableWidth := maxWidth - prefixLen - 1 // -1 for ellipsis display cell
		if availableWidth <= 0 {
			return prefix[:maxWidth-1] + ellipsis
		}
		return prefix + check.Name[:availableWidth] + ellipsis
	}

	// No workflow name - simple truncation
	if maxWidth <= 1 {
		return ellipsis[:maxWidth]
	}
	return name[:maxWidth-1] + ellipsis
}

// FormatLink wraps text in an OSC 8 terminal hyperlink
func FormatLink(url, text string) string {
	if url == "" {
		return text
	}
	return termenv.Hyperlink(url, text)
}

// BuildNameColumn returns a left-aligned name column of exactly widths.NameWidth visible
// characters. If enableLinks is true and the check has a DetailsURL, the visible text is
// wrapped in an OSC 8 hyperlink; padding spaces are appended outside the link so that
// len()-based width measurement stays accurate for the rest of the line.
func BuildNameColumn(check ghclient.CheckRunInfo, widths ColumnWidths, enableLinks bool) string {
	name := FormatCheckNameWithTruncate(check, widths.NameWidth)
	paddingLen := widths.NameWidth - len(name)
	if paddingLen < 0 {
		paddingLen = 0
	}
	padding := strings.Repeat(" ", paddingLen)
	if enableLinks && check.DetailsURL != "" {
		return FormatLink(check.DetailsURL, name) + padding
	}
	return name + padding
}

// FormatAvg returns the historical average duration for a job, or "--" if unavailable.
func FormatAvg(check ghclient.CheckRunInfo, jobAverages map[string]time.Duration) string {
	if jobAverages == nil {
		return "--"
	}
	avg, ok := jobAverages[check.Name]
	if !ok {
		return "--"
	}
	return timing.FormatDuration(avg)
}

// CalculateColumnWidths scans all check runs and determines max width for each column
func CalculateColumnWidths(checkRuns []ghclient.CheckRunInfo, headCommitTime time.Time, jobAverages map[string]time.Duration) ColumnWidths {
	const (
		minNameWidth = 20
		maxNameWidth = 60
		minTimeWidth = 5
	)

	widths := ColumnWidths{
		QueueWidth:    minTimeWidth,
		NameWidth:     minNameWidth,
		DurationWidth: minTimeWidth,
		AvgWidth:      minTimeWidth,
	}

	for _, check := range checkRuns {
		queueText := FormatQueueLatency(check, headCommitTime)
		if len(queueText) > widths.QueueWidth {
			widths.QueueWidth = len(queueText)
		}

		name := FormatCheckName(check)
		nameLen := len(name)
		if nameLen > widths.NameWidth && nameLen <= maxNameWidth {
			widths.NameWidth = nameLen
		} else if nameLen > maxNameWidth {
			widths.NameWidth = maxNameWidth
		}

		durationText := FormatDuration(check)
		if len(durationText) > widths.DurationWidth {
			widths.DurationWidth = len(durationText)
		}

		avgText := FormatAvg(check, jobAverages)
		if len(avgText) > widths.AvgWidth {
			widths.AvgWidth = len(avgText)
		}
	}

	return widths
}

// FormatAlignedColumns formats the four columns with proper padding
func FormatAlignedColumns(queueText, nameText, durationText, avgText string, widths ColumnWidths) (string, string, string, string) {
	queuePadding := widths.QueueWidth - len(queueText)
	if queuePadding < 0 {
		queuePadding = 0
	}
	queueCol := strings.Repeat(" ", queuePadding) + queueText

	namePadding := widths.NameWidth - len(nameText)
	if namePadding < 0 {
		namePadding = 0
	}
	nameCol := nameText + strings.Repeat(" ", namePadding)

	durationPadding := widths.DurationWidth - len(durationText)
	if durationPadding < 0 {
		durationPadding = 0
	}
	durationCol := strings.Repeat(" ", durationPadding) + durationText

	avgPadding := widths.AvgWidth - len(avgText)
	if avgPadding < 0 {
		avgPadding = 0
	}
	avgCol := strings.Repeat(" ", avgPadding) + avgText

	return queueCol, nameCol, durationCol, avgCol
}

// FormatHeaderColumns formats the column headers with proper padding
func FormatHeaderColumns(widths ColumnWidths) (string, string, string, string) {
	queuePad := widths.QueueWidth - 7
	if queuePad < 0 {
		queuePad = 0
	}
	headerQueue := strings.Repeat(" ", queuePad) + "Start"

	namePad := widths.NameWidth - 12
	if namePad < 0 {
		namePad = 0
	}
	headerName := "Workflow/Job" + strings.Repeat(" ", namePad)

	durationPad := widths.DurationWidth - 7
	if durationPad < 0 {
		durationPad = 0
	}
	headerDuration := strings.Repeat(" ", durationPad) + "ThisRun"

	avgPad := widths.AvgWidth - 7 // "HistAvg" is 7 chars
	if avgPad < 0 {
		avgPad = 0
	}
	headerAvg := strings.Repeat(" ", avgPad) + "HistAvg"

	return headerQueue, headerName, headerDuration, headerAvg
}

// FormatDescription truncates description to fit within the total visual width
func FormatDescription(description string, widths ColumnWidths) string {
	if description == "" {
		return ""
	}
	totalWidth := widths.QueueWidth + 1 + 1 + widths.NameWidth + 2 + widths.DurationWidth + 2 + widths.AvgWidth
	maxLen := totalWidth - 4
	if maxLen < 20 {
		maxLen = 20
	}
	if len(description) > maxLen {
		return description[:maxLen-1] + "…"
	}
	return description
}

// SortCheckRuns sorts check runs with three criteria:
// Primary: ThisRun duration ascending (shortest first)
// Secondary: Status priority (in_progress > completed > queued > other)
// Tertiary: Job name alphabetically
func SortCheckRuns(checks []ghclient.CheckRunInfo) {
	sort.Slice(checks, func(i, j int) bool {
		di := sortKeyDuration(checks[i])
		dj := sortKeyDuration(checks[j])
		if di != dj {
			return di < dj
		}
		si := statusPriority(checks[i].Status)
		sj := statusPriority(checks[j].Status)
		if si != sj {
			return si < sj
		}
		return FormatCheckName(checks[i]) < FormatCheckName(checks[j])
	})
}

// statusPriority returns a numeric priority for status sorting.
// Lower values appear first: in_progress (0) > completed (1) > queued (2) > other (3).
func statusPriority(status string) int {
	switch status {
	case "in_progress":
		return 0
	case "completed":
		return 1
	case "queued":
		return 2
	default:
		return 3
	}
}

// sortKeyDuration returns a duration for sorting: actual duration for completed/running,
// 0 for unknown durations, and a large value for queued jobs (so they appear last).
func sortKeyDuration(check ghclient.CheckRunInfo) time.Duration {
	switch check.Status {
	case "completed":
		d := timing.FinalDuration(check)
		if d > 0 {
			return d
		}
		return 0
	case "in_progress":
		d := timing.Runtime(check)
		if d > 0 {
			return d
		}
		return 0
	default:
		return time.Duration(1 << 62)
	}
}
