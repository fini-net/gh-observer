package tui

import (
	"time"

	ghclient "github.com/fini-net/gh-observer/internal/github"
)

// TickMsg is sent on each poll interval
type TickMsg time.Time

// PRInfoMsg contains PR metadata
type PRInfoMsg struct {
	Number         int
	Title          string
	HeadSHA        string
	CreatedAt      time.Time
	HeadCommitTime time.Time
	Err            error
}

// ChecksUpdateMsg contains updated check runs
type ChecksUpdateMsg struct {
	CheckRuns          []ghclient.CheckRunInfo
	RateLimitRemaining int
	Err                error
}

// ErrorMsg contains error information
type ErrorMsg struct {
	Err error
}

// WorkflowsDiscoveredMsg is sent when workflow discovery completes
type WorkflowsDiscoveredMsg struct {
	NewRunIDToWorkflowID map[int64]int64
	WorkflowIDsToFetch   []int64
	Err                  error
}

// JobAveragesPartialMsg is sent for each workflow that finishes history fetch
type JobAveragesPartialMsg struct {
	WorkflowID int64
	Averages   map[string]time.Duration
	Err        error
}

// CopilotReviewMsg carries the Copilot code review state for the PR's HEAD
// commit. Copilot reviews live in PullRequest.reviews (not
// StatusCheckRollup.Contexts), so they require a separate fetch path and
// their own message type (issue #409).
type CopilotReviewMsg struct {
	State              string
	SubmittedAt        time.Time
	CommitOID          string
	Stale              bool
	Pending            bool
	NotRequested       bool
	RateLimitRemaining int
	Err                error
}
