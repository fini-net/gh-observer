package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/mattn/go-runewidth"
)

func TestRepoChecksUpdateFadeOut(t *testing.T) {
	now := time.Now()
	fadeSuccess := 15 * time.Minute
	fadeFailure := 30 * time.Minute

	successCompletedAt := now.Add(-10 * time.Minute)
	failureCompletedAt := now.Add(-20 * time.Minute)
	fadedSuccessCompletedAt := now.Add(-20 * time.Minute)
	fadedFailureCompletedAt := now.Add(-45 * time.Minute)

	tests := []struct {
		name              string
		prData            map[int]ghclient.PRCheckData
		wantPRCount       int
		wantVisibleChecks int
	}{
		{
			name: "active check keeps PR visible",
			prData: map[int]ghclient.PRCheckData{
				1: {
					Number: 1,
					Title:  "Active PR",
					CheckRuns: []ghclient.CheckRunInfo{
						{Status: "in_progress", Name: "build"},
					},
				},
			},
			wantPRCount:       1,
			wantVisibleChecks: 1,
		},
		{
			name: "queued check keeps PR visible",
			prData: map[int]ghclient.PRCheckData{
				1: {
					Number: 1,
					Title:  "Queued PR",
					CheckRuns: []ghclient.CheckRunInfo{
						{Status: "queued", Name: "deploy"},
					},
				},
			},
			wantPRCount:       1,
			wantVisibleChecks: 1,
		},
		{
			name: "recent success stays visible",
			prData: map[int]ghclient.PRCheckData{
				1: {
					Number: 1,
					Title:  "Recent success",
					CheckRuns: []ghclient.CheckRunInfo{
						{Status: "completed", Conclusion: "success", Name: "build", CompletedAt: &successCompletedAt},
					},
				},
			},
			wantPRCount:       1,
			wantVisibleChecks: 1,
		},
		{
			name: "faded success removed",
			prData: map[int]ghclient.PRCheckData{
				1: {
					Number: 1,
					Title:  "Old success",
					CheckRuns: []ghclient.CheckRunInfo{
						{Status: "completed", Conclusion: "success", Name: "build", CompletedAt: &fadedSuccessCompletedAt},
					},
				},
			},
			wantPRCount:       0,
			wantVisibleChecks: 0,
		},
		{
			name: "recent failure stays visible",
			prData: map[int]ghclient.PRCheckData{
				1: {
					Number: 1,
					Title:  "Recent failure",
					CheckRuns: []ghclient.CheckRunInfo{
						{Status: "completed", Conclusion: "failure", Name: "build", CompletedAt: &failureCompletedAt},
					},
				},
			},
			wantPRCount:       1,
			wantVisibleChecks: 1,
		},
		{
			name: "faded failure removed",
			prData: map[int]ghclient.PRCheckData{
				1: {
					Number: 1,
					Title:  "Old failure",
					CheckRuns: []ghclient.CheckRunInfo{
						{Status: "completed", Conclusion: "failure", Name: "build", CompletedAt: &fadedFailureCompletedAt},
					},
				},
			},
			wantPRCount:       0,
			wantVisibleChecks: 0,
		},
		{
			name: "mixed checks - active keeps PR visible, faded dropped",
			prData: map[int]ghclient.PRCheckData{
				1: {
					Number: 1,
					Title:  "Mixed PR",
					CheckRuns: []ghclient.CheckRunInfo{
						{Status: "completed", Conclusion: "success", Name: "old-build", CompletedAt: &fadedSuccessCompletedAt},
						{Status: "in_progress", Name: "test"},
					},
				},
			},
			wantPRCount:       1,
			wantVisibleChecks: 1,
		},
		{
			name: "all faded - PR dropped entirely",
			prData: map[int]ghclient.PRCheckData{
				1: {
					Number: 1,
					Title:  "All faded",
					CheckRuns: []ghclient.CheckRunInfo{
						{Status: "completed", Conclusion: "success", Name: "a", CompletedAt: &fadedSuccessCompletedAt},
						{Status: "completed", Conclusion: "failure", Name: "b", CompletedAt: &fadedFailureCompletedAt},
					},
				},
			},
			wantPRCount:       0,
			wantVisibleChecks: 0,
		},
		{
			name: "multiple PRs - only active ones kept",
			prData: map[int]ghclient.PRCheckData{
				1: {
					Number: 1,
					Title:  "Active",
					CheckRuns: []ghclient.CheckRunInfo{
						{Status: "in_progress", Name: "build"},
					},
				},
				2: {
					Number: 2,
					Title:  "Faded",
					CheckRuns: []ghclient.CheckRunInfo{
						{Status: "completed", Conclusion: "success", Name: "build", CompletedAt: &fadedSuccessCompletedAt},
					},
				},
			},
			wantPRCount:       1,
			wantVisibleChecks: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := RepoModel{
				prs:         make(map[int]PRViewData),
				fadeSuccess: fadeSuccess,
				fadeFailure: fadeFailure,
			}

			msg := RepoChecksUpdateMsg{
				PRData:             tt.prData,
				RateLimitRemaining: 5000,
			}

			newModel, _ := m.Update(msg)
			rm := newModel.(*RepoModel)

			if len(rm.prs) != tt.wantPRCount {
				t.Errorf("visible PRs = %d, want %d", len(rm.prs), tt.wantPRCount)
			}

			visibleChecks := 0
			for _, pr := range rm.prs {
				visibleChecks += len(pr.CheckRuns)
			}
			if visibleChecks != tt.wantVisibleChecks {
				t.Errorf("visible checks = %d, want %d", visibleChecks, tt.wantVisibleChecks)
			}
		})
	}
}

