package tui

import (
	"testing"
	"time"

	ghclient "github.com/fini-net/gh-observer/internal/github"
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
			name: "mixed checks - active keeps PR visible",
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