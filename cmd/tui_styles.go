//go:build tui

package cmd

import "github.com/charmbracelet/lipgloss"

var (
	tuiTitleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	tuiSuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	tuiErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	tuiMutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	tuiBoxStyle     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
	tuiStepDone     = tuiSuccessStyle.Render("●")
	tuiStepCurrent  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("◐")
	tuiStepPending  = tuiMutedStyle.Render("○")
)