func TestRepoRunsUpdateFadeOut(t *testing.T) {
	now := time.Now()
	fadeSuccess := 15 * time.Minute
	fadeFailure := 30 * time.Minute

	recentStart := now.Add(-5 * time.Minute)
	fadedStart := now.Add(-45 * time.Minute)

	tests := []struct {
		name        string
		runs        []ghclient.BranchRunData
		wantVisible int
	}{
		{
			name: "in_progress run stays visible",
			runs: []ghclient.BranchRunData{
				{RunID: 1, Status: "in_progress", Event: "push", HeadBranch: "main"},
			},
			wantVisible: 1,
		},
		{
			name: "queued run stays visible",
			runs: []ghclient.BranchRunData{
				{RunID: 2, Status: "queued", Event: "push", HeadBranch: "main"},
			},
			wantVisible: 1,
		},
		{
			name: "recently completed success stays visible",
			runs: []ghclient.BranchRunData{
				{RunID: 3, Status: "completed", Conclusion: "success", Event: "push", HeadBranch: "main", RunStartedAt: recentStart},
			},
			wantVisible: 1,
		},
		{
			name: "recently completed failure stays visible",
			runs: []ghclient.BranchRunData{
				{RunID: 4, Status: "completed", Conclusion: "failure", Event: "push", HeadBranch: "main", RunStartedAt: recentStart},
			},
			wantVisible: 1,
		},
		{
			name: "faded completed run removed",
			runs: []ghclient.BranchRunData{
				{RunID: 5, Status: "completed", Conclusion: "success", Event: "push", HeadBranch: "main", RunStartedAt: fadedStart},
			},
			wantVisible: 0,
		},
		{
			name: "mixed - active and faded",
			runs: []ghclient.BranchRunData{
				{RunID: 6, Status: "in_progress", Event: "push", HeadBranch: "main"},
				{RunID: 7, Status: "completed", Conclusion: "success", Event: "schedule", HeadBranch: "main", RunStartedAt: fadedStart},
			},
			wantVisible: 1,
		},
		{
			name:        "empty list",
			runs:         []ghclient.BranchRunData{},
			wantVisible: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := RepoModel{
				prs:         make(map[int]PRViewData),
				fadeSuccess: fadeSuccess,
				fadeFailure: fadeFailure,
			}

			msg := RepoRunsUpdateMsg{
				Runs:               tt.runs,
				RateLimitRemaining: 5000,
			}

			newModel, _ := m.Update(msg)
			rm := newModel.(*RepoModel)

			if len(rm.standaloneRuns) != tt.wantVisible {
				t.Errorf("visible branch runs = %d, want %d", len(rm.standaloneRuns), tt.wantVisible)
			}
		})
	}
}

