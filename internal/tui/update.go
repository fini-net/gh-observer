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
	cmds := []tea.Cmd{
		m.spinner.Tick,
		fetchPRInfo(m.ctx, m.token, m.host, m.owner, m.repo, m.prNumber),
		tick(m.refreshInterval),
	}
	// One-shot host capability probe (issue #442): check that the Copilot
	// reviewer app exists on this host before arming any Copilot state. On
	// hosts without it (e.g. GitHub Enterprise Server without Copilot code
	// review) this prevents the gate from ever arming and the row from ever
	// rendering.
	if m.waitForCopilot {
		cmds = append(cmds, fetchCopilotProbe(m.ctx, m.token, m.host))
	}
	return tea.Batch(cmds...)
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
		// Independently cap the Copilot poll loop/row on copilot_max_wait,
		// regardless of check completion (issue #442). This runs BEFORE the
		// rate-limit backoff return below: it is a pure local state
		// transition with no API cost, so it must never be starved by
		// backoff (the #442 GHES report — "192h elapsed" — was exactly this
		// starvation: backoff early-returned on every tick).
		//
		// The poll/rate-limit disjunct keeps the timeout from firing before a
		// single Copilot poll has resolved anything. When the push predates
		// copilot_max_wait (attaching to an old PR), the budget is already
		// spent on arrival, and firing immediately would preempt the polls
		// that distinguish the real cases: a long-completed review (show its
		// state), a never-submitted request (show "giving up"), and no
		// Copilot at all (resolve silently). The rate-limit arm (gated on
		// fetchReceived so the pre-first-response zero value doesn't count)
		// preserves starvation-proofing: when polls are suppressed by a
		// genuinely low quota, the timeout fires anyway.
		//
		// copilotTimedOut is only set when Copilot was actually seen: "giving
		// up" is meaningful for a request that never completed, but for a repo
		// without Copilot it reproduces the #442 confusion. No evidence +
		// spent budget resolves silently.
		if m.waitForCopilot && m.copilotPending && !m.copilotReviewComplete &&
			!m.copilotStale && copilotMaxWaitElapsed(&m) &&
			(!m.copilotLastPoll.IsZero() || (m.fetchReceived && m.rateLimitRemaining < minRateLimitForFetch)) {
			m.copilotPending = false
			m.copilotReviewComplete = true
			m.copilotTimedOut = m.copilotEvidenceSeen
			m.copilotState = ""
			debug.Log("copilot poll timed out", "max_wait", m.copilotMaxWait, "evidence_seen", m.copilotEvidenceSeen)
		}

		// Check rate limit before polling. Gate on fetchReceived so the
		// zero-value rateLimitRemaining (0) before the first successful
		// response doesn't suppress the fetch that would clear that state.
		if m.fetchReceived && m.rateLimitRemaining < rateBackoffThreshold {
			debug.Log("rate limit backoff", "remaining", m.rateLimitRemaining, "threshold", rateBackoffThreshold)
			// Back off if rate limited
			return m, tick(m.refreshInterval * 3)
		}

		cmds := []tea.Cmd{
			fetchCheckRuns(m.ctx, m.token, m.host, m.owner, m.repo, m.prNumber),
			tick(m.refreshInterval),
		}

		// Poll Copilot review on its own cadence, gated on rate limit and
		// the initial delay window (issue #409). copilotPollStartTime is
		// PRInfoMsg-time + copilotInitialDelay; the first poll may fire only
		// after that instant so GitHub has time to create the review request.
		// Hosts the probe found incapable never poll (issue #442).
		if m.waitForCopilot && m.copilotPending && !m.quitting &&
			(!m.copilotHostChecked || m.copilotHostCapable) &&
			m.rateLimitRemaining >= minRateLimitForFetch &&
			!m.copilotPollStartTime.IsZero() && time.Now().After(m.copilotPollStartTime) &&
			(m.copilotLastPoll.IsZero() || time.Since(m.copilotLastPoll) >= m.copilotPollInterval) {
			cmds = append(cmds, fetchCopilotReview(m.ctx, m.token, m.host, m.owner, m.repo, m.prNumber, m.headSHA))
		}

		return m, tea.Batch(cmds...)

	case PRInfoMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, tea.Quit
		}

		oldSHA := m.headSHA
		shaChanged := m.headSHA != "" && m.headSHA != msg.HeadSHA

		m.prTitle = stripVariationSelectors(msg.Title)
		m.headSHA = msg.HeadSHA
		m.prCreatedAt = msg.CreatedAt

		cmds := []tea.Cmd{
			fetchCheckRuns(m.ctx, m.token, m.host, m.owner, m.repo, m.prNumber),
		}

		// Start Copilot review polling once headSHA is known (issue #409).
		// If the head SHA changed (new push), reset copilot state so a stale
		// review from the old commit doesn't gate or falsely complete.
		//
		// The first poll is NOT dispatched here: copilotInitialDelay exists to
		// give GitHub time to create the review request after a push, so we
		// optimistically mark the gate as pending and let the TickMsg path fire
		// the first fetch once the initial-delay window elapses. This also lets
		// the two-consecutive-not-requested streak logic run from a real cold
		// start (copilotPending must be true for TickMsg to re-poll).
		//
		// NOTE: shaChanged is currently unreachable in production — fetchPRInfo
		// is only dispatched once from Init(), so PRInfoMsg fires once per run.
		// The reset is kept for forward compatibility if PR info is ever
		// re-polled (e.g. to detect force-pushes mid-watch).
		if m.waitForCopilot {
			// Do not arm the gate on hosts where the capability probe already
			// determined the Copilot reviewer app doesn't exist (issue #442).
			// While the probe is still in flight, arm optimistically — the
			// probe result (CopilotProbeMsg) disarms below, and the
			// two-consecutive-not-requested rule bounds the wait either way.
			probeBlocks := m.copilotHostChecked && !m.copilotHostCapable
			if !probeBlocks {
				if shaChanged {
					m.copilotState = ""
					m.copilotStale = false
					m.copilotReviewComplete = false
					m.copilotNotReqStreak = 0
					m.copilotTimedOut = false
					debug.Log("copilot state reset on head SHA change", "old", oldSHA, "new", msg.HeadSHA)
				}
				m.copilotPending = true
				m.copilotReviewComplete = false
				// copilotWaitStartTime bounds the total wall-clock wait for a
				// Copilot review (copilot_max_wait); copilotPollStartTime is the
				// initial-delay gate: the first poll may fire only after this
				// instant, giving GitHub time to create the review request after a
				// push. See copilotGateSatisfied and the TickMsg poll gate below.
				//
				// Both are provisional here, anchored to PR-info time (now) as a
				// fallback in case the real push time never arrives. handleChecksUpdate
				// re-anchors them to the actual push time (m.headPushedTime) as soon
				// as it's known, so copilot_max_wait means "since push" like queue
				// latency does, not "since gh-observer attached".
				m.copilotWaitStartTime = time.Now()
				m.copilotPollStartTime = time.Now().Add(m.copilotInitialDelay)
				debug.Log("copilot gate armed",
					"wait_start", m.copilotWaitStartTime,
					"poll_start", m.copilotPollStartTime,
					"max_wait", m.copilotMaxWait,
					"initial_delay", m.copilotInitialDelay)
			}
		}

		return m, tea.Batch(cmds...)

	case ChecksUpdateMsg:
		return m.handleChecksUpdate(msg)

	case CopilotReviewMsg:
		return m.handleCopilotReview(msg)

	case CopilotProbeMsg:
		// One-shot host capability result (issue #442). An incapable host
		// disarms the gate entirely: stop pending, hide the row, never poll.
		m.copilotHostChecked = true
		m.copilotHostCapable = msg.Capable
		if !msg.Capable {
			debug.Log("copilot reviewer absent on host; disabling copilot detection")
			m.copilotPending = false
			m.copilotReviewComplete = true
			m.copilotState = ""
			m.copilotStale = false
			m.copilotTimedOut = false
			m.copilotNotReqStreak = 0
			m.copilotWaitStartTime = time.Time{}
			m.copilotPollStartTime = time.Time{}
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
					workflowCmds = append(workflowCmds, fetchWorkflowHistory(m.ctx, m.token, m.host, m.owner, m.repo, wfID))
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
					workflowCmds = append(workflowCmds, fetchWorkflowHistory(m.ctx, m.token, m.host, m.owner, m.repo, wfID))
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
	// Adopt the GraphQL-sourced push time on the first successful poll
	// where it is non-zero. Subsequent polls overwrite with the same
	// value; if a later poll returns zero (e.g. transient query shape
	// change), preserve the last known value rather than dropping the
	// user-facing "Pushed Xs ago" header.
	if !msg.HeadPushedTime.IsZero() {
		firstPushTime := m.headPushedTime.IsZero()
		m.headPushedTime = msg.HeadPushedTime

		// Re-anchor the Copilot wait budget to the real push time instead of
		// the PRInfoMsg-arrival fallback, so copilot_max_wait is measured
		// "since push" (matching queue latency) and a review request that's
		// already older than the budget on attach doesn't get a fresh
		// timeout. Only before the first poll, so the countdown/elapsed text
		// never jumps mid-poll.
		//
		// The initial-delay window is also skipped when the push is already
		// older than copilotInitialDelay (issue #442): the delay exists to
		// give GitHub time to create the review request after a push, but a
		// push that long predates the watch means any request already exists
		// (or will never exist) — waiting the delay only delays the
		// two-consecutive-not-requested resolution for repos that don't use
		// Copilot, the vast majority case.
		if firstPushTime && m.waitForCopilot && !m.copilotWaitStartTime.IsZero() && m.copilotLastPoll.IsZero() {
			m.copilotWaitStartTime = msg.HeadPushedTime
			if time.Since(msg.HeadPushedTime) > m.copilotInitialDelay {
				m.copilotPollStartTime = time.Now()
				debug.Log("copilot gate re-anchored to push time (initial delay skipped; push already older)",
					"wait_start", m.copilotWaitStartTime, "poll_start", m.copilotPollStartTime)
			} else {
				m.copilotPollStartTime = msg.HeadPushedTime.Add(m.copilotInitialDelay)
				debug.Log("copilot gate re-anchored to push time",
					"wait_start", m.copilotWaitStartTime, "poll_start", m.copilotPollStartTime)
			}
		}
	}
	m.rateLimitRemaining = msg.RateLimitRemaining
	m.fetchReceived = true
	m.lastUpdate = time.Now()
	m.err = nil

	// Inject presumed historical durations for external GitHub App checks
	// (e.g. DCO) that have no Actions workflow run to fetch history for. This
	// is idempotent — it only writes when the job name is absent from
	// m.jobAverages, so real history fetched later always wins.
	ghclient.ApplyPresumedAverages(m.jobAverages, m.checkRuns, m.presumedAverages, m.host)

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
		cmds = append(cmds, discoverWorkflows(m.ctx, m.token, m.host, m.owner, m.repo, msg.CheckRuns, m.runIDToWorkflowID, m.fetchedWorkflowIDs))
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
					cmds = append(cmds, fetchWorkflowHistory(m.ctx, m.token, m.host, m.owner, m.repo, wfID))
				}
			}
		}

		if needsDiscovery {
			m.avgFetchPending = true
			m.avgFetchStartTime = time.Now()
			cmds = append(cmds, discoverWorkflows(m.ctx, m.token, m.host, m.owner, m.repo, msg.CheckRuns, m.runIDToWorkflowID, m.fetchedWorkflowIDs))
		}
	}

	if allChecksComplete(m.checkRuns) && canTrustCompletion(m) && copilotGateSatisfied(m) {
		m.exitCode = determineExitCode(m.checkRuns, m.copilotState, m.waitForCopilot)
		m.checksComplete = true
		if !m.avgFetchPending && len(m.pendingWorkflowFetch) == 0 {
			m.quitting = true
			cmds = append(cmds, tea.Quit)
		}
		return m, tea.Batch(cmds...)
	}

	return m, tea.Batch(cmds...)
}

