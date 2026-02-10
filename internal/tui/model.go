package tui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/google/go-github/v58/github"
	ghclient "github.com/fini-net/gh-observer/internal/github"
)

// Model holds the application state
type Model struct {
	ctx     context.Context
	ghClient *github.Client
	owner    string
	repo     string
	prNumber int

	// PR metadata
	prTitle        string
	headSHA        string
	prCreatedAt    time.Time
	headCommitTime time.Time

	// Check runs
	checkRuns []*github.CheckRun

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
func NewModel(ctx context.Context, owner, repo string, prNumber int, refreshInterval time.Duration, styles Styles) Model {
	client, err := ghclient.NewClient(ctx)

	s := spinner.New()
	s.Spinner = spinner.Dot

	return Model{
		ctx:             ctx,
		ghClient:        client,
		owner:           owner,
		repo:            repo,
		prNumber:        prNumber,
		spinner:         s,
		startTime:       time.Now(),
		lastUpdate:      time.Now(),
		refreshInterval: refreshInterval,
		styles:          styles,
		err:             err,
	}
}

// ExitCode returns the exit code for the program
func (m Model) ExitCode() int {
	return m.exitCode
}

