package tui

import (
	"context"
	"fmt"
	"maps"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/fini-net/gh-observer/internal/debug"
	ghclient "github.com/fini-net/gh-observer/internal/github"
)

// canTrustCompletion returns true when we can trust that all checks have truly
// finished, preventing premature exit when fast checks (e.g., DCO) complete
// before other jobs have even appeared in the API response (issue #236).
func canTrustCompletion(m *Model) bool {
	if m.firstCheckSeenAt.IsZero() {
		return false
	}

	checkCount := len(m.checkRuns)

	if m.noAvg {
		debug.Log("can trust completion: quick mode",
			"check_count", checkCount, "peak", m.peakCheckCount)
		return m.peakCheckCount <= checkCount
	}

	elapsed := time.Since(m.firstCheckSeenAt)

	if elapsed >= startupGracePeriod {
		debug.Log("can trust completion: grace period elapsed",
			"elapsed", elapsed, "check_count", checkCount, "peak", m.peakCheckCount,
			"expected", m.expectedCheckCount)
		return true
	}

	if m.peakCheckCount > checkCount {
		debug.Log("cannot trust completion: checks disappeared",
			"current", checkCount, "peak", m.peakCheckCount)
		return false
	}

	if m.expectedCheckCount > 0 {
		ratio := float64(checkCount) / float64(m.expectedCheckCount)
		if ratio >= minCheckAppearanceRatio {
			debug.Log("can trust completion: appearance ratio met",
				"ratio", ratio, "check_count", checkCount, "expected", m.expectedCheckCount)
			return true
		}
		debug.Log("cannot trust completion: appearance ratio not met",
			"ratio", ratio, "check_count", checkCount, "expected", m.expectedCheckCount)
		return false
	}

	debug.Log("cannot trust completion: no expected count, grace period not elapsed",
		"elapsed", elapsed, "check_count", checkCount)
	return false
}

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
		// Check rate limit before polling. Gate on fetchReceived so the
		// zero-value rateLimitRemaining (0) before the first successful
		// response doesn't suppress the fetch that would clear that state.
		if m.fetchReceived && m.rateLimitRemaining < rateBackoffThreshold {
			debug.Log("rate limit backoff", "remaining", m.rateLimitRemaining, "threshold", rateBackoffThreshold)
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
			// Also discover AdvSec workflows by name matching
			advSecMatches, advSecWFIDs := ghclient.DiscoverAdvSecWorkflows(m.checkRuns, m.fetchedWorkflowIDs)
			for name, wfID := range advSecMatches {
				m.advSecMatchWorkflow[name] = wfID
				if averages, ok := m.workflowAverages[wfID]; ok {
					if _, exists := m.jobAverages[name]; !exists {
						for _, dur := range averages {
							m.jobAverages[name] = dur
							break
						}
					}
				}
			}
			for _, wfID := range advSecWFIDs {
				if !m.dispatchedWorkflowFetch[wfID] {
					m.pendingWorkflowFetch[wfID] = true
					m.dispatchedWorkflowFetch[wfID] = true
					workflowCmds = append(workflowCmds, fetchWorkflowHistory(m.ctx, m.owner, m.repo, wfID))
				}
			}
			// If no new fetches, discovery phase is complete
			if len(workflowCmds) == 0 {
				m.avgFetchPending = false
				m.historyFetchCompleted = true
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
			maps.Copy(m.jobAverages, msg.Averages)
			m.workflowAverages[msg.WorkflowID] = msg.Averages

			// For AdvSec-matched workflows, add an alias in jobAverages
			// keyed by the AdvSec check name, using the first (or only) job's average
			for advSecName, wfID := range m.advSecMatchWorkflow {
				if wfID == msg.WorkflowID {
					if _, exists := m.jobAverages[advSecName]; !exists {
						for _, dur := range msg.Averages {
							m.jobAverages[advSecName] = dur
							break
						}
					}
				}
			}

			m.expectedCheckCount = len(m.jobAverages)
		}

		// Check if all workflow fetches are done
		if len(m.pendingWorkflowFetch) == 0 {
			// Discovery phase complete - record duration and clear error on success
			m.avgFetchPending = false
			m.historyFetchCompleted = true
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

	case ErrorMsg:
		m.err = msg.Err
		return m, nil
	}

	return m, nil
}

// checkKey returns a unique key for a check run, used to detect new jobs.
func checkKey(cr ghclient.CheckRunInfo) string {
	if cr.WorkflowRunID > 0 {
		return fmt.Sprintf("run:%d:%s", cr.WorkflowRunID, cr.Name)
	}
	if cr.DetailsURL != "" {
		return fmt.Sprintf("url:%s:%s", cr.DetailsURL, cr.Name)
	}
	return fmt.Sprintf("name:%s", cr.Name)
}

