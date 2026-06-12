package tui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/fini-net/gh-observer/internal/debug"
	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/google/go-github/v86/github"
)

type RepoTickMsg time.Time

type RepoChecksUpdateMsg struct {
	PRData             map[int]ghclient.PRCheckData
	RateLimitRemaining int
	Err                error
}

type DefaultBranchMsg struct {
	Branch string
	Err    error
}

type RepoBranchRunsMsg struct {
	Runs               []ghclient.BranchRunData
	RateLimitRemaining int
	Err                error
}

func (m RepoModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.spinner.Tick,
		fetchRepoCheckRuns(m.ctx, m.token, m.owner, m.repo),
		repoTick(m.refreshInterval),
	}
	if m.showBranchRuns {
		cmds = append(cmds, fetchDefaultBranch(m.ctx, m.client, m.owner, m.repo))
		if m.allBranches {
			cmds = append(cmds, fetchBranchRunsCmd(m.ctx, m.client, m.owner, m.repo, "", m.fadeWindow()))
		}
	}
	return tea.Batch(cmds...)
}

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
		cmds := []tea.Cmd{
			fetchRepoCheckRuns(m.ctx, m.token, m.owner, m.repo),
			repoTick(m.refreshInterval),
		}
		if m.showBranchRuns && (m.defaultBranch != "" || m.allBranches) {
			branch := m.defaultBranch
			if m.allBranches {
				branch = ""
			}
			cmds = append(cmds, fetchBranchRunsCmd(m.ctx, m.client, m.owner, m.repo, branch, m.fadeWindow()))
		}
		return m, tea.Batch(cmds...)

	case RepoChecksUpdateMsg:
		return m.handleRepoChecksUpdate(msg)

	case DefaultBranchMsg:
		return m.handleDefaultBranch(msg)

	case RepoBranchRunsMsg:
		return m.handleBranchRunsUpdate(msg)
	}

	return m, nil
}

func (m RepoModel) fadeWindow() time.Duration {
	if m.fadeFailure > m.fadeSuccess {
		return m.fadeFailure
	}
	return m.fadeSuccess
}

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

func (m *RepoModel) handleDefaultBranch(msg DefaultBranchMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debug.Log("failed to detect default branch", "err", msg.Err)
		return m, nil
	}
	m.defaultBranch = msg.Branch
	debug.Log("default branch detected", "branch", msg.Branch)
	return m, fetchBranchRunsCmd(m.ctx, m.client, m.owner, m.repo, m.defaultBranch, m.fadeWindow())
}

func (m *RepoModel) handleBranchRunsUpdate(msg RepoBranchRunsMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		debug.Log("branch runs fetch error", "err", msg.Err)
		return m, nil
	}

	if msg.RateLimitRemaining < m.rateLimitRemaining {
		m.rateLimitRemaining = msg.RateLimitRemaining
	}
	m.lastUpdate = time.Now()

	now := time.Now()
	var visible []ghclient.BranchRunData
	for _, run := range msg.Runs {
		if isActiveBranchRun(run.Status) {
			visible = append(visible, run)
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

	debug.Log("branch runs update", "total", len(msg.Runs), "visible", len(visible))

	return m, nil
}

func isActiveBranchRun(status string) bool {
	switch status {
	case "in_progress", "queued", "waiting", "pending":
		return true
	}
	return false
}

func repoTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return RepoTickMsg(t)
	})
}

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

func fetchDefaultBranch(ctx context.Context, client *github.Client, owner, repo string) tea.Cmd {
	return func() tea.Msg {
		branch, err := ghclient.FetchDefaultBranch(ctx, client, owner, repo)
		if err != nil {
			return DefaultBranchMsg{Err: err}
		}
		return DefaultBranchMsg{Branch: branch}
	}
}

func fetchBranchRunsCmd(ctx context.Context, client *github.Client, owner, repo, branch string, fadeWindow time.Duration) tea.Cmd {
	return func() tea.Msg {
		runs, rateLimit, err := ghclient.FetchBranchRuns(ctx, client, owner, repo, branch, fadeWindow)
		if err != nil {
			return RepoBranchRunsMsg{Err: err}
		}

		enriched, rateLimit2, err := ghclient.EnrichBranchRunsWithJobs(ctx, client, owner, repo, runs)
		if err != nil {
			debug.Log("enrich branch runs error", "err", err)
		}
		if rateLimit2 < rateLimit {
			rateLimit = rateLimit2
		}

		return RepoBranchRunsMsg{
			Runs:               enriched,
			RateLimitRemaining: rateLimit,
		}
	}
}