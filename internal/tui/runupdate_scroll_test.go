package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	ghclient "github.com/fini-net/gh-observer/internal/github"
)

// makeRunScrollModel builds a sized run model with jobCount in-progress
// jobs. The body renders the table header, a blank, one row per job, and a
// trailing blank: jobCount+3 lines. In a 16-line terminal the pinned header
// is 3 rows and the footer 3 (blank, blank, hint), leaving a 10-row
// viewport — so 20 jobs overflow it by 13 rows.
func makeRunScrollModel(t *testing.T, jobCount int) *RunModel {
	t.Helper()
	m := makeRunModel()
	m.runInfoLoaded = true
	m.runInfo = ghclient.RunInfo{DisplayTitle: "Scroll Test Run"}
	m.lastUpdate = time.Now()
	for i := 0; i < jobCount; i++ {
		m.jobs = append(m.jobs, ghclient.WorkflowJobInfo{
			Status:       "in_progress",
			WorkflowName: "CI",
			Name:         fmt.Sprintf("job %02d", i),
		})
	}
	newModel, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	return runModelFrom(t, newModel)
}

// pressRunKeys sends each key through Update and returns the resulting model.
func pressRunKeys(t *testing.T, m *RunModel, keys ...tea.KeyPressMsg) *RunModel {
	t.Helper()
	for _, key := range keys {
		newModel, _ := m.Update(key)
		m = runModelFrom(t, newModel)
	}
	return m
}

func TestRunUpdateScrollKeys(t *testing.T) {
	tests := []struct {
		name string
		keys []tea.KeyPressMsg
		want int
	}{
		{"j", []tea.KeyPressMsg{keyJ}, 1},
		{"j then k", []tea.KeyPressMsg{keyJ, keyK}, 0},
		{"k at top", []tea.KeyPressMsg{keyK}, 0},
		{"pgdown", []tea.KeyPressMsg{tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown})}, 10},
		{"ctrl+f", []tea.KeyPressMsg{tea.KeyPressMsg(tea.Key{Code: 'f', Mod: tea.ModCtrl})}, 10},
		{"space", []tea.KeyPressMsg{tea.KeyPressMsg(tea.Key{Text: " ", Code: tea.KeySpace})}, 10},
		{"ctrl+b", []tea.KeyPressMsg{tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown}), tea.KeyPressMsg(tea.Key{Code: 'b', Mod: tea.ModCtrl})}, 0},
		{"ctrl+d", []tea.KeyPressMsg{tea.KeyPressMsg(tea.Key{Code: 'd', Mod: tea.ModCtrl})}, 5},
		{"f", []tea.KeyPressMsg{tea.KeyPressMsg(tea.Key{Text: "f", Code: 'f'})}, 10},
		{"b", []tea.KeyPressMsg{tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown}), tea.KeyPressMsg(tea.Key{Text: "b", Code: 'b'})}, 0},
		{"d", []tea.KeyPressMsg{tea.KeyPressMsg(tea.Key{Text: "d", Code: 'd'})}, 5},
		{"u", []tea.KeyPressMsg{tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown}), tea.KeyPressMsg(tea.Key{Text: "u", Code: 'u'})}, 5},
		{"G", []tea.KeyPressMsg{keyShiftG}, 13},
		{"G clamps", []tea.KeyPressMsg{keyShiftG, keyJ}, 13},
		{"gg", []tea.KeyPressMsg{keyShiftG, keyG, keyG}, 0},
		{"g then j", []tea.KeyPressMsg{keyG, keyJ}, 1},
		{"end then home", []tea.KeyPressMsg{tea.KeyPressMsg(tea.Key{Code: tea.KeyEnd}), tea.KeyPressMsg(tea.Key{Code: tea.KeyHome})}, 0},
		{"l ignored", []tea.KeyPressMsg{tea.KeyPressMsg(tea.Key{Text: "l", Code: 'l'})}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rm := pressRunKeys(t, makeRunScrollModel(t, 20), tt.keys...)
			if got := rm.viewport.YOffset(); got != tt.want {
				t.Errorf("YOffset = %d, want %d", got, tt.want)
			}
			if got := rm.viewport.XOffset(); got != 0 {
				t.Errorf("XOffset = %d, want 0", got)
			}
		})
	}
}

func TestRunUpdateScrollClampsWhenContentShrinks(t *testing.T) {
	m := pressRunKeys(t, makeRunScrollModel(t, 20), keyShiftG)
	newModel, _ := m.Update(RunJobsUpdateMsg{
		Jobs: []ghclient.WorkflowJobInfo{{Status: "in_progress", Name: "build"}},
	})
	rm := runModelFrom(t, newModel)
	if got := rm.viewport.YOffset(); got != 0 {
		t.Errorf("YOffset = %d after content shrank, want 0", got)
	}
}

func TestRunUpdateScrollClampsWhenViewportGrows(t *testing.T) {
	m := pressRunKeys(t, makeRunScrollModel(t, 20), keyShiftG)
	// Grow the terminal past the content: the 23-line body now fits a
	// 34-row viewport, so the bottom-anchored offset must clamp to 0.
	newModel, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	rm := runModelFrom(t, newModel)
	if got := rm.viewport.YOffset(); got != 0 {
		t.Errorf("YOffset = %d after viewport grew past content, want 0", got)
	}
}

