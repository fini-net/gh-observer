package timing

import (
	"fmt"
	"time"

	ghclient "github.com/fini-net/gh-observer/internal/github"
)

// QueueLatency calculates the time from cycle start to check start. The
// cycleStart parameter should be the later of the head commit's pushedDate
// and the PR's creation time (issue #518), not the commit-author or
// committer timestamp: GitHub's actions-queue delay is the gap between when
// the current CI cycle began and when the first check on it starts. A PR
// opened from an already-pushed branch (or a rebase dragging in a stale
// committedDate) would otherwise show queue times that predate the cycle.
// A zero cycleStart (e.g. GraphQL lookup failed) returns 0 so the caller
// can render a placeholder; a negative result (check started before the
// anchor, e.g. a push-triggered workflow on a pre-existing branch) also
// lands in the caller's placeholder branch.
func QueueLatency(cycleStart time.Time, check ghclient.CheckRunInfo) time.Duration {
	if check.StartedAt == nil || cycleStart.IsZero() {
		return 0
	}
	return check.StartedAt.Sub(cycleStart)
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
