package tui

import (
	"strings"
	"testing"
	"time"

	ghclient "github.com/fini-net/gh-observer/internal/github"
)

// stylesForTest returns a Styles instance suitable for render assertions.
// Colors are not asserted on; the test only checks substrings and column
// geometry, so any valid Styles value works.
func stylesForTest() Styles {
	return NewStyles(2, 1, 3, 8)
}

func TestBuildCopilotCheckRun(t *testing.T) {
	now := time.Now()

	t.Run("disabled returns nil", func(t *testing.T) {
		m := &Model{waitForCopilot: false}
		if got := m.buildCopilotCheckRun(); got != nil {
			t.Errorf("expected nil when waitForCopilot disabled, got %+v", got)
		}
	})

	t.Run("never armed returns nil", func(t *testing.T) {
		m := &Model{waitForCopilot: true}
		// copilotWaitStartTime zero
		if got := m.buildCopilotCheckRun(); got != nil {
			t.Errorf("expected nil when never armed, got %+v", got)
		}
	})

	t.Run("initial delay window: queued + pending", func(t *testing.T) {
		m := &Model{
			waitForCopilot:       true,
			copilotPending:       true,
			copilotWaitStartTime: now,
			copilotPollStartTime: now.Add(15 * time.Second),
			copilotInitialDelay:  15 * time.Second,
		}
		got := m.buildCopilotCheckRun()
		if got == nil {
			t.Fatal("expected non-nil row during initial-delay window")
		}
		if got.Kind != "review" {
			t.Errorf("Kind = %q, want \"review\"", got.Kind)
		}
		if got.Status != "queued" {
			t.Errorf("Status = %q, want \"queued\"", got.Status)
		}
		if got.ReviewState != "pending" {
			t.Errorf("ReviewState = %q, want \"pending\"", got.ReviewState)
		}
		if got.StartedAt != nil {
			t.Errorf("StartedAt should be nil while queued, got %v", *got.StartedAt)
		}
		if got.WorkflowName != "Copilot" || got.Name != "Review" {
			t.Errorf("name = %q / %q, want Copilot / Review", got.WorkflowName, got.Name)
		}
	})

	t.Run("polling in progress: in_progress + StartedAt set", func(t *testing.T) {
		pollStart := now.Add(-30 * time.Second) // polling started 30s ago
		m := &Model{
			waitForCopilot:       true,
			copilotPending:       true,
			copilotWaitStartTime: now.Add(-45 * time.Second),
			copilotPollStartTime: pollStart,
		}
		got := m.buildCopilotCheckRun()
		if got == nil {
			t.Fatal("expected non-nil row while polling in progress")
		}
		if got.Status != "in_progress" {
			t.Errorf("Status = %q, want \"in_progress\"", got.Status)
		}
		if got.ReviewState != "pending" {
			t.Errorf("ReviewState = %q, want \"pending\"", got.ReviewState)
		}
		if got.StartedAt == nil {
			t.Fatal("StartedAt should be set to copilotPollStartTime while in_progress")
		}
		if !got.StartedAt.Equal(pollStart) {
			t.Errorf("StartedAt = %v, want %v", *got.StartedAt, pollStart)
		}
		if got.CompletedAt != nil {
			t.Errorf("CompletedAt should remain nil while in_progress")
		}
	})

	t.Run("stale review", func(t *testing.T) {
		m := &Model{
			waitForCopilot:       true,
			copilotStale:         true,
			copilotWaitStartTime: now,
		}
		got := m.buildCopilotCheckRun()
		if got == nil {
			t.Fatal("expected non-nil row for stale review")
		}
		if got.Status != "completed" {
			t.Errorf("Status = %q, want \"completed\" for stale", got.Status)
		}
		if got.ReviewState != "stale" {
			t.Errorf("ReviewState = %q, want \"stale\"", got.ReviewState)
		}
	})

	t.Run("approved review", func(t *testing.T) {
		m := &Model{
			waitForCopilot:        true,
			copilotWaitStartTime:  now,
			copilotReviewComplete: true,
			copilotState:          "approved",
		}
		got := m.buildCopilotCheckRun()
		if got == nil {
			t.Fatal("expected non-nil row for approved review")
		}
		if got.Status != "completed" {
			t.Errorf("Status = %q, want \"completed\"", got.Status)
		}
		if got.ReviewState != "approved" {
			t.Errorf("ReviewState = %q, want \"approved\"", got.ReviewState)
		}
		if got.StartedAt != nil || got.CompletedAt != nil {
			t.Errorf("timestamps should be nil for completed review")
		}
	})

	t.Run("changes_requested review", func(t *testing.T) {
		m := &Model{
			waitForCopilot:        true,
			copilotWaitStartTime:  now,
			copilotReviewComplete: true,
			copilotState:          "changes_requested",
		}
		got := m.buildCopilotCheckRun()
		if got == nil {
			t.Fatal("expected non-nil row for changes_requested review")
		}
		if got.ReviewState != "changes_requested" {
			t.Errorf("ReviewState = %q, want \"changes_requested\"", got.ReviewState)
		}
	})

	t.Run("commented review", func(t *testing.T) {
		m := &Model{
			waitForCopilot:        true,
			copilotWaitStartTime:  now,
			copilotReviewComplete: true,
			copilotState:          "commented",
		}
		got := m.buildCopilotCheckRun()
		if got == nil {
			t.Fatal("expected non-nil row for commented review")
		}
		if got.ReviewState != "commented" {
			t.Errorf("ReviewState = %q, want \"commented\"", got.ReviewState)
		}
	})

	t.Run("dismissed review", func(t *testing.T) {
		m := &Model{
			waitForCopilot:        true,
			copilotWaitStartTime:  now,
			copilotReviewComplete: true,
			copilotState:          "dismissed",
		}
		got := m.buildCopilotCheckRun()
		if got == nil {
			t.Fatal("expected non-nil row for dismissed review")
		}
		if got.ReviewState != "dismissed" {
			t.Errorf("ReviewState = %q, want \"dismissed\"", got.ReviewState)
		}
	})

	t.Run("complete but empty state returns nil", func(t *testing.T) {
		// Two-consecutive-not-requested path leaves copilotState empty even
		// though copilotReviewComplete is true. No row should render.
		m := &Model{
			waitForCopilot:        true,
			copilotWaitStartTime:  now,
			copilotReviewComplete: true,
			copilotState:          "",
		}
		if got := m.buildCopilotCheckRun(); got != nil {
			t.Errorf("expected nil when state empty, got %+v", got)
		}
	})
}

