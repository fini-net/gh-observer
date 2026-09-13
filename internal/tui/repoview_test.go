package tui

import "testing"

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
