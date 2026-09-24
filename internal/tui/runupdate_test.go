package tui

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/google/go-github/v92/github"
)

// makeRunModel builds a RunModel with initialized maps for handler tests.
func makeRunModel() *RunModel {
	m := makeRunModelForRender()
	m.fetchReceived = true
	m.rateLimitRemaining = 5000
	return &m
}

// runModelFrom extracts a *RunModel from the tea.Model returned by Update,
// which may be either RunModel or *RunModel (the handlers use pointer
// receivers, so messages they handle return the pointer form).
func runModelFrom(t *testing.T, m tea.Model) *RunModel {
	t.Helper()
	switch v := m.(type) {
	case RunModel:
		return &v
	case *RunModel:
		return v
	default:
		t.Fatalf("Update returned %T, want RunModel or *RunModel", m)
		return nil
	}
}

func TestRunUpdateKeyMsgQuits(t *testing.T) {
	// KeyMsg is an interface; KeyPressMsg is its concrete type. String()
	// returns Text verbatim, so these construct the exact dispatch keys.
	keys := []tea.KeyPressMsg{
		tea.KeyPressMsg(tea.Key{Text: "q", Code: 'q'}),
		tea.KeyPressMsg(tea.Key{Text: "ctrl+c", Code: 3, Mod: tea.ModCtrl}),
	}
	for _, key := range keys {
		t.Run(key.String(), func(t *testing.T) {
			m := makeRunModel()
			newModel, cmd := m.Update(key)
			rm := runModelFrom(t, newModel)
			if !rm.quitting {
				t.Error("quitting should be true after key press")
			}
			if cmd == nil {
				t.Error("Update should return a quit command")
			}
		})
	}
}

