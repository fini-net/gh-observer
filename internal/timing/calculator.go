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
	return time.Since(*check.StartedAt)
}

// FinalDuration calculates the total runtime for completed checks
func FinalDuration(check ghclient.CheckRunInfo) time.Duration {
	if check.StartedAt == nil || check.CompletedAt == nil {
		return 0
	}
	return check.CompletedAt.Sub(*check.StartedAt)
}

// RunJobRuntime calculates elapsed time for an in-progress job.
func RunJobRuntime(startedAt *time.Time) time.Duration {
	if startedAt == nil {
		return 0
	}
	return time.Since(*startedAt)
}

// RunJobDuration calculates the total duration for a completed job.
func RunJobDuration(startedAt, completedAt *time.Time) time.Duration {
	if startedAt == nil || completedAt == nil {
		return 0
	}
	return completedAt.Sub(*startedAt)
}

func FormatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d <= 0 {
		return "0s"
	}

	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	minutes := int(d / time.Minute)
	d -= time.Duration(minutes) * time.Minute
	seconds := int(d / time.Second)

	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}
