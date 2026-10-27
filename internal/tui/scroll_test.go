package tui

import (
	"testing"
)

// TestSectionHeight locks in the empty-section rule from scroll.go's sync:
// lipgloss.Height("") is 1, so an empty pinned section (a startup-phase
// footer, a pre-PRInfoMsg header) must be special-cased to zero rows or it
// steals a row from the viewport.
func TestSectionHeight(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"lone newline", "\n", 0},
		{"single line", "header\n", 1},
		{"single line no newline", "header", 1},
		{"two lines", "a\nb\n", 2},
		{"two lines no trailing newline", "a\nb", 2},
		{"blank rows count", "\n\n", 2},
		{"blank plus content", "\nhint\n", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sectionHeight(tt.in); got != tt.want {
				t.Errorf("sectionHeight(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
