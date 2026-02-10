package tui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	ghclient "github.com/fini-net/gh-observer/internal/github"
)

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		fetchPRInfo(m.ctx, m.token, m.owner, m.repo, m.prNumber),
		tick(m.refreshInterval),
	)
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	case TickMsg:
		// Check rate limit before polling
		if m.rateLimitRemaining < 10 {
			// Back off if rate limited
			return m, tick(m.refreshInterval * 3)
		}

		// Only poll if we have the PR number
		return m, tea.Batch(
			fetchCheckRuns(m.ctx, m.token, m.owner, m.repo, m.prNumber),
			tick(m.refreshInterval),
		)

		// Re-schedule tick even if we can't poll yet
		return m, tick(m.refreshInterval)

	case PRInfoMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, tea.Quit
		}

		m.prTitle = msg.Title
		m.headSHA = msg.HeadSHA
		m.prCreatedAt = msg.CreatedAt
		m.headCommitTime = msg.HeadCommitTime

		// Start polling checks now that we have the PR info
		return m, fetchCheckRuns(m.ctx, m.token, m.owner, m.repo, m.prNumber)

	case ChecksUpdateMsg:
		if msg.Err != nil {
			// Network errors shouldn't be fatal - continue polling
			m.err = msg.Err
			return m, nil
		}

		m.checkRuns = msg.CheckRuns
		m.rateLimitRemaining = msg.RateLimitRemaining
		m.lastUpdate = time.Now()
		m.err = nil // Clear any previous errors

		// Check if all checks are complete
		if allChecksComplete(m.checkRuns) {
			m.exitCode = determineExitCode(m.checkRuns)
			m.quitting = true
			return m, tea.Quit
		}

		return m, nil

	case ErrorMsg:
		m.err = msg.Err
		return m, nil
	}

	return m, nil
}

// tick creates a command that sends a TickMsg after duration d
func tick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// fetchPRInfo fetches PR metadata
func fetchPRInfo(ctx context.Context, token, owner, repo string, prNumber int) tea.Cmd {
	return func() tea.Msg {
		// Create temporary client for PR info (REST API)
		client, err := ghclient.NewClient(ctx)
		if err != nil {
			return PRInfoMsg{Err: err}
		}

		prInfo, err := ghclient.FetchPRInfo(ctx, client, owner, repo, prNumber)
		if err != nil {
			return PRInfoMsg{Err: err}
		}

		createdAt, _ := time.Parse("2006-01-02T15:04:05Z", prInfo.CreatedAt)
		headCommitTime, _ := time.Parse("2006-01-02T15:04:05Z", prInfo.HeadCommitDate)

		return PRInfoMsg{
			Number:         prInfo.Number,
			Title:          prInfo.Title,
			HeadSHA:        prInfo.HeadSHA,
			CreatedAt:      createdAt,
			HeadCommitTime: headCommitTime,
		}
	}
}

// fetchCheckRuns fetches check runs using GraphQL
func fetchCheckRuns(ctx context.Context, token, owner, repo string, prNumber int) tea.Cmd {
	return func() tea.Msg {
		checkRuns, rateLimit, err := ghclient.FetchCheckRunsGraphQL(ctx, token, owner, repo, prNumber)
		if err != nil {
			return ChecksUpdateMsg{Err: err}
		}

		return ChecksUpdateMsg{
			CheckRuns:          checkRuns,
			RateLimitRemaining: rateLimit,
		}
	}
}

// allChecksComplete returns true if all checks have finished
func allChecksComplete(checks []ghclient.CheckRunInfo) bool {
	if len(checks) == 0 {
		return false
	}

	for _, check := range checks {
		if check.Status != "completed" {
			return false
		}
	}

	return true
}

// determineExitCode returns 1 if any check failed, 0 otherwise
func determineExitCode(checks []ghclient.CheckRunInfo) int {
	for _, check := range checks {
		if check.Conclusion == "failure" || check.Conclusion == "timed_out" || check.Conclusion == "action_required" {
			return 1
		}
	}
	return 0
}
