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
		if m.rateLimitRemaining < rateBackoffThreshold {
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
		return m.handleChecksUpdate(msg)

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

	case WorkflowsDiscoveredMsg:
		if msg.Err != nil {
			m.avgFetchPending = false
			m.avgFetchErr = msg.Err
		} else {
			// Add new run→workflow mappings to cache
			maps.Copy(m.runIDToWorkflowID, msg.NewRunIDToWorkflowID)
			// Track pending workflow fetches and dispatch them immediately
			var workflowCmds []tea.Cmd
			for _, wfID := range msg.WorkflowIDsToFetch {
				if !m.dispatchedWorkflowFetch[wfID] {
					m.pendingWorkflowFetch[wfID] = true
					m.dispatchedWorkflowFetch[wfID] = true
					workflowCmds = append(workflowCmds, fetchWorkflowHistory(m.ctx, m.owner, m.repo, wfID))
				}
			}
			// If no new fetches, discovery phase is complete
			if len(workflowCmds) == 0 {
				m.avgFetchPending = false
				if len(m.pendingWorkflowFetch) == 0 {
					m.avgFetchLastDuration = time.Since(m.avgFetchStartTime)
				}
			}
			// If checks already finished while we were fetching, and no pending fetches, quit now
			if m.checksComplete && len(m.pendingWorkflowFetch) == 0 {
				m.quitting = true
				return m, tea.Quit
			}
			return m, tea.Batch(workflowCmds...)
		}

		// Error case: check if we should quit
		if m.checksComplete && len(m.pendingWorkflowFetch) == 0 {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil

	case JobAveragesPartialMsg:
		// Remove from pending set
		delete(m.pendingWorkflowFetch, msg.WorkflowID)
		m.fetchedWorkflowIDs[msg.WorkflowID] = true

		if msg.Err == nil && msg.Averages != nil {
			// Merge averages into model
			maps.Copy(m.jobAverages, msg.Averages)
		}

		// Check if all workflow fetches are done
		if len(m.pendingWorkflowFetch) == 0 {
			// Discovery phase complete - record duration and clear error on success
			m.avgFetchPending = false
			m.avgFetchLastDuration = time.Since(m.avgFetchStartTime)
			if msg.Err == nil {
				m.avgFetchErr = nil
			}
			if m.checksComplete {
				m.quitting = true
				return m, tea.Quit
			}
		}
		return m, nil

	case SlowJobLogsMsg:
		delete(m.slowLogFetching, msg.JobURL)
		if msg.Err == nil && len(msg.Lines) > 0 {
			m.slowLogs[msg.JobURL] = msg.Lines
		}
		return m, nil

	case ErrorMsg:
		m.err = msg.Err
		return m, nil
	}

	return m, nil
}

