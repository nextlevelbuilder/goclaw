//go:build tui

package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// setupTUIModel is the Bubble Tea model for the setup wizard.
type setupTUIModel struct {
	steps       []string
	currentStep int
	done        bool
	quitting    bool
}

func newSetupTUIModel() setupTUIModel {
	return setupTUIModel{
		steps: []string{"Providers", "Agent", "Channel", "Summary"},
	}
}

func (m setupTUIModel) Init() tea.Cmd { return nil }

func (m setupTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		case "s": // skip step
			if m.currentStep < len(m.steps)-1 {
				m.currentStep++
			}
		}
	}
	return m, nil
}

func (m setupTUIModel) View() string {
	if m.quitting {
		return tuiMutedStyle.Render("Setup cancelled.\n")
	}
	if m.done {
		return tuiSuccessStyle.Render("Setup complete!\n")
	}

	s := tuiHeader("GoClaw — Setup Wizard", m.currentStep, len(m.steps))
	s += "\n"

	// Step indicator
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
	s += tuiBoxStyle.Render(fmt.Sprintf("Step: %s\n\nPress Enter to configure, 's' to skip, 'q' to quit.",
		m.steps[m.currentStep]))
	s += "\n"

	return s
}

// runSetupTUI runs the Bubble Tea setup wizard (tui build).
// Falls through to the huh-based wizard for actual configuration since
// each step uses huh forms for data collection.
func runSetupTUI() {
	p := tea.NewProgram(newSetupTUIModel())
	model, err := p.Run()
	if err != nil {
		fmt.Printf("TUI error: %v\n", err)
		return
	}

	m := model.(setupTUIModel)
	if m.quitting {
		return
	}

	// After TUI navigation, run the actual huh-based setup steps
	runSetup()
}