// copilotGateSatisfied returns true when the Copilot review is not blocking
// exit — either because the feature is disabled, the review is complete, the
// review is stale (self-limiting; next poll re-evaluates), or the max wait
// has elapsed (issue #409).
func copilotGateSatisfied(m *Model) bool {
	if !m.waitForCopilot {
		return true
	}
	if !m.copilotPending {
		return true
	}
	// A stale review doesn't block — it's informational and self-corrects.
	if m.copilotStale {
		return true
	}
	// Max wait elapsed — stop waiting and proceed. copilot_max_wait measures
	// from the real push time once handleChecksUpdate re-anchors
	// copilotWaitStartTime to it (falling back to PR-info time if the push
	// time never arrives), so this includes the initial-delay window
	// (issue #409). In practice the TickMsg timeout check above already
	// clears copilotPending once this elapses, so this branch is a safety
	// net for exit timing that races ahead of the next tick.
	if copilotMaxWaitElapsed(m) {
		debug.Log("copilot max wait elapsed, proceeding", "max_wait", m.copilotMaxWait)
		return true
	}
	return false
}

// copilotMaxWaitElapsed returns true once copilot_max_wait has elapsed since
// copilotWaitStartTime was armed (issue #442). Shared by copilotGateSatisfied
// (bounds program exit) and the TickMsg handler (bounds the poll loop and row
// display independently of check completion).
func copilotMaxWaitElapsed(m *Model) bool {
	return !m.copilotWaitStartTime.IsZero() && time.Since(m.copilotWaitStartTime) >= m.copilotMaxWait
}

