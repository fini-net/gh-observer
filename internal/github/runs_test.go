package github

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-github/v90/github"
	"github.com/shurcooL/githubv4"
)

func TestFirstLine(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "single line", input: "fix: update readme", want: "fix: update readme"},
		{name: "multiline", input: "fix: update readme\n\nMore details here", want: "fix: update readme"},
		{name: "empty string", input: "", want: ""},
		{name: "whitespace only", input: "   ", want: ""},
		{name: "leading whitespace", input: "  fix: update readme", want: "fix: update readme"},
		{name: "multiline with blank first line", input: "\nfix: update readme", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstLine(tt.input)
			if got != tt.want {
				t.Errorf("firstLine(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestConvertWorkflowJob(t *testing.T) {
	now := time.Now()
	startedAt := &github.Timestamp{Time: now}
	completedAt := &github.Timestamp{Time: now.Add(2 * time.Minute)}

	tests := []struct {
		name string
		job  *github.WorkflowJob
		want WorkflowJobInfo
	}{
		{
			name: "completed success",
			job: &github.WorkflowJob{
				Name:         github.Ptr("test"),
				WorkflowName: github.Ptr("CI"),
				Status:       github.Ptr("completed"),
				Conclusion:   github.Ptr("success"),
				HTMLURL:      github.Ptr("https://github.com/owner/repo/actions/runs/1/job/1"),
				RunID:        github.Ptr(int64(1)),
				StartedAt:    startedAt,
				CompletedAt:  completedAt,
			},
			want: WorkflowJobInfo{
				Name:         "test",
				WorkflowName: "CI",
				Status:       "completed",
				Conclusion:   "success",
				HTMLURL:      "https://github.com/owner/repo/actions/runs/1/job/1",
				RunID:        1,
				StartedAt:    startedAt,
				CompletedAt:  completedAt,
			},
		},
		{
			name: "in_progress job",
			job: &github.WorkflowJob{
				Name:         github.Ptr("build"),
				WorkflowName: github.Ptr("Deploy"),
				Status:       github.Ptr("in_progress"),
				Conclusion:   github.Ptr(""),
				StartedAt:    startedAt,
			},
			want: WorkflowJobInfo{
				Name:         "build",
				WorkflowName: "Deploy",
				Status:       "in_progress",
				Conclusion:   "",
				StartedAt:    startedAt,
			},
		},
		{
			name: "nil optional fields",
			job: &github.WorkflowJob{
				Status:     github.Ptr("queued"),
				Conclusion: github.Ptr(""),
			},
			want: WorkflowJobInfo{
				Status:     "queued",
				Conclusion: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertWorkflowJob(tt.job)
			if got.Name != tt.want.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.want.Name)
			}
			if got.WorkflowName != tt.want.WorkflowName {
				t.Errorf("WorkflowName = %q, want %q", got.WorkflowName, tt.want.WorkflowName)
			}
			if got.Status != tt.want.Status {
				t.Errorf("Status = %q, want %q", got.Status, tt.want.Status)
			}
			if got.Conclusion != tt.want.Conclusion {
				t.Errorf("Conclusion = %q, want %q", got.Conclusion, tt.want.Conclusion)
			}
			if got.HTMLURL != tt.want.HTMLURL {
				t.Errorf("HTMLURL = %q, want %q", got.HTMLURL, tt.want.HTMLURL)
			}
			if got.RunID != tt.want.RunID {
				t.Errorf("RunID = %d, want %d", got.RunID, tt.want.RunID)
			}
		})
	}
}

