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
	"github.com/google/go-github/v89/github"
)

// RunTickMsg is sent on each poll interval for run-watching mode.
type RunTickMsg time.Time

// RunInfoMsg contains run metadata.
type RunInfoMsg struct {
	RunInfo ghclient.RunInfo
	Err     error
}

// RunJobsUpdateMsg contains updated job statuses for a workflow run.
type RunJobsUpdateMsg struct {
	Jobs               []ghclient.WorkflowJobInfo
	RateLimitRemaining int
	Err                error
}

// RunWorkflowsDiscoveredMsg is sent when workflow discovery completes for run mode.
type RunWorkflowsDiscoveredMsg struct {
	NewRunIDToWorkflowID map[int64]int64
	WorkflowIDsToFetch   []int64
	Err                   error
}

// RunJobAveragesPartialMsg is sent for each workflow that finishes history fetch in run mode.
type RunJobAveragesPartialMsg struct {
	WorkflowID int64
	Averages   map[string]time.Duration
	Err        error
}

// RunErrorMsg contains error information for run mode.
type RunErrorMsg struct {
	Err error
}

// Init initializes the run model.
func (m RunModel) Init() tea.Cmd {
		return tea.Batch(
		m.spinner.Tick,
		fetchRunInfo(m.ctx, m.client, m.owner, m.repo, m.runID),
		runTick(m.refreshInterval),
	)
}

// Update handles messages for run-watching mode.
func (m RunModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	case RunTickMsg:
		// Gate backoff on fetchReceived so the zero-value rateLimitRemaining
		// (0) before the first successful response doesn't suppress fetches.
		if m.fetchReceived && m.rateLimitRemaining < rateBackoffThreshold {
			debug.Log("rate limit backoff (run)", "remaining", m.rateLimitRemaining, "threshold", rateBackoffThreshold)
			return m, runTick(m.refreshInterval*3)
		}
		return m, tea.Batch(
			fetchRunJobs(m.ctx, m.client, m.owner, m.repo, m.runID),
			runTick(m.refreshInterval),
		)

	case RunInfoMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, tea.Quit
		}
		m.runInfo = msg.RunInfo
		m.runInfoLoaded = true
		return m, fetchRunJobs(m.ctx, m.client, m.owner, m.repo, m.runID)

	case RunJobsUpdateMsg:
		return m.handleRunJobsUpdate(msg)

	case RunWorkflowsDiscoveredMsg:
		return m.handleRunWorkflowsDiscovered(msg)

	case RunJobAveragesPartialMsg:
		return m.handleRunJobAveragesPartial(msg)

	case RunErrorMsg:
		m.err = msg.Err
		return m, nil
	}

	return m, nil
}

// handleRunJobsUpdate processes job status updates.
func (m *RunModel) handleRunJobsUpdate(msg RunJobsUpdateMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.err = msg.Err
		return m, nil
	}

	m.jobs = msg.Jobs
	SortRunJobs(m.jobs)
	m.rateLimitRemaining = msg.RateLimitRemaining
	m.fetchReceived = true
	m.lastUpdate = time.Now()
	m.err = nil

	// Inject presumed historical durations for external GitHub App checks
	// (e.g. DCO). Run mode jobs are almost always real Actions jobs, but we
	// apply the same logic for consistency. Idempotent — real history wins.
	ghclient.ApplyPresumedAverages(m.jobAverages, ghclient.WorkflowJobInfoToCheckRuns(msg.Jobs), m.presumedAverages)

	debug.Log("run jobs update", "count", len(msg.Jobs), "rate_limit_remaining", msg.RateLimitRemaining)

	newJobs := hasNewRunJobs(msg.Jobs, m.seenJobKeys)
	markRunJobsSeen(msg.Jobs, m.seenJobKeys)

	var cmds []tea.Cmd

	allComplete := ghclient.AllJobsComplete(msg.Jobs)

	// Trigger history discovery if we have new jobs and haven't fetched yet
	if newJobs && !m.noAvg && !m.avgFetchPending && m.rateLimitRemaining >= minRateLimitForFetch {
		checkRuns := ghclient.WorkflowJobInfoToCheckRuns(msg.Jobs)
		if len(checkRuns) > 0 {
			m.avgFetchPending = true
			m.avgFetchStartTime = time.Now()
			cmds = append(cmds, discoverRunWorkflows(m.ctx, m.client, m.owner, m.repo, checkRuns, m.runIDToWorkflowID, m.fetchedWorkflowIDs))
		}
	}

	if allComplete {
		m.exitCode = ghclient.DetermineRunExitCode(m.jobs)
		m.jobsComplete = true
		if !m.avgFetchPending && len(m.pendingWorkflowFetch) == 0 {
			m.quitting = true
			cmds = append(cmds, tea.Quit)
		}
	}

	return m, tea.Batch(cmds...)
}

