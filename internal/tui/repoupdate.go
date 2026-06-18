package tui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/fini-net/gh-observer/internal/debug"
	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/google/go-github/v88/github"
)

// RepoTickMsg is sent on each repo-mode poll interval.
type RepoTickMsg time.Time

// RepoChecksUpdateMsg carries PR check data from the batched GraphQL query.
type RepoChecksUpdateMsg struct {
	PRData             map[int]ghclient.PRCheckData
	RateLimitRemaining int
	Err                error
}

// RepoRunsUpdateMsg carries standalone (non-PR) workflow runs from REST.
type RepoRunsUpdateMsg struct {
	Runs               []ghclient.BranchRunData
	RateLimitRemaining int
	Err                error
}

// Init kicks off the spinner, the first PR fetch, the first standalone-runs
// fetch, and the tick that drives subsequent polls.
func (m RepoModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		fetchRepoCheckRuns(m.ctx, m.token, m.owner, m.repo),
		fetchRepoRuns(m.ctx, m.token, m.owner, m.repo, m.fadeWindow()),
		repoTick(m.refreshInterval),
	)
}

// Update dispatches messages to the appropriate handlers.
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
		// Rate-limit backoff: triple the interval when quota is critically low.
		// Gate on fetchReceived so the zero-value rateLimitRemaining (0) before
		// the first successful response doesn't pin us in backoff forever and
		// suppress the fetch commands that would clear that initial state.
		if m.fetchReceived && m.rateLimitRemaining < rateBackoffThreshold {
			debug.Log("rate limit backoff (repo)", "remaining", m.rateLimitRemaining, "threshold", rateBackoffThreshold)
			return m, repoTick(m.refreshInterval * 3)
		}
		cmds := []tea.Cmd{
			fetchRepoCheckRuns(m.ctx, m.token, m.owner, m.repo),
			fetchRepoRuns(m.ctx, m.token, m.owner, m.repo, m.fadeWindow()),
			repoTick(m.refreshInterval),
		}
		return m, tea.Batch(cmds...)

	case RepoChecksUpdateMsg:
		return m.handleRepoChecksUpdate(msg)

	case RepoRunsUpdateMsg:
		return m.handleRepoRunsUpdate(msg)
	}

	return m, nil
}

// handleRepoChecksUpdate applies fade-out filtering to PR checks and stores
// the surviving PRs in m.prs. A PR stays visible while it has at least one
// active (in_progress/queued) check, or a completed check whose CompletedAt
// is within the configured fade window (fadeSuccess or fadeFailure).
//
// Transient fetch errors (e.g. 504 Gateway Timeout) are non-fatal: the last
// good m.prs is preserved on screen, the error is surfaced via
// m.fetchErrChecks for the view to render as a status line, and polling
// continues. Only the PR-checks error is touched here; the standalone-runs
// error (m.fetchErrRuns) is managed by handleRepoRunsUpdate so a success
// from one source cannot mask an ongoing error from the other.
func (m *RepoModel) handleRepoChecksUpdate(msg RepoChecksUpdateMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.fetchErrChecks = msg.Err
		m.fetchErrChecksAt = time.Now()
		debug.Log("repo checks fetch error", "err", msg.Err)
		return m, nil
	}

	// Take the minimum across sources, but accept the first observed value
	// so the zero default doesn't pin rateLimitRemaining at 0 forever (which
	// would trigger permanent rate-limit backoff and show "0 remaining").
	// Mirrors handleRepoRunsUpdate so neither source can raise the value
	// past what the other already observed.
	if !m.fetchReceived || msg.RateLimitRemaining < m.rateLimitRemaining {
		m.rateLimitRemaining = msg.RateLimitRemaining
	}
	m.lastUpdate = time.Now()
	m.fetchErrChecks = nil
	m.fetchErrChecksAt = time.Time{}
	m.fetchReceived = true

	now := time.Now()
	activePRs := make(map[int]PRViewData)

	for prNum, prData := range msg.PRData {
		checkRuns := prData.CheckRuns
		SortCheckRuns(checkRuns)

		var visible []ghclient.CheckRunInfo
		for _, cr := range checkRuns {
			if cr.Status == "in_progress" || cr.Status == "queued" || cr.Status == "waiting" {
				visible = append(visible, cr)
				continue
			}
			if cr.Status != "completed" {
				continue
			}
			fadeTimeout := m.fadeSuccess
			if ghclient.FailureConclusion(cr.Conclusion) {
				fadeTimeout = m.fadeFailure
			}
			if cr.CompletedAt != nil && now.Sub(*cr.CompletedAt) < fadeTimeout {
				visible = append(visible, cr)
			}
		}

		if len(visible) == 0 {
			continue
		}
		activePRs[prNum] = PRViewData{
			Title:          prData.Title,
			CheckRuns:      visible,
			HeadCommitTime: prData.HeadCommitTime,
		}
	}

	m.prs = activePRs

	debug.Log("repo checks update", "total_prs", len(msg.PRData), "active_prs", len(activePRs),
		"rate_limit_remaining", msg.RateLimitRemaining)

	return m, nil
}

