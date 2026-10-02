package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ghclient "github.com/fini-net/gh-observer/internal/github"
	"github.com/google/go-github/v92/github"
	"github.com/mattn/go-runewidth"
)

// makeRunModelForRender builds a RunModel with sensible defaults for
// exercising the run-mode render path.
func makeRunModelForRender() RunModel {
	return NewRunModel(
		context.Background(),
		"test-token",
		"github.com",
		"owner",
		"repo",
		12345,
		10*time.Second,
		NewStyles(2, 1, 3, 8),
		true,
		false,
		nil,
	)
}

// TestRunView_AltScreenEnabled guards against issue #451 recurring: every
// View() return path must set AltScreen, since bubbletea v2 toggles the
// terminal's alternate-screen mode based on any mismatch between frames,
// and a single path reverting to a raw tea.NewView(...) would reintroduce
// the stale-frame bug this fix addresses.
func TestRunView_AltScreenEnabled(t *testing.T) {
	t.Run("startup phase", func(t *testing.T) {
		m := &RunModel{}
		if !m.View().AltScreen {
			t.Error("startup-phase View() should have AltScreen enabled")
		}
	})

	t.Run("error path", func(t *testing.T) {
		m := &RunModel{err: errors.New("boom")}
		if !m.View().AltScreen {
			t.Error("error-path View() should have AltScreen enabled")
		}
	})
}

