package tui

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/mattn/go-runewidth"
)

// probe is a fixed sentinel rendered through each style to compare identity.
const probe = "probe"

// ansiPattern matches SGR-style escape sequences emitted by lipgloss.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripANSI removes escape sequences so render assertions can match
// against plain text regardless of styling.
func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

// repoStylesForTest returns a Styles instance suitable for render assertions.
// Colors are not asserted on; tests only check substrings and geometry.
func repoStylesForTest() Styles {
	return NewStyles(2, 1, 3, 8)
}

// TestRepoView_AltScreenEnabled guards against issue #451 recurring: every
// View() return path must set AltScreen, since bubbletea v2 toggles the
// terminal's alternate-screen mode based on any mismatch between frames,
// and a single path reverting to a raw tea.NewView(...) would reintroduce
// the stale-frame bug this fix addresses.
func TestRepoView_AltScreenEnabled(t *testing.T) {
	m := &RepoModel{}
	if !m.View().AltScreen {
		t.Error("View() should have AltScreen enabled")
	}
}

func TestStyleForCheck(t *testing.T) {
	styles := repoStylesForTest()

	tests := []struct {
		name       string
		status     string
		conclusion string
		want       func(Styles) string
	}{
		{name: "completed success", status: "completed", conclusion: "success", want: func(s Styles) string { return s.Success.Render(probe) }},
		{name: "completed failure", status: "completed", conclusion: "failure", want: func(s Styles) string { return s.Failure.Render(probe) }},
		{name: "completed timed_out", status: "completed", conclusion: "timed_out", want: func(s Styles) string { return s.Failure.Render(probe) }},
		{name: "completed action_required", status: "completed", conclusion: "action_required", want: func(s Styles) string { return s.Running.Render(probe) }},
		{name: "completed neutral", status: "completed", conclusion: "neutral", want: func(s Styles) string { return s.Queued.Render(probe) }},
		{name: "in_progress", status: "in_progress", conclusion: "", want: func(s Styles) string { return s.Running.Render(probe) }},
		{name: "queued", status: "queued", conclusion: "", want: func(s Styles) string { return s.Queued.Render(probe) }},
		{name: "waiting", status: "waiting", conclusion: "", want: func(s Styles) string { return s.Queued.Render(probe) }},
		{name: "unknown status", status: "weird", conclusion: "", want: func(s Styles) string { return s.Queued.Render(probe) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := styleForCheck(tt.status, tt.conclusion, styles)
			if got.Render(probe) != tt.want(styles) {
				t.Error("styleForCheck() returned unexpected style")
			}
		})
	}
}

func TestFormatBranchRunDuration(t *testing.T) {
	tests := []struct {
		name   string
		run    ghclient.BranchRunData
		wantFn func(string) bool
	}{
		{
			name:   "zero start time",
			run:    ghclient.BranchRunData{Status: "in_progress"},
			wantFn: func(s string) bool { return s == "-" },
		},
		{
			name: "in_progress with start time",
			run:  ghclient.BranchRunData{Status: "in_progress", RunStartedAt: time.Now().Add(-3 * time.Minute)},
			wantFn: func(s string) bool {
				return s != "-" && s != ""
			},
		},
		{
			name: "queued with start time",
			run:  ghclient.BranchRunData{Status: "queued", RunStartedAt: time.Now().Add(-30 * time.Second)},
			wantFn: func(s string) bool {
				return s != "-" && s != ""
			},
		},
		{
			name: "completed ignores start time",
			run:  ghclient.BranchRunData{Status: "completed", RunStartedAt: time.Now().Add(-3 * time.Minute)},
			wantFn: func(s string) bool {
				return s == "-"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBranchRunDuration(tt.run)
			if !tt.wantFn(got) {
				t.Errorf("formatBranchRunDuration() = %q, unexpected", got)
			}
		})
	}
}

