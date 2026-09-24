package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/mattn/go-runewidth"
)

func TestNewModelAndExitCode(t *testing.T) {
	m := NewModel(
		context.Background(),
		"test-token",
		"owner",
		"repo",
		42,
		10*time.Second,
		NewStyles(2, 1, 3, 8),
		false,
		false,
		map[string]time.Duration{"dco": time.Second},
		true,
		10*time.Minute,
		30*time.Second,
		15*time.Second,
	)

	if m.ExitCode() != 0 {
		t.Errorf("fresh model ExitCode() = %d, want 0", m.ExitCode())
	}
	m.exitCode = 1
	if m.ExitCode() != 1 {
		t.Errorf("ExitCode() = %d, want 1", m.ExitCode())
	}

	// Constructor must initialize the map fields (nil maps would panic
	// when handlers merge averages).
	if m.jobAverages == nil || m.workflowAverages == nil || m.runIDToWorkflowID == nil ||
		m.fetchedWorkflowIDs == nil || m.pendingWorkflowFetch == nil ||
		m.dispatchedWorkflowFetch == nil || m.seenCheckKeys == nil || m.advSecMatchWorkflow == nil {
		t.Error("NewModel left a map field nil")
	}
}

func TestNewRepoModelAndExitCode(t *testing.T) {
	m := NewRepoModel(
		context.Background(),
		"test-token",
		"owner",
		"repo",
		30*time.Second,
		NewStyles(2, 1, 3, 8),
		true,
		15*time.Minute,
		30*time.Minute,
	)

	if m.ExitCode() != 0 {
		t.Errorf("repo model ExitCode() = %d, want 0 (persistent mode)", m.ExitCode())
	}
	if m.prs == nil {
		t.Error("NewRepoModel left prs map nil")
	}
}

func TestRenderErrorBox(t *testing.T) {
	m := &Model{styles: stylesForTest()}
	widths := ColumnWidths{QueueWidth: 5, NameWidth: 20, DurationWidth: 5, AvgWidth: 5}

	t.Run("message only", func(t *testing.T) {
		check := ghclient.CheckRunInfo{
			Annotations: []ghclient.Annotation{{Message: "build failed"}},
		}
		out := stripANSI(m.renderErrorBox(check, widths))
		if !strings.Contains(out, "build failed") {
			t.Errorf("renderErrorBox() missing message, got:\n%s", out)
		}
	})

	t.Run("title and message combined", func(t *testing.T) {
		check := ghclient.CheckRunInfo{
			Annotations: []ghclient.Annotation{{Title: "Compilation error", Message: "undefined: Foo"}},
		}
		out := stripANSI(m.renderErrorBox(check, widths))
		if !strings.Contains(out, "Compilation error: undefined: Foo") {
			t.Errorf("renderErrorBox() missing combined text, got:\n%s", out)
		}
	})

	t.Run("title only", func(t *testing.T) {
		check := ghclient.CheckRunInfo{
			Annotations: []ghclient.Annotation{{Title: "Just a title"}},
		}
		out := stripANSI(m.renderErrorBox(check, widths))
		if !strings.Contains(out, "Just a title") {
			t.Errorf("renderErrorBox() missing title, got:\n%s", out)
		}
	})

	t.Run("skips empty annotations", func(t *testing.T) {
		check := ghclient.CheckRunInfo{
			Annotations: []ghclient.Annotation{{}, {Message: "real error"}},
		}
		out := stripANSI(m.renderErrorBox(check, widths))
		if !strings.Contains(out, "real error") {
			t.Errorf("renderErrorBox() missing real annotation, got:\n%s", out)
		}
		if strings.Count(out, "real error") != 1 {
			t.Errorf("renderErrorBox() should render only non-empty annotations, got:\n%s", out)
		}
	})

	t.Run("path with line number prefixes message", func(t *testing.T) {
		check := ghclient.CheckRunInfo{
			Annotations: []ghclient.Annotation{{Path: "main.go", StartLine: 42, Message: "boom"}},
		}
		out := stripANSI(m.renderErrorBox(check, widths))
		if !strings.Contains(out, "main.go:42 - boom") {
			t.Errorf("renderErrorBox() missing file:line prefix, got:\n%s", out)
		}
	})

	t.Run("path without line number", func(t *testing.T) {
		check := ghclient.CheckRunInfo{
			Annotations: []ghclient.Annotation{{Path: "main.go", Message: "boom"}},
		}
		out := stripANSI(m.renderErrorBox(check, widths))
		if !strings.Contains(out, "main.go - boom") {
			t.Errorf("renderErrorBox() missing file prefix, got:\n%s", out)
		}
	})

	t.Run("no annotations returns empty", func(t *testing.T) {
		out := m.renderErrorBox(ghclient.CheckRunInfo{}, widths)
		if out != "" {
			t.Errorf("renderErrorBox() = %q, want empty", out)
		}
	})
}

