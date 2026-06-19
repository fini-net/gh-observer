package tui

import (
	"context"
	"testing"
	"time"

	ghclient "github.com/fini-net/gh-observer/internal/github"
)

func makeModel() *Model {
	return &Model{
		ctx:                     context.Background(),
		token:                   "test-token",
		owner:                   "test-owner",
		repo:                    "test-repo",
		rateLimitRemaining:      5000,
		jobAverages:             make(map[string]time.Duration),
		workflowAverages:        make(map[int64]map[string]time.Duration),
		advSecMatchWorkflow:    make(map[string]int64),
		runIDToWorkflowID:       make(map[int64]int64),
		fetchedWorkflowIDs:      make(map[int64]bool),
		pendingWorkflowFetch:    make(map[int64]bool),
		dispatchedWorkflowFetch: make(map[int64]bool),
		seenCheckKeys:          make(map[string]bool),
	}
}

//nolint:unused // test helper for pointer time values
//go:fix inline
func ptrTime(t time.Time) *time.Time {
	return &t
}

func TestAllChecksComplete(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name   string
		checks []ghclient.CheckRunInfo
		want   bool
	}{
		{
			name:   "empty list returns false",
			checks: []ghclient.CheckRunInfo{},
			want:   false,
		},
		{
			name: "all completed returns true",
			checks: []ghclient.CheckRunInfo{
				{Status: "completed", Conclusion: "success"},
				{Status: "completed", Conclusion: "failure"},
				{Status: "completed", Conclusion: "skipped"},
			},
			want: true,
		},
		{
			name: "one in_progress returns false",
			checks: []ghclient.CheckRunInfo{
				{Status: "completed", Conclusion: "success"},
				{Status: "in_progress", StartedAt: &now},
			},
			want: false,
		},
		{
			name: "one queued returns false",
			checks: []ghclient.CheckRunInfo{
				{Status: "completed", Conclusion: "success"},
				{Status: "queued"},
			},
			want: false,
		},
		{
			name: "single completed returns true",
			checks: []ghclient.CheckRunInfo{
				{Status: "completed", Conclusion: "success"},
			},
			want: true,
		},
		{
			name: "single in_progress returns false",
			checks: []ghclient.CheckRunInfo{
				{Status: "in_progress", StartedAt: &now},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := allChecksComplete(tt.checks)
			if got != tt.want {
				t.Errorf("allChecksComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetermineExitCode(t *testing.T) {
	tests := []struct {
		name   string
		checks []ghclient.CheckRunInfo
		want   int
	}{
		{
			name:   "empty list returns 0",
			checks: []ghclient.CheckRunInfo{},
			want:   0,
		},
		{
			name: "all success returns 0",
			checks: []ghclient.CheckRunInfo{
				{Status: "completed", Conclusion: "success"},
				{Status: "completed", Conclusion: "success"},
			},
			want: 0,
		},
		{
			name: "one failure returns 1",
			checks: []ghclient.CheckRunInfo{
				{Status: "completed", Conclusion: "success"},
				{Status: "completed", Conclusion: "failure"},
			},
			want: 1,
		},
		{
			name: "timed_out returns 1",
			checks: []ghclient.CheckRunInfo{
				{Status: "completed", Conclusion: "timed_out"},
			},
			want: 1,
		},
		{
			name: "action_required returns 1",
			checks: []ghclient.CheckRunInfo{
				{Status: "completed", Conclusion: "action_required"},
			},
			want: 1,
		},
		{
			name: "cancelled returns 0",
			checks: []ghclient.CheckRunInfo{
				{Status: "completed", Conclusion: "cancelled"},
			},
			want: 0,
		},
		{
			name: "skipped returns 0",
			checks: []ghclient.CheckRunInfo{
				{Status: "completed", Conclusion: "skipped"},
			},
			want: 0,
		},
		{
			name: "in_progress does not affect exit code",
			checks: []ghclient.CheckRunInfo{
				{Status: "in_progress"},
				{Status: "completed", Conclusion: "success"},
			},
			want: 0,
		},
		{
			name: "multiple failures returns 1",
			checks: []ghclient.CheckRunInfo{
				{Status: "completed", Conclusion: "failure"},
				{Status: "completed", Conclusion: "failure"},
				{Status: "completed", Conclusion: "success"},
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineExitCode(tt.checks)
			if got != tt.want {
				t.Errorf("determineExitCode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandleChecksUpdate(t *testing.T) {
	tests := []struct {
		name               string
		msg                ChecksUpdateMsg
		rateLimit          int
		expectedCheckCount int
		wantErr            bool
		wantExitCode       int
		wantQuitting       bool
		wantChecksStored   bool
	}{
		{
			name:               "error in message stores error and returns nil cmd",
			msg:                ChecksUpdateMsg{Err: context.Canceled},
			wantErr:            true,
			wantExitCode:       0,
			wantQuitting:       false,
			wantChecksStored:   false,
		},
		{
			name:               "successful update stores check runs",
			msg:                ChecksUpdateMsg{CheckRuns: []ghclient.CheckRunInfo{{Status: "in_progress", Conclusion: ""}}, RateLimitRemaining: 5000},
			rateLimit:          5000,
			wantErr:            false,
			wantChecksStored:   true,
		},
		{
			name:               "all checks complete sets exit code 0 on success",
			msg:                ChecksUpdateMsg{CheckRuns: []ghclient.CheckRunInfo{{Status: "completed", Conclusion: "success"}}, RateLimitRemaining: 5000},
			rateLimit:          5000,
			expectedCheckCount: 1,
			wantErr:            false,
			wantExitCode:       0,
			wantQuitting:       true,
			wantChecksStored:   true,
		},
		{
			name:               "all checks complete sets exit code 1 on failure",
			msg:                ChecksUpdateMsg{CheckRuns: []ghclient.CheckRunInfo{{Status: "completed", Conclusion: "failure"}}, RateLimitRemaining: 5000},
			rateLimit:          5000,
			expectedCheckCount: 1,
			wantErr:            false,
			wantExitCode:       1,
			wantQuitting:       true,
			wantChecksStored:   true,
		},
		{
			name:             "in_progress checks do not quit",
			msg:              ChecksUpdateMsg{CheckRuns: []ghclient.CheckRunInfo{{Status: "in_progress"}}, RateLimitRemaining: 5000},
			rateLimit:        5000,
			wantErr:          false,
			wantQuitting:     false,
			wantChecksStored: true,
		},
		{
			name:               "premature exit blocked when expected count not met",
			msg:                ChecksUpdateMsg{CheckRuns: []ghclient.CheckRunInfo{{Status: "completed", Conclusion: "success"}}, RateLimitRemaining: 5000},
			rateLimit:          5000,
			expectedCheckCount: 10,
			wantErr:            false,
			wantExitCode:       0,
			wantQuitting:       false,
			wantChecksStored:   true,
		},
		{
			name:               "no expected count blocks premature exit",
			msg:                ChecksUpdateMsg{CheckRuns: []ghclient.CheckRunInfo{{Status: "completed", Conclusion: "success"}}, RateLimitRemaining: 5000},
			rateLimit:          5000,
			expectedCheckCount: 0,
			wantErr:            false,
			wantExitCode:       0,
			wantQuitting:       false,
			wantChecksStored:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := makeModel()
			m.rateLimitRemaining = tt.rateLimit
			m.expectedCheckCount = tt.expectedCheckCount
			m.firstCheckSeenAt = time.Now().Add(-15 * time.Second)

			model, _ := m.handleChecksUpdate(tt.msg)
			result := model.(*Model)

			if tt.wantErr && result.err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && result.err != nil {
				t.Errorf("unexpected error: %v", result.err)
			}

			if tt.wantChecksStored && len(result.checkRuns) != len(tt.msg.CheckRuns) {
				t.Errorf("checkRuns not stored, got %d, want %d", len(result.checkRuns), len(tt.msg.CheckRuns))
			}

			if tt.wantExitCode != result.exitCode {
				t.Errorf("exitCode = %d, want %d", result.exitCode, tt.wantExitCode)
			}

			if tt.wantQuitting != result.quitting {
				t.Errorf("quitting = %v, want %v", result.quitting, tt.wantQuitting)
			}
		})
	}
}

func TestFirstCheckSeenAt(t *testing.T) {
	t.Run("sets firstCheckSeenAt on first check run", func(t *testing.T) {
		m := makeModel()
		m.rateLimitRemaining = 5000
		if !m.firstCheckSeenAt.IsZero() {
			t.Error("firstCheckSeenAt should start as zero")
		}

		msg := ChecksUpdateMsg{
			CheckRuns:          []ghclient.CheckRunInfo{{Status: "in_progress"}},
			RateLimitRemaining: 5000,
		}

		model, _ := m.handleChecksUpdate(msg)
		result := model.(*Model)

		if result.firstCheckSeenAt.IsZero() {
			t.Error("firstCheckSeenAt should be set after first check run")
		}
	})

	t.Run("does not overwrite firstCheckSeenAt on subsequent updates", func(t *testing.T) {
		m := makeModel()
		m.rateLimitRemaining = 5000
		originalTime := time.Now().Add(-5 * time.Second)
		m.firstCheckSeenAt = originalTime

		msg := ChecksUpdateMsg{
			CheckRuns:          []ghclient.CheckRunInfo{{Status: "in_progress"}},
			RateLimitRemaining: 5000,
		}

		model, _ := m.handleChecksUpdate(msg)
		result := model.(*Model)

		if !result.firstCheckSeenAt.Equal(originalTime) {
			t.Error("firstCheckSeenAt should not be overwritten")
		}
	})

	t.Run("empty check runs do not set firstCheckSeenAt", func(t *testing.T) {
		m := makeModel()
		m.rateLimitRemaining = 5000

		msg := ChecksUpdateMsg{
			CheckRuns:          []ghclient.CheckRunInfo{},
			RateLimitRemaining: 5000,
		}

		model, _ := m.handleChecksUpdate(msg)
		result := model.(*Model)

		if !result.firstCheckSeenAt.IsZero() {
			t.Error("firstCheckSeenAt should remain zero for empty check runs")
		}
	})
}

func TestHistoryFetchDelay(t *testing.T) {
	t.Run("history fetch blocked when hold-off not elapsed", func(t *testing.T) {
		m := makeModel()
		m.rateLimitRemaining = 5000
		m.firstCheckSeenAt = time.Now().Add(-5 * time.Second)
		m.runIDToWorkflowID = make(map[int64]int64)

		msg := ChecksUpdateMsg{
			CheckRuns: []ghclient.CheckRunInfo{
				{Status: "in_progress", DetailsURL: "https://github.com/test/test/actions/runs/123/job/456"},
			},
			RateLimitRemaining: 5000,
		}

		model, _ := m.handleChecksUpdate(msg)
		result := model.(*Model)

		if result.avgFetchPending {
			t.Error("avgFetchPending should be false when hold-off not elapsed (5s < 10s)")
		}
	})

	t.Run("history fetch proceeds when hold-off elapsed", func(t *testing.T) {
		m := makeModel()
		m.rateLimitRemaining = 5000
		m.firstCheckSeenAt = time.Now().Add(-15 * time.Second)
		m.runIDToWorkflowID = make(map[int64]int64)

		msg := ChecksUpdateMsg{
			CheckRuns: []ghclient.CheckRunInfo{
				{Status: "in_progress", DetailsURL: "https://github.com/test/test/actions/runs/123/job/456"},
			},
			RateLimitRemaining: 5000,
		}

		model, _ := m.handleChecksUpdate(msg)
		result := model.(*Model)

		if !result.avgFetchPending {
			t.Error("avgFetchPending should be true when hold-off elapsed (15s > 10s)")
		}
	})

	t.Run("history fetch blocked on first update despite firstCheckSeenAt being set", func(t *testing.T) {
		m := makeModel()
		m.rateLimitRemaining = 5000
		m.runIDToWorkflowID = make(map[int64]int64)

		msg := ChecksUpdateMsg{
			CheckRuns: []ghclient.CheckRunInfo{
				{Status: "in_progress", DetailsURL: "https://github.com/test/test/actions/runs/123/job/456"},
			},
			RateLimitRemaining: 5000,
		}

		model, _ := m.handleChecksUpdate(msg)
		result := model.(*Model)

		if result.firstCheckSeenAt.IsZero() {
			t.Error("firstCheckSeenAt should be set on first check run update")
		}
		if result.avgFetchPending {
			t.Error("avgFetchPending should be false on first update (elapsed < delay)")
		}
	})

	t.Run("old PR with checks already present fetches after delay", func(t *testing.T) {
		m := makeModel()
		m.rateLimitRemaining = 5000
		m.firstCheckSeenAt = time.Now().Add(-11 * time.Second)
		m.runIDToWorkflowID = make(map[int64]int64)

		msg := ChecksUpdateMsg{
			CheckRuns: []ghclient.CheckRunInfo{
				{Status: "completed", Conclusion: "success", DetailsURL: "https://github.com/test/test/actions/runs/123/job/456"},
			},
			RateLimitRemaining: 5000,
		}

		model, _ := m.handleChecksUpdate(msg)
		result := model.(*Model)

		if !result.avgFetchPending {
			t.Error("avgFetchPending should be true for old PR after delay")
		}
	})

	t.Run("fetches immediately when all checks complete on first update", func(t *testing.T) {
		m := makeModel()
		m.rateLimitRemaining = 5000
		m.runIDToWorkflowID = make(map[int64]int64)

		msg := ChecksUpdateMsg{
			CheckRuns: []ghclient.CheckRunInfo{
				{Status: "completed", Conclusion: "success", DetailsURL: "https://github.com/test/test/actions/runs/123/job/456"},
			},
			RateLimitRemaining: 5000,
		}

		model, _ := m.handleChecksUpdate(msg)
		result := model.(*Model)

		if result.firstCheckSeenAt.IsZero() {
			t.Error("firstCheckSeenAt should be set")
		}
		if !result.avgFetchPending {
			t.Error("avgFetchPending should be true when all checks complete on first update")
		}
	})

	t.Run("fetches immediately on first update if all checks already complete", func(t *testing.T) {
		m := makeModel()
		m.rateLimitRemaining = 5000
		m.runIDToWorkflowID = make(map[int64]int64)

		msg := ChecksUpdateMsg{
			CheckRuns: []ghclient.CheckRunInfo{
				{Status: "completed", Conclusion: "success", DetailsURL: "https://github.com/test/test/actions/runs/123/job/456"},
				{Status: "completed", Conclusion: "success", DetailsURL: "https://github.com/test/test/actions/runs/124/job/789"},
			},
			RateLimitRemaining: 5000,
		}

		model, _ := m.handleChecksUpdate(msg)
		result := model.(*Model)

		if !result.avgFetchPending {
			t.Error("avgFetchPending should be true - should fetch immediately for already-complete checks")
		}
	})
}

func TestWorkflowsDiscoveredMsg(t *testing.T) {
	t.Run("error sets avgFetchErr and clears pending", func(t *testing.T) {
		m := makeModel()
		m.avgFetchPending = true
		m.avgFetchStartTime = time.Now().Add(-1 * time.Second)

		msg := WorkflowsDiscoveredMsg{Err: context.Canceled}
		model, _ := m.Update(msg)
		result := model.(Model)

		if result.avgFetchPending {
			t.Error("avgFetchPending should be false after error")
		}
		if result.avgFetchErr == nil {
			t.Error("avgFetchErr should be set")
		}
	})

	t.Run("successful discovery tracks pending workflows", func(t *testing.T) {
		m := makeModel()
		m.avgFetchPending = true
		m.avgFetchStartTime = time.Now().Add(-1 * time.Second)
		m.pendingWorkflowFetch = make(map[int64]bool)
		m.dispatchedWorkflowFetch = make(map[int64]bool)

		msg := WorkflowsDiscoveredMsg{
			NewRunIDToWorkflowID: map[int64]int64{123: 456, 789: 456},
			WorkflowIDsToFetch:   []int64{456},
		}
		model, _ := m.Update(msg)
		result := model.(Model)

		if result.runIDToWorkflowID[123] != 456 {
			t.Error("run ID to workflow ID mapping should be stored")
		}
		if !result.pendingWorkflowFetch[456] {
			t.Error("workflow ID should be in pending set")
		}
		if !result.dispatchedWorkflowFetch[456] {
			t.Error("workflow ID should be in dispatched set")
		}
	})

	t.Run("no workflows to fetch completes immediately", func(t *testing.T) {
		m := makeModel()
		m.avgFetchPending = true
		m.avgFetchStartTime = time.Now().Add(-1 * time.Second)
		m.pendingWorkflowFetch = make(map[int64]bool)
		m.dispatchedWorkflowFetch = make(map[int64]bool)

		msg := WorkflowsDiscoveredMsg{
			NewRunIDToWorkflowID: map[int64]int64{123: 456},
			WorkflowIDsToFetch:   []int64{},
		}
		model, _ := m.Update(msg)
		result := model.(Model)

		if result.avgFetchPending {
			t.Error("avgFetchPending should be false when no workflows to fetch")
		}
		if result.avgFetchLastDuration == 0 {
			t.Error("avgFetchLastDuration should be set")
		}
	})

	t.Run("already dispatched workflow is not redispatched", func(t *testing.T) {
		m := makeModel()
		m.avgFetchPending = true
		m.avgFetchStartTime = time.Now().Add(-1 * time.Second)
		m.pendingWorkflowFetch = make(map[int64]bool)
		m.dispatchedWorkflowFetch = map[int64]bool{456: true}

		msg := WorkflowsDiscoveredMsg{
			WorkflowIDsToFetch: []int64{456},
		}
		model, _ := m.Update(msg)
		result := model.(Model)

		if result.pendingWorkflowFetch[456] {
			t.Error("workflow should not be added to pending if already dispatched")
		}
	})

	t.Run("quits when checks complete and no pending fetches", func(t *testing.T) {
		m := makeModel()
		m.avgFetchPending = true
		m.avgFetchStartTime = time.Now()
		m.checksComplete = true
		m.pendingWorkflowFetch = make(map[int64]bool)
		m.dispatchedWorkflowFetch = make(map[int64]bool)

		msg := WorkflowsDiscoveredMsg{
			WorkflowIDsToFetch: []int64{},
		}
		model, _ := m.Update(msg)
		result := model.(Model)

		if !result.quitting {
			t.Error("should quit when checks complete and no pending fetches")
		}
	})
}

func TestJobAveragesPartialMsg(t *testing.T) {
	t.Run("merges averages and removes from pending", func(t *testing.T) {
		m := makeModel()
		m.pendingWorkflowFetch = map[int64]bool{456: true, 789: true}
		m.fetchedWorkflowIDs = make(map[int64]bool)
		m.jobAverages = make(map[string]time.Duration)
		m.avgFetchStartTime = time.Now().Add(-2 * time.Second)

		msg := JobAveragesPartialMsg{
			WorkflowID: 456,
			Averages: map[string]time.Duration{
				"build": time.Minute,
				"test":  2 * time.Minute,
			},
		}
		model, _ := m.Update(msg)
		result := model.(Model)

		if result.pendingWorkflowFetch[456] {
			t.Error("456 should be removed from pending")
		}
		if !result.fetchedWorkflowIDs[456] {
			t.Error("456 should be in fetched set")
		}
		if result.jobAverages["build"] != time.Minute {
			t.Error("build average should be merged")
		}
		if result.avgFetchPending {
			t.Error("avgFetchPending should be false while fetches pending")
		}
	})

	t.Run("sets avgFetchLastDuration when all fetches complete", func(t *testing.T) {
		m := makeModel()
		m.pendingWorkflowFetch = map[int64]bool{456: true}
		m.fetchedWorkflowIDs = make(map[int64]bool)
		m.jobAverages = make(map[string]time.Duration)
		m.avgFetchStartTime = time.Now().Add(-2 * time.Second)

		msg := JobAveragesPartialMsg{
			WorkflowID: 456,
			Averages: map[string]time.Duration{
				"build": time.Minute,
			},
		}
		model, _ := m.Update(msg)
		result := model.(Model)

		if result.avgFetchLastDuration == 0 {
			t.Error("avgFetchLastDuration should be set when all fetches complete")
		}
		if result.avgFetchPending {
			t.Error("avgFetchPending should be false when all fetches complete")
		}
	})

	t.Run("handles error and continues", func(t *testing.T) {
		m := makeModel()
		m.pendingWorkflowFetch = map[int64]bool{456: true}
		m.fetchedWorkflowIDs = make(map[int64]bool)
		m.avgFetchStartTime = time.Now().Add(-1 * time.Second)

		msg := JobAveragesPartialMsg{
			WorkflowID: 456,
			Err:        context.Canceled,
		}
		model, _ := m.Update(msg)
		result := model.(Model)

		if result.pendingWorkflowFetch[456] {
			t.Error("456 should be removed from pending even on error")
		}
		if !result.fetchedWorkflowIDs[456] {
			t.Error("456 should be in fetched set even on error")
		}
	})

	t.Run("quits when checks complete and fetches finish", func(t *testing.T) {
		m := makeModel()
		m.pendingWorkflowFetch = map[int64]bool{456: true}
		m.fetchedWorkflowIDs = make(map[int64]bool)
		m.jobAverages = make(map[string]time.Duration)
		m.avgFetchStartTime = time.Now().Add(-1 * time.Second)
		m.checksComplete = true

		msg := JobAveragesPartialMsg{
			WorkflowID: 456,
			Averages: map[string]time.Duration{
				"build": time.Minute,
			},
		}
		model, _ := m.Update(msg)
		result := model.(Model)

		if !result.quitting {
			t.Error("should quit when checks complete and fetches finish")
		}
	})

	t.Run("updates expectedCheckCount from averages", func(t *testing.T) {
		m := makeModel()
		m.pendingWorkflowFetch = map[int64]bool{456: true}
		m.fetchedWorkflowIDs = make(map[int64]bool)
		m.jobAverages = make(map[string]time.Duration)
		m.avgFetchStartTime = time.Now().Add(-1 * time.Second)

		msg := JobAveragesPartialMsg{
			WorkflowID: 456,
			Averages: map[string]time.Duration{
				"build": time.Minute,
				"test":  2 * time.Minute,
				"lint":  30 * time.Second,
			},
		}
		model, _ := m.Update(msg)
		result := model.(Model)

		if result.expectedCheckCount != 3 {
			t.Errorf("expectedCheckCount = %d, want 3", result.expectedCheckCount)
		}
	})

	t.Run("AdvSec matched workflow adds alias to jobAverages", func(t *testing.T) {
		m := makeModel()
		m.pendingWorkflowFetch = map[int64]bool{789: true}
		m.fetchedWorkflowIDs = make(map[int64]bool)
		m.jobAverages = make(map[string]time.Duration)
		m.workflowAverages = make(map[int64]map[string]time.Duration)
		m.advSecMatchWorkflow = map[string]int64{"CodeQL": 789}
		m.avgFetchStartTime = time.Now().Add(-1 * time.Second)

		msg := JobAveragesPartialMsg{
			WorkflowID: 789,
			Averages: map[string]time.Duration{
				"Analyze (go)": 2 * time.Minute,
			},
		}
		model, _ := m.Update(msg)
		result := model.(Model)

		if result.jobAverages["CodeQL"] != 2*time.Minute {
			t.Errorf("jobAverages[CodeQL] = %v, want %v", result.jobAverages["CodeQL"], 2*time.Minute)
		}
		if result.workflowAverages[789]["Analyze (go)"] != 2*time.Minute {
			t.Errorf("workflowAverages[789][Analyze (go)] = %v, want %v", result.workflowAverages[789]["Analyze (go)"], 2*time.Minute)
		}
	})
}

func TestCanTrustCompletion(t *testing.T) {
	now := time.Now()

	t.Run("returns false when firstCheckSeenAt is zero", func(t *testing.T) {
		m := makeModel()
		m.firstCheckSeenAt = time.Time{}
		if canTrustCompletion(m) {
			t.Error("should not trust completion when firstCheckSeenAt is zero")
		}
	})

	t.Run("returns true after grace period elapsed even with no expected count", func(t *testing.T) {
		m := makeModel()
		m.firstCheckSeenAt = now.Add(-3 * time.Minute)
		m.checkRuns = []ghclient.CheckRunInfo{{Status: "completed", Conclusion: "success"}}
		m.peakCheckCount = 1
		if !canTrustCompletion(m) {
			t.Error("should trust completion after grace period")
		}
	})

	t.Run("returns false when expected count not met within ratio", func(t *testing.T) {
		m := makeModel()
		m.firstCheckSeenAt = now.Add(-30 * time.Second)
		m.checkRuns = []ghclient.CheckRunInfo{{Status: "completed", Conclusion: "success"}}
		m.peakCheckCount = 1
		m.expectedCheckCount = 10
		if canTrustCompletion(m) {
			t.Error("should not trust completion when only 1/10 checks seen (10% < 30% threshold)")
		}
	})

	t.Run("returns true when appearance ratio met", func(t *testing.T) {
		m := makeModel()
		m.firstCheckSeenAt = now.Add(-30 * time.Second)
		checks := make([]ghclient.CheckRunInfo, 4)
		for i := range checks {
			checks[i] = ghclient.CheckRunInfo{Status: "completed", Conclusion: "success"}
		}
		m.checkRuns = checks
		m.peakCheckCount = 4
		m.expectedCheckCount = 10
		if !canTrustCompletion(m) {
			t.Error("should trust completion when 4/10 checks seen (40% >= 30% threshold)")
		}
	})

	t.Run("returns true when expected count equals check count", func(t *testing.T) {
		m := makeModel()
		m.firstCheckSeenAt = now.Add(-30 * time.Second)
		m.checkRuns = []ghclient.CheckRunInfo{
			{Status: "completed", Conclusion: "success"},
			{Status: "completed", Conclusion: "success"},
		}
		m.peakCheckCount = 2
		m.expectedCheckCount = 2
		if !canTrustCompletion(m) {
			t.Error("should trust completion when all expected checks present")
		}
	})

	t.Run("returns false when peak count exceeds current count", func(t *testing.T) {
		m := makeModel()
		m.firstCheckSeenAt = now.Add(-30 * time.Second)
		m.checkRuns = []ghclient.CheckRunInfo{{Status: "completed", Conclusion: "success"}}
		m.peakCheckCount = 5
		m.expectedCheckCount = 10
		if canTrustCompletion(m) {
			t.Error("should not trust completion when checks disappeared (peak 5 > current 1)")
		}
	})

	t.Run("returns false with no expected count before grace period", func(t *testing.T) {
		m := makeModel()
		m.firstCheckSeenAt = now.Add(-30 * time.Second)
		m.checkRuns = []ghclient.CheckRunInfo{{Status: "completed", Conclusion: "success"}}
		m.peakCheckCount = 1
		m.expectedCheckCount = 0
		if canTrustCompletion(m) {
			t.Error("should not trust completion with no expected count before grace period")
		}
	})

	t.Run("quick mode returns true when peak matches current count", func(t *testing.T) {
		m := makeModel()
		m.noAvg = true
		m.firstCheckSeenAt = now.Add(-5 * time.Second)
		m.checkRuns = []ghclient.CheckRunInfo{{Status: "completed", Conclusion: "success"}}
		m.peakCheckCount = 1
		m.expectedCheckCount = 0
		if !canTrustCompletion(m) {
			t.Error("should trust completion in quick mode when peak matches current count")
		}
	})

	t.Run("quick mode returns false when peak exceeds current count", func(t *testing.T) {
		m := makeModel()
		m.noAvg = true
		m.firstCheckSeenAt = now.Add(-5 * time.Second)
		m.checkRuns = []ghclient.CheckRunInfo{{Status: "completed", Conclusion: "success"}}
		m.peakCheckCount = 3
		if canTrustCompletion(m) {
			t.Error("should not trust completion in quick mode when checks disappeared")
		}
	})

	t.Run("quick mode returns false when firstCheckSeenAt is zero", func(t *testing.T) {
		m := makeModel()
		m.noAvg = true
		m.firstCheckSeenAt = time.Time{}
		if canTrustCompletion(m) {
			t.Error("should not trust completion in quick mode when firstCheckSeenAt is zero")
		}
	})

	t.Run("quick mode ignores expectedCheckCount and grace period", func(t *testing.T) {
		m := makeModel()
		m.noAvg = true
		m.firstCheckSeenAt = now.Add(-5 * time.Second)
		m.checkRuns = []ghclient.CheckRunInfo{
			{Status: "completed", Conclusion: "success"},
			{Status: "completed", Conclusion: "success"},
		}
		m.peakCheckCount = 2
		m.expectedCheckCount = 10
		if !canTrustCompletion(m) {
			t.Error("should trust completion in quick mode regardless of expected count")
		}
	})
}

func TestPeakCheckCountTracking(t *testing.T) {
	t.Run("trackCheckCount updates peakCheckCount", func(t *testing.T) {
		m := makeModel()
		m.rateLimitRemaining = 5000
		m.expectedCheckCount = 3
		m.firstCheckSeenAt = time.Now().Add(-15 * time.Second)

		msg := ChecksUpdateMsg{
			CheckRuns: []ghclient.CheckRunInfo{
				{Status: "in_progress"},
				{Status: "in_progress"},
				{Status: "in_progress"},
			},
			RateLimitRemaining: 5000,
		}

		model, _ := m.handleChecksUpdate(msg)
		result := model.(*Model)

		if result.peakCheckCount != 3 {
			t.Errorf("peakCheckCount = %d, want 3", result.peakCheckCount)
		}
	})
}

func TestRediscoveryOnNewJobs(t *testing.T) {
	t.Run("new check runs trigger re-discovery after initial discovery completed", func(t *testing.T) {
		m := makeModel()
		m.rateLimitRemaining = 5000
		m.firstCheckSeenAt = time.Now().Add(-15 * time.Second)
		m.runIDToWorkflowID = map[int64]int64{100: 200}
		m.fetchedWorkflowIDs = map[int64]bool{200: true}
		m.dispatchedWorkflowFetch = map[int64]bool{200: true}
		m.jobAverages = map[string]time.Duration{"build": time.Minute}

		initialCheck := ghclient.CheckRunInfo{
			Name:           "build",
			Status:         "completed",
			Conclusion:     "success",
			WorkflowRunID:  100,
			WorkflowID:     200,
			DetailsURL:     "https://github.com/test/test/actions/runs/100/job/1",
		}
		key := checkKey(initialCheck)
		m.seenCheckKeys[key] = true

		m.avgFetchPending = false
		m.pendingWorkflowFetch = map[int64]bool{}

		newCheck := ghclient.CheckRunInfo{
			Name:           "test",
			Status:         "in_progress",
			WorkflowRunID:  300,
			DetailsURL:     "https://github.com/test/test/actions/runs/300/job/2",
		}

		msg := ChecksUpdateMsg{
			CheckRuns:          []ghclient.CheckRunInfo{initialCheck, newCheck},
			RateLimitRemaining: 5000,
		}

		model, _ := m.handleChecksUpdate(msg)
		result := model.(*Model)

		if !result.avgFetchPending {
			t.Error("avgFetchPending should be true when new checks appear after discovery completed")
		}
	})

	t.Run("no re-discovery for already-seen check runs", func(t *testing.T) {
		m := makeModel()
		m.rateLimitRemaining = 5000
		m.firstCheckSeenAt = time.Now().Add(-15 * time.Second)
		m.runIDToWorkflowID = map[int64]int64{100: 200}
		m.fetchedWorkflowIDs = map[int64]bool{200: true}
		m.dispatchedWorkflowFetch = map[int64]bool{200: true}
		m.avgFetchPending = false
		m.pendingWorkflowFetch = map[int64]bool{}

		existingCheck := ghclient.CheckRunInfo{
			Name:           "build",
			Status:         "completed",
			Conclusion:     "success",
			WorkflowRunID:  100,
			WorkflowID:     200,
			DetailsURL:     "https://github.com/test/test/actions/runs/100/job/1",
		}
		key := checkKey(existingCheck)
		m.seenCheckKeys[key] = true

		msg := ChecksUpdateMsg{
			CheckRuns:          []ghclient.CheckRunInfo{existingCheck},
			RateLimitRemaining: 5000,
		}

		model, _ := m.handleChecksUpdate(msg)
		result := model.(*Model)

		if result.avgFetchPending {
			t.Error("avgFetchPending should remain false when no new checks appear")
		}
	})
}

func TestCheckKey(t *testing.T) {
	t.Run("uses WorkflowRunID when available", func(t *testing.T) {
		cr := ghclient.CheckRunInfo{Name: "build", WorkflowRunID: 100}
		key := checkKey(cr)
		if key != "run:100:build" {
			t.Errorf("checkKey = %q, want run:100:build", key)
		}
	})

	t.Run("falls back to DetailsURL", func(t *testing.T) {
		cr := ghclient.CheckRunInfo{
			Name:       "test",
			DetailsURL: "https://github.com/o/r/actions/runs/42/job/7",
		}
		key := checkKey(cr)
		expected := "url:https://github.com/o/r/actions/runs/42/job/7:test"
		if key != expected {
			t.Errorf("checkKey = %q, want %q", key, expected)
		}
	})

	t.Run("falls back to name only", func(t *testing.T) {
		cr := ghclient.CheckRunInfo{Name: "lint"}
		key := checkKey(cr)
		if key != "name:lint" {
			t.Errorf("checkKey = %q, want name:lint", key)
		}
	})
}

func TestHasNewChecks(t *testing.T) {
	t.Run("returns true when new check appears", func(t *testing.T) {
		seen := map[string]bool{"run:100:build": true}
		checks := []ghclient.CheckRunInfo{
			{Name: "build", WorkflowRunID: 100},
			{Name: "test", WorkflowRunID: 200},
		}
		if !hasNewChecks(checks, seen) {
			t.Error("should detect new check")
		}
	})

	t.Run("returns false when all checks seen", func(t *testing.T) {
		seen := map[string]bool{"run:100:build": true, "run:200:test": true}
		checks := []ghclient.CheckRunInfo{
			{Name: "build", WorkflowRunID: 100},
			{Name: "test", WorkflowRunID: 200},
		}
		if hasNewChecks(checks, seen) {
			t.Error("should not detect new checks when all are seen")
		}
	})

	t.Run("returns false for empty checks", func(t *testing.T) {
		seen := map[string]bool{}
		if hasNewChecks([]ghclient.CheckRunInfo{}, seen) {
			t.Error("empty checks should not report new")
		}
	})
}

func TestAdvSecAliasOnRediscovery(t *testing.T) {
	t.Run("AdvSec alias created from cached workflowAverages when re-discovery delivers WorkflowsDiscoveredMsg", func(t *testing.T) {
		m := makeModel()
		m.rateLimitRemaining = 5000
		m.firstCheckSeenAt = time.Now().Add(-15 * time.Second)
		m.avgFetchPending = true
		m.historyFetchCompleted = true
		m.pendingWorkflowFetch = map[int64]bool{}
		m.dispatchedWorkflowFetch = map[int64]bool{}
		m.fetchedWorkflowIDs = map[int64]bool{789: true}
		m.workflowAverages = map[int64]map[string]time.Duration{
			789: {"Analyze (go)": 2 * time.Minute},
		}
		m.jobAverages = map[string]time.Duration{"Analyze (go)": 2 * time.Minute}
		m.runIDToWorkflowID = map[int64]int64{100: 789}

		ciCheck := ghclient.CheckRunInfo{
			Name:          "Analyze (go)",
			Status:        "in_progress",
			WorkflowRunID: 100,
			WorkflowID:    789,
			WorkflowName:  "CodeQL",
			DetailsURL:    "https://github.com/test/test/actions/runs/100/job/1",
		}
		advSecCheck := ghclient.CheckRunInfo{
			Name:       "CodeQL",
			Status:     "in_progress",
			AppName:    "GitHub",
			DetailsURL: "https://github.com/test/test/runs/73263098935",
		}
		m.checkRuns = []ghclient.CheckRunInfo{ciCheck, advSecCheck}

		msg := WorkflowsDiscoveredMsg{
			NewRunIDToWorkflowID: map[int64]int64{},
			WorkflowIDsToFetch:   []int64{},
		}
		model, _ := m.Update(msg)
		result := model.(Model)

		if _, ok := result.jobAverages["CodeQL"]; !ok {
			t.Error("jobAverages should have CodeQL alias from workflowAverages cache")
		}
		if result.jobAverages["CodeQL"] != 2*time.Minute {
			t.Errorf("jobAverages[CodeQL] = %v, want %v", result.jobAverages["CodeQL"], 2*time.Minute)
		}
		if result.advSecMatchWorkflow["CodeQL"] != 789 {
			t.Errorf("advSecMatchWorkflow[CodeQL] = %d, want 789", result.advSecMatchWorkflow["CodeQL"])
		}
	})

	t.Run("AdvSec alias created from workflowAverages in WorkflowsDiscoveredMsg handler", func(t *testing.T) {
		m := makeModel()
		m.avgFetchPending = true
		m.avgFetchStartTime = time.Now().Add(-1 * time.Second)
		m.pendingWorkflowFetch = map[int64]bool{}
		m.dispatchedWorkflowFetch = map[int64]bool{}
		m.fetchedWorkflowIDs = map[int64]bool{789: true}
		m.workflowAverages = map[int64]map[string]time.Duration{
			789: {"Analyze (go)": 2 * time.Minute},
		}
		m.jobAverages = map[string]time.Duration{"Analyze (go)": 2 * time.Minute}
		m.checkRuns = []ghclient.CheckRunInfo{
			{Name: "Analyze (go)", WorkflowRunID: 100, WorkflowID: 789, WorkflowName: "CodeQL", DetailsURL: "https://github.com/test/test/actions/runs/100/job/1"},
			{Name: "CodeQL", AppName: "GitHub", DetailsURL: "https://github.com/test/test/runs/73263098935"},
		}

		msg := WorkflowsDiscoveredMsg{
			NewRunIDToWorkflowID: map[int64]int64{100: 789},
			WorkflowIDsToFetch:   []int64{},
		}
		model, _ := m.Update(msg)
		result := model.(Model)

		if result.jobAverages["CodeQL"] != 2*time.Minute {
			t.Errorf("jobAverages[CodeQL] = %v, want %v", result.jobAverages["CodeQL"], 2*time.Minute)
		}
		if result.advSecMatchWorkflow["CodeQL"] != 789 {
			t.Errorf("advSecMatchWorkflow[CodeQL] = %d, want 789", result.advSecMatchWorkflow["CodeQL"])
		}
	})

	t.Run("AdvSec alias not overwritten if already in jobAverages", func(t *testing.T) {
		m := makeModel()
		m.avgFetchPending = true
		m.avgFetchStartTime = time.Now().Add(-1 * time.Second)
		m.pendingWorkflowFetch = map[int64]bool{}
		m.dispatchedWorkflowFetch = map[int64]bool{}
		m.fetchedWorkflowIDs = map[int64]bool{789: true}
		m.workflowAverages = map[int64]map[string]time.Duration{
			789: {"Analyze (go)": 2 * time.Minute},
		}
		m.jobAverages = map[string]time.Duration{
			"Analyze (go)": 2 * time.Minute,
			"CodeQL":        3 * time.Minute,
		}
		m.checkRuns = []ghclient.CheckRunInfo{
			{Name: "Analyze (go)", WorkflowRunID: 100, WorkflowID: 789, WorkflowName: "CodeQL", DetailsURL: "https://github.com/test/test/actions/runs/100/job/1"},
			{Name: "CodeQL", AppName: "GitHub", DetailsURL: "https://github.com/test/test/runs/73263098935"},
		}

		msg := WorkflowsDiscoveredMsg{
			NewRunIDToWorkflowID: map[int64]int64{},
			WorkflowIDsToFetch:   []int64{},
		}
		model, _ := m.Update(msg)
		result := model.(Model)

		if result.jobAverages["CodeQL"] != 3*time.Minute {
			t.Errorf("jobAverages[CodeQL] = %v, want %v (should not overwrite existing)", result.jobAverages["CodeQL"], 3*time.Minute)
		}
	})
}

func TestPresumedAverages(t *testing.T) {
	dco := ghclient.CheckRunInfo{
		Name:       "DCO",
		AppName:    "DCO",
		Status:     "completed",
		Conclusion: "success",
		DetailsURL: "https://probot.github.io/apps/dco/",
	}
	build := ghclient.CheckRunInfo{
		Name:          "build",
		WorkflowRunID: 100,
		WorkflowID:    200,
		Status:        "in_progress",
		DetailsURL:    "https://github.com/o/r/actions/runs/100/job/1",
	}

	t.Run("handleChecksUpdate injects presumed DCO average", func(t *testing.T) {
		m := makeModel()
		m.rateLimitRemaining = 5000
		m.firstCheckSeenAt = time.Now().Add(-15 * time.Second)
		m.presumedAverages = map[string]time.Duration{"DCO": 1 * time.Second}

		msg := ChecksUpdateMsg{
			CheckRuns:          []ghclient.CheckRunInfo{dco, build},
			RateLimitRemaining: 5000,
		}
		model, _ := m.handleChecksUpdate(msg)
		result := model.(*Model)

		if result.jobAverages["DCO"] != 1*time.Second {
			t.Errorf("jobAverages[DCO] = %v, want 1s", result.jobAverages["DCO"])
		}
		if _, present := result.jobAverages["build"]; present {
			t.Errorf("jobAverages[build] should not be presumed-set, got %v", result.jobAverages["build"])
		}
	})

	t.Run("handleChecksUpdate does not overwrite real history", func(t *testing.T) {
		m := makeModel()
		m.rateLimitRemaining = 5000
		m.firstCheckSeenAt = time.Now().Add(-15 * time.Second)
		m.presumedAverages = map[string]time.Duration{"DCO": 1 * time.Second}
		m.jobAverages = map[string]time.Duration{"DCO": 5 * time.Second}

		msg := ChecksUpdateMsg{
			CheckRuns:          []ghclient.CheckRunInfo{dco},
			RateLimitRemaining: 5000,
		}
		model, _ := m.handleChecksUpdate(msg)
		result := model.(*Model)

		if result.jobAverages["DCO"] != 5*time.Second {
			t.Errorf("jobAverages[DCO] = %v, want 5s (real history should win)", result.jobAverages["DCO"])
		}
	})

	t.Run("handleChecksUpdate no-op when presumedAverages is nil", func(t *testing.T) {
		m := makeModel()
		m.rateLimitRemaining = 5000
		m.firstCheckSeenAt = time.Now().Add(-15 * time.Second)

		msg := ChecksUpdateMsg{
			CheckRuns:          []ghclient.CheckRunInfo{dco},
			RateLimitRemaining: 5000,
		}
		model, _ := m.handleChecksUpdate(msg)
		result := model.(*Model)

		if _, present := result.jobAverages["DCO"]; present {
			t.Errorf("jobAverages[DCO] should not be set with nil presumedAverages, got %v", result.jobAverages["DCO"])
		}
	})
}
