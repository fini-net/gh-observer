package tui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/fini-net/gh-observer/internal/debug"
	ghclient "github.com/fini-net/gh-observer/internal/github"
)

// RepoTickMsg is sent on each poll interval for repo-watching mode.
type RepoTickMsg time.Time

// RepoChecksUpdateMsg contains updated check data for all active PRs.
type RepoChecksUpdateMsg struct {
	PRData             map[int]ghclient.PRCheckData
	RateLimitRemaining int
	Err                error
}

// Init initializes the repo model.
func (m RepoModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		fetchRepoCheckRuns(m.ctx, m.token, m.owner, m.repo),
		repoTick(m.refreshInterval),
	)
}

// Update handles messages for repo-watching mode.
func (m RepoModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	case RepoTickMsg:
		if m.rateLimitRemaining < rateBackoffThreshold {
			debug.Log("rate limit backoff (repo)", "remaining", m.rateLimitRemaining, "threshold", rateBackoffThreshold)
			return m, repoTick(m.refreshInterval * 3)
		}
		return m, tea.Batch(
			fetchRepoCheckRuns(m.ctx, m.token, m.owner, m.repo),
			repoTick(m.refreshInterval),
		)

	case RepoChecksUpdateMsg:
		return m.handleRepoChecksUpdate(msg)
	}

	return m, nil
}

// handleRepoChecksUpdate processes updated check data for all PRs.
func (m *RepoModel) handleRepoChecksUpdate(msg RepoChecksUpdateMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.err = msg.Err
		return m, nil
	}

	m.rateLimitRemaining = msg.RateLimitRemaining
	m.lastUpdate = time.Now()
	m.err = nil

	now := time.Now()
	activePRs := make(map[int]PRViewData)

	for prNum, prData := range msg.PRData {
		checkRuns := prData.CheckRuns
		SortCheckRuns(checkRuns)

		var visibleChecks []ghclient.CheckRunInfo

		for _, cr := range checkRuns {
			if cr.Status == "in_progress" || cr.Status == "queued" {
				visibleChecks = append(visibleChecks, cr)
				continue
			}

			if cr.Status == "completed" {
				fadeTimeout := m.fadeSuccess
				if ghclient.FailureConclusion(cr.Conclusion) {
					fadeTimeout = m.fadeFailure
				}
				if cr.CompletedAt != nil && now.Sub(*cr.CompletedAt) < fadeTimeout {
					visibleChecks = append(visibleChecks, cr)
				}
			}
		}

		if len(visibleChecks) == 0 {
			continue
		}
		activePRs[prNum] = PRViewData{
			Title:          prData.Title,
			CheckRuns:      visibleChecks,
			HeadCommitTime: prData.HeadCommitTime,
		}
	}

	m.prs = activePRs

	debug.Log("repo checks update", "total_prs", len(msg.PRData), "active_prs", len(activePRs), "rate_limit_remaining", msg.RateLimitRemaining)

	return m, nil
}

// repoTick creates a command that sends a RepoTickMsg after duration d.
func repoTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return RepoTickMsg(t)
	})
}

// fetchRepoCheckRuns fetches check runs for all open PRs in a repo.
func fetchRepoCheckRuns(ctx context.Context, token, owner, repo string) tea.Cmd {
	return func() tea.Msg {
		prData, rateLimit, err := ghclient.FetchRepoCheckRunsGraphQL(ctx, token, owner, repo)
		if err != nil {
			return RepoChecksUpdateMsg{Err: err}
		}

		return RepoChecksUpdateMsg{
			PRData:             prData,
			RateLimitRemaining: rateLimit,
		}
	}
}