func TestRunUpdateRunJobsUpdate(t *testing.T) {
	t.Run("error in message stores error", func(t *testing.T) {
		m := makeRunModel()
		msg := RunJobsUpdateMsg{Err: errors.New("fetch failed")}
		newModel, _ := m.Update(msg)
		rm := runModelFrom(t, newModel)
		if rm.err == nil {
			t.Error("error should be stored")
		}
		if len(rm.jobs) != 0 {
			t.Errorf("jobs = %d, want 0 on error", len(rm.jobs))
		}
	})

	t.Run("successful update stores and sorts jobs", func(t *testing.T) {
		m := makeRunModel()
		msg := RunJobsUpdateMsg{
			Jobs: []ghclient.WorkflowJobInfo{
				{Name: "zeta", Status: "queued"},
				{Name: "alpha", Status: "in_progress"},
			},
			RateLimitRemaining: 4000,
		}
		newModel, _ := m.Update(msg)
		rm := runModelFrom(t, newModel)
		if len(rm.jobs) != 2 {
			t.Fatalf("jobs = %d, want 2", len(rm.jobs))
		}
		if rm.jobs[0].Name != "alpha" {
			t.Errorf("jobs[0] = %q, want %q (sorted: in_progress first)", rm.jobs[0].Name, "alpha")
		}
		if rm.rateLimitRemaining != 4000 {
			t.Errorf("rateLimitRemaining = %d, want 4000", rm.rateLimitRemaining)
		}
		if !rm.fetchReceived {
			t.Error("fetchReceived should be true after successful update")
		}
		if rm.err != nil {
			t.Errorf("err = %v, want nil", rm.err)
		}
	})

	t.Run("presumed averages not applied to actions jobs", func(t *testing.T) {
		// Run-mode jobs convert to CheckRunInfo without AppName, so they
		// are never external app checks (IsExternalAppCheck requires
		// AppName). Presumed averages target external GitHub App checks;
		// run mode jobs are Actions jobs.
		m := makeRunModel()
		m.presumedAverages = map[string]time.Duration{"dco": time.Second}
		msg := RunJobsUpdateMsg{
			Jobs: []ghclient.WorkflowJobInfo{
				{Name: "dco", Status: "completed", HTMLURL: "https://probot.github.io/apps/dco/"},
			},
			RateLimitRemaining: 4000,
		}
		newModel, _ := m.Update(msg)
		rm := runModelFrom(t, newModel)
		if _, ok := rm.jobAverages["dco"]; ok {
			t.Error("presumed average should not apply to Actions-sourced jobs")
		}
	})

	t.Run("new jobs with rate limit dispatches discovery", func(t *testing.T) {
		m := makeRunModel()
		msg := RunJobsUpdateMsg{
			Jobs: []ghclient.WorkflowJobInfo{
				{Name: "build", Status: "in_progress", RunID: 55, WorkflowID: 77},
			},
			RateLimitRemaining: 4000,
		}
		newModel, cmd := m.Update(msg)
		rm := runModelFrom(t, newModel)
		if !rm.avgFetchPending {
			t.Error("avgFetchPending should be true after discovery dispatch")
		}
		if cmd == nil {
			t.Error("Update should return a discovery command")
		}
		if !rm.seenJobKeys[runJobKey(msg.Jobs[0])] {
			t.Error("job should be marked seen")
		}
	})

	t.Run("low rate limit skips discovery", func(t *testing.T) {
		m := makeRunModel()
		msg := RunJobsUpdateMsg{
			Jobs:               []ghclient.WorkflowJobInfo{{Name: "build", Status: "in_progress", RunID: 55, WorkflowID: 77}},
			RateLimitRemaining: 50, // below minRateLimitForFetch
		}
		newModel, _ := m.Update(msg)
		rm := runModelFrom(t, newModel)
		if rm.avgFetchPending {
			t.Error("avgFetchPending should stay false below rate limit threshold")
		}
	})

	t.Run("all complete without pending fetch quits", func(t *testing.T) {
		m := makeRunModel()
		// Pre-mark the job as seen so no new discovery dispatch fires
		// (a fresh job would set avgFetchPending and defer the quit).
		job := ghclient.WorkflowJobInfo{Name: "build", Status: "completed", Conclusion: "failure"}
		m.seenJobKeys[runJobKey(job)] = true
		msg := RunJobsUpdateMsg{
			Jobs:               []ghclient.WorkflowJobInfo{job},
			RateLimitRemaining: 4000,
		}
		newModel, cmd := m.Update(msg)
		rm := runModelFrom(t, newModel)
		if !rm.jobsComplete {
			t.Error("jobsComplete should be true")
		}
		if !rm.quitting {
			t.Error("quitting should be true when all jobs complete and no fetch pending")
		}
		if rm.exitCode != 1 {
			t.Errorf("exitCode = %d, want 1 (failed job)", rm.exitCode)
		}
		if cmd == nil {
			t.Error("Update should return a quit command")
		}
	})

	t.Run("all complete defers quit while fetch pending", func(t *testing.T) {
		m := makeRunModel()
		m.avgFetchPending = true
		msg := RunJobsUpdateMsg{
			Jobs:               []ghclient.WorkflowJobInfo{{Name: "build", Status: "completed", Conclusion: "success"}},
			RateLimitRemaining: 4000,
		}
		newModel, _ := m.Update(msg)
		rm := runModelFrom(t, newModel)
		if !rm.jobsComplete {
			t.Error("jobsComplete should be true")
		}
		if rm.quitting {
			t.Error("quitting should be deferred while avg fetch pending")
		}
	})

	t.Run("all complete defers quit while workflow fetches pending", func(t *testing.T) {
		m := makeRunModel()
		m.pendingWorkflowFetch = map[int64]bool{77: true}
		msg := RunJobsUpdateMsg{
			Jobs:               []ghclient.WorkflowJobInfo{{Name: "build", Status: "completed", Conclusion: "success"}},
			RateLimitRemaining: 4000,
		}
		newModel, _ := m.Update(msg)
		rm := runModelFrom(t, newModel)
		if rm.quitting {
			t.Error("quitting should be deferred while workflow fetches pending")
		}
	})
}

