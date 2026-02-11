package tui

import (
	"fmt"
	"strings"
	"time"

	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/fini-net/gh-observer/internal/timing"
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
	if len(name) > maxWidth {
		return name[:maxWidth-1] + "…"
	}
	return name
}

// CalculateColumnWidths scans all check runs and determines max width for each column
func CalculateColumnWidths(checkRuns []ghclient.CheckRunInfo, headCommitTime time.Time) ColumnWidths {
	const (
		minNameWidth = 20
		maxNameWidth = 60
		minTimeWidth = 5
	)

	widths := ColumnWidths{
		QueueWidth:    minTimeWidth,
		NameWidth:     minNameWidth,
		DurationWidth: minTimeWidth,
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
	}

	return widths
}

// FormatAlignedColumns formats the three columns with proper padding
func FormatAlignedColumns(queueText, nameText, durationText string, widths ColumnWidths) (string, string, string) {
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

	return queueCol, nameCol, durationCol
}

// FormatHeaderColumns formats the column headers with proper padding
func FormatHeaderColumns(widths ColumnWidths) (string, string, string) {
	queuePad := widths.QueueWidth - 7
	if queuePad < 0 {
		queuePad = 0
	}
	headerQueue := strings.Repeat(" ", queuePad) + "Startup"

	namePad := widths.NameWidth - 12
	if namePad < 0 {
		namePad = 0
	}
	headerName := "Workflow/Job" + strings.Repeat(" ", namePad)

	durationPad := widths.DurationWidth - 8
	if durationPad < 0 {
		durationPad = 0
	}
	headerDuration := strings.Repeat(" ", durationPad) + "Duration"

	return headerQueue, headerName, headerDuration
}