// handleChecksUpdate processes check run updates and returns the updated model.
func (m *Model) handleChecksUpdate(msg ChecksUpdateMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.err = msg.Err
		return m, nil
	}

	m.checkRuns = msg.CheckRuns
	SortCheckRuns(m.checkRuns)
	m.rateLimitRemaining = msg.RateLimitRemaining
	m.lastUpdate = time.Now()
	m.err = nil

	if m.firstCheckSeenAt.IsZero() && len(msg.CheckRuns) > 0 {
		m.firstCheckSeenAt = time.Now()
	}

	var cmds []tea.Cmd

	// Clean up log state for completed jobs and trigger fetches for slow in-progress jobs.
	for _, cr := range msg.CheckRuns {
		if cr.DetailsURL == "" {
			continue
		}
		if cr.Status == "completed" {
			delete(m.slowLogs, cr.DetailsURL)
			delete(m.slowLogFetching, cr.DetailsURL)
			continue
		}
		if cr.Status != "in_progress" || cr.StartedAt == nil {
			continue
		}
		if time.Since(*cr.StartedAt) < slowLogThreshold {
			continue
		}
		if m.slowLogFetching[cr.DetailsURL] {
			continue
		}
		m.slowLogFetching[cr.DetailsURL] = true
		cmds = append(cmds, fetchSlowJobLogs(m.ctx, m.owner, m.repo, cr.DetailsURL))
	}

	allComplete := allChecksComplete(msg.CheckRuns)
	elapsed := time.Since(m.firstCheckSeenAt)
	readyForHistory := !m.noAvg && !m.firstCheckSeenAt.IsZero() && (allComplete || elapsed >= historyFetchDelay)
	if readyForHistory && !m.avgFetchPending && m.rateLimitRemaining >= minRateLimitForFetch {
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
			cmds = append(cmds, discoverWorkflows(m.ctx, m.owner, m.repo, msg.CheckRuns, m.runIDToWorkflowID, m.fetchedWorkflowIDs))
		}
	}

	if allChecksComplete(m.checkRuns) {
		m.exitCode = determineExitCode(m.checkRuns)
		m.checksComplete = true
		// Only quit if no pending/dispatched workflow fetches
		if !m.avgFetchPending && len(m.pendingWorkflowFetch) == 0 {
			m.quitting = true
			cmds = append(cmds, tea.Quit)
		}
		return m, tea.Batch(cmds...)
	}

	return m, tea.Batch(cmds...)
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

		createdAt, _ := ghclient.ParseTimestamp(prInfo.CreatedAt)
		headCommitTime, _ := ghclient.ParseTimestamp(prInfo.HeadCommitDate)

		return PRInfoMsg{
			Number:         prInfo.Number,
			Title:          prInfo.Title,
			HeadSHA:        prInfo.HeadSHA,
			CreatedAt:      createdAt,
			HeadCommitTime: headCommitTime,
		}
	}
}

// discoverWorkflows resolves run IDs to workflow IDs and returns which workflows need history fetches.
func discoverWorkflows(ctx context.Context, owner, repo string, checkRuns []ghclient.CheckRunInfo, knownRunIDToWorkflowID map[int64]int64, knownFetchedWorkflowIDs map[int64]bool) tea.Cmd {
	return func() tea.Msg {
		client, err := ghclient.NewClient(ctx)
		if err != nil {
			return WorkflowsDiscoveredMsg{Err: err}
		}
		newRunIDToWorkflowID, workflowIDsToFetch, err := ghclient.DiscoverWorkflows(ctx, client, owner, repo, checkRuns, knownRunIDToWorkflowID, knownFetchedWorkflowIDs)
		if err != nil {
			return WorkflowsDiscoveredMsg{Err: err}
		}
		return WorkflowsDiscoveredMsg{
			NewRunIDToWorkflowID: newRunIDToWorkflowID,
			WorkflowIDsToFetch:   workflowIDsToFetch,
		}
	}
}

// fetchWorkflowHistory fetches historical job durations for a single workflow.
func fetchWorkflowHistory(ctx context.Context, owner, repo string, workflowID int64) tea.Cmd {
	return func() tea.Msg {
		client, err := ghclient.NewClient(ctx)
		if err != nil {
			return JobAveragesPartialMsg{WorkflowID: workflowID, Err: err}
		}
		averages, err := ghclient.FetchWorkflowHistory(ctx, client, owner, repo, workflowID)
		if err != nil {
			return JobAveragesPartialMsg{WorkflowID: workflowID, Err: err}
		}
		return JobAveragesPartialMsg{
			WorkflowID: workflowID,
			Averages:   averages,
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

// fetchSlowJobLogs fetches the last N log lines for a slow in-progress job.
func fetchSlowJobLogs(ctx context.Context, owner, repo, detailsURL string) tea.Cmd {
	return func() tea.Msg {
		client, err := ghclient.NewClient(ctx)
		if err != nil {
			return SlowJobLogsMsg{JobURL: detailsURL, Err: err}
		}
		jobID, err := ghclient.ParseJobIDFromURL(detailsURL)
		if err != nil {
			return SlowJobLogsMsg{JobURL: detailsURL, Err: err}
		}
		lines, err := ghclient.FetchLastNJobLines(ctx, client, owner, repo, jobID, slowLogLineCount)
		if err != nil {
			return SlowJobLogsMsg{JobURL: detailsURL, Err: err}
		}
		return SlowJobLogsMsg{JobURL: detailsURL, Lines: lines}
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
		if ghclient.FailureConclusion(check.Conclusion) {
			return 1
		}
	}
	return 0
}