func TestRenderCopilotReviewCheckRun(t *testing.T) {
	widths := ColumnWidths{
		QueueWidth:    5,
		NameWidth:     20,
		DurationWidth: 7,
		AvgWidth:      7,
	}
	m := &Model{styles: stylesForTest()}

	t.Run("approved row contains icon and name, blank queue and avg", func(t *testing.T) {
		row := m.renderCopilotReviewCheckRun(ghclient.CheckRunInfo{
			Kind:         "review",
			WorkflowName: "Copilot",
			Name:         "Review",
			Status:       "completed",
			ReviewState:  "approved",
		}, widths)
		if !strings.Contains(row, "✓") {
			t.Errorf("approved row missing ✓ icon: %q", row)
		}
		if !strings.Contains(row, "Copilot / Review") {
			t.Errorf("approved row missing name: %q", row)
		}
		// Row should end with newline
		if !strings.HasSuffix(row, "\n") {
			t.Errorf("row should end with newline: %q", row)
		}
	})

	t.Run("commented row uses chat-bubble icon", func(t *testing.T) {
		row := m.renderCopilotReviewCheckRun(ghclient.CheckRunInfo{
			Kind:         "review",
			WorkflowName: "Copilot",
			Name:         "Review",
			Status:       "completed",
			ReviewState:  "commented",
		}, widths)
		if !strings.Contains(row, "💬") {
			t.Errorf("commented row missing 💬 icon: %q", row)
		}
	})

	t.Run("changes_requested row uses x icon", func(t *testing.T) {
		row := m.renderCopilotReviewCheckRun(ghclient.CheckRunInfo{
			Kind:         "review",
			WorkflowName: "Copilot",
			Name:         "Review",
			Status:       "completed",
			ReviewState:  "changes_requested",
		}, widths)
		if !strings.Contains(row, "✗") {
			t.Errorf("changes_requested row missing ✗ icon: %q", row)
		}
	})

	t.Run("pending row uses in_progress icon", func(t *testing.T) {
		start := time.Now().Add(-30 * time.Second)
		row := m.renderCopilotReviewCheckRun(ghclient.CheckRunInfo{
			Kind:         "review",
			WorkflowName: "Copilot",
			Name:         "Review",
			Status:       "in_progress",
			ReviewState:  "pending",
			StartedAt:    &start,
		}, widths)
		if !strings.Contains(row, "◐") {
			t.Errorf("pending row missing ◐ icon: %q", row)
		}
	})

	t.Run("stale row uses dedicated warning icon", func(t *testing.T) {
		row := m.renderCopilotReviewCheckRun(ghclient.CheckRunInfo{
			Kind:         "review",
			WorkflowName: "Copilot",
			Name:         "Review",
			Status:       "completed",
			ReviewState:  "stale",
		}, widths)
		// "stale" has its own icon in GetCopilotReviewIcon (⚠), distinct
		// from the generic "?" used for unrecognized states, so the user
		// can tell a stale review apart from an unknown one by icon alone.
		if !strings.Contains(row, "⚠") {
			t.Errorf("stale row missing ⚠ icon: %q", row)
		}
		if strings.Contains(row, "?") {
			t.Errorf("stale row should not fall through to generic '?' icon: %q", row)
		}
	})
}

