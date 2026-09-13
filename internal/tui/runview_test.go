package tui

import (
	"errors"
	"testing"
)

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
