package tui

import (
	"time"

	ghclient "github.com/fini-net/gh-observer/internal/github"
)

// TickMsg is sent on each poll interval
type TickMsg time.Time

// PRInfoMsg contains PR metadata. HeadCommitTime was previously sourced from
// the REST commit fetch in FetchPRInfo; that fetch has been removed (issue
// #349) and the push time now arrives via ChecksUpdateMsg from the GraphQL
// check-runs query (which selects pushedDate in the same round-trip).
type PRInfoMsg struct {
	Number    int
	Title     string
	HeadSHA   string
	CreatedAt time.Time
	Err       error
}

// ChecksUpdateMsg contains updated check runs. HeadPushedTime carries the
// PR head commit's push time (pushedDate, with committedDate fallback)
// sourced from the same GraphQL query that produced CheckRuns. Callers
// use it for the "Pushed Xs ago" header and the queue-latency column.
type ChecksUpdateMsg struct {
	CheckRuns          []ghclient.CheckRunInfo
	HeadPushedTime     time.Time
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
	Stale              bool
	Pending            bool
	NotRequested       bool
	RateLimitRemaining int
	Err                error
}
