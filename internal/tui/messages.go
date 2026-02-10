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
