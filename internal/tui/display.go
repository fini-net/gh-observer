package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/fini-net/gh-observer/internal/timing"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
)

// ColumnWidths holds pre-calculated column widths for aligned rendering
type ColumnWidths struct {
	QueueWidth    int // Right-aligned queue latency
	NameWidth     int // Left-aligned check name
	DurationWidth int // Right-aligned duration
	AvgWidth      int // Right-aligned historical average
}

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
	if runewidth.StringWidth(name) <= maxWidth {
		return name
	}

	if check.WorkflowName != "" {
		prefix := check.WorkflowName + " / "
		prefixWidth := runewidth.StringWidth(prefix)

		if prefixWidth >= maxWidth {
			return runewidth.Truncate(name, maxWidth, "…")
		}

		remainingWidth := maxWidth - prefixWidth
		if runewidth.StringWidth(check.Name) <= remainingWidth {
			return prefix + check.Name
		}
		return prefix + runewidth.Truncate(check.Name, remainingWidth, "…")
	}

	return runewidth.Truncate(name, maxWidth, "…")
}

// FormatLink wraps text in an OSC 8 terminal hyperlink
func FormatLink(url, text string) string {
	if url == "" {
		return text
	}
	return termenv.Hyperlink(url, text)
}

// BuildNameColumn returns a left-aligned name column of exactly widths.NameWidth
// terminal display cells. If enableLinks is true and the check has a DetailsURL, the
// visible text is wrapped in an OSC 8 hyperlink; padding spaces are appended outside
// the link so that display-width measurement stays accurate for the rest of the line.
func BuildNameColumn(check ghclient.CheckRunInfo, widths ColumnWidths, enableLinks bool) string {
	name := FormatCheckNameWithTruncate(check, widths.NameWidth)
	paddingLen := max(widths.NameWidth-runewidth.StringWidth(name), 0)
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
		if runewidth.StringWidth(queueText) > widths.QueueWidth {
			widths.QueueWidth = runewidth.StringWidth(queueText)
		}

		name := FormatCheckName(check)
		nameLen := runewidth.StringWidth(name)
		if nameLen > widths.NameWidth && nameLen <= maxNameWidth {
			widths.NameWidth = nameLen
		} else if nameLen > maxNameWidth {
			widths.NameWidth = maxNameWidth
		}

		durationText := FormatDuration(check)
		if runewidth.StringWidth(durationText) > widths.DurationWidth {
			widths.DurationWidth = runewidth.StringWidth(durationText)
		}

		avgText := FormatAvg(check, jobAverages)
		if runewidth.StringWidth(avgText) > widths.AvgWidth {
			widths.AvgWidth = runewidth.StringWidth(avgText)
		}
	}

	return widths
}

// FormatAlignedColumns formats the four columns with proper padding
func FormatAlignedColumns(queueText, nameText, durationText, avgText string, widths ColumnWidths) (string, string, string, string) {
	queuePadding := max(widths.QueueWidth-runewidth.StringWidth(queueText), 0)
	queueCol := strings.Repeat(" ", queuePadding) + queueText

	namePadding := max(widths.NameWidth-runewidth.StringWidth(nameText), 0)
	nameCol := nameText + strings.Repeat(" ", namePadding)

	durationPadding := max(widths.DurationWidth-runewidth.StringWidth(durationText), 0)
	durationCol := strings.Repeat(" ", durationPadding) + durationText

	avgPadding := max(widths.AvgWidth-runewidth.StringWidth(avgText), 0)
	avgCol := strings.Repeat(" ", avgPadding) + avgText

	return queueCol, nameCol, durationCol, avgCol
}

// FormatHeaderColumns formats the column headers with proper padding
func FormatHeaderColumns(widths ColumnWidths) (string, string, string, string) {
	queuePad := max(widths.QueueWidth-7, 0)
	headerQueue := strings.Repeat(" ", queuePad) + "Start"

	namePad := max(widths.NameWidth-12, 0)
	headerName := "Workflow/Job" + strings.Repeat(" ", namePad)

	durationPad := max(widths.DurationWidth-7, 0)
	headerDuration := strings.Repeat(" ", durationPad) + "ThisRun"

	avgPad := max(
		// "HistAvg" is 7 chars
		widths.AvgWidth-7, 0)
	headerAvg := strings.Repeat(" ", avgPad) + "HistAvg"

	return headerQueue, headerName, headerDuration, headerAvg
}

// FormatDescription truncates description to fit within the total visual width
func FormatDescription(description string, widths ColumnWidths) string {
	if description == "" {
		return ""
	}
	totalWidth := widths.QueueWidth + 1 + 1 + widths.NameWidth + 2 + widths.DurationWidth + 2 + widths.AvgWidth
	maxLen := max(totalWidth-4, 20)
	return runewidth.Truncate(description, maxLen, "…")
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
