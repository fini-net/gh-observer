package tui

import (
	"testing"
	"time"

	ghclient "github.com/fini-net/gh-observer/internal/github"
)

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
				{Status: "in_progress", StartedAt: ptrTime(now)},
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
				{Status: "in_progress", StartedAt: ptrTime(now)},
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
