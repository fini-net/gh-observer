package tui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
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
}

// NewModel creates a new TUI model
func NewModel(ctx context.Context, token, owner, repo string, prNumber int, refreshInterval time.Duration, styles Styles) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot

	return Model{
		ctx:             ctx,
		token:           token,
		owner:           owner,
		repo:            repo,
		prNumber:        prNumber,
		spinner:         s,
		startTime:       time.Now(),
		lastUpdate:      time.Now(),
		refreshInterval: refreshInterval,
		styles:          styles,
	}
}

// ExitCode returns the exit code for the program
func (m Model) ExitCode() int {
	return m.exitCode
}

