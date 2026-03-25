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
		runIDToWorkflowID:       make(map[int64]int64),
		fetchedWorkflowIDs:      make(map[int64]bool),
		pendingWorkflowFetch:    make(map[int64]bool),
		dispatchedWorkflowFetch: make(map[int64]bool),
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
		name             string
		msg              ChecksUpdateMsg
		rateLimit        int
		wantErr          bool
		wantExitCode     int
		wantQuitting     bool
		wantChecksStored bool
	}{
		{
			name:             "error in message stores error and returns nil cmd",
			msg:              ChecksUpdateMsg{Err: context.Canceled},
			wantErr:          true,
			wantExitCode:     0,
			wantQuitting:     false,
			wantChecksStored: false,
		},
		{
			name:             "successful update stores check runs",
			msg:              ChecksUpdateMsg{CheckRuns: []ghclient.CheckRunInfo{{Status: "in_progress", Conclusion: ""}}, RateLimitRemaining: 5000},
			rateLimit:        5000,
			wantErr:          false,
			wantChecksStored: true,
		},
		{
			name:             "all checks complete sets exit code 0 on success",
			msg:              ChecksUpdateMsg{CheckRuns: []ghclient.CheckRunInfo{{Status: "completed", Conclusion: "success"}}, RateLimitRemaining: 5000},
			rateLimit:        5000,
			wantErr:          false,
			wantExitCode:     0,
			wantQuitting:     true,
			wantChecksStored: true,
		},
		{
			name:             "all checks complete sets exit code 1 on failure",
			msg:              ChecksUpdateMsg{CheckRuns: []ghclient.CheckRunInfo{{Status: "completed", Conclusion: "failure"}}, RateLimitRemaining: 5000},
			rateLimit:        5000,
			wantErr:          false,
			wantExitCode:     1,
			wantQuitting:     true,
			wantChecksStored: true,
		},
		{
			name:             "in_progress checks do not quit",
			msg:              ChecksUpdateMsg{CheckRuns: []ghclient.CheckRunInfo{{Status: "in_progress"}}, RateLimitRemaining: 5000},
			rateLimit:        5000,
			wantErr:          false,
			wantQuitting:     false,
			wantChecksStored: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := makeModel()
			m.rateLimitRemaining = tt.rateLimit

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
}