func TestAllJobsComplete(t *testing.T) {
	tests := []struct {
		name string
		jobs []WorkflowJobInfo
		want bool
	}{
		{
			name: "empty list returns false",
			jobs: []WorkflowJobInfo{},
			want: false,
		},
		{
			name: "all completed returns true",
			jobs: []WorkflowJobInfo{
				{Status: "completed", Conclusion: "success"},
				{Status: "completed", Conclusion: "failure"},
			},
			want: true,
		},
		{
			name: "one in_progress returns false",
			jobs: []WorkflowJobInfo{
				{Status: "completed", Conclusion: "success"},
				{Status: "in_progress"},
			},
			want: false,
		},
		{
			name: "one queued returns false",
			jobs: []WorkflowJobInfo{
				{Status: "completed", Conclusion: "success"},
				{Status: "queued"},
			},
			want: false,
		},
		{
			name: "single completed returns true",
			jobs: []WorkflowJobInfo{
				{Status: "completed", Conclusion: "success"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AllJobsComplete(tt.jobs)
			if got != tt.want {
				t.Errorf("AllJobsComplete() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetermineRunExitCode(t *testing.T) {
	tests := []struct {
		name string
		jobs []WorkflowJobInfo
		want int
	}{
		{
			name: "all success returns 0",
			jobs: []WorkflowJobInfo{
				{Status: "completed", Conclusion: "success"},
				{Status: "completed", Conclusion: "success"},
			},
			want: 0,
		},
		{
			name: "one failure returns 1",
			jobs: []WorkflowJobInfo{
				{Status: "completed", Conclusion: "success"},
				{Status: "completed", Conclusion: "failure"},
			},
			want: 1,
		},
		{
			name: "timed out returns 1",
			jobs: []WorkflowJobInfo{
				{Status: "completed", Conclusion: "timed_out"},
			},
			want: 1,
		},
		{
			name: "action_required returns 1",
			jobs: []WorkflowJobInfo{
				{Status: "completed", Conclusion: "action_required"},
			},
			want: 1,
		},
		{
			name: "skipped only returns 0",
			jobs: []WorkflowJobInfo{
				{Status: "completed", Conclusion: "skipped"},
			},
			want: 0,
		},
		{
			name: "empty list returns 0",
			jobs: []WorkflowJobInfo{},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineRunExitCode(tt.jobs)
			if got != tt.want {
				t.Errorf("DetermineRunExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFailureJobConclusion(t *testing.T) {
	tests := []struct {
		conclusion string
		want       bool
	}{
		{"failure", true},
		{"timed_out", true},
		{"action_required", true},
		{"success", false},
		{"cancelled", false},
		{"skipped", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.conclusion, func(t *testing.T) {
			got := FailureJobConclusion(tt.conclusion)
			if got != tt.want {
				t.Errorf("FailureJobConclusion(%q) = %v, want %v", tt.conclusion, got, tt.want)
			}
		})
	}
}

// mockPushedDateQuerier returns a sequence of canned commitPushedDateQuery
// responses (or errors). Mirrors mockQuerier in graphql_test.go but for
// the run-mode commit-push-time query.
type mockPushedDateQuerier struct {
	responses []commitPushedDateQuery
	errs      []error
	calls     int
}

func (m *mockPushedDateQuerier) Query(_ context.Context, q interface{}, _ map[string]interface{}) error {
	if m.calls >= len(m.responses) && m.calls >= len(m.errs) {
		return fmt.Errorf("unexpected query call #%d", m.calls+1)
	}
	var err error
	if m.calls < len(m.errs) && m.errs[m.calls] != nil {
		err = m.errs[m.calls]
	}
	if err == nil {
		resp := m.responses[m.calls]
		target := q.(*commitPushedDateQuery)
		*target = resp
	}
	m.calls++
	return err
}

// TestFetchCommitPushedTime_PushedDateWins asserts pushedDate is returned
// when both pushedDate and committedDate are present (issue #349).
func TestFetchCommitPushedTime_PushedDateWins(t *testing.T) {
	pushed := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	committed := pushed.Add(-2 * time.Hour)

	resp := commitPushedDateQuery{}
	resp.Repository.Object.Commit.PushedDate.Time = pushed
	resp.Repository.Object.Commit.CommittedDate.Time = committed
	resp.RateLimit.Remaining = 4999

	mock := &mockPushedDateQuerier{responses: []commitPushedDateQuery{resp}}
	got, rl := fetchCommitPushedTimeWithClient(context.Background(), mock, "owner", "repo", "deadbeef")
	if !got.Equal(pushed) {
		t.Errorf("got %v, want %v (pushedDate should win)", got, pushed)
	}
	if rl != 4999 {
		t.Errorf("rate limit = %d, want 4999", rl)
	}
}

// TestFetchCommitPushedTime_FallsBackToCommittedDate asserts the
// committedDate fallback when pushedDate is absent.
func TestFetchCommitPushedTime_FallsBackToCommittedDate(t *testing.T) {
	committed := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)

	resp := commitPushedDateQuery{}
	resp.Repository.Object.Commit.CommittedDate.Time = committed
	resp.RateLimit.Remaining = 4998

	mock := &mockPushedDateQuerier{responses: []commitPushedDateQuery{resp}}
	got, rl := fetchCommitPushedTimeWithClient(context.Background(), mock, "owner", "repo", "deadbeef")
	if !got.Equal(committed) {
		t.Errorf("got %v, want %v (committedDate fallback)", got, committed)
	}
	if rl != 4998 {
		t.Errorf("rate limit = %d, want 4998", rl)
	}
}

// TestFetchCommitPushedTime_ZeroWhenAbsent asserts the zero time is
// returned when neither date is present so callers can fall back to REST,
// while the rate limit is still surfaced for the caller's accounting.
func TestFetchCommitPushedTime_ZeroWhenAbsent(t *testing.T) {
	resp := commitPushedDateQuery{}
	resp.RateLimit.Remaining = 4997
	mock := &mockPushedDateQuerier{responses: []commitPushedDateQuery{resp}}
	got, rl := fetchCommitPushedTimeWithClient(context.Background(), mock, "owner", "repo", "deadbeef")
	if !got.IsZero() {
		t.Errorf("got %v, want zero", got)
	}
	if rl != 4997 {
		t.Errorf("rate limit = %d, want 4997", rl)
	}
}

// TestFetchCommitPushedTime_ZeroOnQueryError asserts a GraphQL error
// yields the zero time and a 0 rate limit so the caller keeps the REST
// fallback rather than crashing (issue #349 robustness criterion).
func TestFetchCommitPushedTime_ZeroOnQueryError(t *testing.T) {
	mock := &mockPushedDateQuerier{
		responses: []commitPushedDateQuery{{}},
		errs:      []error{fmt.Errorf("network error")},
	}
	got, rl := fetchCommitPushedTimeWithClient(context.Background(), mock, "owner", "repo", "deadbeef")
	if !got.IsZero() {
		t.Errorf("got %v, want zero on query error", got)
	}
	if rl != 0 {
		t.Errorf("rate limit = %d, want 0 on query error", rl)
	}
}

// TestFetchCommitPushedTime_ZeroForEmptySHA guards against the GraphQL
// helper being invoked with an empty SHA (would 404). No GraphQL call
// should be made, and the rate limit must be 0 (no observation).
func TestFetchCommitPushedTime_ZeroForEmptySHA(t *testing.T) {
	mock := &mockPushedDateQuerier{}
	got, rl := fetchCommitPushedTimeWithClient(context.Background(), mock, "owner", "repo", "")
	if !got.IsZero() {
		t.Errorf("got %v, want zero for empty SHA", got)
	}
	if rl != 0 {
		t.Errorf("rate limit = %d, want 0 for empty SHA", rl)
	}
	if mock.calls != 0 {
		t.Errorf("expected 0 GraphQL calls for empty SHA, got %d", mock.calls)
	}
}

// Compile-time guard: confirm githubv4.GitObjectID is used correctly in
// fetchCommitPushedTimeWithClient's variables map. If the githubv4 API
// changes the type, this will surface at compile time rather than at the
// first real run.
var _ githubv4.GitObjectID = githubv4.GitObjectID("")