func TestRunViewScrolling(t *testing.T) {
	t.Run("top of overflowing view", func(t *testing.T) {
		out := stripANSI(makeRunScrollModel(t, 20).View().Content)
		if lines := strings.Count(out, "\n") + 1; lines != 16 {
			t.Errorf("view has %d lines, want 16", lines)
		}
		if !strings.Contains(out, "Scroll Test Run") || !strings.Contains(out, "[Top]") {
			t.Errorf("view missing pinned header or footer, got:\n%s", out)
		}
		if strings.Contains(out, "job 19") {
			t.Errorf("view should not show rows past the viewport, got:\n%s", out)
		}
	})

	t.Run("bottom of overflowing view", func(t *testing.T) {
		m := pressRunKeys(t, makeRunScrollModel(t, 20), keyShiftG)
		out := stripANSI(m.View().Content)
		if !strings.Contains(out, "[Bot]") || !strings.Contains(out, "job 19") {
			t.Errorf("bottom view missing [Bot] or last job, got:\n%s", out)
		}
		// Header and footer stay pinned while scrolled.
		if !strings.Contains(out, "Scroll Test Run") {
			t.Errorf("bottom view missing pinned header, got:\n%s", out)
		}
	})

	t.Run("non-overflowing view keeps plain hint", func(t *testing.T) {
		out := stripANSI(makeRunScrollModel(t, 1).View().Content)
		if !strings.Contains(out, "Press q to quit") {
			t.Errorf("hint missing, got:\n%s", out)
		}
		if strings.Contains(out, "j/k scroll") {
			t.Errorf("scroll hints should hide when content fits, got:\n%s", out)
		}
	})

	t.Run("scroll hint hides when quitting", func(t *testing.T) {
		m := pressRunKeys(t, makeRunScrollModel(t, 20), keyShiftG)
		m.quitting = true
		out := stripANSI(m.View().Content)
		if strings.Contains(out, "j/k scroll") {
			t.Errorf("scroll hints should hide when quitting, got:\n%s", out)
		}
	})
}

// TestRunUpdateScrollKeysBeforeWindowSize pins the no-op guard in
// scrollState.handleKey: scroll keys typed before the first
// tea.WindowSizeMsg must not move an unsized viewport (height 0) and must
// not panic.
func TestRunUpdateScrollKeysBeforeWindowSize(t *testing.T) {
	m := makeRunModel()
	keys := []tea.KeyPressMsg{
		keyJ,
		keyShiftG,
		keyG, keyG,
		tea.KeyPressMsg(tea.Key{Text: " ", Code: tea.KeySpace}),
	}
	for _, key := range keys {
		newModel, cmd := m.Update(key)
		rm := runModelFrom(t, newModel)
		if got := rm.viewport.YOffset(); got != 0 {
			t.Errorf("YOffset = %d after %q before WindowSizeMsg, want 0", got, key.String())
		}
		if cmd != nil {
			t.Errorf("Update returned a command after %q before WindowSizeMsg, want nil", key.String())
		}
		m = rm
	}
}

// TestRunViewScrollingStartupPhase covers the sized-terminal startup path:
// with no run info (empty header) and no jobs (empty footer), the viewport
// fills the whole terminal through composeView's empty-footer branch.
func TestRunViewScrollingStartupPhase(t *testing.T) {
	m := makeRunModel()
	newModel, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	rm := runModelFrom(t, newModel)
	out := stripANSI(rm.View().Content)
	if lines := strings.Count(out, "\n") + 1; lines != 16 {
		t.Errorf("view has %d lines, want 16", lines)
	}
	if !strings.Contains(out, "Loading run info") {
		t.Errorf("view missing startup text, got:\n%s", out)
	}
	if strings.Contains(out, "Press q to quit") {
		t.Errorf("startup-phase footer should be empty, got:\n%s", out)
	}
}

// TestRunUpdateScrollPreservedAcrossRefresh pins the watcher UX: a
// same-sized RunJobsUpdateMsg — the routine poll refresh — must not yank the
// scroll position back to the top. SetContent only clamps an offset that
// exceeds the content, so identical-height content keeps it.
func TestRunUpdateScrollPreservedAcrossRefresh(t *testing.T) {
	m := pressRunKeys(t, makeRunScrollModel(t, 20), keyJ, keyJ, keyJ) // offset 3
	refreshed := make([]ghclient.WorkflowJobInfo, len(m.jobs))
	copy(refreshed, m.jobs)
	newModel, _ := m.Update(RunJobsUpdateMsg{Jobs: refreshed, RateLimitRemaining: 5000})
	rm := runModelFrom(t, newModel)
	if got := rm.viewport.YOffset(); got != 3 {
		t.Errorf("YOffset = %d after same-sized refresh, want 3", got)
	}
}

// TestRunFullViewUnclipped also pins that the quit-time reprint carries no
// scroll hints: renderFooter gates the hint on !quitting, so the summary
// lands in scrollback with a plain tail, not a stale "j/k scroll" ruler.
func TestRunFullViewUnclipped(t *testing.T) {
	// Auto-quit while scrolled to the bottom: the scrollback reprint
	// (printFinalFrame) must render the full body, not the scrolled window.
	m := pressRunKeys(t, makeRunScrollModel(t, 20), keyShiftG)
	m.quitting = true

	if out := stripANSI(m.View().Content); strings.Contains(out, "job 00") {
		t.Errorf("scrolled view should not show the first job, got:\n%s", out)
	}

	full := stripANSI(m.FullView())
	for _, want := range []string{"job 00", "job 19", "Scroll Test Run"} {
		if !strings.Contains(full, want) {
			t.Errorf("FullView missing %q, got:\n%s", want, full)
		}
	}
	if strings.Contains(full, "j/k scroll") {
		t.Errorf("FullView should not print scroll hints at quit time, got:\n%s", full)
	}
}
