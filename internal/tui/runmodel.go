package tui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/spinner"
	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/google/go-github/v88/github"
)

// RunModel holds the application state for watching a workflow run.
type RunModel struct {
	ctx   context.Context
	token string
	client *github.Client
	owner string
	repo  string
	runID int64

	// Run metadata
	runInfo       ghclient.RunInfo
	runInfoLoaded bool

	// Jobs in the run
	jobs []ghclient.WorkflowJobInfo

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
	exitCode      int
	quitting      bool
	jobsComplete  bool

	// Error state
	err error

	// Feature flags
	enableLinks bool

	// Historical job averages (incrementally updated)
	jobAverages             map[string]time.Duration
	workflowAverages        map[int64]map[string]time.Duration
	runIDToWorkflowID       map[int64]int64
	fetchedWorkflowIDs      map[int64]bool
	pendingWorkflowFetch    map[int64]bool
	dispatchedWorkflowFetch map[int64]bool
	avgFetchPending         bool
	avgFetchStartTime       time.Time
	avgFetchLastDuration    time.Duration
	avgFetchErr             error
	noAvg                   bool

	// Tracks which jobs we've already triggered discovery for.
	seenJobKeys map[string]bool

	// historyFetchCompleted is true after at least one discovery cycle
	// has finished (all pending workflow fetches resolved).
	historyFetchCompleted bool

	// Presumed historical averages for external GitHub App checks (e.g. DCO).
	// See Model.presumedAverages for the full rationale. Run mode rarely has
	// external app checks, but we apply the same logic for consistency.
	presumedAverages map[string]time.Duration
}

// NewRunModel creates a new TUI model for watching a workflow run.
func NewRunModel(ctx context.Context, token, owner, repo string, runID int64, refreshInterval time.Duration, styles Styles, enableLinks bool, noAvg bool, presumedAverages map[string]time.Duration) RunModel {
	s := spinner.New(spinner.WithSpinner(spinner.Dot))

	client, _ := ghclient.NewClientFromToken(token)

	return RunModel{
		ctx:                     ctx,
		token:                   token,
		client:                  client,
		owner:                   owner,
		repo:                    repo,
		runID:                   runID,
		spinner:                 s,
		startTime:               time.Now(),
		lastUpdate:              time.Now(),
		refreshInterval:         refreshInterval,
		styles:                  styles,
		enableLinks:             enableLinks,
		noAvg:                   noAvg,
		jobAverages:             make(map[string]time.Duration),
		workflowAverages:        make(map[int64]map[string]time.Duration),
		runIDToWorkflowID:       make(map[int64]int64),
		fetchedWorkflowIDs:      make(map[int64]bool),
		pendingWorkflowFetch:    make(map[int64]bool),
		dispatchedWorkflowFetch: make(map[int64]bool),
		seenJobKeys:             make(map[string]bool),
		presumedAverages:        presumedAverages,
	}
}

// ExitCode returns the exit code for the program
func (m RunModel) ExitCode() int {
	return m.exitCode
}