// hasNewChecks returns true if any check runs in the update are new
// (not previously seen by this model).
func hasNewChecks(checkRuns []ghclient.CheckRunInfo, seen map[string]bool) bool {
	for _, cr := range checkRuns {
		key := checkKey(cr)
		if !seen[key] {
			return true
		}
	}
	return false
}

// markChecksSeen records all check run keys as seen.
func markChecksSeen(checkRuns []ghclient.CheckRunInfo, seen map[string]bool) {
	for _, cr := range checkRuns {
		seen[checkKey(cr)] = true
	}
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
	m.fetchReceived = true
	m.lastUpdate = time.Now()
	m.err = nil

	if len(msg.CheckRuns) > m.peakCheckCount {
		m.peakCheckCount = len(msg.CheckRuns)
	}

	debug.Log("checks update", "count", len(msg.CheckRuns), "peak", m.peakCheckCount, "expected", m.expectedCheckCount, "rate_limit_remaining", msg.RateLimitRemaining)

	if m.firstCheckSeenAt.IsZero() && len(msg.CheckRuns) > 0 {
		m.firstCheckSeenAt = time.Now()
	}

	newChecks := hasNewChecks(msg.CheckRuns, m.seenCheckKeys)
	markChecksSeen(msg.CheckRuns, m.seenCheckKeys)

	var cmds []tea.Cmd

	allComplete := allChecksComplete(msg.CheckRuns)
	elapsed := time.Since(m.firstCheckSeenAt)
	readyForHistory := !m.noAvg && !m.firstCheckSeenAt.IsZero() && (allComplete || elapsed >= historyFetchDelay)

	reDiscover := newChecks && m.historyFetchCompleted && !m.avgFetchPending && m.rateLimitRemaining >= minRateLimitForFetch
	if reDiscover {
		m.avgFetchPending = true
		m.avgFetchStartTime = time.Now()
		cmds = append(cmds, discoverWorkflows(m.ctx, m.owner, m.repo, msg.CheckRuns, m.runIDToWorkflowID, m.fetchedWorkflowIDs))
	}

	if readyForHistory && !m.avgFetchPending && m.rateLimitRemaining >= minRateLimitForFetch {
		needsDiscovery := false
		for _, cr := range msg.CheckRuns {
			if cr.WorkflowID > 0 {
				if !m.fetchedWorkflowIDs[cr.WorkflowID] && !m.dispatchedWorkflowFetch[cr.WorkflowID] {
					needsDiscovery = true
				}
				continue
			}
			if cr.WorkflowRunID > 0 {
				if _, known := m.runIDToWorkflowID[cr.WorkflowRunID]; !known {
					needsDiscovery = true
				}
				continue
			}
			if cr.DetailsURL != "" {
				runID, err := ghclient.ParseRunIDFromURL(cr.DetailsURL)
				if err != nil {
					continue
				}
				if _, known := m.runIDToWorkflowID[runID]; !known {
					needsDiscovery = true
				}
			}
		}

		if !needsDiscovery {
			advSecMatches, advSecWFIDs := ghclient.DiscoverAdvSecWorkflows(msg.CheckRuns, m.fetchedWorkflowIDs)
			for name, wfID := range advSecMatches {
				m.advSecMatchWorkflow[name] = wfID
				if averages, ok := m.workflowAverages[wfID]; ok {
					if _, exists := m.jobAverages[name]; !exists {
						for _, dur := range averages {
							m.jobAverages[name] = dur
							break
						}
					}
				}
			}
			for _, wfID := range advSecWFIDs {
				if !m.dispatchedWorkflowFetch[wfID] {
					m.pendingWorkflowFetch[wfID] = true
					m.dispatchedWorkflowFetch[wfID] = true
					cmds = append(cmds, fetchWorkflowHistory(m.ctx, m.owner, m.repo, wfID))
				}
			}
		}

		if needsDiscovery {
			m.avgFetchPending = true
			m.avgFetchStartTime = time.Now()
			cmds = append(cmds, discoverWorkflows(m.ctx, m.owner, m.repo, msg.CheckRuns, m.runIDToWorkflowID, m.fetchedWorkflowIDs))
		}
	}

	if allChecksComplete(m.checkRuns) && canTrustCompletion(m) {
		m.exitCode = determineExitCode(m.checkRuns)
		m.checksComplete = true
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

		createdAt, err := ghclient.ParseTimestamp(prInfo.CreatedAt)
		if err != nil {
			debug.Log("timestamp parse error", "field", "CreatedAt", "value", prInfo.CreatedAt, "err", err)
		}
		headCommitTime, err := ghclient.ParseTimestamp(prInfo.HeadCommitDate)
		if err != nil {
			debug.Log("timestamp parse error", "field", "HeadCommitDate", "value", prInfo.HeadCommitDate, "err", err)
		}

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
		if ghclient.FailureConclusion(check.Conclusion) {
			return 1
		}
	}
	return 0
}