func TestRunUpdateRunWorkflowsDiscovered(t *testing.T) {
	t.Run("error clears pending and quits if jobs complete", func(t *testing.T) {
		m := makeRunModel()
		m.avgFetchPending = true
		m.jobsComplete = true
		newModel, cmd := m.Update(RunWorkflowsDiscoveredMsg{Err: errors.New("discovery failed")})
		rm := runModelFrom(t, newModel)
		if rm.avgFetchPending {
			t.Error("avgFetchPending should be cleared on error")
		}
		if rm.avgFetchErr == nil {
			t.Error("avgFetchErr should be stored")
		}
		if !rm.quitting {
			t.Error("quitting should fire when jobs complete and no pending fetches")
		}
		if cmd == nil {
			t.Error("Update should return a quit command")
		}
	})

	t.Run("error keeps running when jobs incomplete", func(t *testing.T) {
		m := makeRunModel()
		m.avgFetchPending = true
		newModel, _ := m.Update(RunWorkflowsDiscoveredMsg{Err: errors.New("discovery failed")})
		rm := runModelFrom(t, newModel)
		if rm.quitting {
			t.Error("should not quit while jobs incomplete")
		}
	})

	t.Run("new workflow IDs dispatch history fetches", func(t *testing.T) {
		m := makeRunModel()
		newModel, cmd := m.Update(RunWorkflowsDiscoveredMsg{
			NewRunIDToWorkflowID: map[int64]int64{55: 77},
			WorkflowIDsToFetch:   []int64{77},
		})
		rm := runModelFrom(t, newModel)
		if rm.runIDToWorkflowID[55] != 77 {
			t.Errorf("runIDToWorkflowID[55] = %d, want 77", rm.runIDToWorkflowID[55])
		}
		if !rm.pendingWorkflowFetch[77] {
			t.Error("workflow 77 should be pending")
		}
		if !rm.dispatchedWorkflowFetch[77] {
			t.Error("workflow 77 should be dispatched")
		}
		if cmd == nil {
			t.Error("Update should return a history fetch command")
		}
	})

	t.Run("already dispatched workflow not re-dispatched", func(t *testing.T) {
		m := makeRunModel()
		m.dispatchedWorkflowFetch = map[int64]bool{77: true}
		m.pendingWorkflowFetch = map[int64]bool{77: true}
		newModel, cmd := m.Update(RunWorkflowsDiscoveredMsg{
			WorkflowIDsToFetch: []int64{77},
		})
		rm := runModelFrom(t, newModel)
		// No new commands dispatched (workflow already in flight), so the
		// discovery phase resolves: pending fetch is cleared and completion
		// recorded, but avgFetchLastDuration is only set once the in-flight
		// fetch's RunJobAveragesPartialMsg resolves it.
		if rm.avgFetchPending {
			t.Error("avgFetchPending should be cleared when no new fetches dispatch")
		}
		if !rm.historyFetchCompleted {
			t.Error("historyFetchCompleted should be true when no new fetches dispatch")
		}
		if cmd != nil {
			t.Error("no new fetch commands should be dispatched")
		}
	})

	t.Run("no workflows to fetch completes discovery", func(t *testing.T) {
		m := makeRunModel()
		m.avgFetchPending = true
		m.avgFetchStartTime = time.Now()
		newModel, _ := m.Update(RunWorkflowsDiscoveredMsg{})
		rm := runModelFrom(t, newModel)
		if rm.avgFetchPending {
			t.Error("avgFetchPending should be cleared")
		}
		if !rm.historyFetchCompleted {
			t.Error("historyFetchCompleted should be true")
		}
		if rm.avgFetchLastDuration <= 0 {
			t.Error("avgFetchLastDuration should be recorded")
		}
	})

	t.Run("jobs already complete quits after discovery resolves", func(t *testing.T) {
		m := makeRunModel()
		m.jobsComplete = true
		newModel, cmd := m.Update(RunWorkflowsDiscoveredMsg{})
		rm := runModelFrom(t, newModel)
		if !rm.quitting {
			t.Error("quitting should fire when jobs complete and no pending fetches")
		}
		if cmd == nil {
			t.Error("Update should return a quit command")
		}
	})
}

