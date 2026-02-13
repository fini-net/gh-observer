package timing

import (
	"fmt"
	"time"

	ghclient "github.com/fini-net/gh-observer/internal/github"
)

// QueueLatency calculates the time from commit push to check start
func QueueLatency(commitTime time.Time, check ghclient.CheckRunInfo) time.Duration {
	if check.StartedAt == nil || commitTime.IsZero() {
		return 0
	}
	return check.StartedAt.Sub(commitTime)
}

// Runtime calculates elapsed time for in_progress checks
func Runtime(check ghclient.CheckRunInfo) time.Duration {
	if check.Status != "in_progress" || check.StartedAt == nil {
		return 0
	}
	elapsed := time.Since(*check.StartedAt)
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

// FinalDuration calculates the total runtime for completed checks
func FinalDuration(check ghclient.CheckRunInfo) time.Duration {
	if check.StartedAt == nil {
		return 0
	}
	if check.CompletedAt != nil {
		return check.CompletedAt.Sub(*check.StartedAt)
	}
	// Fallback: if completed but CompletedAt is nil, calculate from StartedAt to now
	// This can happen briefly when a job first transitions to completed
	if check.Status == "completed" {
		return time.Since(*check.StartedAt)
	}
	return 0
}

// FormatDuration formats a duration in human-readable form
func FormatDuration(d time.Duration) string {
	// Round to seconds
	d = d.Round(time.Second)

	// Handle zero or negative durations
	if d <= 0 {
		return "0s"
	}

	hours := d / time.Hour
	d -= hours * time.Hour
	minutes := d / time.Minute
	d -= minutes * time.Minute
	seconds := d / time.Second

	if hours > 0 {
		return formatParts(int(hours), "h", int(minutes), "m", int(seconds), "s")
	}
	if minutes > 0 {
		return formatParts(int(minutes), "m", int(seconds), "s", 0, "")
	}
	return formatParts(int(seconds), "s", 0, "", 0, "")
}

func formatParts(v1 int, u1 string, v2 int, u2 string, v3 int, u3 string) string {
	result := ""
	if v1 > 0 {
		result += formatUnit(v1, u1)
	}
	if v2 > 0 {
		if result != "" {
			result += " "
		}
		result += formatUnit(v2, u2)
	}
	if v3 > 0 {
		if result != "" {
			result += " "
		}
		result += formatUnit(v3, u3)
	}
	return result
}

func formatUnit(value int, unit string) string {
	if value == 0 {
		return ""
	}
	return fmt.Sprintf("%d%s", value, unit)
}