// handleCopilotReview processes Copilot review state updates (issue #409).
func (m *Model) handleCopilotReview(msg CopilotReviewMsg) (tea.Model, tea.Cmd) {
	m.copilotLastPoll = time.Now()

	if msg.Err != nil {
		debug.Log("copilot review fetch error", "err", msg.Err)
		m.err = msg.Err
		return m, nil
	}

	// Fold the Copilot query's rate-limit observation into the model's
	// accounting, taking the minimum. The >0 guard keeps error-path zeros
	// (no response was received) from spuriously engaging backoff; a real
	// 0 from a successful query is preserved by the source-layer
	// normalization only when absent (see normalizeRateLimit).
	if msg.RateLimitRemaining > 0 && msg.RateLimitRemaining < m.rateLimitRemaining {
		m.rateLimitRemaining = msg.RateLimitRemaining
	}

	// Two-consecutive-not-found rule: require two consecutive "not requested
	// and no HEAD review" polls before declaring Copilot not requested. This
	// absorbs GraphQL read lag after a REST POST that requested the review.
	if msg.NotRequested && !msg.Stale {
		m.copilotNotReqStreak++
		if m.copilotNotReqStreak < 2 {
			debug.Log("copilot not requested (streak incomplete)", "streak", m.copilotNotReqStreak)
			return m, nil
		}
		// Two consecutive not-requested: Copilot isn't on this PR.
		m.copilotPending = false
		m.copilotReviewComplete = true
		m.copilotState = ""
		debug.Log("copilot not requested (confirmed)")
		return m, nil
	}
	m.copilotNotReqStreak = 0

	// Any state here is positive evidence of Copilot on this PR — a request,
	// a review, or a stale review from a previous commit. Used to gate the
	// synthetic row's visibility (issue #442): repos that never use Copilot
	// see "checking…" rather than a phantom queued/in-progress row.
	m.copilotEvidenceSeen = true

	m.copilotState = msg.State
	m.copilotStale = msg.Stale

	if msg.Pending {
		m.copilotPending = true
		m.copilotReviewComplete = false
		debug.Log("copilot review in progress")
		return m, nil
	}

	// Review complete (or stale with no pending) — stop blocking.
	m.copilotPending = false
	m.copilotReviewComplete = true
	debug.Log("copilot review complete", "state", msg.State, "stale", msg.Stale)

	// If checks are already done and averages fetched, quit now.
	if m.checksComplete && !m.avgFetchPending && len(m.pendingWorkflowFetch) == 0 {
		m.exitCode = determineExitCode(m.checkRuns, m.copilotState, m.waitForCopilot)
		m.quitting = true
		return m, tea.Quit
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
func fetchPRInfo(ctx context.Context, token, host, owner, repo string, prNumber int) tea.Cmd {
	return func() tea.Msg {
		// Create temporary client for PR info (REST API)
		client, err := ghclient.NewClientFromToken(token, host)
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

		// The head push time arrives via ChecksUpdateMsg from the GraphQL
		// check-runs query (FetchCheckRunsGraphQL), not from PRInfo (issue #349).

		return PRInfoMsg{
			Number:    prInfo.Number,
			Title:     prInfo.Title,
			HeadSHA:   prInfo.HeadSHA,
			CreatedAt: createdAt,
		}
	}
}

// discoverWorkflows resolves run IDs to workflow IDs and returns which workflows need history fetches.
func discoverWorkflows(ctx context.Context, token, host, owner, repo string, checkRuns []ghclient.CheckRunInfo, knownRunIDToWorkflowID map[int64]int64, knownFetchedWorkflowIDs map[int64]bool) tea.Cmd {
	return func() tea.Msg {
		client, err := ghclient.NewClientFromToken(token, host)
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
func fetchWorkflowHistory(ctx context.Context, token, host, owner, repo string, workflowID int64) tea.Cmd {
	return func() tea.Msg {
		client, err := ghclient.NewClientFromToken(token, host)
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
func fetchCheckRuns(ctx context.Context, token, host, owner, repo string, prNumber int) tea.Cmd {
	return func() tea.Msg {
		checkRuns, headPushedTime, rateLimit, err := ghclient.FetchCheckRunsGraphQL(ctx, token, host, owner, repo, prNumber)
		if err != nil {
			return ChecksUpdateMsg{Err: err}
		}

		return ChecksUpdateMsg{
			CheckRuns:          checkRuns,
			HeadPushedTime:     headPushedTime,
			RateLimitRemaining: rateLimit,
		}
	}
}

// fetchCopilotReview fetches the Copilot code review state via GraphQL
// (issue #409). This is a second query path alongside fetchCheckRuns, hitting
// PullRequest.reviews instead of StatusCheckRollup.Contexts.
func fetchCopilotReview(ctx context.Context, token, host, owner, repo string, prNumber int, headSHA string) tea.Cmd {
	return func() tea.Msg {
		review, rateLimit, err := ghclient.FetchCopilotReview(ctx, token, host, owner, repo, prNumber, headSHA)
		if err != nil {
			return CopilotReviewMsg{Err: err, RateLimitRemaining: rateLimit}
		}
		return CopilotReviewMsg{
			State:              review.State,
			Stale:              review.Stale,
			Pending:            review.Pending,
			NotRequested:       review.NotRequested,
			RateLimitRemaining: rateLimit,
		}
	}
}

// fetchCopilotProbe runs the one-shot host capability probe (issue #442):
// does the Copilot reviewer GitHub App exist on this host at all? Dispatched
// from Init alongside fetchPRInfo so the result typically lands before the
// initial-delay window closes and the first Copilot poll would fire.
func fetchCopilotProbe(ctx context.Context, token, host string) tea.Cmd {
	return func() tea.Msg {
		return CopilotProbeMsg{Capable: ghclient.CopilotReviewerExistsOnHost(ctx, token, host)}
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

// determineExitCode returns 1 if any check failed or the Copilot review
// requested changes, 0 otherwise. The copilotState and waitForCopilot params
// are only meaningful in PR mode (issue #409); run mode passes "" and false.
func determineExitCode(checks []ghclient.CheckRunInfo, copilotState string, waitForCopilot bool) int {
	for _, check := range checks {
		if ghclient.FailureConclusion(check.Conclusion) {
			return 1
		}
	}
	if waitForCopilot && ghclient.CopilotReviewFails(copilotState) {
		return 1
	}
	return 0
}