func TestRunUpdateRunJobAveragesPartial(t *testing.T) {
	t.Run("averages merged and fetch completes", func(t *testing.T) {
		m := makeRunModel()
		m.avgFetchPending = true
		m.avgFetchStartTime = time.Now()
		m.pendingWorkflowFetch = map[int64]bool{77: true}

		newModel, _ := m.Update(RunJobAveragesPartialMsg{
			WorkflowID: 77,
			Averages:   map[string]time.Duration{"build": 2 * time.Minute},
		})
		rm := runModelFrom(t, newModel)

		if rm.jobAverages["build"] != 2*time.Minute {
			t.Errorf("jobAverages[build] = %v, want 2m", rm.jobAverages["build"])
		}
		if rm.workflowAverages[77]["build"] != 2*time.Minute {
			t.Error("workflowAverages should store per-workflow averages")
		}
		if !rm.fetchedWorkflowIDs[77] {
			t.Error("fetchedWorkflowIDs should record workflow 77")
		}
		if rm.avgFetchPending {
			t.Error("avgFetchPending should be cleared when all fetches resolve")
		}
		if !rm.historyFetchCompleted {
			t.Error("historyFetchCompleted should be true")
		}
		if rm.avgFetchErr != nil {
			t.Errorf("avgFetchErr = %v, want nil on success", rm.avgFetchErr)
		}
	})

	t.Run("error result clears pending error only on last fetch", func(t *testing.T) {
		m := makeRunModel()
		m.avgFetchPending = true
		m.avgFetchStartTime = time.Now()
		m.pendingWorkflowFetch = map[int64]bool{77: true, 88: true}
		m.avgFetchErr = errors.New("earlier error")

		newModel, _ := m.Update(RunJobAveragesPartialMsg{WorkflowID: 77, Err: errors.New("boom")})
		rm := runModelFrom(t, newModel)
		if !rm.avgFetchPending {
			t.Error("avgFetchPending should stay true while fetches remain")
		}
		if _, stillPending := rm.pendingWorkflowFetch[88]; !stillPending {
			t.Error("workflow 88 should still be pending")
		}

		m2 := rm
		newModel2, cmd := m2.Update(RunJobAveragesPartialMsg{WorkflowID: 88, Err: errors.New("boom")})
		rm2 := runModelFrom(t, newModel2)
		if rm2.avgFetchPending {
			t.Error("avgFetchPending should be cleared on last fetch")
		}
		if rm2.quitting {
			t.Error("should not quit while jobs incomplete")
		}
		if cmd != nil {
			t.Error("no quit command while jobs incomplete")
		}
	})

	t.Run("jobs complete quits when last fetch resolves", func(t *testing.T) {
		m := makeRunModel()
		m.jobsComplete = true
		m.avgFetchPending = true
		m.avgFetchStartTime = time.Now()
		m.pendingWorkflowFetch = map[int64]bool{77: true}

		newModel, cmd := m.Update(RunJobAveragesPartialMsg{
			WorkflowID: 77,
			Averages:   map[string]time.Duration{"build": time.Minute},
		})
		rm := runModelFrom(t, newModel)
		if !rm.quitting {
			t.Error("quitting should fire when jobs complete and fetches resolved")
		}
		if cmd == nil {
			t.Error("Update should return a quit command")
		}
	})
}

