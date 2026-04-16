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
		{
			name: "CJK characters in job name no truncation",
			check: ghclient.CheckRunInfo{
				WorkflowName: "CI",
				Name:         "ビルド",
			},
			maxWidth: 20,
			want:     "CI / ビルド",
		},
		{
			name: "CJK characters in job name truncation",
			check: ghclient.CheckRunInfo{
				WorkflowName: "CI",
				Name:         "テストビルド",
			},
			maxWidth: 10,
			want:     "CI / テス…",
		},
		{
			name: "emoji in job name no truncation",
			check: ghclient.CheckRunInfo{
				WorkflowName: "Build",
				Name:         "🚀 deploy",
			},
			maxWidth: 20,
			want:     "Build / 🚀 deploy",
		},
		{
			name: "emoji in job name truncation",
			check: ghclient.CheckRunInfo{
				WorkflowName: "Build",
				Name:         "🚀 deploy-prod",
			},
			maxWidth: 15,
			want:     "Build / 🚀 dep…",
		},
		{
			name: "CJK workflow name truncation",
			check: ghclient.CheckRunInfo{
				WorkflowName: "検証ワークフロー",
				Name:         "テスト",
			},
			maxWidth: 10,
			want:     "検証ワー…",
		},
		{
			name: "no workflow CJK truncation",
			check: ghclient.CheckRunInfo{
				WorkflowName: "",
				Name:         "ビルドテスト実行",
			},
			maxWidth: 5,
			want:     "ビル…",
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

func TestBuildNameColumnCJK(t *testing.T) {
	widths := ColumnWidths{NameWidth: 20}
	check := ghclient.CheckRunInfo{
		WorkflowName: "CI",
		Name:         "ビルド",
	}
	got := BuildNameColumn(check, widths, false)
	// "CI / ビルド" has display width 11, so 9 padding spaces
	want := "CI / ビルド         "
	if got != want {
		t.Errorf("BuildNameColumn() = %q, want %q", got, want)
	}
}

func TestFormatAlignedColumnsCJK(t *testing.T) {
	widths := ColumnWidths{
		QueueWidth:    5,
		NameWidth:     10,
		DurationWidth: 5,
		AvgWidth:      5,
	}
	queueCol, nameCol, durationCol, avgCol := FormatAlignedColumns("30s", "ビルド", "1m", "--", widths)
	// "ビルド" has display width 6, NameWidth=10, padding=4
	if nameCol != "ビルド    " {
		t.Errorf("nameCol = %q, want %q", nameCol, "ビルド    ")
	}
	// "30s" = 3 display cells, QueueWidth=5, padding=2
	if queueCol != "  30s" {
		t.Errorf("queueCol = %q, want %q", queueCol, "  30s")
	}
	// "1m" = 2 display cells, DurationWidth=5, padding=3
	if durationCol != "   1m" {
		t.Errorf("durationCol = %q, want %q", durationCol, "   1m")
	}
	// "--" = 2 display cells, AvgWidth=5, padding=3
	if avgCol != "   --" {
		t.Errorf("avgCol = %q, want %q", avgCol, "   --")
	}
}

func TestCalculateColumnWidthsCJK(t *testing.T) {
	now := time.Now()
	started := now.Add(-5 * time.Minute)
	inProgressStarted := now.Add(-2 * time.Minute)
	checks := []ghclient.CheckRunInfo{
		{Name: "ビルドテスト実行チェックを確認するプロセス", Status: "completed", Conclusion: "success", StartedAt: &started, CompletedAt: &now},
		{Name: "deploy", WorkflowName: "🚀配信ワークフロー実行プロセス", Status: "in_progress", StartedAt: &inProgressStarted},
	}
	widths := CalculateColumnWidths(checks, time.Time{}, nil)

	// "🚀配信ワークフロー実行プロセス / deploy" has display width 39
	// "ビルドテスト実行チェックを確認するプロセス" has display width 42, capped at maxNameWidth=60
	// max of these display widths is 42
	if widths.NameWidth != 42 {
		t.Errorf("NameWidth = %d, want 42", widths.NameWidth)
	}
}

func TestFormatDescriptionCJK(t *testing.T) {
	widths := ColumnWidths{
		QueueWidth:    5,
		NameWidth:     10,
		DurationWidth: 5,
		AvgWidth:      5,
	}
	// totalWidth = 5+1+1+10+2+5+2+5 = 31, maxLen = 27
	// Display width of the CJK string is 58, truncated to 27 display cells
	got := FormatDescription("テスト実行の詳細説明を入力してくださいここに追加情報ですよ", widths)
	want := "テスト実行の詳細説明を入力…"
	if got != want {
		t.Errorf("FormatDescription() = %q, want %q", got, want)
	}
}