// handleRunWorkflowsDiscovered processes workflow discovery results for run mode.
func (m *RunModel) handleRunWorkflowsDiscovered(msg RunWorkflowsDiscoveredMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.avgFetchPending = false
		m.avgFetchErr = msg.Err
		if m.jobsComplete && len(m.pendingWorkflowFetch) == 0 {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	}

	maps.Copy(m.runIDToWorkflowID, msg.NewRunIDToWorkflowID)

	var workflowCmds []tea.Cmd
	for _, wfID := range msg.WorkflowIDsToFetch {
		if !m.dispatchedWorkflowFetch[wfID] {
			m.pendingWorkflowFetch[wfID] = true
			m.dispatchedWorkflowFetch[wfID] = true
			workflowCmds = append(workflowCmds, fetchRunWorkflowHistory(m.ctx, m.client, m.owner, m.repo, wfID))
		}
	}

	if len(workflowCmds) == 0 {
		m.avgFetchPending = false
		m.historyFetchCompleted = true
		if len(m.pendingWorkflowFetch) == 0 {
			m.avgFetchLastDuration = time.Since(m.avgFetchStartTime)
		}
	}

	if m.jobsComplete && len(m.pendingWorkflowFetch) == 0 {
		m.quitting = true
		return m, tea.Quit
	}

	return m, tea.Batch(workflowCmds...)
}

// handleRunJobAveragesPartial processes history fetch results for run mode.
func (m *RunModel) handleRunJobAveragesPartial(msg RunJobAveragesPartialMsg) (tea.Model, tea.Cmd) {
	delete(m.pendingWorkflowFetch, msg.WorkflowID)
	m.fetchedWorkflowIDs[msg.WorkflowID] = true

	if msg.Err == nil && msg.Averages != nil {
		maps.Copy(m.jobAverages, msg.Averages)
		m.workflowAverages[msg.WorkflowID] = msg.Averages
	}

	if len(m.pendingWorkflowFetch) == 0 {
		m.avgFetchPending = false
		m.historyFetchCompleted = true
		m.avgFetchLastDuration = time.Since(m.avgFetchStartTime)
		if msg.Err == nil {
			m.avgFetchErr = nil
		}
		if m.jobsComplete {
			m.quitting = true
			return m, tea.Quit
		}
	}

	return m, nil
}

// runTick creates a command that sends a RunTickMsg after duration d.
func runTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return RunTickMsg(t)
	})
}

// fetchRunInfo fetches workflow run metadata.
func fetchRunInfo(ctx context.Context, client *github.Client, owner, repo string, runID int64) tea.Cmd {
	return func() tea.Msg {
		runInfo, err := ghclient.FetchRunInfo(ctx, client, owner, repo, runID)
		if err != nil {
			return RunInfoMsg{Err: err}
		}

		return RunInfoMsg{RunInfo: *runInfo}
	}
}

// fetchRunJobs fetches the jobs for a workflow run.
func fetchRunJobs(ctx context.Context, client *github.Client, owner, repo string, runID int64) tea.Cmd {
	return func() tea.Msg {
		jobs, rateLimit, err := ghclient.FetchRunJobs(ctx, client, owner, repo, runID)
		if err != nil {
			return RunJobsUpdateMsg{Err: err}
		}

		return RunJobsUpdateMsg{
			Jobs:               jobs,
			RateLimitRemaining: rateLimit,
		}
	}
}

// discoverRunWorkflows resolves workflow IDs from job data for history fetching.
func discoverRunWorkflows(ctx context.Context, client *github.Client, owner, repo string, checkRuns []ghclient.CheckRunInfo, knownRunIDToWorkflowID map[int64]int64, knownFetchedWorkflowIDs map[int64]bool) tea.Cmd {
	return func() tea.Msg {
		newRunIDToWorkflowID, workflowIDsToFetch, err := ghclient.DiscoverWorkflows(ctx, client, owner, repo, checkRuns, knownRunIDToWorkflowID, knownFetchedWorkflowIDs)
		if err != nil {
			return RunWorkflowsDiscoveredMsg{Err: err}
		}
		return RunWorkflowsDiscoveredMsg{
			NewRunIDToWorkflowID: newRunIDToWorkflowID,
			WorkflowIDsToFetch:   workflowIDsToFetch,
		}
	}
}

// fetchRunWorkflowHistory fetches historical job durations for a single workflow.
func fetchRunWorkflowHistory(ctx context.Context, client *github.Client, owner, repo string, workflowID int64) tea.Cmd {
	return func() tea.Msg {
		averages, err := ghclient.FetchWorkflowHistory(ctx, client, owner, repo, workflowID)
		if err != nil {
			return RunJobAveragesPartialMsg{WorkflowID: workflowID, Err: err}
		}
		return RunJobAveragesPartialMsg{
			WorkflowID: workflowID,
			Averages:   averages,
		}
	}
}

// hasNewRunJobs returns true if any jobs in the update are new.
func hasNewRunJobs(jobs []ghclient.WorkflowJobInfo, seen map[string]bool) bool {
	for _, job := range jobs {
		key := runJobKey(job)
		if !seen[key] {
			return true
		}
	}
	return false
}

// markRunJobsSeen records all job keys as seen.
func markRunJobsSeen(jobs []ghclient.WorkflowJobInfo, seen map[string]bool) {
	for _, job := range jobs {
		seen[runJobKey(job)] = true
	}
}

// runJobKey returns a unique key for a job.
func runJobKey(job ghclient.WorkflowJobInfo) string {
	if job.RunID > 0 {
		return fmt.Sprintf("run:%d:%s", job.RunID, job.Name)
	}
	return fmt.Sprintf("name:%s", job.Name)
}