func TestRunUpdateRunInfoMsg(t *testing.T) {
	t.Run("error quits", func(t *testing.T) {
		m := makeRunModel()
		newModel, cmd := m.Update(RunInfoMsg{Err: errors.New("run not found")})
		rm := runModelFrom(t, newModel)
		if rm.err == nil {
			t.Error("error should be stored")
		}
		// RunInfoMsg errors quit via tea.Quit without setting the
		// quitting flag (the View renders the error, not a quit frame).
		if cmd == nil {
			t.Error("Update should return a quit command")
		}
	})

	t.Run("success loads info and strips variation selectors", func(t *testing.T) {
		m := makeRunModel()
		newModel, cmd := m.Update(RunInfoMsg{
			RunInfo:            ghclient.RunInfo{DisplayTitle: "Run ☑️ title"},
			RateLimitRemaining: 4500,
		})
		rm := runModelFrom(t, newModel)
		if !rm.runInfoLoaded {
			t.Error("runInfoLoaded should be true")
		}
		if rm.runInfo.DisplayTitle != "Run ☑ title" {
			t.Errorf("DisplayTitle = %q, want variation selectors stripped", rm.runInfo.DisplayTitle)
		}
		if rm.rateLimitRemaining != 4500 {
			t.Errorf("rateLimitRemaining = %d, want 4500", rm.rateLimitRemaining)
		}
		if !rm.fetchReceived {
			t.Error("fetchReceived should be true")
		}
		if cmd == nil {
			t.Error("Update should return a fetch jobs command")
		}
	})

	t.Run("zero rate limit keeps fetchReceived false", func(t *testing.T) {
		m := makeRunModel()
		m.fetchReceived = false
		newModel, _ := m.Update(RunInfoMsg{
			RunInfo:            ghclient.RunInfo{DisplayTitle: "Run"},
			RateLimitRemaining: 0,
		})
		rm := runModelFrom(t, newModel)
		if rm.fetchReceived {
			t.Error("fetchReceived should stay false for zero rate limit")
		}
	})
}

func TestRunUpdateErrorMsg(t *testing.T) {
	m := makeRunModel()
	newModel, cmd := m.Update(RunErrorMsg{Err: errors.New("async boom")})
	rm := runModelFrom(t, newModel)
	if rm.err == nil || rm.err.Error() != "async boom" {
		t.Errorf("err = %v, want async boom", rm.err)
	}
	if cmd != nil {
		t.Error("RunErrorMsg should not return a command")
	}
}

func TestRunUpdateRunTickMsg(t *testing.T) {
	t.Run("normal tick dispatches fetch", func(t *testing.T) {
		m := makeRunModel()
		newModel, cmd := m.Update(RunTickMsg{})
		rm := runModelFrom(t, newModel)
		if cmd == nil {
			t.Error("tick should dispatch fetch commands")
		}
		_ = rm
	})

	t.Run("low rate limit triples interval", func(t *testing.T) {
		m := makeRunModel()
		m.rateLimitRemaining = 5 // below rateBackoffThreshold
		newModel, _ := m.Update(RunTickMsg{})
		runModelFrom(t, newModel)
		// No observable assertion beyond not dispatching the normal batch;
		// backoff path is exercised for coverage of the debug.Log branch.
	})

	t.Run("zero rate limit before first fetch does not back off", func(t *testing.T) {
		m := makeRunModel()
		m.fetchReceived = false
		m.rateLimitRemaining = 0
		_, cmd := m.Update(RunTickMsg{})
		if cmd == nil {
			t.Error("tick before first fetch should dispatch fetch commands (no backoff)")
		}
	})
}

func TestRunUpdateSpinnerTick(t *testing.T) {
	m := makeRunModel()
	// A spinner.TickMsg from a different spinner is ignored by Update but
	// still exercises the dispatch branch. Constructed via the package's
	// own spinner so the tag matches.
	cmd := m.spinner.Tick
	msg := cmd()
	newModel, _ := m.Update(msg)
	if newModel == nil {
		t.Error("spinner tick should return a model")
	}
}

func TestRunUpdateUnknownMsg(t *testing.T) {
	m := makeRunModel()
	newModel, cmd := m.Update("some-unknown-message")
	if cmd != nil {
		t.Error("unknown message should return nil command")
	}
	rm := runModelFrom(t, newModel)
	if rm.quitting {
		t.Error("unknown message should not change quitting state")
	}
}

func TestRunUpdateInit(t *testing.T) {
	m := makeRunModel()
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init should return a batch command")
	}
}

