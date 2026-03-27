package tui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/spinner"
	ghclient "github.com/fini-net/gh-observer/internal/github"
)

// Model holds the application state
type Model struct {
	ctx      context.Context
	token    string
	owner    string
	repo     string
	prNumber int

	// PR metadata
	prTitle        string
	headSHA        string
	prCreatedAt    time.Time
	headCommitTime time.Time

	// Check runs
	checkRuns []ghclient.CheckRunInfo

	// Rate limiting
	rateLimitRemaining int

	// UI state
	spinner         spinner.Model
	startTime       time.Time
	lastUpdate      time.Time
	refreshInterval time.Duration
	styles          Styles

	// Exit tracking
	exitCode int
	quitting bool

	// Error state
	err error

	// Feature flags
	enableLinks bool

	// Historical job averages (incrementally updated as new workflows appear)
	jobAverages             map[string]time.Duration
	runIDToWorkflowID       map[int64]int64
	fetchedWorkflowIDs      map[int64]bool
	pendingWorkflowFetch    map[int64]bool
	dispatchedWorkflowFetch map[int64]bool
	avgFetchPending         bool
	avgFetchStartTime       time.Time
	avgFetchLastDuration    time.Duration
	avgFetchErr             error
	noAvg                   bool
	firstCheckSeenAt        time.Time

	// Set when all checks complete; used to defer quit until avgFetchDone
	checksComplete bool

	// Slow job live log display
	slowLogs        map[string][]ghclient.LogLine // keyed by DetailsURL
	slowLogFetching map[string]bool               // prevents duplicate in-flight fetches
	slowLogErr      map[string]error              // per-job fetch errors, keyed by DetailsURL
}

// ColumnWidths holds pre-calculated column widths for aligned rendering
type ColumnWidths struct {
	QueueWidth    int // Right-aligned queue latency
	NameWidth     int // Left-aligned check name
	DurationWidth int // Right-aligned duration
	AvgWidth      int // Right-aligned historical average
}

// NewModel creates a new TUI model
func NewModel(ctx context.Context, token, owner, repo string, prNumber int, refreshInterval time.Duration, styles Styles, enableLinks bool, noAvg bool) Model {
	s := spinner.New(spinner.WithSpinner(spinner.Dot))

	return Model{
		ctx:                     ctx,
		token:                   token,
		owner:                   owner,
		repo:                    repo,
		prNumber:                prNumber,
		spinner:                 s,
		startTime:               time.Now(),
		lastUpdate:              time.Now(),
		refreshInterval:         refreshInterval,
		styles:                  styles,
		enableLinks:             enableLinks,
		noAvg:                   noAvg,
		jobAverages:             make(map[string]time.Duration),
		runIDToWorkflowID:       make(map[int64]int64),
		fetchedWorkflowIDs:      make(map[int64]bool),
		pendingWorkflowFetch:    make(map[int64]bool),
		dispatchedWorkflowFetch: make(map[int64]bool),
		slowLogs:                make(map[string][]ghclient.LogLine),
		slowLogFetching:         make(map[string]bool),
		slowLogErr:              make(map[string]error),
	}
}

// ExitCode returns the exit code for the program
func (m Model) ExitCode() int {
	return m.exitCode
}
