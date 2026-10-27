package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	ghclient "github.com/fini-net/gh-observer/internal/github"
)

// prModelFrom extracts a *Model from the tea.Model returned by Update, which
// may be either Model or *Model (the data handlers use pointer receivers).
func prModelFrom(t *testing.T, m tea.Model) *Model {
	t.Helper()
	switch v := m.(type) {
	case Model:
		return &v
	case *Model:
		return v
	default:
		t.Fatalf("Update returned %T, want Model or *Model", m)
		return nil
	}
}

// makePRScrollModel builds a sized PR model with checkCount in-progress
// checks. The body renders the table header, a blank, one row per check,
// and a trailing blank: checkCount+3 lines. In a 16-line terminal the
// pinned header is 3 rows and the footer 3 (blank, blank, hint), leaving a
// 10-row viewport — so 20 checks overflow it by 13 rows.
func makePRScrollModel(t *testing.T, checkCount int) *Model {
	t.Helper()
	m := makeModel()
	// The zero-value viewport self-initializes with the DEFAULT keymap
	// (no ctrl+f/ctrl+b) on first Update, so the fixture must wire the
	// shared scroll keymap exactly like NewModel does.
	m.scrollState = newScrollState()
	m.styles = stylesForTest()
	m.prTitle = "Scroll Test PR"
	m.prNumber = 1
	m.lastUpdate = time.Now()
	for i := 0; i < checkCount; i++ {
		m.checkRuns = append(m.checkRuns, ghclient.CheckRunInfo{
			Status:       "in_progress",
			WorkflowName: "CI",
			Name:         fmt.Sprintf("check %02d", i),
		})
	}
	newModel, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	return prModelFrom(t, newModel)
}

// pressPRKeys sends each key through Update and returns the resulting model.
func pressPRKeys(t *testing.T, m *Model, keys ...tea.KeyPressMsg) *Model {
	t.Helper()
	for _, key := range keys {
		newModel, _ := m.Update(key)
		m = prModelFrom(t, newModel)
	}
	return m
}

func TestPRUpdateKeyMsgQuits(t *testing.T) {
	keys := []tea.KeyPressMsg{
		tea.KeyPressMsg(tea.Key{Text: "q", Code: 'q'}),
		tea.KeyPressMsg(tea.Key{Text: "ctrl+c", Code: 3, Mod: tea.ModCtrl}),
	}
	for _, key := range keys {
		t.Run(key.String(), func(t *testing.T) {
			m := makePRScrollModel(t, 1)
			newModel, cmd := m.Update(key)
			pm := prModelFrom(t, newModel)
			if !pm.quitting {
				t.Error("quitting should be true after key press")
			}
			if cmd == nil {
				t.Error("Update should return a quit command")
			}
		})
	}
}

func TestPRUpdateScrollKeys(t *testing.T) {
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
			pm := pressPRKeys(t, makePRScrollModel(t, 20), tt.keys...)
			if got := pm.viewport.YOffset(); got != tt.want {
				t.Errorf("YOffset = %d, want %d", got, tt.want)
			}
			if got := pm.viewport.XOffset(); got != 0 {
				t.Errorf("XOffset = %d, want 0", got)
			}
		})
	}
}

func TestPRUpdateScrollClampsWhenContentShrinks(t *testing.T) {
	m := pressPRKeys(t, makePRScrollModel(t, 20), keyShiftG)
	newModel, _ := m.Update(ChecksUpdateMsg{
		CheckRuns: []ghclient.CheckRunInfo{{Status: "in_progress", Name: "build"}},
	})
	pm := prModelFrom(t, newModel)
	if got := pm.viewport.YOffset(); got != 0 {
		t.Errorf("YOffset = %d after content shrank, want 0", got)
	}
}

func TestPRUpdateScrollClampsWhenViewportGrows(t *testing.T) {
	m := pressPRKeys(t, makePRScrollModel(t, 20), keyShiftG)
	// Grow the terminal past the content: the 23-line body now fits a
	// 34-row viewport, so the bottom-anchored offset must clamp to 0.
	newModel, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	pm := prModelFrom(t, newModel)
	if got := pm.viewport.YOffset(); got != 0 {
		t.Errorf("YOffset = %d after viewport grew past content, want 0", got)
	}
}

