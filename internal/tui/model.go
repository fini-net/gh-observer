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
	jobAverages          map[string]time.Duration
	runIDToWorkflowID    map[int64]int64
	fetchedWorkflowIDs   map[int64]bool
	avgFetchPending      bool
	avgFetchStartTime    time.Time
	avgFetchLastDuration time.Duration
	avgFetchErr          error
	noAvg                bool
	firstCheckSeenAt     time.Time

	// Job log errors (fetched async for failed checks, cached for the session)
	jobLogErrors    map[int64][]string
	logFetchPending map[int64]bool

	// Slow non-error job logs (for jobs running >1 minute)
	slowNonerror        bool
	jobSlowLogs         map[int64][]string
	slowLogFetchPending map[int64]bool
	slowLogLastFetch    map[int64]time.Time

	// Set when all checks complete; used to defer quit until avgFetchDone
	checksComplete bool
}

// ColumnWidths holds pre-calculated column widths for aligned rendering
type ColumnWidths struct {
	QueueWidth    int // Right-aligned queue latency
	NameWidth     int // Left-aligned check name
	DurationWidth int // Right-aligned duration
	AvgWidth      int // Right-aligned historical average
}

// NewModel creates a new TUI model
func NewModel(ctx context.Context, token, owner, repo string, prNumber int, refreshInterval time.Duration, styles Styles, enableLinks bool, noAvg bool, slowNonerror bool) Model {
	s := spinner.New(spinner.WithSpinner(spinner.Dot))

	return Model{
		ctx:                 ctx,
		token:               token,
		owner:               owner,
		repo:                repo,
		prNumber:            prNumber,
		spinner:             s,
		startTime:           time.Now(),
		lastUpdate:          time.Now(),
		refreshInterval:     refreshInterval,
		styles:              styles,
		enableLinks:         enableLinks,
		noAvg:               noAvg,
		slowNonerror:        slowNonerror,
		jobAverages:         make(map[string]time.Duration),
		runIDToWorkflowID:   make(map[int64]int64),
		fetchedWorkflowIDs:  make(map[int64]bool),
		jobLogErrors:        make(map[int64][]string),
		logFetchPending:     make(map[int64]bool),
		jobSlowLogs:         make(map[int64][]string),
		slowLogFetchPending: make(map[int64]bool),
		slowLogLastFetch:    make(map[int64]time.Time),
	}
}

// ExitCode returns the exit code for the program
func (m Model) ExitCode() int {
	return m.exitCode
}