func TestFormatBranchJobName(t *testing.T) {
	tests := []struct {
		name string
		job  ghclient.CheckRunInfo
		want string
	}{
		{
			name: "with workflow name",
			job:  ghclient.CheckRunInfo{Name: "build", WorkflowName: "CI"},
			want: "CI / build",
		},
		{
			name: "without workflow name",
			job:  ghclient.CheckRunInfo{Name: "deploy"},
			want: "deploy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBranchJobName(tt.job)
			if got != tt.want {
				t.Errorf("formatBranchJobName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatBranchJobNameTruncate(t *testing.T) {
	tests := []struct {
		name string
		job  ghclient.CheckRunInfo
		max  int
		want string
	}{
		{
			name: "under limit unchanged",
			job:  ghclient.CheckRunInfo{Name: "build", WorkflowName: "CI"},
			max:  20,
			want: "CI / build",
		},
		{
			name: "over limit truncated with ellipsis",
			job:  ghclient.CheckRunInfo{Name: "a-very-long-job-name-here", WorkflowName: "CI"},
			max:  12,
			want: "CI / a-very…",
		},
		{
			name: "no workflow name over limit",
			job:  ghclient.CheckRunInfo{Name: "a-very-long-job-name-here"},
			max:  10,
			want: "a-very-lo…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBranchJobNameTruncate(tt.job, tt.max)
			if got != tt.want {
				t.Errorf("formatBranchJobNameTruncate() = %q, want %q", got, tt.want)
			}
			if runewidth.StringWidth(got) > tt.max {
				t.Errorf("width = %d exceeds max %d", runewidth.StringWidth(got), tt.max)
			}
		})
	}
}

func TestFormatBranchJobDuration(t *testing.T) {
	now := time.Now()
	startedAt := now.Add(-2 * time.Minute)
	completedAt := now.Add(-1 * time.Minute)

	tests := []struct {
		name string
		job  ghclient.CheckRunInfo
		want string
	}{
		{
			name: "completed with both timestamps",
			job: ghclient.CheckRunInfo{
				Status:      "completed",
				StartedAt:   &startedAt,
				CompletedAt: &completedAt,
			},
			want: "1m 0s",
		},
		{
			name: "completed missing completedAt",
			job:  ghclient.CheckRunInfo{Status: "completed", StartedAt: &startedAt},
			want: "-",
		},
		{
			name: "completed missing startedAt",
			job:  ghclient.CheckRunInfo{Status: "completed", CompletedAt: &completedAt},
			want: "-",
		},
		{
			name: "completed zero duration",
			job:  ghclient.CheckRunInfo{Status: "completed", StartedAt: &now, CompletedAt: &now},
			want: "-",
		},
		{
			name: "in_progress with startedAt",
			job:  ghclient.CheckRunInfo{Status: "in_progress", StartedAt: &startedAt},
			want: "2m 0s",
		},
		{
			name: "in_progress missing startedAt",
			job:  ghclient.CheckRunInfo{Status: "in_progress"},
			want: "-",
		},
		{
			name: "in_progress started in future",
			job:  ghclient.CheckRunInfo{Status: "in_progress", StartedAt: ptrTime(now.Add(1 * time.Hour))},
			want: "-",
		},
		{
			name: "queued",
			job:  ghclient.CheckRunInfo{Status: "queued"},
			want: "-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBranchJobDuration(tt.job)
			if got != tt.want {
				t.Errorf("formatBranchJobDuration() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCalculateBranchRunColumnWidths(t *testing.T) {
	shortJob := ghclient.CheckRunInfo{Name: "build", WorkflowName: "CI"}
	longJob := ghclient.CheckRunInfo{
		Name:         "a-job-name-that-is-quite-long-and-exceeds-the-default",
		WorkflowName: "SomeWorkflow",
	}
	veryLongJob := ghclient.CheckRunInfo{
		Name:         strings.Repeat("x", 100),
		WorkflowName: "WF",
	}
	completedJob := ghclient.CheckRunInfo{
		Status:      "completed",
		StartedAt:   ptrTime(time.Now().Add(-1 * time.Hour)),
		CompletedAt: ptrTime(time.Now()),
	}

	t.Run("empty jobs returns minimums", func(t *testing.T) {
		w := calculateBranchRunColumnWidths(nil)
		if w.nameWidth != 20 || w.durationWidth != 5 {
			t.Errorf("got %+v, want minimums (20, 5)", w)
		}
	})

	t.Run("short job stays at name minimum", func(t *testing.T) {
		w := calculateBranchRunColumnWidths([]ghclient.CheckRunInfo{shortJob})
		if w.nameWidth != 20 {
			t.Errorf("nameWidth = %d, want 20", w.nameWidth)
		}
	})

	t.Run("long job grows to fit", func(t *testing.T) {
		w := calculateBranchRunColumnWidths([]ghclient.CheckRunInfo{shortJob, longJob})
		want := runewidth.StringWidth("SomeWorkflow / " + longJob.Name)
		if want > 60 {
			want = 60 // capped at maxNameWidth
		}
		if w.nameWidth != want {
			t.Errorf("nameWidth = %d, want %d", w.nameWidth, want)
		}
	})

	t.Run("oversize job capped at max", func(t *testing.T) {
		w := calculateBranchRunColumnWidths([]ghclient.CheckRunInfo{veryLongJob})
		if w.nameWidth != 60 {
			t.Errorf("nameWidth = %d, want capped at 60", w.nameWidth)
		}
	})

	t.Run("duration width grows for long durations", func(t *testing.T) {
		w := calculateBranchRunColumnWidths([]ghclient.CheckRunInfo{completedJob})
		if w.durationWidth <= 5 {
			t.Errorf("durationWidth = %d, want > 5", w.durationWidth)
		}
	})
}

func TestGroupBranchRunsByBranch(t *testing.T) {
	m := RepoModel{
		standaloneRuns: []ghclient.BranchRunData{
			{RunID: 1, HeadBranch: "main"},
			{RunID: 2, HeadBranch: "dev"},
			{RunID: 3, HeadBranch: ""},
			{RunID: 4, HeadBranch: "main"},
		},
	}

	groups := m.groupBranchRunsByBranch()

	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3", len(groups))
	}
	if len(groups["main"]) != 2 {
		t.Errorf("main group = %d runs, want 2", len(groups["main"]))
	}
	if len(groups["dev"]) != 1 {
		t.Errorf("dev group = %d runs, want 1", len(groups["dev"]))
	}
	if len(groups["(unknown)"]) != 1 {
		t.Errorf("(unknown) group = %d runs, want 1 (empty branch label)", len(groups["(unknown)"]))
	}
}

func TestSortedBranchNames(t *testing.T) {
	groups := map[string][]ghclient.BranchRunData{
		"zebra":  nil,
		"alpha":  nil,
		"midway": nil,
	}

	got := sortedBranchNames(groups)
	want := []string{"alpha", "midway", "zebra"}

	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sortedBranchNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// makeRepoModelForRender builds a RepoModel with sensible defaults for
// exercising the repo-mode render path.
func makeRepoModelForRender() RepoModel {
	m := NewRepoModel(
		context.Background(),
		"test-token",
		"github.com",
		"owner",
		"repo",
		30*time.Second,
		repoStylesForTest(),
		true,
		15*time.Minute,
		30*time.Minute,
	)
	return m
}

func TestRenderPRGroup(t *testing.T) {
	now := time.Now()
	pushed := now.Add(-10 * time.Minute)
	failureCompleted := now.Add(-2 * time.Minute)

	m := makeRepoModelForRender()

	t.Run("with checks renders header and rows", func(t *testing.T) {
		mm := m
		mm.prs = map[int]PRViewData{
			42: {
				Title: "Add feature",
				CheckRuns: []ghclient.CheckRunInfo{
					{Name: "build", WorkflowName: "CI", Status: "in_progress", StartedAt: ptrTime(now.Add(-1 * time.Minute))},
					{Name: "lint", WorkflowName: "CI", Status: "completed", Conclusion: "success", StartedAt: ptrTime(now.Add(-2 * time.Minute)), CompletedAt: ptrTime(now.Add(-1 * time.Minute))},
				},
				HeadPushedTime: pushed,
			},
		}

		var b strings.Builder
		mm.renderPRGroup(&b, 42, mm.prs[42])
		out := stripANSI(b.String())

		if !strings.Contains(out, "PR #42: Add feature") {
			t.Errorf("output missing PR header, got:\n%s", out)
		}
		if !strings.Contains(out, "CI / build") {
			t.Errorf("output missing build check row, got:\n%s", out)
		}
		if !strings.Contains(out, "CI / lint") {
			t.Errorf("output missing lint check row, got:\n%s", out)
		}
	})

	t.Run("no checks renders placeholder", func(t *testing.T) {
		mm := m
		mm.prs = map[int]PRViewData{
			1: {Title: "Empty"},
		}

		var b strings.Builder
		mm.renderPRGroup(&b, 1, mm.prs[1])
		out := stripANSI(b.String())

		if !strings.Contains(out, "No checks") {
			t.Errorf("output missing 'No checks' placeholder, got:\n%s", out)
		}
	})

	t.Run("failed check styles name", func(t *testing.T) {
		mm := m
		mm.prs = map[int]PRViewData{
			7: {
				Title: "Broken build",
				CheckRuns: []ghclient.CheckRunInfo{
					{Name: "test", WorkflowName: "CI", Status: "completed", Conclusion: "failure", StartedAt: ptrTime(now.Add(-3 * time.Minute)), CompletedAt: &failureCompleted},
				},
				HeadPushedTime: pushed,
			},
		}

		var b strings.Builder
		mm.renderPRGroup(&b, 7, mm.prs[7])
		out := stripANSI(b.String())

		if !strings.Contains(out, "CI / test") {
			t.Errorf("output missing failed check row, got:\n%s", out)
		}
		if !strings.Contains(out, "✗") {
			t.Errorf("output missing failure icon, got:\n%s", out)
		}
	})

	t.Run("extra check runs merged for display", func(t *testing.T) {
		mm := m
		mm.prs = map[int]PRViewData{
			9: {
				Title: "Mixed sources",
				CheckRuns: []ghclient.CheckRunInfo{
					{Name: "build", WorkflowName: "CI", Status: "in_progress", StartedAt: ptrTime(now.Add(-1 * time.Minute))},
				},
				ExtraCheckRuns: []ghclient.CheckRunInfo{
					{Name: "coding-agent", WorkflowName: "Copilot", AppName: "Copilot", Status: "in_progress"},
				},
				HeadPushedTime: pushed,
			},
		}

		var b strings.Builder
		mm.renderPRGroup(&b, 9, mm.prs[9])
		out := stripANSI(b.String())

		if !strings.Contains(out, "CI / build") {
			t.Errorf("output missing GraphQL check row, got:\n%s", out)
		}
		if !strings.Contains(out, "Copilot / coding-agent") {
			t.Errorf("output missing extra check row, got:\n%s", out)
		}
	})
}

func TestRenderRepoCheckRun(t *testing.T) {
	m := makeRepoModelForRender()
	now := time.Now()
	pushed := now.Add(-10 * time.Minute)

	checks := []ghclient.CheckRunInfo{
		{Name: "build", WorkflowName: "CI", Status: "in_progress", StartedAt: ptrTime(now.Add(-1 * time.Minute))},
		{Name: "test", WorkflowName: "CI", Status: "completed", Conclusion: "failure", StartedAt: ptrTime(now.Add(-3 * time.Minute)), CompletedAt: ptrTime(now.Add(-1 * time.Minute))},
		{Name: "lint", WorkflowName: "CI", Status: "completed", Conclusion: "success", StartedAt: ptrTime(now.Add(-2 * time.Minute)), CompletedAt: ptrTime(now.Add(-1 * time.Minute))},
		{Name: "deploy", Status: "queued"},
	}

	widths := CalculateColumnWidths(checks, pushed, nil)
	for _, check := range checks {
		line := m.renderRepoCheckRun(check, pushed, widths)
		if !strings.HasSuffix(line, "\n") {
			t.Errorf("renderRepoCheckRun(%q) should end with newline", check.Name)
		}
		if !strings.Contains(line, FormatCheckName(check)) {
			t.Errorf("renderRepoCheckRun(%q) missing check name, got %q", check.Name, line)
		}
	}
}

func TestRenderStandaloneRunsSection(t *testing.T) {
	m := makeRepoModelForRender()
	now := time.Now()

	m.standaloneRuns = []ghclient.BranchRunData{
		{
			RunID:        101,
			DisplayTitle: "Nightly build",
			HeadBranch:   "main",
			Event:        "schedule",
			Status:       "in_progress",
			RunStartedAt: now.Add(-5 * time.Minute),
			Jobs: []ghclient.CheckRunInfo{
				{Name: "nightly-test", WorkflowName: "Nightly", Status: "in_progress", StartedAt: ptrTime(now.Add(-4 * time.Minute))},
			},
		},
		{
			RunID:        102,
			DisplayTitle: "Manual dispatch",
			HeadBranch:   "dev",
			Event:        "push",
			Status:       "queued",
		},
		{
			RunID:        103,
			DisplayTitle: "",
			WorkflowName: "Fallback WF",
			HeadBranch:   "dev",
			Status:       "completed",
			Conclusion:   "success",
			RunStartedAt: now.Add(-2 * time.Minute),
		},
	}

	var b strings.Builder
	m.renderStandaloneRunsSection(&b)
	out := stripANSI(b.String())

	if !strings.Contains(out, "Branch: main") {
		t.Errorf("output missing 'Branch: main' header, got:\n%s", out)
	}
	if !strings.Contains(out, "Branch: dev") {
		t.Errorf("output missing 'Branch: dev' header (groups must render sorted), got:\n%s", out)
	}
	if !strings.Contains(out, "Nightly build") {
		t.Errorf("output missing run title, got:\n%s", out)
	}
	if !strings.Contains(out, "(schedule)") {
		t.Errorf("output missing event annotation, got:\n%s", out)
	}
	if strings.Contains(out, "(push)") {
		t.Errorf("push event should not be annotated, got:\n%s", out)
	}
	if !strings.Contains(out, "Fallback WF") {
		t.Errorf("output missing workflow-name fallback for empty title, got:\n%s", out)
	}
	if !strings.Contains(out, "Nightly / nightly-test") {
		t.Errorf("output missing job row, got:\n%s", out)
	}
	if !strings.Contains(out, "Waiting for jobs...") {
		t.Errorf("output missing waiting placeholder for active run with no jobs, got:\n%s", out)
	}
}

func TestRenderBranchRunHeader(t *testing.T) {
	m := makeRepoModelForRender()

	tests := []struct {
		name        string
		run         ghclient.BranchRunData
		wantSubstr  string
		wantMissing string
	}{
		{
			name:       "push event not annotated",
			run:        ghclient.BranchRunData{DisplayTitle: "Commit run", Event: "push", Status: "completed", Conclusion: "success"},
			wantSubstr: "Commit run",
		},
		{
			name:       "workflow_dispatch event annotated",
			run:        ghclient.BranchRunData{DisplayTitle: "Dispatched run", Event: "workflow_dispatch", Status: "completed", Conclusion: "success"},
			wantSubstr: "Dispatched run (workflow_dispatch)",
		},
		{
			name:       "variation selectors stripped from title",
			run:        ghclient.BranchRunData{DisplayTitle: "Title ☑️", Event: "push", Status: "completed", Conclusion: "success"},
			wantSubstr: "Title ☑",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b strings.Builder
			m.renderBranchRunHeader(&b, tt.run)
			out := stripANSI(b.String())
			if !strings.Contains(out, tt.wantSubstr) {
				t.Errorf("output missing %q, got:\n%s", tt.wantSubstr, out)
			}
			if tt.wantMissing != "" && strings.Contains(out, tt.wantMissing) {
				t.Errorf("output should not contain %q, got:\n%s", tt.wantMissing, out)
			}
		})
	}
}

func TestRenderBranchRunJob(t *testing.T) {
	m := makeRepoModelForRender()
	now := time.Now()

	jobs := []ghclient.CheckRunInfo{
		{Name: "build", WorkflowName: "CI", Status: "completed", Conclusion: "failure", StartedAt: ptrTime(now.Add(-2 * time.Minute)), CompletedAt: ptrTime(now.Add(-1 * time.Minute))},
		{Name: "test", WorkflowName: "CI", Status: "completed", Conclusion: "success", StartedAt: ptrTime(now.Add(-2 * time.Minute)), CompletedAt: ptrTime(now.Add(-1 * time.Minute))},
	}

	widths := calculateBranchRunColumnWidths(jobs)

	for _, job := range jobs {
		line := m.renderBranchRunJob(job, widths)
		if !strings.HasSuffix(line, "\n") {
			t.Errorf("renderBranchRunJob(%q) should end with newline", job.Name)
		}
		if !strings.Contains(line, "CI / "+job.Name) {
			t.Errorf("renderBranchRunJob(%q) missing job name, got %q", job.Name, line)
		}
	}
}

func TestRepoViewRendering(t *testing.T) {
	now := time.Now()
	pushed := now.Add(-10 * time.Minute)

	t.Run("empty state spinner", func(t *testing.T) {
		m := makeRepoModelForRender()
		out := stripANSI(m.View().Content)
		if !strings.Contains(out, "No active PRs or branch runs...") {
			t.Errorf("empty state missing spinner message, got:\n%s", out)
		}
	})

	t.Run("summary counts", func(t *testing.T) {
		m := makeRepoModelForRender()
		m.prs = map[int]PRViewData{
			1: {Title: "One", CheckRuns: []ghclient.CheckRunInfo{{Status: "in_progress", Name: "build"}}},
		}
		m.standaloneRuns = []ghclient.BranchRunData{
			{RunID: 1, HeadBranch: "main", Status: "in_progress"},
			{RunID: 2, HeadBranch: "main", Status: "in_progress"},
		}
		m.fetchReceived = true
		m.rateLimitRemaining = 4999
		m.lastUpdate = now

		out := stripANSI(m.View().Content)
		if !strings.Contains(out, "1 active PR") {
			t.Errorf("summary missing singular PR count, got:\n%s", out)
		}
		if !strings.Contains(out, "2 branch runs") {
			t.Errorf("summary missing plural branch run count, got:\n%s", out)
		}
	})

	t.Run("rate limit indicators", func(t *testing.T) {
		m := makeRepoModelForRender()
		m.fetchReceived = true
		m.rateLimitRemaining = 5
		if got := stripANSI(m.View().Content); !strings.Contains(got, "[Rate limit: 5 remaining]") {
			t.Errorf("critical rate limit not shown, got:\n%s", got)
		}

		m2 := makeRepoModelForRender()
		m2.fetchReceived = true
		m2.rateLimitRemaining = 200
		if got := stripANSI(m2.View().Content); !strings.Contains(got, "[Rate limit: 200 remaining]") {
			t.Errorf("warning rate limit not shown, got:\n%s", got)
		}

		m3 := makeRepoModelForRender()
		m3.fetchReceived = false
		m3.rateLimitRemaining = 0
		if got := stripANSI(m3.View().Content); strings.Contains(got, "Rate limit") {
			t.Errorf("rate limit must not render before first response, got:\n%s", got)
		}

		m4 := makeRepoModelForRender()
		m4.fetchReceived = true
		m4.rateLimitRemaining = 5000
		if got := stripANSI(m4.View().Content); strings.Contains(got, "Rate limit") {
			t.Errorf("rate limit must not render when healthy, got:\n%s", got)
		}
	})

	t.Run("fetch error status line", func(t *testing.T) {
		m := makeRepoModelForRender()
		m.prs = map[int]PRViewData{
			1: {Title: "One", CheckRuns: []ghclient.CheckRunInfo{{Status: "in_progress", Name: "build"}}, HeadPushedTime: pushed},
		}
		m.fetchErrChecks = errors.New("504 Gateway Timeout")
		m.fetchErrChecksAt = now.Add(-5 * time.Second)

		out := stripANSI(m.View().Content)
		if !strings.Contains(out, "PR checks fetch error") {
			t.Errorf("output missing checks-source error line, got:\n%s", out)
		}
		if !strings.Contains(out, "504 Gateway Timeout") {
			t.Errorf("output missing error text, got:\n%s", out)
		}
	})

	t.Run("runs-source error line takes precedence when newer", func(t *testing.T) {
		m := makeRepoModelForRender()
		m.prs = map[int]PRViewData{
			1: {Title: "One", CheckRuns: []ghclient.CheckRunInfo{{Status: "in_progress", Name: "build"}}, HeadPushedTime: pushed},
		}
		m.fetchErrChecks = errors.New("graphql boom")
		m.fetchErrChecksAt = now.Add(-30 * time.Second)
		m.fetchErrRuns = errors.New("rest boom")
		m.fetchErrRunsAt = now.Add(-5 * time.Second)

		out := stripANSI(m.View().Content)
		if !strings.Contains(out, "Repo runs fetch error") {
			t.Errorf("output missing runs-source error line, got:\n%s", out)
		}
		if strings.Contains(out, "graphql boom") {
			t.Errorf("older checks-source error should be hidden, got:\n%s", out)
		}
	})

	t.Run("quitting hides quit hint", func(t *testing.T) {
		m := makeRepoModelForRender()
		if got := stripANSI(m.View().Content); !strings.Contains(got, "Press q to quit") {
			t.Errorf("quit hint missing while running, got:\n%s", got)
		}

		m.quitting = true
		if got := stripANSI(m.View().Content); strings.Contains(got, "Press q to quit") {
			t.Errorf("quit hint should hide when quitting, got:\n%s", got)
		}
	})

	t.Run("full view with PRs and standalone runs", func(t *testing.T) {
		m := makeRepoModelForRender()
		m.prs = map[int]PRViewData{
			1: {Title: "First PR", CheckRuns: []ghclient.CheckRunInfo{{Status: "in_progress", Name: "build", WorkflowName: "CI", StartedAt: ptrTime(now.Add(-1 * time.Minute))}}, HeadPushedTime: pushed},
			2: {Title: "Second PR", CheckRuns: []ghclient.CheckRunInfo{{Status: "completed", Conclusion: "success", Name: "test", WorkflowName: "CI", CompletedAt: ptrTime(now.Add(-1 * time.Minute))}}, HeadPushedTime: pushed},
		}
		m.standaloneRuns = []ghclient.BranchRunData{
			{RunID: 1, DisplayTitle: "Post-merge run", HeadBranch: "main", Event: "push", Status: "in_progress", RunStartedAt: now.Add(-1 * time.Minute)},
		}
		m.fetchReceived = true
		m.rateLimitRemaining = 4999
		m.lastUpdate = now

		out := stripANSI(m.View().Content)
		if !strings.Contains(out, "PR #1: First PR") {
			t.Errorf("output missing first PR group, got:\n%s", out)
		}
		if !strings.Contains(out, "PR #2: Second PR") {
			t.Errorf("output missing second PR group, got:\n%s", out)
		}
		if !strings.Contains(out, "Branch: main") {
			t.Errorf("output missing branch group, got:\n%s", out)
		}
	})
}