// handleRepoRunsUpdate applies the same fade-out logic to standalone runs and
// stores the survivors in m.standaloneRuns. Active runs are always kept;
// completed runs are kept if RunStartedAt is within the fade window.
//
// Transient fetch errors are non-fatal: the last good m.standaloneRuns is
// preserved, the error is surfaced via m.fetchErrRuns, and polling continues.
// Only the standalone-runs error is touched here; the PR-checks error
// (m.fetchErrChecks) is managed by handleRepoChecksUpdate so a success from
// one source cannot mask an ongoing error from the other.
func (m *RepoModel) handleRepoRunsUpdate(msg RepoRunsUpdateMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.fetchErrRuns = msg.Err
		m.fetchErrRunsAt = time.Now()
		debug.Log("repo runs fetch error", "err", msg.Err)
		return m, nil
	}

	// Take the minimum across sources, but accept the first observed value
	// so the zero default doesn't pin rateLimitRemaining at 0 forever (which
	// would trigger permanent rate-limit backoff and show "0 remaining").
	if !m.fetchReceived || msg.RateLimitRemaining < m.rateLimitRemaining {
		m.rateLimitRemaining = msg.RateLimitRemaining
	}
	m.lastUpdate = time.Now()
	m.fetchErrRuns = nil
	m.fetchErrRunsAt = time.Time{}
	m.fetchReceived = true

	now := time.Now()
	var visible []ghclient.BranchRunData
	for _, run := range msg.Runs {
		if isActiveBranchRun(run.Status) {
			visible = append(visible, run)
			continue
		}
		if run.Status != "completed" {
			continue
		}
		fadeTimeout := m.fadeSuccess
		if ghclient.FailureConclusion(run.Conclusion) {
			fadeTimeout = m.fadeFailure
		}
		if !run.RunStartedAt.IsZero() && now.Sub(run.RunStartedAt) < fadeTimeout {
			visible = append(visible, run)
		}
	}

	m.standaloneRuns = visible

	debug.Log("repo runs update", "total", len(msg.Runs), "visible", len(visible),
		"rate_limit_remaining", msg.RateLimitRemaining)

	return m, nil
}

// isActiveBranchRun returns true for statuses that should always be shown.
func isActiveBranchRun(status string) bool {
	switch status {
	case "in_progress", "queued", "waiting", "pending":
		return true
	}
	return false
}

// repoTick schedules the next RepoTickMsg after d.
func repoTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return RepoTickMsg(t)
	})
}

// fetchRepoCheckRuns issues the batched GraphQL query for all open PRs.
func fetchRepoCheckRuns(ctx context.Context, token, owner, repo string) tea.Cmd {
	return func() tea.Msg {
		prData, rateLimit, err := ghclient.FetchRepoCheckRunsGraphQL(ctx, token, owner, repo)
		return RepoChecksUpdateMsg{
			PRData:             prData,
			RateLimitRemaining: rateLimit,
			Err:                err,
		}
	}
}

// fetchRepoRuns fetches standalone (non-PR) workflow runs and enriches them
// with per-run jobs in a single command. Job enrichment failure is non-fatal:
// runs are still returned with empty Jobs so their headers can render.
func fetchRepoRuns(ctx context.Context, token, owner, repo string, fadeWindow time.Duration) tea.Cmd {
	return func() tea.Msg {
		client, err := github.NewClient(github.WithAuthToken(token))
		if err != nil {
			return RepoRunsUpdateMsg{Err: err}
		}

		runs, rateLimit, err := ghclient.FetchRepoWorkflowRuns(ctx, client, owner, repo, fadeWindow)
		if err != nil {
			return RepoRunsUpdateMsg{Err: err}
		}

		enriched, rl2, err := ghclient.EnrichRepoRunsWithJobs(ctx, client, owner, repo, runs)
		if err != nil {
			debug.Log("enrich repo runs error", "err", err)
		}
		if rl2 < rateLimit {
			rateLimit = rl2
		}

		return RepoRunsUpdateMsg{
			Runs:               enriched,
			RateLimitRemaining: rateLimit,
		}
	}
}
