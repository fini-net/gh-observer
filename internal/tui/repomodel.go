package tui

import (
	"context"
	"sort"
	"time"

	"charm.land/bubbles/v2/spinner"
	ghclient "github.com/fini-net/gh-observer/internal/github"
)

// PRViewData holds the display state for a single PR in repo mode.
type PRViewData struct {
	Title          string
	CheckRuns      []ghclient.CheckRunInfo
	HeadCommitTime time.Time
}

// RepoModel holds the application state for persistent repo-watching mode.
type RepoModel struct {
	ctx      context.Context
	token    string
	owner    string
	repo     string
	prs     map[int]PRViewData

	// Rate limiting
	rateLimitRemaining int

	// UI state
	spinner         spinner.Model
	startTime       time.Time
	lastUpdate      time.Time
	refreshInterval time.Duration
	styles          Styles

	// Fade-out timeouts
	fadeSuccess time.Duration
	fadeFailure time.Duration

	// Exit tracking
	exitCode int
	quitting bool

	// Error state
	err error

	// Feature flags
	enableLinks bool
}

// NewRepoModel creates a new TUI model for persistent repo-watching mode.
func NewRepoModel(ctx context.Context, token, owner, repo string, refreshInterval time.Duration, styles Styles, enableLinks bool, fadeSuccess, fadeFailure time.Duration) RepoModel {
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

// ExitCode returns the exit code for the program
func (m RepoModel) ExitCode() int {
	return m.exitCode
}

// sortedPRNumbers returns PR numbers sorted numerically (ascending).
func (m RepoModel) sortedPRNumbers() []int {
	nums := make([]int, 0, len(m.prs))
	for n := range m.prs {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	return nums
}