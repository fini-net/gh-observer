package tui

import (
	"context"
	"maps"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
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
			m.err = msg.Err
			return m, nil
		}

		m.checkRuns = msg.CheckRuns
		SortCheckRuns(m.checkRuns)
		m.rateLimitRemaining = msg.RateLimitRemaining
		m.lastUpdate = time.Now()
		m.err = nil

		var cmds []tea.Cmd

		// Check for new run IDs that need history fetching
		if !m.noAvg && !m.avgFetchPending && m.rateLimitRemaining >= 100 {
			var newRunIDs []int64
			for _, cr := range msg.CheckRuns {
				if cr.DetailsURL == "" {
					continue
				}
				runID, err := ghclient.ParseRunIDFromURL(cr.DetailsURL)
				if err != nil {
					continue
				}
				if _, known := m.runIDToWorkflowID[runID]; !known {
					newRunIDs = append(newRunIDs, runID)
				}
			}
			if len(newRunIDs) > 0 {
				m.avgFetchPending = true
				m.avgFetchStartTime = time.Now()
				cmds = append(cmds, fetchJobAverages(m.ctx, m.owner, m.repo, msg.CheckRuns, m.runIDToWorkflowID, m.fetchedWorkflowIDs))
			}
		}

		// Fetch logs for newly failed checks (only if rate limit >= 100)
		if m.rateLimitRemaining >= 100 {
			for _, check := range msg.CheckRuns {
				if check.Conclusion == "failure" || check.Conclusion == "timed_out" {
					jobID, err := ghclient.ParseJobIDFromURL(check.DetailsURL)
					if err != nil {
						continue
					}
					if m.logFetchPending[jobID] || m.jobLogErrors[jobID] != nil {
						continue
					}
					m.logFetchPending[jobID] = true
					cmds = append(cmds, fetchJobLogs(m.ctx, m.owner, m.repo, jobID))
				}
			}
		}

		if allChecksComplete(m.checkRuns) {
			m.exitCode = determineExitCode(m.checkRuns)
			m.checksComplete = true
			// Quit only if no pending average fetch
			if !m.avgFetchPending {
				m.quitting = true
				cmds = append(cmds, tea.Quit)
			}
			return m, tea.Batch(cmds...)
		}

		return m, tea.Batch(cmds...)

	case JobAveragesMsg:
		m.avgFetchPending = false
		m.avgFetchLastDuration = time.Since(m.avgFetchStartTime)

		if msg.Err != nil {
			m.avgFetchErr = msg.Err
		} else {
			// Merge averages
			maps.Copy(m.jobAverages, msg.Averages)
			// Add new run→workflow mappings to cache
			maps.Copy(m.runIDToWorkflowID, msg.NewRunIDToWorkflowID)
			// Mark newly-fetched workflow IDs
			for _, wfID := range msg.NewFetchedWorkflowIDs {
				m.fetchedWorkflowIDs[wfID] = true
			}
		}

		// If checks already finished while we were fetching, quit now
		if m.checksComplete {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil

	case JobLogMsg:
		// Clear pending flag and store results
		delete(m.logFetchPending, msg.JobID)
		if msg.Err == nil && len(msg.Errors) > 0 {
			m.jobLogErrors[msg.JobID] = msg.Errors
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

// fetchJobAverages fetches historical average runtimes for newly-discovered workflows.
func fetchJobAverages(ctx context.Context, owner, repo string, checkRuns []ghclient.CheckRunInfo, knownRunIDToWorkflowID map[int64]int64, knownFetchedWorkflowIDs map[int64]bool) tea.Cmd {
	return func() tea.Msg {
		client, err := ghclient.NewClient(ctx)
		if err != nil {
			return JobAveragesMsg{Err: err}
		}
		averages, newRunIDToWorkflowID, newFetchedWorkflowIDs, err := ghclient.FetchJobAverages(ctx, client, owner, repo, checkRuns, knownRunIDToWorkflowID, knownFetchedWorkflowIDs)
		if err != nil {
			return JobAveragesMsg{Err: err}
		}
		return JobAveragesMsg{
			Averages:              averages,
			NewRunIDToWorkflowID:  newRunIDToWorkflowID,
			NewFetchedWorkflowIDs: newFetchedWorkflowIDs,
		}
	}
}

// fetchJobLogs fetches actual job logs for a failed check to extract error lines.
func fetchJobLogs(ctx context.Context, owner, repo string, jobID int64) tea.Cmd {
	return func() tea.Msg {
		client, err := ghclient.NewClient(ctx)
		if err != nil {
			return JobLogMsg{JobID: jobID, Err: err}
		}

		errors, err := ghclient.FetchJobLogs(ctx, client, owner, repo, jobID)
		if err != nil {
			return JobLogMsg{JobID: jobID, Err: err}
		}
		return JobLogMsg{JobID: jobID, Errors: errors}
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
