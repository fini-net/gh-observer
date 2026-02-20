package tui

import (
	"testing"

	ghclient "github.com/fini-net/gh-observer/internal/github"
)

func TestGetCheckIcon(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		conclusion string
		want       string
	}{
		{"completed success", "completed", "success", "✓"},
		{"completed failure", "completed", "failure", "✗"},
		{"completed cancelled", "completed", "cancelled", "⊗"},
		{"completed skipped", "completed", "skipped", "⊘"},
		{"completed timed_out", "completed", "timed_out", "⏱"},
		{"completed action_required", "completed", "action_required", "!"},
		{"completed unknown conclusion", "completed", "unknown", "?"},
		{"in_progress", "in_progress", "", "◐"},
		{"queued", "queued", "", "⏸"},
		{"unknown status", "unknown", "", "?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetCheckIcon(tt.status, tt.conclusion)
			if got != tt.want {
				t.Errorf("GetCheckIcon(%q, %q) = %q, want %q", tt.status, tt.conclusion, got, tt.want)
			}
		})
	}
}

func TestFormatCheckName(t *testing.T) {
	tests := []struct {
		name  string
		check ghclient.CheckRunInfo
		want  string
	}{
		{
			name: "with workflow name",
			check: ghclient.CheckRunInfo{
				WorkflowName: "CI",
				Name:         "test",
			},
			want: "CI / test",
		},
		{
			name: "without workflow name",
			check: ghclient.CheckRunInfo{
				WorkflowName: "",
				Name:         "legacy-check",
			},
			want: "legacy-check",
		},
		{
			name: "empty names",
			check: ghclient.CheckRunInfo{
				WorkflowName: "",
				Name:         "",
			},
			want: "",
		},
		{
			name: "workflow with special characters",
			check: ghclient.CheckRunInfo{
				WorkflowName: "Build & Deploy",
				Name:         "deploy-prod",
			},
			want: "Build & Deploy / deploy-prod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatCheckName(tt.check)
			if got != tt.want {
				t.Errorf("FormatCheckName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatCheckNameWithTruncate(t *testing.T) {
	tests := []struct {
		name     string
		check    ghclient.CheckRunInfo
		maxWidth int
		want     string
	}{
		{
			name: "no truncation needed",
			check: ghclient.CheckRunInfo{
				WorkflowName: "CI",
				Name:         "test",
			},
			maxWidth: 20,
			want:     "CI / test",
		},
		{
			name: "truncation needed",
			check: ghclient.CheckRunInfo{
				WorkflowName: "CI",
				Name:         "very-long-job-name-here",
			},
			maxWidth: 15,
			want:     "CI / very-long…",
		},
		{
			name: "exact fit",
			check: ghclient.CheckRunInfo{
				WorkflowName: "CI",
				Name:         "test",
			},
			maxWidth: 10,
			want:     "CI / test",
		},
		{
			name: "very small width",
			check: ghclient.CheckRunInfo{
				WorkflowName: "CI",
				Name:         "test",
			},
			maxWidth: 5,
			want:     "CI /…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatCheckNameWithTruncate(tt.check, tt.maxWidth)
			if got != tt.want {
				t.Errorf("FormatCheckNameWithTruncate() = %q, want %q", got, tt.want)
			}
		})
	}
}
