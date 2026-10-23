package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	ghclient "github.com/fini-net/gh-observer/internal/github"
)

var (
	keyJ      = tea.KeyPressMsg(tea.Key{Text: "j", Code: 'j'})
	keyK      = tea.KeyPressMsg(tea.Key{Text: "k", Code: 'k'})
	keyG      = tea.KeyPressMsg(tea.Key{Text: "g", Code: 'g'})
	keyShiftG = tea.KeyPressMsg(tea.Key{Text: "G", Code: 'g', Mod: tea.ModShift})
)

// makeScrollModel builds a sized repo model with prCount PR groups. Each group
// is three body lines, so 10 PRs give 30 lines in a 10-line viewport (15-line
// terminal minus a 3-line header and 2-line footer), for a max offset of 20.
func makeScrollModel(t *testing.T, prCount int) *RepoModel {
	t.Helper()
	m := makeRepoModelForRender()
	for i := 1; i <= prCount; i++ {
		m.prs[i] = PRViewData{
			Title:     fmt.Sprintf("PR %d", i),
			CheckRuns: []ghclient.CheckRunInfo{{Status: "in_progress", Name: "build"}},
		}
	}
	newModel, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 15})
	return repoModelFrom(t, newModel)
}

// pressKeys sends each key through Update and returns the resulting model.
func pressKeys(t *testing.T, m *RepoModel, keys ...tea.KeyPressMsg) *RepoModel {
	t.Helper()
	for _, key := range keys {
		newModel, _ := m.Update(key)
		m = repoModelFrom(t, newModel)
	}
	return m
}

func TestRepoUpdateScrollKeys(t *testing.T) {
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
		{"G", []tea.KeyPressMsg{keyShiftG}, 20},
		{"G clamps", []tea.KeyPressMsg{keyShiftG, keyJ}, 20},
		{"gg", []tea.KeyPressMsg{keyShiftG, keyG, keyG}, 0},
		{"g then j", []tea.KeyPressMsg{keyG, keyJ}, 1},
		{"end then home", []tea.KeyPressMsg{tea.KeyPressMsg(tea.Key{Code: tea.KeyEnd}), tea.KeyPressMsg(tea.Key{Code: tea.KeyHome})}, 0},
		{"l ignored", []tea.KeyPressMsg{tea.KeyPressMsg(tea.Key{Text: "l", Code: 'l'})}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rm := pressKeys(t, makeScrollModel(t, 10), tt.keys...)
			if got := rm.viewport.YOffset(); got != tt.want {
				t.Errorf("YOffset = %d, want %d", got, tt.want)
			}
			if got := rm.viewport.XOffset(); got != 0 {
				t.Errorf("XOffset = %d, want 0", got)
			}
		})
	}
}

func TestRepoUpdateScrollClampsWhenContentShrinks(t *testing.T) {
	m := pressKeys(t, makeScrollModel(t, 10), keyShiftG)
	newModel, _ := m.Update(RepoChecksUpdateMsg{PRData: map[int]ghclient.PRCheckData{
		1: {Title: "PR 1", CheckRuns: []ghclient.CheckRunInfo{{Status: "in_progress", Name: "build"}}},
	}})
	rm := repoModelFrom(t, newModel)
	if got := rm.viewport.YOffset(); got != 0 {
		t.Errorf("YOffset = %d after content shrank, want 0", got)
	}
}

func TestRepoViewScrolling(t *testing.T) {
	t.Run("top of overflowing view", func(t *testing.T) {
		out := stripANSI(makeScrollModel(t, 10).View().Content)
		if lines := strings.Count(out, "\n") + 1; lines != 15 {
			t.Errorf("view has %d lines, want 15", lines)
		}
		if !strings.Contains(out, "owner/repo") || !strings.Contains(out, "[Top]") {
			t.Errorf("view missing pinned header or footer, got:\n%s", out)
		}
		if strings.Contains(out, "PR #10") {
			t.Errorf("view should not show rows past the viewport, got:\n%s", out)
		}
	})

	t.Run("bottom of overflowing view", func(t *testing.T) {
		m := pressKeys(t, makeScrollModel(t, 10), keyShiftG)
		out := stripANSI(m.View().Content)
		if !strings.Contains(out, "[Bot]") || !strings.Contains(out, "PR #10") {
			t.Errorf("bottom view missing [Bot] or last PR, got:\n%s", out)
		}
	})

	t.Run("non-overflowing view keeps plain hint", func(t *testing.T) {
		out := stripANSI(makeScrollModel(t, 1).View().Content)
		if !strings.Contains(out, "Press q to quit") {
			t.Errorf("hint missing, got:\n%s", out)
		}
	})
}

func TestScrollHint(t *testing.T) {
	tests := []struct {
		name  string
		width int
		want  string
	}{
		{"all hints fit", 80, "j/k scroll • ^f/^b page • ^d/^u half • gg/G top/bottom • q quit  [Top]"},
		{"drops hints from the end", 30, "j/k scroll • ^f/^b page  [Top]"},
		{"ruler only", 10, "  [Top]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := RepoModel{width: tt.width}
			if got := m.scrollHint(); got != tt.want {
				t.Errorf("scrollHint() = %q, want %q", got, tt.want)
			}
		})
	}
}