func TestIsActiveBranchRun(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{"in_progress", true},
		{"queued", true},
		{"waiting", true},
		{"pending", true},
		{"completed", false},
		{"", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := isActiveBranchRun(tt.status); got != tt.want {
				t.Errorf("isActiveBranchRun(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestRepoModelSortedPRNumbers(t *testing.T) {
	m := RepoModel{
		prs: map[int]PRViewData{
			3: {Title: "c"},
			1: {Title: "a"},
			2: {Title: "b"},
		},
	}
	got := m.sortedPRNumbers()
	want := []int{1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sortedPRNumbers[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestRepoModelFadeWindow(t *testing.T) {
	tests := []struct {
		name        string
		fadeSuccess time.Duration
		fadeFailure time.Duration
		want         time.Duration
	}{
		{"failure larger", 15 * time.Minute, 30 * time.Minute, 30 * time.Minute},
		{"success larger", 30 * time.Minute, 15 * time.Minute, 30 * time.Minute},
		{"equal", 15 * time.Minute, 15 * time.Minute, 15 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := RepoModel{fadeSuccess: tt.fadeSuccess, fadeFailure: tt.fadeFailure}
			if got := m.fadeWindow(); got != tt.want {
				t.Errorf("fadeWindow() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPluralS(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "s"},
		{1, ""},
		{2, "s"},
		{10, "s"},
	}
	for _, tt := range tests {
		if got := pluralS(tt.n); got != tt.want {
			t.Errorf("pluralS(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestRepoChecksUpdateErrorNonFatal(t *testing.T) {
	fadeSuccess := 15 * time.Minute
	fadeFailure := 30 * time.Minute

	// Seed the model with a known good PR so we can verify the error path
	// preserves it rather than clearing it.
	now := time.Now()
	goodCompletedAt := now.Add(-1 * time.Minute)
	seedPR := RepoModel{
		prs: map[int]PRViewData{
			42: {
				Title: "Pre-existing PR",
				CheckRuns: []ghclient.CheckRunInfo{
					{Status: "in_progress", Name: "build"},
					{Status: "completed", Conclusion: "success", Name: "lint", CompletedAt: &goodCompletedAt},
				},
				HeadCommitTime: now.Add(-2 * time.Minute),
			},
		},
		fadeSuccess:    fadeSuccess,
		fadeFailure:    fadeFailure,
		fetchReceived:  true,
		rateLimitRemaining: 4000,
	}

	t.Run("error preserves last good state and sets fetchErr", func(t *testing.T) {
		m := seedPR
		msg := RepoChecksUpdateMsg{
			Err: fmt.Errorf("non-200 OK status code: 504 Gateway Timeout body: \"<!DOCTYPE html>..."),
		}

		newModel, _ := m.Update(msg)
		rm := newModel.(*RepoModel)

		if rm.fetchErr == nil {
			t.Error("fetchErr should be set on error")
		}
		if rm.fetchErrAt.IsZero() {
			t.Error("fetchErrAt should be set on error")
		}
		if rm.fetchReceived != true {
			t.Error("fetchReceived should remain true after error (already received before)")
		}
		// Last good PRs preserved.
		if len(rm.prs) != 1 {
			t.Errorf("prs = %d, want 1 (last good state preserved)", len(rm.prs))
		}
		if _, ok := rm.prs[42]; !ok {
			t.Error("PR #42 should still be present after error")
		}
		// Rate limit should be unchanged from before (not reset to 5000-default-on-error).
		if rm.rateLimitRemaining != 4000 {
			t.Errorf("rateLimitRemaining = %d, want 4000 (unchanged on error)", rm.rateLimitRemaining)
		}
	})

	t.Run("success clears fetchErr and updates state", func(t *testing.T) {
		// Start from an errored state.
		m := seedPR
		m.fetchErr = fmt.Errorf("previous 504")
		m.fetchErrAt = time.Now().Add(-1 * time.Minute)

		msg := RepoChecksUpdateMsg{
			PRData: map[int]ghclient.PRCheckData{
				7: {
					Number: 7,
					Title:  "New PR",
					CheckRuns: []ghclient.CheckRunInfo{
						{Status: "in_progress", Name: "test"},
					},
				},
			},
			RateLimitRemaining: 4900,
		}

		newModel, _ := m.Update(msg)
		rm := newModel.(*RepoModel)

		if rm.fetchErr != nil {
			t.Error("fetchErr should be cleared on success")
		}
		if !rm.fetchErrAt.IsZero() {
			t.Error("fetchErrAt should be cleared on success")
		}
		if rm.fetchReceived != true {
			t.Error("fetchReceived should remain true")
		}
		if rm.rateLimitRemaining != 4900 {
			t.Errorf("rateLimitRemaining = %d, want 4900", rm.rateLimitRemaining)
		}
		// The PR map is replaced wholesale with the new message's PRs (fade-out
		// only filters within the new message — it does not carry over PRs
		// from the previous render). So only PR #7 should be present.
		if len(rm.prs) != 1 {
			t.Errorf("prs = %d, want 1 (new message replaces map)", len(rm.prs))
		}
		if _, ok := rm.prs[7]; !ok {
			t.Error("PR #7 should be present after success")
		}
	})
}

func TestRepoRunsUpdateErrorNonFatal(t *testing.T) {
	fadeSuccess := 15 * time.Minute
	fadeFailure := 30 * time.Minute

	seedRuns := []ghclient.BranchRunData{
		{RunID: 1, Status: "in_progress", Event: "push", HeadBranch: "main"},
	}
	m := RepoModel{
		standaloneRuns:    seedRuns,
		fadeSuccess:       fadeSuccess,
		fadeFailure:       fadeFailure,
		fetchReceived:     true,
		rateLimitRemaining: 4000,
	}

	t.Run("error preserves last good runs and sets fetchErr", func(t *testing.T) {
		msg := RepoRunsUpdateMsg{
			Err: fmt.Errorf("non-200 OK status code: 502 Bad Gateway"),
		}

		newModel, _ := m.Update(msg)
		rm := newModel.(*RepoModel)

		if rm.fetchErr == nil {
			t.Error("fetchErr should be set on error")
		}
		if rm.fetchErrAt.IsZero() {
			t.Error("fetchErrAt should be set on error")
		}
		if len(rm.standaloneRuns) != 1 {
			t.Errorf("standaloneRuns = %d, want 1 (last good state preserved)", len(rm.standaloneRuns))
		}
	})

	t.Run("success clears fetchErr", func(t *testing.T) {
		m2 := m
		m2.fetchErr = fmt.Errorf("previous 502")

		msg := RepoRunsUpdateMsg{
			Runs: []ghclient.BranchRunData{
				{RunID: 2, Status: "in_progress", Event: "push", HeadBranch: "develop"},
			},
			RateLimitRemaining: 4900,
		}

		newModel, _ := m2.Update(msg)
		rm := newModel.(*RepoModel)

		if rm.fetchErr != nil {
			t.Error("fetchErr should be cleared on success")
		}
		if len(rm.standaloneRuns) != 1 {
			t.Errorf("standaloneRuns = %d, want 1 (new active run)", len(rm.standaloneRuns))
		}
		if rm.standaloneRuns[0].RunID != 2 {
			t.Errorf("standaloneRuns[0].RunID = %d, want 2", rm.standaloneRuns[0].RunID)
		}
	})
}

func TestRepoModelFetchReceivedGatesRateLimit(t *testing.T) {
	// Verify the fetchReceived flag starts false on a fresh model.
	m := NewRepoModel(
		context.Background(), "tok", "o", "r",
		30*time.Second, NewStyles(10, 9, 11, 8), true,
		15*time.Minute, 30*time.Minute,
	)
	if m.fetchReceived {
		t.Error("fetchReceived should be false on a fresh model")
	}

	// A successful RepoChecksUpdateMsg flips it true.
	msg := RepoChecksUpdateMsg{
		PRData:             map[int]ghclient.PRCheckData{},
		RateLimitRemaining: 4500,
	}
	newModel, _ := m.Update(msg)
	rm := newModel.(*RepoModel)
	if !rm.fetchReceived {
		t.Error("fetchReceived should be true after a successful checks update")
	}
}

func TestTruncateFetchError(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		max    int
		want   string
	}{
		{
			name:  "short passthrough",
			input: "connection refused",
			max:   80,
			want:  "connection refused",
		},
		{
			name:  "under-max passthrough",
			input: "0123456789012345678901234567890123456789012345678901234567890123", // 64 chars
			max:   80,
			want:  "0123456789012345678901234567890123456789012345678901234567890123",
		},
		{
			name:  "truncates long 504 body",
			input: `non-200 OK status code: 504 Gateway Timeout body: "<!DOCTYPE html><html><head><title>Server Error</title></head><body><h1>Server Error</h1><p>Sorry about that.</p></body></html>`,
			max:   80,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateFetchError(tt.input, tt.max)
			if tt.want != "" {
				if got != tt.want {
					t.Errorf("truncateFetchError() = %q, want %q", got, tt.want)
				}
				return
			}
			// For truncation cases, just verify it fits and ends with ellipsis.
			if runewidth.StringWidth(got) > tt.max {
				t.Errorf("truncateFetchError() width = %d, want <= %d", runewidth.StringWidth(got), tt.max)
			}
			if !strings.HasSuffix(got, "…") {
				t.Errorf("truncateFetchError() = %q, want suffix …", got)
			}
		})
	}
}