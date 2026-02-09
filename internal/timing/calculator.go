package timing

import (
	"fmt"
	"time"

	"github.com/google/go-github/v58/github"
)

// QueueLatency calculates the time from PR creation to check start
func QueueLatency(prCreatedAt time.Time, check *github.CheckRun) time.Duration {
	if check.StartedAt == nil || prCreatedAt.IsZero() {
		return 0
	}
	return check.StartedAt.Time.Sub(prCreatedAt)
}

// Runtime calculates elapsed time for in_progress checks
func Runtime(check *github.CheckRun) time.Duration {
	if check.GetStatus() != "in_progress" || check.StartedAt == nil {
		return 0
	}
	return time.Since(check.StartedAt.Time)
}

// FinalDuration calculates the total runtime for completed checks
func FinalDuration(check *github.CheckRun) time.Duration {
	if check.StartedAt == nil || check.CompletedAt == nil {
		return 0
	}
	return check.CompletedAt.Time.Sub(check.StartedAt.Time)
}

// FormatDuration formats a duration in human-readable form
func FormatDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}

	// Round to seconds
	d = d.Round(time.Second)

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