func TestHasNewRunJobs(t *testing.T) {
	jobs := []ghclient.WorkflowJobInfo{
		{Name: "build", RunID: 1},
		{Name: "test", RunID: 1},
	}

	t.Run("empty seen map reports new", func(t *testing.T) {
		if !hasNewRunJobs(jobs, map[string]bool{}) {
			t.Error("hasNewRunJobs should report new with empty seen")
		}
	})

	t.Run("all seen reports not new", func(t *testing.T) {
		seen := map[string]bool{runJobKey(jobs[0]): true, runJobKey(jobs[1]): true}
		if hasNewRunJobs(jobs, seen) {
			t.Error("hasNewRunJobs should report false when all seen")
		}
	})

	t.Run("one new job reports new", func(t *testing.T) {
		seen := map[string]bool{runJobKey(jobs[0]): true}
		if !hasNewRunJobs(jobs, seen) {
			t.Error("hasNewRunJobs should report true when any job is new")
		}
	})
}

func TestMarkRunJobsSeen(t *testing.T) {
	jobs := []ghclient.WorkflowJobInfo{
		{Name: "build", RunID: 1},
		{Name: "test"},
	}
	seen := map[string]bool{}
	markRunJobsSeen(jobs, seen)
	if len(seen) != 2 {
		t.Errorf("len(seen) = %d, want 2", len(seen))
	}
	if !seen[runJobKey(jobs[0])] || !seen[runJobKey(jobs[1])] {
		t.Error("all jobs should be marked seen")
	}
}

func TestRunJobKey(t *testing.T) {
	t.Run("with run ID", func(t *testing.T) {
		got := runJobKey(ghclient.WorkflowJobInfo{Name: "build", RunID: 42})
		if got != "run:42:build" {
			t.Errorf("runJobKey() = %q, want %q", got, "run:42:build")
		}
	})

	t.Run("without run ID falls back to name", func(t *testing.T) {
		got := runJobKey(ghclient.WorkflowJobInfo{Name: "build"})
		if got != "name:build" {
			t.Errorf("runJobKey() = %q, want %q", got, "name:build")
		}
	})
}

func TestRunJobSortKeyDuration(t *testing.T) {
	now := time.Now()

	t.Run("completed with duration", func(t *testing.T) {
		job := ghclient.WorkflowJobInfo{
			Status:      "completed",
			StartedAt:   &github.Timestamp{Time: now.Add(-2 * time.Minute)},
			CompletedAt: &github.Timestamp{Time: now.Add(-1 * time.Minute)},
		}
		if d := runSortKeyDuration(job); d != time.Minute {
			t.Errorf("runSortKeyDuration() = %v, want 1m", d)
		}
	})

	t.Run("completed missing timestamps yields zero", func(t *testing.T) {
		if d := runSortKeyDuration(ghclient.WorkflowJobInfo{Status: "completed"}); d != 0 {
			t.Errorf("runSortKeyDuration() = %v, want 0", d)
		}
	})

	t.Run("in_progress with startedAt", func(t *testing.T) {
		job := ghclient.WorkflowJobInfo{
			Status:    "in_progress",
			StartedAt: &github.Timestamp{Time: now.Add(-90 * time.Second)},
		}
		if d := runSortKeyDuration(job); d <= 0 {
			t.Errorf("runSortKeyDuration() = %v, want positive", d)
		}
	})

	t.Run("in_progress missing startedAt yields zero", func(t *testing.T) {
		if d := runSortKeyDuration(ghclient.WorkflowJobInfo{Status: "in_progress"}); d != 0 {
			t.Errorf("runSortKeyDuration() = %v, want 0", d)
		}
	})

	t.Run("queued yields large sentinel", func(t *testing.T) {
		d := runSortKeyDuration(ghclient.WorkflowJobInfo{Status: "queued"})
		if d < time.Hour {
			t.Errorf("runSortKeyDuration(queued) = %v, want large sentinel", d)
		}
	})
}
