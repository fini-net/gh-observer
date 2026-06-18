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
			runs:        []ghclient.BranchRunData{},
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
		want        time.Duration
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
		fadeSuccess:        fadeSuccess,
		fadeFailure:        fadeFailure,
		fetchReceived:      true,
		rateLimitRemaining: 4000,
	}

	t.Run("error preserves last good state and sets fetchErrChecks", func(t *testing.T) {
		m := seedPR
		msg := RepoChecksUpdateMsg{
			Err: fmt.Errorf("non-200 OK status code: 504 Gateway Timeout body: \"<!DOCTYPE html>..."),
		}

		newModel, _ := m.Update(msg)
		rm := newModel.(*RepoModel)

		if rm.fetchErrChecks == nil {
			t.Error("fetchErrChecks should be set on error")
		}
		if rm.fetchErrChecksAt.IsZero() {
			t.Error("fetchErrChecksAt should be set on error")
		}
		// The runs-source error should be untouched.
		if rm.fetchErrRuns != nil {
			t.Error("fetchErrRuns should not be touched by a checks-source error")
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

	t.Run("success clears fetchErrChecks only and updates state", func(t *testing.T) {
		// Start from an errored state on BOTH sources. The checks success
		// must clear only fetchErrChecks, leaving fetchErrRuns intact.
		m := seedPR
		m.fetchErrChecks = fmt.Errorf("previous 504")
		m.fetchErrChecksAt = time.Now().Add(-1 * time.Minute)
		m.fetchErrRuns = fmt.Errorf("ongoing 502 from runs source")
		m.fetchErrRunsAt = time.Now().Add(-30 * time.Second)

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

		if rm.fetchErrChecks != nil {
			t.Error("fetchErrChecks should be cleared on checks success")
		}
		if !rm.fetchErrChecksAt.IsZero() {
			t.Error("fetchErrChecksAt should be cleared on checks success")
		}
		// The runs-side error must survive the checks success — this is
		// the core isolation property fix #2 adds.
		if rm.fetchErrRuns == nil {
			t.Error("fetchErrRuns should survive a checks-source success")
		}
		if rm.fetchErrRunsAt.IsZero() {
			t.Error("fetchErrRunsAt should survive a checks-source success")
		}
		if rm.fetchReceived != true {
			t.Error("fetchReceived should remain true")
		}
		// Fix #4: handleRepoChecksUpdate now applies the min-across-sources
		// guard, so a 4900 message against a seeded 4000 leaves 4000 in
		// place rather than overwriting with 4900.
		if rm.rateLimitRemaining != 4000 {
			t.Errorf("rateLimitRemaining = %d, want 4000 (min across sources)", rm.rateLimitRemaining)
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

	t.Run("checks success does not clear runs error", func(t *testing.T) {
		// Minimal isolation check: seed only fetchErrRuns, send a successful
		// RepoChecksUpdateMsg, and assert fetchErrRuns is still set.
		m := seedPR
		m.fetchErrRuns = fmt.Errorf("ongoing 502")
		m.fetchErrRunsAt = time.Now().Add(-30 * time.Second)

		msg := RepoChecksUpdateMsg{
			PRData:             map[int]ghclient.PRCheckData{},
			RateLimitRemaining: 4900,
		}

		newModel, _ := m.Update(msg)
		rm := newModel.(*RepoModel)

		if rm.fetchErrRuns == nil {
			t.Error("fetchErrRuns should survive a checks-source success")
		}
		if rm.fetchErrChecks != nil {
			t.Error("fetchErrChecks should be nil on a checks success")
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
		standaloneRuns:     seedRuns,
		fadeSuccess:        fadeSuccess,
		fadeFailure:        fadeFailure,
		fetchReceived:      true,
		rateLimitRemaining: 4000,
	}

	t.Run("error preserves last good runs and sets fetchErrRuns", func(t *testing.T) {
		msg := RepoRunsUpdateMsg{
			Err: fmt.Errorf("non-200 OK status code: 502 Bad Gateway"),
		}

		newModel, _ := m.Update(msg)
		rm := newModel.(*RepoModel)

		if rm.fetchErrRuns == nil {
			t.Error("fetchErrRuns should be set on error")
		}
		if rm.fetchErrRunsAt.IsZero() {
			t.Error("fetchErrRunsAt should be set on error")
		}
		// The checks-source error should be untouched.
		if rm.fetchErrChecks != nil {
			t.Error("fetchErrChecks should not be touched by a runs-source error")
		}
		if len(rm.standaloneRuns) != 1 {
			t.Errorf("standaloneRuns = %d, want 1 (last good state preserved)", len(rm.standaloneRuns))
		}
	})

	t.Run("success clears fetchErrRuns only", func(t *testing.T) {
		// Seed both source errors; the runs success must clear only
		// fetchErrRuns, leaving fetchErrChecks intact.
		m2 := m
		m2.fetchErrRuns = fmt.Errorf("previous 502")
		m2.fetchErrRunsAt = time.Now().Add(-1 * time.Minute)
		m2.fetchErrChecks = fmt.Errorf("ongoing 504 from checks source")
		m2.fetchErrChecksAt = time.Now().Add(-30 * time.Second)

		msg := RepoRunsUpdateMsg{
			Runs: []ghclient.BranchRunData{
				{RunID: 2, Status: "in_progress", Event: "push", HeadBranch: "develop"},
			},
			RateLimitRemaining: 4900,
		}

		newModel, _ := m2.Update(msg)
		rm := newModel.(*RepoModel)

		if rm.fetchErrRuns != nil {
			t.Error("fetchErrRuns should be cleared on runs success")
		}
		// The checks-side error must survive the runs success.
		if rm.fetchErrChecks == nil {
			t.Error("fetchErrChecks should survive a runs-source success")
		}
		if rm.fetchErrChecksAt.IsZero() {
			t.Error("fetchErrChecksAt should survive a runs-source success")
		}
		if len(rm.standaloneRuns) != 1 {
			t.Errorf("standaloneRuns = %d, want 1 (new active run)", len(rm.standaloneRuns))
		}
		if rm.standaloneRuns[0].RunID != 2 {
			t.Errorf("standaloneRuns[0].RunID = %d, want 2", rm.standaloneRuns[0].RunID)
		}
	})

	t.Run("runs success does not clear checks error", func(t *testing.T) {
		// Minimal isolation check: seed only fetchErrChecks, send a
		// successful RepoRunsUpdateMsg, and assert fetchErrChecks is still set.
		m3 := m
		m3.fetchErrChecks = fmt.Errorf("ongoing 504")
		m3.fetchErrChecksAt = time.Now().Add(-30 * time.Second)

		msg := RepoRunsUpdateMsg{
			Runs: []ghclient.BranchRunData{
				{RunID: 3, Status: "in_progress", Event: "push", HeadBranch: "main"},
			},
			RateLimitRemaining: 4900,
		}

		newModel, _ := m3.Update(msg)
		rm := newModel.(*RepoModel)

		if rm.fetchErrChecks == nil {
			t.Error("fetchErrChecks should survive a runs-source success")
		}
		if rm.fetchErrRuns != nil {
			t.Error("fetchErrRuns should be nil on a runs success")
		}
	})
}

// TestRepoChecksUpdateRateLimitMinAcrossSources verifies fix #4: the checks
// source applies a min-across-sources guard (mirroring handleRepoRunsUpdate)
// so a GraphQL response with a higher remaining quota cannot raise the model
// value past what the REST runs source already observed.
func TestRepoChecksUpdateRateLimitMinAcrossSources(t *testing.T) {
	t.Run("min wins when checks quota is higher", func(t *testing.T) {
		m := RepoModel{
			prs:                make(map[int]PRViewData),
			fadeSuccess:        15 * time.Minute,
			fadeFailure:        30 * time.Minute,
			fetchReceived:      true,
			rateLimitRemaining: 4000,
		}
		msg := RepoChecksUpdateMsg{
			PRData:             map[int]ghclient.PRCheckData{},
			RateLimitRemaining: 4900,
		}
		newModel, _ := m.Update(msg)
		rm := newModel.(*RepoModel)
		if rm.rateLimitRemaining != 4000 {
			t.Errorf("rateLimitRemaining = %d, want 4000 (min across sources)", rm.rateLimitRemaining)
		}
	})

	t.Run("first observed wins on a fresh model", func(t *testing.T) {
		m := RepoModel{
			prs:           make(map[int]PRViewData),
			fadeSuccess:   15 * time.Minute,
			fadeFailure:   30 * time.Minute,
			fetchReceived: false,
		}
		msg := RepoChecksUpdateMsg{
			PRData:             map[int]ghclient.PRCheckData{},
			RateLimitRemaining: 4500,
		}
		newModel, _ := m.Update(msg)
		rm := newModel.(*RepoModel)
		if rm.rateLimitRemaining != 4500 {
			t.Errorf("rateLimitRemaining = %d, want 4500 (first observed)", rm.rateLimitRemaining)
		}
		if !rm.fetchReceived {
			t.Error("fetchReceived should be true after first successful checks update")
		}
	})
}

// TestRepoRunsUpdateRateLimitMinAcrossSources hardens the existing behavior
// on the runs side (the min-across-sources guard in handleRepoRunsUpdate) so
// it stays tested alongside the new checks-side equivalent above.
func TestRepoRunsUpdateRateLimitMinAcrossSources(t *testing.T) {
	t.Run("min wins when runs quota is higher", func(t *testing.T) {
		m := RepoModel{
			standaloneRuns:     []ghclient.BranchRunData{},
			fadeSuccess:        15 * time.Minute,
			fadeFailure:        30 * time.Minute,
			fetchReceived:      true,
			rateLimitRemaining: 4000,
		}
		msg := RepoRunsUpdateMsg{
			Runs:               []ghclient.BranchRunData{},
			RateLimitRemaining: 4900,
		}
		newModel, _ := m.Update(msg)
		rm := newModel.(*RepoModel)
		if rm.rateLimitRemaining != 4000 {
			t.Errorf("rateLimitRemaining = %d, want 4000 (min across sources)", rm.rateLimitRemaining)
		}
	})

	t.Run("first observed wins on a fresh model", func(t *testing.T) {
		m := RepoModel{
			standaloneRuns: []ghclient.BranchRunData{},
			fadeSuccess:    15 * time.Minute,
			fadeFailure:    30 * time.Minute,
			fetchReceived:  false,
		}
		msg := RepoRunsUpdateMsg{
			Runs:               []ghclient.BranchRunData{},
			RateLimitRemaining: 4500,
		}
		newModel, _ := m.Update(msg)
		rm := newModel.(*RepoModel)
		if rm.rateLimitRemaining != 4500 {
			t.Errorf("rateLimitRemaining = %d, want 4500 (first observed)", rm.rateLimitRemaining)
		}
		if !rm.fetchReceived {
			t.Error("fetchReceived should be true after first successful runs update")
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
		name  string
		input string
		max   int
		want  string
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