func TestFormatLink(t *testing.T) {
	t.Run("empty url returns text unchanged", func(t *testing.T) {
		got := FormatLink("", "plain text")
		if got != "plain text" {
			t.Errorf("FormatLink() = %q, want %q", got, "plain text")
		}
	})

	t.Run("non-empty url produces hyperlink", func(t *testing.T) {
		got := FormatLink("https://example.com", "link text")
		if !strings.Contains(got, "https://example.com") || !strings.Contains(got, "link text") {
			t.Errorf("FormatLink() = %q, want hyperlink containing both", got)
		}
	})
}

func TestGetCopilotReviewIcon(t *testing.T) {
	tests := []struct {
		state string
		icon  string
	}{
		{"approved", "✓"},
		{"changes_requested", "✗"},
		{"commented", "💬"},
		{"dismissed", "⊘"},
		{"stale", "⚠"},
		{"timed_out", "⏱"},
		{"unknown-state", "?"},
	}

	for _, tt := range tests {
		if got := GetCopilotReviewIcon(tt.state, ""); got != tt.icon {
			t.Errorf("GetCopilotReviewIcon(%q) = %q, want %q", tt.state, got, tt.icon)
		}
	}

	if got := GetCopilotReviewIcon("pending", "queued"); got != "⏸" {
		t.Errorf("pending queued icon = %q, want %q", got, "⏸")
	}
	if got := GetCopilotReviewIcon("pending", "in_progress"); got != "◐" {
		t.Errorf("pending in_progress icon = %q, want %q", got, "◐")
	}
}

func TestFormatQueueLatencyQueued(t *testing.T) {
	pushed := time.Now().Add(-5 * time.Minute)

	t.Run("queued with push time shows elapsed", func(t *testing.T) {
		got := FormatQueueLatency(ghclient.CheckRunInfo{Status: "queued"}, pushed)
		if got == "-" || got == "" {
			t.Errorf("FormatQueueLatency(queued) = %q, want elapsed duration", got)
		}
	})

	t.Run("queued without push time shows dash", func(t *testing.T) {
		got := FormatQueueLatency(ghclient.CheckRunInfo{Status: "queued"}, time.Time{})
		if got != "-" {
			t.Errorf("FormatQueueLatency(queued, zero time) = %q, want %q", got, "-")
		}
	})

	t.Run("started check with positive latency", func(t *testing.T) {
		check := ghclient.CheckRunInfo{Status: "in_progress", StartedAt: ptrTime(pushed.Add(2 * time.Minute))}
		got := FormatQueueLatency(check, pushed)
		if got != "2m 0s" {
			t.Errorf("FormatQueueLatency() = %q, want %q", got, "2m 0s")
		}
	})

	t.Run("started check with zero latency shows dash", func(t *testing.T) {
		check := ghclient.CheckRunInfo{Status: "in_progress", StartedAt: ptrTime(pushed)}
		got := FormatQueueLatency(check, pushed)
		if got != "-" {
			t.Errorf("FormatQueueLatency() = %q, want %q", got, "-")
		}
	})
}

func TestFormatDescriptionBoundaries(t *testing.T) {
	widths := ColumnWidths{QueueWidth: 5, NameWidth: 20, DurationWidth: 5, AvgWidth: 5}

	t.Run("empty description returns empty", func(t *testing.T) {
		if got := FormatDescription("", widths); got != "" {
			t.Errorf("FormatDescription(empty) = %q, want empty", got)
		}
	})

	t.Run("short description unchanged", func(t *testing.T) {
		got := FormatDescription("short", widths)
		if got != "short" {
			t.Errorf("FormatDescription() = %q, want %q", got, "short")
		}
	})

	t.Run("long description truncated", func(t *testing.T) {
		got := FormatDescription(strings.Repeat("x", 100), widths)
		if runewidth.StringWidth(got) >= 100 {
			t.Errorf("FormatDescription() not truncated, width = %d", runewidth.StringWidth(got))
		}
	})
}

func TestBuildNameColumnLinkPaths(t *testing.T) {
	widths := ColumnWidths{NameWidth: 20}

	t.Run("link enabled with URL wraps name", func(t *testing.T) {
		check := ghclient.CheckRunInfo{Name: "build", DetailsURL: "https://github.com/owner/repo/actions/runs/1"}
		got := BuildNameColumn(check, widths, true)
		if !strings.Contains(got, "https://github.com/owner/repo/actions/runs/1") {
			t.Errorf("BuildNameColumn() should contain URL, got %q", got)
		}
	})

	t.Run("link enabled without URL is plain", func(t *testing.T) {
		check := ghclient.CheckRunInfo{Name: "build"}
		got := BuildNameColumn(check, widths, true)
		if strings.Contains(got, "\x1b]8") {
			t.Errorf("BuildNameColumn() should not hyperlink, got %q", got)
		}
	})
}

func TestRenderSummaryLine(t *testing.T) {
	m := &Model{styles: stylesForTest()}
	widths := ColumnWidths{QueueWidth: 5}

	t.Run("empty summary returns empty", func(t *testing.T) {
		if got := m.renderSummary(ghclient.CheckRunInfo{}, widths); got != "" {
			t.Errorf("renderSummary(empty) = %q, want empty", got)
		}
	})

	t.Run("summary indented and rendered", func(t *testing.T) {
		out := stripANSI(m.renderSummary(ghclient.CheckRunInfo{Summary: "details here"}, widths))
		if !strings.Contains(out, "details here") {
			t.Errorf("renderSummary() missing text, got %q", out)
		}
		if !strings.HasPrefix(out, strings.Repeat(" ", 8)) {
			t.Errorf("renderSummary() should be indented, got %q", out)
		}
	})
}