func TestPRViewScrolling(t *testing.T) {
	t.Run("top of overflowing view", func(t *testing.T) {
		out := stripANSI(makePRScrollModel(t, 20).View().Content)
		if lines := strings.Count(out, "\n") + 1; lines != 16 {
			t.Errorf("view has %d lines, want 16", lines)
		}
		if !strings.Contains(out, "Scroll Test PR") || !strings.Contains(out, "[Top]") {
			t.Errorf("view missing pinned header or footer, got:\n%s", out)
		}
		if strings.Contains(out, "check 19") {
			t.Errorf("view should not show rows past the viewport, got:\n%s", out)
		}
	})

	t.Run("bottom of overflowing view", func(t *testing.T) {
		m := pressPRKeys(t, makePRScrollModel(t, 20), keyShiftG)
		out := stripANSI(m.View().Content)
		if !strings.Contains(out, "[Bot]") || !strings.Contains(out, "check 19") {
			t.Errorf("bottom view missing [Bot] or last check, got:\n%s", out)
		}
		// Header stays pinned while scrolled.
		if !strings.Contains(out, "Scroll Test PR") {
			t.Errorf("bottom view missing pinned header, got:\n%s", out)
		}
	})

	t.Run("non-overflowing view keeps plain hint", func(t *testing.T) {
		out := stripANSI(makePRScrollModel(t, 1).View().Content)
		if !strings.Contains(out, "Press q to quit") {
			t.Errorf("hint missing, got:\n%s", out)
		}
		if strings.Contains(out, "j/k scroll") {
			t.Errorf("scroll hints should hide when content fits, got:\n%s", out)
		}
	})

	t.Run("scroll hint hides when quitting", func(t *testing.T) {
		m := pressPRKeys(t, makePRScrollModel(t, 20), keyShiftG)
		m.quitting = true
		out := stripANSI(m.View().Content)
		if strings.Contains(out, "j/k scroll") {
			t.Errorf("scroll hints should hide when quitting, got:\n%s", out)
		}
	})
}

// TestPRViewScrollsErrorAnnotations locks in the core motivation for issue
// #554: a failing check's summary and error-annotation lines must be
// scrollable rather than clipped out of existence in alt-screen mode.
func TestPRViewScrollsErrorAnnotations(t *testing.T) {
	m := makePRScrollModel(t, 10)
	m.checkRuns = append(m.checkRuns, ghclient.CheckRunInfo{
		Status:      "completed",
		Conclusion:  "failure",
		WorkflowName: "CI",
		Name:        "flaky",
		Summary:     "tests failed",
		Annotations: []ghclient.Annotation{
			{Path: "a_test.go", StartLine: 7, Message: "assert failed"},
			{Path: "b_test.go", StartLine: 9, Message: "assert failed again"},
		},
	})
	// Re-sync with the taller body (the fixture sized the viewport for the
	// shorter one).
	newModel, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	m = prModelFrom(t, newModel)

	t.Run("annotations clipped at top", func(t *testing.T) {
		out := stripANSI(m.View().Content)
		if strings.Contains(out, "assert failed") {
			t.Errorf("annotations should be below the viewport at top, got:\n%s", out)
		}
	})

	t.Run("annotations reachable by scrolling", func(t *testing.T) {
		scrolled := pressPRKeys(t, m, keyShiftG)
		out := stripANSI(scrolled.View().Content)
		if !strings.Contains(out, "a_test.go:7 - assert failed") || !strings.Contains(out, "b_test.go:9 - assert failed again") {
			t.Errorf("bottom view missing annotations, got:\n%s", out)
		}
		if !strings.Contains(out, "tests failed") {
			t.Errorf("bottom view missing summary line, got:\n%s", out)
		}
	})
}

func TestPRFullViewUnclipped(t *testing.T) {
	// Auto-quit while scrolled to the bottom: the scrollback reprint
	// (printFinalFrame) must render the full body, not the scrolled window.
	m := pressPRKeys(t, makePRScrollModel(t, 20), keyShiftG)
	m.quitting = true

	if out := stripANSI(m.View().Content); strings.Contains(out, "check 00") {
		t.Errorf("scrolled view should not show the first check, got:\n%s", out)
	}

	full := stripANSI(m.FullView())
	for _, want := range []string{"check 00", "check 19", "Scroll Test PR"} {
		if !strings.Contains(full, want) {
			t.Errorf("FullView missing %q, got:\n%s", want, full)
		}
	}
}
