package tui

import (
	"testing"
	"time"

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

func TestFormatAvg(t *testing.T) {
	check := ghclient.CheckRunInfo{Name: "my-job"}

	t.Run("nil map", func(t *testing.T) {
		got := FormatAvg(check, nil)
		if got != "--" {
			t.Errorf("FormatAvg() = %q, want %q", got, "--")
		}
	})

	t.Run("missing key", func(t *testing.T) {
		got := FormatAvg(check, map[string]time.Duration{"other-job": 5 * time.Minute})
		if got != "--" {
			t.Errorf("FormatAvg() = %q, want %q", got, "--")
		}
	})

	t.Run("zero duration", func(t *testing.T) {
		got := FormatAvg(check, map[string]time.Duration{"my-job": 0})
		if got != "0s" {
			t.Errorf("FormatAvg() = %q, want %q", got, "0s")
		}
	})

	t.Run("valid duration", func(t *testing.T) {
		got := FormatAvg(check, map[string]time.Duration{"my-job": 2*time.Minute + 30*time.Second})
		if got != "2m 30s" {
			t.Errorf("FormatAvg() = %q, want %q", got, "2m 30s")
		}
	})
}