func TestSortCheckRunsStatusTieBreak(t *testing.T) {
	// Same duration key (both queued → max duration), different statuses:
	// waiting and unknown both fall in the "other" priority bucket, and
	// name breaks the tie alphabetically.
	checks := []ghclient.CheckRunInfo{
		{Name: "zulu", Status: "obscure"},
		{Name: "alpha", Status: "obscure"},
	}
	SortCheckRuns(checks)
	if checks[0].Name != "alpha" {
		t.Errorf("SortCheckRuns()[0] = %q, want %q", checks[0].Name, "alpha")
	}
}

func TestViewWaitingForMoreChecks(t *testing.T) {
	// allChecksComplete true but canTrustCompletion false → renders the
	// "Waiting for more checks" block and its expected-count line.
	m := &Model{
		styles:             stylesForTest(),
		prTitle:            "Test PR",
		checkRuns:          []ghclient.CheckRunInfo{{Status: "completed", Conclusion: "success"}},
		firstCheckSeenAt:   time.Now().Add(-5 * time.Second),
		expectedCheckCount: 10,
		lastUpdate:         time.Now(),
		fetchReceived:      true,
		rateLimitRemaining: 5000,
	}

	out := stripANSI(m.View().Content)
	if !strings.Contains(out, "Waiting for more checks to appear") {
		t.Errorf("View missing waiting message, got:\n%s", out)
	}
	if !strings.Contains(out, "Seen 1 of ~10 expected checks") {
		t.Errorf("View missing expected-count line, got:\n%s", out)
	}
}

func TestViewWaitingQuickMode(t *testing.T) {
	m := &Model{
		styles:           stylesForTest(),
		checkRuns:        []ghclient.CheckRunInfo{{Status: "completed", Conclusion: "success"}},
		noAvg:            true,
		peakCheckCount:   5, // > len(checkRuns): cannot trust completion
		firstCheckSeenAt: time.Now().Add(-1 * time.Second),
		lastUpdate:       time.Now(),
	}

	out := stripANSI(m.View().Content)
	if !strings.Contains(out, "Waiting for all seen checks to finish") {
		t.Errorf("View missing quick-mode waiting message, got:\n%s", out)
	}
}

func TestViewGracePeriodCountdown(t *testing.T) {
	m := &Model{
		styles:           stylesForTest(),
		checkRuns:        []ghclient.CheckRunInfo{{Status: "completed", Conclusion: "success"}},
		firstCheckSeenAt: time.Now().Add(-1 * time.Second), // grace period still running
		lastUpdate:       time.Now(),
	}

	out := stripANSI(m.View().Content)
	if !strings.Contains(out, "Grace period:") {
		t.Errorf("View missing grace period countdown, got:\n%s", out)
	}
}

func TestViewHeaderPushedLine(t *testing.T) {
	m := &Model{
		styles:             stylesForTest(),
		prTitle:            "Test PR",
		headPushedTime:     time.Now().Add(-3 * time.Minute),
		checkRuns:          []ghclient.CheckRunInfo{{Status: "in_progress", Name: "build"}},
		lastUpdate:         time.Now(),
		fetchReceived:      true,
		rateLimitRemaining: 5000,
	}

	out := stripANSI(m.View().Content)
	if !strings.Contains(out, "Pushed") {
		t.Errorf("View missing pushed line, got:\n%s", out)
	}
	if !strings.Contains(out, "PR #0: Test PR") {
		t.Errorf("View missing PR header, got:\n%s", out)
	}
}

func TestViewErrorAnnotationsRendered(t *testing.T) {
	m := &Model{
		styles: stylesForTest(),
		checkRuns: []ghclient.CheckRunInfo{{
			Name:        "test",
			Status:      "completed",
			Conclusion:  "failure",
			Annotations: []ghclient.Annotation{{Path: "a_test.go", StartLine: 7, Message: "assert failed"}},
		}},
		lastUpdate: time.Now(),
	}

	out := stripANSI(m.View().Content)
	if !strings.Contains(out, "a_test.go:7 - assert failed") {
		t.Errorf("View missing annotation box, got:\n%s", out)
	}
}

func TestViewSummaryRenderedForFailedCheck(t *testing.T) {
	m := &Model{
		styles: stylesForTest(),
		checkRuns: []ghclient.CheckRunInfo{{
			Name:       "test",
			Status:     "completed",
			Conclusion: "timed_out",
			Summary:    "job exceeded 30m limit",
		}},
		lastUpdate: time.Now(),
	}

	out := stripANSI(m.View().Content)
	if !strings.Contains(out, "job exceeded 30m limit") {
		t.Errorf("View missing summary line, got:\n%s", out)
	}
}
