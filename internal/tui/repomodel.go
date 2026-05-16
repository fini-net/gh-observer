package tui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/spinner"
	ghclient "github.com/fini-net/gh-observer/internal/github"
)

// RepoWatchModel holds the application state for watching a repository's workflow runs.
type RepoWatchModel struct {
	ctx   context.Context
	token string
	owner string
	repo  string

	// Runs data
	runs []ghclient.RepositoryRunInfo

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
	enableLinks            bool
	persist                bool
	persistRefreshInterval time.Duration

	// Track which runs we've seen (by ID) to detect new runs
	seenRunIDs map[int64]bool
}

// NewRepoWatchModel creates a new TUI model for watching repository workflow runs.
func NewRepoWatchModel(ctx context.Context, token, owner, repo string, refreshInterval time.Duration, styles Styles, enableLinks bool, persist bool, persistRefreshInterval time.Duration) RepoWatchModel {
	s := spinner.New(spinner.WithSpinner(spinner.Dot))

	return RepoWatchModel{
		ctx:                    ctx,
		token:                  token,
		owner:                  owner,
		repo:                   repo,
		spinner:                s,
		startTime:              time.Now(),
		lastUpdate:             time.Now(),
		refreshInterval:        refreshInterval,
		styles:                 styles,
		enableLinks:            enableLinks,
		persist:                persist,
		persistRefreshInterval: persistRefreshInterval,
		seenRunIDs:             make(map[int64]bool),
	}
}

// ExitCode returns the exit code for the program
func (m RepoWatchModel) ExitCode() int {
	return m.exitCode
}