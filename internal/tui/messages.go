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

// JobAveragesMsg contains historical job average durations and cache state
type JobAveragesMsg struct {
	Averages              map[string]time.Duration
	NewRunIDToWorkflowID  map[int64]int64
	NewFetchedWorkflowIDs []int64
	Err                   error
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
