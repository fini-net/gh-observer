package github

import (
	"testing"
	"time"

	"github.com/google/go-github/v86/github"
)

func TestIsActiveRun(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{"in_progress", true},
		{"queued", true},
		{"waiting", true},
		{"pending", true},
		{"IN_PROGRESS", true},
		{"completed", false},
		{"success", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := isActiveRun(tt.status); got != tt.want {
				t.Errorf("isActiveRun(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestIncludeRun(t *testing.T) {
	fadeWindow := 30 * time.Minute

	tests := []struct {
		name    string
		run     *github.WorkflowRun
		want    bool
	}{
		{
			name: "in_progress run included",
			run: &github.WorkflowRun{
				Status: github.Ptr("in_progress"),
			},
			want: true,
		},
		{
			name: "queued run included",
			run: &github.WorkflowRun{
				Status: github.Ptr("queued"),
			},
			want: true,
		},
		{
			name: "recently completed run included",
			run: &github.WorkflowRun{
				Status:    github.Ptr("completed"),
				UpdatedAt: &github.Timestamp{Time: time.Now().Add(-5 * time.Minute)},
			},
			want: true,
		},
		{
			name: "old completed run excluded",
			run: &github.WorkflowRun{
				Status:    github.Ptr("completed"),
				UpdatedAt: &github.Timestamp{Time: time.Now().Add(-45 * time.Minute)},
			},
			want: false,
		},
		{
			name: "completed run with nil UpdatedAt excluded",
			run: &github.WorkflowRun{
				Status: github.Ptr("completed"),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := includeRun(tt.run, fadeWindow); got != tt.want {
				t.Errorf("includeRun() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertBranchRun(t *testing.T) {
	now := time.Now()
	runID := int64(12345)
	wfID := int64(678)

	tests := []struct {
		name string
		run  *github.WorkflowRun
		want BranchRunData
	}{
		{
			name: "full run data",
			run: &github.WorkflowRun{
				ID:           &runID,
				DisplayTitle: github.Ptr("Deploy to production"),
				HeadBranch:   github.Ptr("main"),
				Event:        github.Ptr("push"),
				WorkflowID:   &wfID,
				Status:       github.Ptr("in_progress"),
				Conclusion:   github.Ptr(""),
				CreatedAt:    &github.Timestamp{Time: now},
				RunStartedAt: &github.Timestamp{Time: now.Add(5 * time.Second)},
			},
			want: BranchRunData{
				RunID:        12345,
				DisplayTitle: "Deploy to production",
				HeadBranch:   "main",
				Event:        "push",
				WorkflowID:   678,
				Status:       "in_progress",
				Conclusion:   "",
				CreatedAt:    now,
				RunStartedAt: now.Add(5 * time.Second),
			},
		},
		{
			name: "falls back to Name when DisplayTitle empty",
			run: &github.WorkflowRun{
				ID:           &runID,
				Name:         github.Ptr("CI"),
				DisplayTitle: github.Ptr(""),
				HeadBranch:   github.Ptr("main"),
				Event:        github.Ptr("schedule"),
				WorkflowID:   &wfID,
				Status:       github.Ptr("completed"),
				Conclusion:   github.Ptr("success"),
			},
			want: BranchRunData{
				RunID:        12345,
				DisplayTitle: "CI",
				HeadBranch:   "main",
				Event:        "schedule",
				WorkflowID:   678,
				Status:       "completed",
				Conclusion:   "success",
			},
		},
		{
			name: "scheduled workflow",
			run: &github.WorkflowRun{
				ID:           &runID,
				DisplayTitle: github.Ptr("Nightly scan"),
				HeadBranch:   github.Ptr("main"),
				Event:        github.Ptr("schedule"),
				WorkflowID:   &wfID,
				Status:       github.Ptr("completed"),
				Conclusion:   github.Ptr("success"),
			},
			want: BranchRunData{
				RunID:        12345,
				DisplayTitle: "Nightly scan",
				HeadBranch:   "main",
				Event:        "schedule",
				WorkflowID:   678,
				Status:       "completed",
				Conclusion:   "success",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertBranchRun(tt.run)
			if got.RunID != tt.want.RunID {
				t.Errorf("RunID = %d, want %d", got.RunID, tt.want.RunID)
			}
			if got.DisplayTitle != tt.want.DisplayTitle {
				t.Errorf("DisplayTitle = %q, want %q", got.DisplayTitle, tt.want.DisplayTitle)
			}
			if got.HeadBranch != tt.want.HeadBranch {
				t.Errorf("HeadBranch = %q, want %q", got.HeadBranch, tt.want.HeadBranch)
			}
			if got.Event != tt.want.Event {
				t.Errorf("Event = %q, want %q", got.Event, tt.want.Event)
			}
			if got.WorkflowID != tt.want.WorkflowID {
				t.Errorf("WorkflowID = %d, want %d", got.WorkflowID, tt.want.WorkflowID)
			}
			if got.Status != tt.want.Status {
				t.Errorf("Status = %q, want %q", got.Status, tt.want.Status)
			}
			if got.Conclusion != tt.want.Conclusion {
				t.Errorf("Conclusion = %q, want %q", got.Conclusion, tt.want.Conclusion)
			}
		})
	}
}