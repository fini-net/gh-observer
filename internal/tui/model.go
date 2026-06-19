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
	// fetchReceived is true after the first successful API response that
	// populated rateLimitRemaining. Before that, rateLimitRemaining is the
	// Go zero value (0) and the view must not render a misleading
	// "[Rate limit: 0 remaining]" indicator or trigger backoff.
	fetchReceived bool

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
	workflowAverages        map[int64]map[string]time.Duration
	advSecMatchWorkflow    map[string]int64
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

	// Premature exit prevention (issue #236)
	expectedCheckCount int
	peakCheckCount     int

	// Tracks which check runs we've already triggered discovery for,
	// so we can re-trigger when new jobs appear.
	seenCheckKeys map[string]bool

	// historyFetchCompleted is true after at least one discovery cycle
	// has finished (all pending workflow fetches resolved).
	historyFetchCompleted bool

	// Presumed historical averages for external GitHub App checks (e.g. DCO)
	// that can never have real Actions history. Injected into jobAverages by
	// handleChecksUpdate on each refresh so the HistAvg column shows a value
	// (e.g. "1s") instead of blank. Configured via config.yaml's
	// presumed_averages map.
	presumedAverages map[string]time.Duration
}

// NewModel creates a new TUI model
func NewModel(ctx context.Context, token, owner, repo string, prNumber int, refreshInterval time.Duration, styles Styles, enableLinks bool, noAvg bool, presumedAverages map[string]time.Duration) Model {
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
		workflowAverages:        make(map[int64]map[string]time.Duration),
		advSecMatchWorkflow:    make(map[string]int64),
		runIDToWorkflowID:       make(map[int64]int64),
		fetchedWorkflowIDs:      make(map[int64]bool),
		pendingWorkflowFetch:    make(map[int64]bool),
		dispatchedWorkflowFetch: make(map[int64]bool),
		seenCheckKeys:          make(map[string]bool),
		presumedAverages:        presumedAverages,
	}
}

// ExitCode returns the exit code for the program
func (m Model) ExitCode() int {
	return m.exitCode
}
