package tui

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

// Styles holds all lipgloss styles for rendering
type Styles struct {
	Success     lipgloss.Style
	Failure     lipgloss.Style
	Running     lipgloss.Style
	Queued      lipgloss.Style
	Error       lipgloss.Style
	Header      lipgloss.Style
	Info        lipgloss.Style
	ErrorBox    lipgloss.Style
	Description lipgloss.Style
	LogError    lipgloss.Style
	LogWarning  lipgloss.Style
	LogNotice   lipgloss.Style
	LogGroup    lipgloss.Style
	LogDebug    lipgloss.Style
	LogDefault  lipgloss.Style
}

// NewStyles creates styled renderers based on config colors
func NewStyles(successColor, failureColor, runningColor, queuedColor int) Styles {
	return Styles{
		Success: lipgloss.NewStyle().Foreground(lipgloss.Color(fmt.Sprint(successColor))).Bold(true),
		Failure: lipgloss.NewStyle().Foreground(lipgloss.Color(fmt.Sprint(failureColor))).Bold(true),
		Running: lipgloss.NewStyle().Foreground(lipgloss.Color(fmt.Sprint(runningColor))).Bold(true),
		Queued:  lipgloss.NewStyle().Foreground(lipgloss.Color(fmt.Sprint(queuedColor))),
		Error:   lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true),
		Header:  lipgloss.NewStyle().Bold(true).Underline(true),
		Info:    lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
		ErrorBox: lipgloss.NewStyle().
			BorderLeft(true).
			BorderForeground(lipgloss.Color("9")).
			PaddingLeft(1).
			Foreground(lipgloss.Color("243")),
		Description: lipgloss.NewStyle().Foreground(lipgloss.Color("243")),
		LogError:    lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true),
		LogWarning:  lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		LogNotice:   lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
		LogGroup:    lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true),
		LogDebug:    lipgloss.NewStyle().Foreground(lipgloss.Color("243")),
		LogDefault:  lipgloss.NewStyle().Foreground(lipgloss.Color("243")),
	}
}
