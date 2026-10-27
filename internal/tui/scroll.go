package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// scrollState bundles the vim-style scroll plumbing shared by all interactive
// modes (repo, PR, and run). Each model embeds it and keeps its own pinned
// renderHeader/renderBody/renderFooter split; sync() sizes the viewport to
// the space between those sections, handleKey() applies the scroll keymap,
// and scrollable()/scrollHint()/scrollPosition() drive the footer hint.
type scrollState struct {
	viewport viewport.Model
	width    int
	height   int
	pendingG bool
}

// newScrollState returns a scroll state with a viewport wired with vim-style
// vertical scroll keys. Horizontal keys are disabled so left/right are inert.
func newScrollState() scrollState {
	vp := viewport.New()
	vp.KeyMap.PageDown.SetKeys("pgdown", "space", "f", "ctrl+f")
	vp.KeyMap.PageUp.SetKeys("pgup", "b", "ctrl+b")
	vp.KeyMap.Left.SetEnabled(false)
	vp.KeyMap.Right.SetEnabled(false)
	return scrollState{viewport: vp}
}

// handleKey applies a scroll key to the body viewport, including the gg/G and
// home/end jumps handled outside the viewport keymap. Returns the command
// (if any) from the viewport update. A no-op until the terminal size is
// known, so keys typed before the first tea.WindowSizeMsg don't scroll an
// unsized viewport.
func (s *scrollState) handleKey(msg tea.KeyMsg) tea.Cmd {
	if s.height == 0 {
		return nil
	}

	switch msg.String() {
	case "G", "end":
		s.pendingG = false
		s.viewport.GotoBottom()
		return nil
	case "home":
		s.pendingG = false
		s.viewport.GotoTop()
		return nil
	case "g":
		if s.pendingG {
			s.viewport.GotoTop()
		}
		s.pendingG = !s.pendingG
		return nil
	}

	s.pendingG = false
	var cmd tea.Cmd
	s.viewport, cmd = s.viewport.Update(msg)
	return cmd
}

// sync sizes the viewport to the space between the pinned header and footer
// and refreshes its content. A no-op until the terminal size is known.
func (s *scrollState) sync(header, body, footer string) {
	if s.height == 0 {
		return
	}
	// bubbletea clips long lines instead of wrapping them, so each line is one row.
	// lipgloss.Height("") is 1, so empty sections (e.g. a startup-phase footer
	// or a pre-PRInfoMsg header) must not consume a row.
	bodyHeight := max(s.height-sectionHeight(header)-sectionHeight(footer), 1)
	s.viewport.SetWidth(s.width)
	s.viewport.SetHeight(bodyHeight)
	s.viewport.SetContent(strings.TrimSuffix(body, "\n"))
}

// sectionHeight returns the rendered row count of a pinned section, with
// empty sections counting as zero rows.
func sectionHeight(s string) int {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return 0
	}
	return lipgloss.Height(s)
}

// composeView stitches the pinned header, scrollable body viewport, and
// pinned footer into a frame for a sized terminal. footer must be
// newline-trimmed; an empty footer (e.g. the startup phase) drops the
// separator newline too.
func (s scrollState) composeView(header, footer string) string {
	if footer == "" {
		return header + s.viewport.View()
	}
	return header + s.viewport.View() + "\n" + footer
}

// scrollable reports whether the body overflows the viewport.
func (s scrollState) scrollable() bool {
	return s.height > 0 && s.viewport.TotalLineCount() > s.viewport.Height()
}

// scrollHint returns the key hints and ruler, dropping hints from the end
// until the line fits the terminal so the ruler is never cut off.
func (s scrollState) scrollHint() string {
	ruler := fmt.Sprintf("  [%s]", s.scrollPosition())
	hints := []string{"j/k scroll", "^f/^b page", "^d/^u half", "gg/G top/bottom", "q quit"}
	for len(hints) > 0 && lipgloss.Width(strings.Join(hints, " • ")+ruler) > s.width {
		hints = hints[:len(hints)-1]
	}
	return strings.Join(hints, " • ") + ruler
}

// scrollPosition returns a vim-style ruler: Top, Bot, or a percentage.
func (s scrollState) scrollPosition() string {
	switch {
	case s.viewport.AtTop():
		return "Top"
	case s.viewport.AtBottom():
		return "Bot"
	default:
		return fmt.Sprintf("%d%%", int(s.viewport.ScrollPercent()*100))
	}
}