// TestBuildCopilotCheckRun_StaleSummary verifies that the synthetic stale
// review row carries a Summary line with the short SHA and actionable hint,
// restoring the information the pre-refactor renderCopilotStatusLine carried.
func TestBuildCopilotCheckRun_StaleSummary(t *testing.T) {
	now := time.Now()
	m := &Model{
		waitForCopilot:       true,
		copilotStale:         true,
		copilotWaitStartTime: now,
		headSHA:              "abcd1234ef567890",
	}
	got := m.buildCopilotCheckRun()
	if got == nil {
		t.Fatal("expected non-nil row for stale review")
	}
	if got.Summary == "" {
		t.Fatal("expected non-empty Summary for stale review")
	}
	if !strings.Contains(got.Summary, "abcd123") {
		t.Errorf("Summary should contain the 7-char short SHA, got %q", got.Summary)
	}
	if !strings.Contains(got.Summary, "refresh or re-request") {
		t.Errorf("Summary should contain actionable hint, got %q", got.Summary)
	}
}

// TestRenderCopilotStatusLine_StartupPhase verifies that the Copilot status
// line is rendered during the startup phase (when len(m.checkRuns) == 0 and
// the table itself is suppressed). This guards the regression introduced by
// the original refactor where the Copilot row moved below the early return.
func TestRenderCopilotStatusLine_StartupPhase(t *testing.T) {
	m := &Model{
		styles:               stylesForTest(),
		waitForCopilot:       true,
		copilotPending:       true,
		copilotWaitStartTime: time.Now(),
		copilotPollStartTime: time.Now().Add(15 * time.Second),
	}
	line := m.renderCopilotStatusLine()
	if line == "" {
		t.Fatal("expected non-empty status line while pending during startup phase")
	}
	if !strings.Contains(line, "Copilot review queued, polling in") {
		t.Errorf("status line should show countdown while in initial-delay window, got %q", line)
	}
}

// TestRenderCopilotStatusLine_Stale verifies that the stale warning carries
// the short SHA and actionable hint, matching the pre-refactor behavior.
func TestRenderCopilotStatusLine_Stale(t *testing.T) {
	m := &Model{
		styles:               stylesForTest(),
		waitForCopilot:       true,
		copilotStale:         true,
		copilotWaitStartTime: time.Now(),
		headSHA:              "abcd1234ef567890",
	}
	line := m.renderCopilotStatusLine()
	if !strings.Contains(line, "⚠") {
		t.Errorf("stale status line should start with ⚠, got %q", line)
	}
	if !strings.Contains(line, "abcd123") {
		t.Errorf("stale status line should contain short SHA, got %q", line)
	}
	if !strings.Contains(line, "refresh or re-request") {
		t.Errorf("stale status line should contain actionable hint, got %q", line)
	}
}

// TestRenderCopilotStatusLine_CompletedReturnsEmpty verifies that the status
// line is suppressed once the review reaches a non-stale terminal state, so
// the table row is the sole presenter of that state (no double-printing).
func TestRenderCopilotStatusLine_CompletedReturnsEmpty(t *testing.T) {
	m := &Model{
		styles:                stylesForTest(),
		waitForCopilot:        true,
		copilotWaitStartTime:  time.Now(),
		copilotReviewComplete: true,
		copilotState:          "approved",
	}
	if got := m.renderCopilotStatusLine(); got != "" {
		t.Errorf("status line should be empty for non-stale terminal state, got %q", got)
	}
}

// TestRenderCheckRun_RegularRowUnchanged verifies that the Kind==""
// fast path in renderCheckRun still renders regular check rows correctly
// after the refactor introduced the review branch.
func TestRenderCheckRun_RegularRowUnchanged(t *testing.T) {
	m := &Model{styles: stylesForTest()}
	startedAt := time.Now().Add(-5 * time.Minute)
	completedAt := startedAt.Add(90 * time.Second)
	check := ghclient.CheckRunInfo{
		Name:         "build",
		WorkflowName: "CI",
		Status:       "completed",
		Conclusion:   "success",
		StartedAt:    &startedAt,
		CompletedAt:  &completedAt,
	}
	widths := ColumnWidths{
		QueueWidth:    5,
		NameWidth:     20,
		DurationWidth: 7,
		AvgWidth:      7,
	}
	row := m.renderCheckRun(check, widths)
	if !strings.Contains(row, "✓") {
		t.Errorf("success row missing ✓ icon: %q", row)
	}
	if !strings.Contains(row, "CI / build") {
		t.Errorf("row missing name: %q", row)
	}
	if !strings.Contains(row, "1m 30s") {
		t.Errorf("row missing duration text: %q", row)
	}
}
