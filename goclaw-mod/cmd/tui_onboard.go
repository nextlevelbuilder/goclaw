//go:build tui

package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// onboardTUIModel is the Bubble Tea model for the onboard wizard.
type onboardTUIModel struct {
	steps       []string
	currentStep int
	done        bool
	quitting    bool
}

func newOnboardTUIModel() onboardTUIModel {
	return onboardTUIModel{
		steps: []string{"Database", "Test Connection", "Migrations", "Keys", "Save", "Summary"},
	}
}

func (m onboardTUIModel) Init() tea.Cmd { return nil }

func (m onboardTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			if m.currentStep < len(m.steps)-1 {
				m.currentStep++
			} else {
				m.done = true
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m onboardTUIModel) View() string {
	if m.quitting {
		return tuiMutedStyle.Render("Onboard cancelled.\n")
	}
	if m.done {
		return tuiSuccessStyle.Render("Onboard complete! Run 'goclaw setup' next.\n")
	}

	s := tuiHeader("GoClaw — Onboard", m.currentStep, len(m.steps))
	s += "\n"

	for i, step := range m.steps {
		indicator := tuiStepPending
		if i < m.currentStep {
			indicator = tuiStepDone
		} else if i == m.currentStep {
			indicator = tuiStepCurrent
		}
		s += fmt.Sprintf("  %s %s\n", indicator, step)
	}

	s += "\n"
	s += tuiBoxStyle.Render(fmt.Sprintf("Step: %s\n\nPress Enter to continue, 'q' to quit.",
		m.steps[m.currentStep]))
	s += "\n"

	return s
}

// runOnboardTUI runs the Bubble Tea onboard wizard (tui build).
func runOnboardTUI() {
	p := tea.NewProgram(newOnboardTUIModel())
	if _, err := p.Run(); err != nil {
		fmt.Printf("TUI error: %v\n", err)
	}
}
