package tui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/fini-net/gh-observer/internal/debug"
	ghclient "github.com/fini-net/gh-observer/internal/github"
)

// RepoWatchTickMsg is sent on each poll interval for repo-watch mode.
type RepoWatchTickMsg time.Time

// Init initializes the repo watch model.
func (m RepoWatchModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		fetchRepositoryRuns(m.ctx, m.token, m.owner, m.repo),
		repoWatchTick(m.refreshInterval),
	)
}

// Update handles messages for repo-watch mode.
func (m RepoWatchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case RepoWatchTickMsg:
		if m.rateLimitRemaining < rateBackoffThreshold {
			debug.Log("rate limit backoff (repo-watch)", "remaining", m.rateLimitRemaining, "threshold", rateBackoffThreshold)
			return m, repoWatchTick(m.refreshInterval * 3)
		}

		interval := m.refreshInterval
		allComplete := ghclient.AllRunsComplete(m.runs)
		if m.persist && allComplete && len(m.runs) > 0 {
			interval = m.persistRefreshInterval
		}

		return m, tea.Batch(
			fetchRepositoryRuns(m.ctx, m.token, m.owner, m.repo),
			repoWatchTick(interval),
		)

	case RepoRunsUpdateMsg:
		return m.handleRepoRunsUpdate(msg)
	}

	return m, nil
}

func (m *RepoWatchModel) handleRepoRunsUpdate(msg RepoRunsUpdateMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.err = msg.Err
		return m, nil
	}

	m.runs = msg.Runs
	m.rateLimitRemaining = msg.RateLimitRemaining
	m.lastUpdate = time.Now()
	m.err = nil

	debug.Log("repo runs update", "count", len(msg.Runs), "rate_limit_remaining", msg.RateLimitRemaining)

	// Track new run IDs
	for _, run := range msg.Runs {
		m.seenRunIDs[run.ID] = true
	}

	allComplete := ghclient.AllRunsComplete(m.runs)

	if allComplete && len(m.runs) > 0 && !m.persist {
		m.exitCode = ghclient.DetermineRepoWatchExitCode(m.runs)
		m.quitting = true
		return m, tea.Quit
	}

	// Update exit code even in persist mode for when user quits
	if allComplete && len(m.runs) > 0 {
		m.exitCode = ghclient.DetermineRepoWatchExitCode(m.runs)
	}

	return m, nil
}

// repoWatchTick creates a command that sends a RepoWatchTickMsg after duration d.
func repoWatchTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return RepoWatchTickMsg(t)
	})
}

// fetchRepositoryRuns fetches recent workflow runs for the repository.
func fetchRepositoryRuns(ctx context.Context, token, owner, repo string) tea.Cmd {
	return func() tea.Msg {
		client, err := ghclient.NewClient(ctx)
		if err != nil {
			return RepoRunsUpdateMsg{Err: err}
		}

		runs, rateLimit, err := ghclient.FetchRepositoryRuns(ctx, client, owner, repo)
		if err != nil {
			return RepoRunsUpdateMsg{Err: err}
		}

		return RepoRunsUpdateMsg{
			Runs:               runs,
			RateLimitRemaining: rateLimit,
		}
	}
}