func TestFormatRunJobName(t *testing.T) {
	tests := []struct {
		name string
		job  ghclient.WorkflowJobInfo
		want string
	}{
		{
			name: "with workflow name",
			job:  ghclient.WorkflowJobInfo{Name: "build", WorkflowName: "CI"},
			want: "CI / build",
		},
		{
			name: "without workflow name",
			job:  ghclient.WorkflowJobInfo{Name: "deploy"},
			want: "deploy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatRunJobName(tt.job)
			if got != tt.want {
				t.Errorf("FormatRunJobName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatRunJobNameWithTruncate(t *testing.T) {
	tests := []struct {
		name string
		job  ghclient.WorkflowJobInfo
		max  int
		want string
	}{
		{
			name: "under limit unchanged",
			job:  ghclient.WorkflowJobInfo{Name: "build", WorkflowName: "CI"},
			max:  20,
			want: "CI / build",
		},
		{
			name: "over limit truncates job part",
			job:  ghclient.WorkflowJobInfo{Name: "a-very-long-job-name-here", WorkflowName: "CI"},
			max:  12,
			want: "CI / a-very…",
		},
		{
			name: "prefix wider than max truncates whole name",
			job:  ghclient.WorkflowJobInfo{Name: "job", WorkflowName: "an-extremely-long-workflow-name"},
			max:  10,
			want: "an-extrem…",
		},
		{
			name: "no workflow over limit truncates name",
			job:  ghclient.WorkflowJobInfo{Name: "a-very-long-job-name-here"},
			max:  10,
			want: "a-very-lo…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatRunJobNameWithTruncate(tt.job, tt.max)
			if got != tt.want {
				t.Errorf("FormatRunJobNameWithTruncate() = %q, want %q", got, tt.want)
			}
			if runewidth.StringWidth(got) > tt.max {
				t.Errorf("width = %d exceeds max %d", runewidth.StringWidth(got), tt.max)
			}
		})
	}
}

func TestBuildRunJobNameColumn(t *testing.T) {
	widths := RunColumnWidths{NameWidth: 20, DurationWidth: 5, AvgWidth: 5}

	t.Run("pads short names to width", func(t *testing.T) {
		job := ghclient.WorkflowJobInfo{Name: "build", WorkflowName: "CI"}
		got := BuildRunJobNameColumn(job, widths, false)
		if runewidth.StringWidth(got) != widths.NameWidth {
			t.Errorf("column width = %d, want %d", runewidth.StringWidth(got), widths.NameWidth)
		}
		if !strings.HasPrefix(got, "CI / build") {
			t.Errorf("column = %q, want prefix %q", got, "CI / build")
		}
	})

	t.Run("wraps name in hyperlink when enabled", func(t *testing.T) {
		job := ghclient.WorkflowJobInfo{Name: "build", WorkflowName: "CI", HTMLURL: "https://github.com/owner/repo/actions/runs/1/job/1"}
		got := BuildRunJobNameColumn(job, widths, true)
		if !strings.Contains(got, "https://github.com/owner/repo/actions/runs/1/job/1") {
			t.Errorf("column should contain hyperlink URL, got %q", got)
		}
	})

	t.Run("no hyperlink when URL empty", func(t *testing.T) {
		job := ghclient.WorkflowJobInfo{Name: "build"}
		got := BuildRunJobNameColumn(job, widths, true)
		if strings.Contains(got, "\x1b]8") {
			t.Errorf("column should not contain OSC 8 hyperlink, got %q", got)
		}
	})
}

func TestFormatRunJobDuration(t *testing.T) {
	now := time.Now()
	startedAt := &github.Timestamp{Time: now.Add(-2 * time.Minute)}
	completedAt := &github.Timestamp{Time: now.Add(-1 * time.Minute)}

	tests := []struct {
		name string
		job  ghclient.WorkflowJobInfo
		want string
	}{
		{
			name: "completed with timestamps",
			job:  ghclient.WorkflowJobInfo{Status: "completed", StartedAt: startedAt, CompletedAt: completedAt},
			want: "1m 0s",
		},
		{
			name: "completed missing timestamps",
			job:  ghclient.WorkflowJobInfo{Status: "completed"},
			want: "-",
		},
		{
			name: "in_progress with startedAt",
			job:  ghclient.WorkflowJobInfo{Status: "in_progress", StartedAt: startedAt},
			want: "2m 0s",
		},
		{
			name: "in_progress missing startedAt",
			job:  ghclient.WorkflowJobInfo{Status: "in_progress"},
			want: "-",
		},
		{
			name: "queued",
			job:  ghclient.WorkflowJobInfo{Status: "queued"},
			want: "-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatRunJobDuration(tt.job)
			if got != tt.want {
				t.Errorf("FormatRunJobDuration() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatRunJobAvg(t *testing.T) {
	averages := map[string]time.Duration{"build": 90 * time.Second}

	tests := []struct {
		name     string
		job      ghclient.WorkflowJobInfo
		averages map[string]time.Duration
		want     string
	}{
		{name: "nil averages map", job: ghclient.WorkflowJobInfo{Name: "build"}, averages: nil, want: "--"},
		{name: "missing job name", job: ghclient.WorkflowJobInfo{Name: "test"}, averages: averages, want: "--"},
		{name: "known job name", job: ghclient.WorkflowJobInfo{Name: "build"}, averages: averages, want: "1m 30s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatRunJobAvg(tt.job, tt.averages)
			if got != tt.want {
				t.Errorf("FormatRunJobAvg() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSortRunJobs(t *testing.T) {
	now := time.Now()
	started := &github.Timestamp{Time: now.Add(-10 * time.Minute)}

	tests := []struct {
		name     string
		jobs     []ghclient.WorkflowJobInfo
		wantName string
	}{
		{
			name: "shortest duration first",
			jobs: []ghclient.WorkflowJobInfo{
				{Name: "slow", Status: "completed", StartedAt: started, CompletedAt: &github.Timestamp{Time: now.Add(-1 * time.Minute)}},
				{Name: "fast", Status: "completed", StartedAt: started, CompletedAt: &github.Timestamp{Time: now.Add(-9 * time.Minute)}},
			},
			wantName: "fast",
		},
		{
			name: "in_progress before queued",
			jobs: []ghclient.WorkflowJobInfo{
				{Name: "queued-job", Status: "queued"},
				{Name: "running-job", Status: "in_progress", StartedAt: &github.Timestamp{Time: now.Add(-1 * time.Minute)}},
			},
			wantName: "running-job",
		},
		{
			name: "alphabetical tie-break",
			jobs: []ghclient.WorkflowJobInfo{
				{Name: "zeta", Status: "queued"},
				{Name: "alpha", Status: "queued"},
			},
			wantName: "alpha",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SortRunJobs(tt.jobs)
			if tt.jobs[0].Name != tt.wantName {
				t.Errorf("SortRunJobs()[0] = %q, want %q", tt.jobs[0].Name, tt.wantName)
			}
		})
	}
}

func TestCalculateRunColumnWidths(t *testing.T) {
	now := time.Now()

	t.Run("empty jobs returns minimums", func(t *testing.T) {
		w := CalculateRunColumnWidths(nil, nil)
		if w.NameWidth != 20 || w.DurationWidth != 5 || w.AvgWidth != 5 {
			t.Errorf("got %+v, want minimums", w)
		}
	})

	t.Run("long name capped at max", func(t *testing.T) {
		jobs := []ghclient.WorkflowJobInfo{{Name: strings.Repeat("x", 100), WorkflowName: "WF"}}
		w := CalculateRunColumnWidths(jobs, nil)
		if w.NameWidth != 60 {
			t.Errorf("NameWidth = %d, want 60", w.NameWidth)
		}
	})

	t.Run("name grows within cap", func(t *testing.T) {
		jobs := []ghclient.WorkflowJobInfo{{Name: "medium-length-job", WorkflowName: "CI"}}
		w := CalculateRunColumnWidths(jobs, nil)
		want := runewidth.StringWidth("CI / medium-length-job")
		if w.NameWidth != want {
			t.Errorf("NameWidth = %d, want %d", w.NameWidth, want)
		}
	})

	t.Run("avg width grows for long averages", func(t *testing.T) {
		jobs := []ghclient.WorkflowJobInfo{{Name: "build"}}
		averages := map[string]time.Duration{"build": 2 * time.Hour}
		w := CalculateRunColumnWidths(jobs, averages)
		if w.AvgWidth <= 5 {
			t.Errorf("AvgWidth = %d, want > 5", w.AvgWidth)
		}
	})

	t.Run("duration width grows for long durations", func(t *testing.T) {
		jobs := []ghclient.WorkflowJobInfo{
			{Status: "completed", StartedAt: &github.Timestamp{Time: now.Add(-3 * time.Hour)}, CompletedAt: &github.Timestamp{Time: now}},
		}
		w := CalculateRunColumnWidths(jobs, nil)
		if w.DurationWidth <= 5 {
			t.Errorf("DurationWidth = %d, want > 5", w.DurationWidth)
		}
	})
}

func TestFormatRunHeaderColumns(t *testing.T) {
	widths := RunColumnWidths{NameWidth: 20, DurationWidth: 10, AvgWidth: 8}
	name, duration, avg := FormatRunHeaderColumns(widths)

	if !strings.HasPrefix(name, "Workflow/Job") {
		t.Errorf("name header = %q, want prefix %q", name, "Workflow/Job")
	}
	if !strings.HasSuffix(duration, "ThisRun") {
		t.Errorf("duration header = %q, want suffix %q", duration, "ThisRun")
	}
	if !strings.HasSuffix(avg, "HistAvg") {
		t.Errorf("avg header = %q, want suffix %q", avg, "HistAvg")
	}

	// Narrow widths must not produce negative padding (max(x, 0)).
	narrow := RunColumnWidths{NameWidth: 5, DurationWidth: 3, AvgWidth: 3}
	nName, nDuration, nAvg := FormatRunHeaderColumns(narrow)
	if strings.Contains(nName, "\x00") || nDuration == "" || nAvg == "" {
		t.Errorf("narrow headers malformed: %q %q %q", nName, nDuration, nAvg)
	}
}

func TestTimestampToTimePtr(t *testing.T) {
	t.Run("nil timestamp returns nil", func(t *testing.T) {
		if got := timestampToTimePtr(nil); got != nil {
			t.Errorf("timestampToTimePtr(nil) = %v, want nil", got)
		}
	})

	t.Run("non-nil timestamp converts", func(t *testing.T) {
		ts := &github.Timestamp{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
		got := timestampToTimePtr(ts)
		if got == nil || !got.Equal(ts.Time) {
			t.Errorf("timestampToTimePtr() = %v, want %v", got, ts.Time)
		}
	})
}

func TestRenderRunJob(t *testing.T) {
	m := makeRunModelForRender()
	now := time.Now()

	jobs := []ghclient.WorkflowJobInfo{
		{Name: "build", WorkflowName: "CI", Status: "in_progress", StartedAt: &github.Timestamp{Time: now.Add(-1 * time.Minute)}},
		{Name: "test", WorkflowName: "CI", Status: "completed", Conclusion: "failure", StartedAt: &github.Timestamp{Time: now.Add(-3 * time.Minute)}, CompletedAt: &github.Timestamp{Time: now.Add(-1 * time.Minute)}},
		{Name: "lint", WorkflowName: "CI", Status: "completed", Conclusion: "success", StartedAt: &github.Timestamp{Time: now.Add(-2 * time.Minute)}, CompletedAt: &github.Timestamp{Time: now.Add(-1 * time.Minute)}},
		{Name: "deploy", WorkflowName: "CI", Status: "completed", Conclusion: "cancelled", StartedAt: &github.Timestamp{Time: now.Add(-2 * time.Minute)}, CompletedAt: &github.Timestamp{Time: now.Add(-1 * time.Minute)}},
		{Name: "scan", WorkflowName: "CI", Status: "completed", Conclusion: "skipped", StartedAt: &github.Timestamp{Time: now.Add(-2 * time.Minute)}, CompletedAt: &github.Timestamp{Time: now.Add(-1 * time.Minute)}},
		{Name: "audit", WorkflowName: "CI", Status: "completed", Conclusion: "action_required", StartedAt: &github.Timestamp{Time: now.Add(-2 * time.Minute)}, CompletedAt: &github.Timestamp{Time: now.Add(-1 * time.Minute)}},
		{Name: "weird", WorkflowName: "CI", Status: "completed", Conclusion: "bizarre", StartedAt: &github.Timestamp{Time: now.Add(-2 * time.Minute)}, CompletedAt: &github.Timestamp{Time: now.Add(-1 * time.Minute)}},
		{Name: "hold", WorkflowName: "CI", Status: "waiting"},
		{Name: "mystery", WorkflowName: "CI", Status: "obscure"},
	}

	widths := CalculateRunColumnWidths(jobs, nil)
	for _, job := range jobs {
		line := m.renderRunJob(job, widths)
		if !strings.HasSuffix(line, "\n") {
			t.Errorf("renderRunJob(%q) should end with newline", job.Name)
		}
		if !strings.Contains(line, job.Name) {
			t.Errorf("renderRunJob(%q) missing job name, got %q", job.Name, line)
		}
	}
}

func TestRunViewView(t *testing.T) {
	now := time.Now()
	pushed := &github.Timestamp{Time: now.Add(-10 * time.Minute)}
	created := &github.Timestamp{Time: now.Add(-12 * time.Minute)}

	t.Run("error state renders error message", func(t *testing.T) {
		m := makeRunModelForRender()
		m.err = errors.New("kaboom")
		out := stripANSI(m.View().Content)
		if !strings.Contains(out, "Error: kaboom") {
			t.Errorf("error path missing message, got:\n%s", out)
		}
	})

	t.Run("startup phase before info loaded", func(t *testing.T) {
		m := makeRunModelForRender()
		out := stripANSI(m.View().Content)
		if !strings.Contains(out, "Loading run info") {
			t.Errorf("startup missing loading message, got:\n%s", out)
		}
	})

	t.Run("slow startup shows no-jobs hint", func(t *testing.T) {
		m := makeRunModelForRender()
		m.startTime = now.Add(-3 * time.Minute)
		out := stripANSI(m.View().Content)
		if !strings.Contains(out, "No jobs found.") {
			t.Errorf("slow startup missing hint, got:\n%s", out)
		}
	})

	t.Run("loaded info with pushed time", func(t *testing.T) {
		m := makeRunModelForRender()
		m.runInfoLoaded = true
		m.runInfo = ghclient.RunInfo{DisplayTitle: "My Run", HeadPushedTime: pushed}
		m.lastUpdate = now
		out := stripANSI(m.View().Content)
		if !strings.Contains(out, "Pushed") {
			t.Errorf("header missing pushed line, got:\n%s", out)
		}
	})

	t.Run("loaded info falls back to created time", func(t *testing.T) {
		m := makeRunModelForRender()
		m.runInfoLoaded = true
		m.runInfo = ghclient.RunInfo{DisplayTitle: "My Run", CreatedAt: created}
		m.lastUpdate = now
		out := stripANSI(m.View().Content)
		if !strings.Contains(out, "Created") {
			t.Errorf("header missing created line, got:\n%s", out)
		}
	})

	t.Run("loaded info with no timestamps", func(t *testing.T) {
		m := makeRunModelForRender()
		m.runInfoLoaded = true
		m.runInfo = ghclient.RunInfo{DisplayTitle: "My Run"}
		m.lastUpdate = now
		out := stripANSI(m.View().Content)
		if strings.Contains(out, "Pushed") || strings.Contains(out, "Created") {
			t.Errorf("header should not show pushed/created, got:\n%s", out)
		}
	})

	t.Run("fetching averages indicator", func(t *testing.T) {
		m := makeRunModelForRender()
		m.runInfoLoaded = true
		m.runInfo = ghclient.RunInfo{DisplayTitle: "My Run"}
		m.lastUpdate = now
		m.avgFetchPending = true
		m.avgFetchStartTime = now
		out := stripANSI(m.View().Content)
		if !strings.Contains(out, "Fetching historical averages") {
			t.Errorf("missing fetching indicator, got:\n%s", out)
		}
	})

	t.Run("averages unavailable indicator", func(t *testing.T) {
		m := makeRunModelForRender()
		m.runInfoLoaded = true
		m.runInfo = ghclient.RunInfo{DisplayTitle: "My Run"}
		m.lastUpdate = now
		m.avgFetchErr = errors.New("history unavailable")
		out := stripANSI(m.View().Content)
		if !strings.Contains(out, "Historical averages unavailable") {
			t.Errorf("missing unavailable indicator, got:\n%s", out)
		}
	})

	t.Run("averages ready indicator", func(t *testing.T) {
		m := makeRunModelForRender()
		m.runInfoLoaded = true
		m.runInfo = ghclient.RunInfo{DisplayTitle: "My Run"}
		m.lastUpdate = now
		m.avgFetchLastDuration = 5 * time.Second
		m.fetchedWorkflowIDs = map[int64]bool{1: true}
		out := stripANSI(m.View().Content)
		if !strings.Contains(out, "Historical averages ready") {
			t.Errorf("missing ready indicator, got:\n%s", out)
		}
	})

	t.Run("no indicators in quick mode", func(t *testing.T) {
		m := makeRunModelForRender()
		m.noAvg = true
		m.runInfoLoaded = true
		m.runInfo = ghclient.RunInfo{DisplayTitle: "My Run"}
		m.lastUpdate = now
		m.avgFetchErr = errors.New("history unavailable")
		out := stripANSI(m.View().Content)
		if strings.Contains(out, "Historical averages") {
			t.Errorf("quick mode should hide average indicators, got:\n%s", out)
		}
	})

	t.Run("jobs table with rate limit indicators", func(t *testing.T) {
		m := makeRunModelForRender()
		m.runInfoLoaded = true
		m.runInfo = ghclient.RunInfo{DisplayTitle: "My Run"}
		m.lastUpdate = now
		m.jobs = []ghclient.WorkflowJobInfo{
			{Name: "build", WorkflowName: "CI", Status: "completed", Conclusion: "success", StartedAt: &github.Timestamp{Time: now.Add(-2 * time.Minute)}, CompletedAt: &github.Timestamp{Time: now.Add(-1 * time.Minute)}},
		}
		m.jobAverages = map[string]time.Duration{"build": 60 * time.Second}
		m.fetchReceived = true
		m.rateLimitRemaining = 50
		out := stripANSI(m.View().Content)
		if !strings.Contains(out, "[Rate limit: 50 remaining]") {
			t.Errorf("missing rate limit indicator, got:\n%s", out)
		}

		m.rateLimitRemaining = 250
		out = stripANSI(m.View().Content)
		if !strings.Contains(out, "[Rate limit: 250 remaining]") {
			t.Errorf("missing warning rate limit indicator, got:\n%s", out)
		}

		m.rateLimitRemaining = 5000
		out = stripANSI(m.View().Content)
		if strings.Contains(out, "Rate limit") {
			t.Errorf("healthy rate limit should hide indicator, got:\n%s", out)
		}
	})

	t.Run("quitting hides quit hint", func(t *testing.T) {
		m := makeRunModelForRender()
		m.jobs = []ghclient.WorkflowJobInfo{{Name: "build", Status: "in_progress"}}
		if got := stripANSI(m.View().Content); !strings.Contains(got, "Press q to quit") {
			t.Errorf("quit hint missing while running, got:\n%s", got)
		}

		m.quitting = true
		if got := stripANSI(m.View().Content); strings.Contains(got, "Press q to quit") {
			t.Errorf("quit hint should hide when quitting, got:\n%s", got)
		}
	})
}

func TestNewRunModelExitCode(t *testing.T) {
	m := NewRunModel(
		context.Background(),
		"test-token",
		"github.com",
		"owner",
		"repo",
		99,
		5*time.Second,
		NewStyles(2, 1, 3, 8),
		false,
		false,
		map[string]time.Duration{"dco": time.Second},
	)

	if m.ExitCode() != 0 {
		t.Errorf("fresh model ExitCode() = %d, want 0", m.ExitCode())
	}
	m.exitCode = 1
	if m.ExitCode() != 1 {
		t.Errorf("ExitCode() = %d, want 1", m.ExitCode())
	}
}
