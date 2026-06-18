package tui

import (
	"context"
	"sort"
	"time"

	"charm.land/bubbles/v2/spinner"
	ghclient "github.com/fini-net/gh-observer/internal/github"
)

// PRViewData is the per-PR view state held by RepoModel after fade-out filtering.
type PRViewData struct {
	Title          string
	CheckRuns      []ghclient.CheckRunInfo
	HeadCommitTime time.Time
}

// RepoModel holds the application state for persistent repo watching.
// It tracks both PR-associated checks (via the batched GraphQL query) and
// standalone non-PR workflow runs (via REST), applying fade-out to completed
// checks and runs so the view stays focused on what's active.
type RepoModel struct {
	ctx   context.Context
	token string
	owner string
	repo  string

	// PR data after fade-out filtering: PR number -> view data.
	prs map[int]PRViewData

	// Standalone (non-PR) workflow runs after fade-out filtering.
	standaloneRuns []ghclient.BranchRunData

	// Rate limiting
	rateLimitRemaining int

	// UI state
	spinner         spinner.Model
	startTime       time.Time
	lastUpdate      time.Time
	refreshInterval time.Duration
	styles          Styles

	// Fade-out windows for completed checks/runs.
	fadeSuccess time.Duration
	fadeFailure time.Duration

	// Exit tracking. Repo mode is persistent: exitCode stays 0 and we only
	// quit on q/ctrl+c. The field exists for symmetry with Model/RunModel.
	exitCode int
	quitting bool

	// Non-fatal fetch error tracking, split per source. The GraphQL PR
	// checks fetch and the REST standalone-runs fetch run independently,
	// so a success from one source must not clear an ongoing error from
	// the other. The view renders whichever source's error is most recent.
	fetchErrChecks   error
	fetchErrChecksAt time.Time
	fetchErrRuns     error
	fetchErrRunsAt   time.Time
	fetchReceived    bool

	// Feature flags
	enableLinks bool
}

// NewRepoModel creates a new persistent repo-watch TUI model.
func NewRepoModel(
	ctx context.Context,
	token string,
	owner, repo string,
	refreshInterval time.Duration,
	styles Styles,
	enableLinks bool,
	fadeSuccess, fadeFailure time.Duration,
) RepoModel {
	s := spinner.New(spinner.WithSpinner(spinner.Dot))

	return RepoModel{
		ctx:             ctx,
		token:           token,
		owner:           owner,
		repo:            repo,
		prs:             make(map[int]PRViewData),
		spinner:         s,
		startTime:       time.Now(),
		lastUpdate:      time.Now(),
		refreshInterval: refreshInterval,
		styles:          styles,
		enableLinks:     enableLinks,
		fadeSuccess:     fadeSuccess,
		fadeFailure:     fadeFailure,
	}
}

// ExitCode returns the exit code for the program. Repo mode is persistent and
// only exits on user quit, so this is always 0.
func (m RepoModel) ExitCode() int {
	return m.exitCode
}

// sortedPRNumbers returns PR numbers in ascending order for stable rendering.
func (m RepoModel) sortedPRNumbers() []int {
	nums := make([]int, 0, len(m.prs))
	for n := range m.prs {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	return nums
}

// fadeWindow returns the larger of the two fade durations, used to bound the
// "recently completed" REST query window for standalone runs.
func (m RepoModel) fadeWindow() time.Duration {
	if m.fadeFailure > m.fadeSuccess {
		return m.fadeFailure
	}
	return m.fadeSuccess
}
