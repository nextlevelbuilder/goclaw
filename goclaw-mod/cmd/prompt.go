package cmd

import (
	"github.com/charmbracelet/huh"
)

// runWithHelp wraps a huh field in a Form with help hints visible at the bottom.
func runWithHelp(fields ...huh.Field) error {
	return huh.NewForm(huh.NewGroup(fields...)).WithShowHelp(true).Run()
}

// promptString prompts for a text input using huh TUI.
// If defaultVal is non-empty it is shown as placeholder; pressing Enter returns it.
func promptString(title, description, defaultVal string) (string, error) {
	var value string
	inp := huh.NewInput().
		Title(title).
		Value(&value)

	if description != "" {
		inp = inp.Description(description)
	}
	if defaultVal != "" {
		inp = inp.Placeholder(defaultVal)
	}

	if err := runWithHelp(inp); err != nil {
		return "", err
	}
	if value == "" {
		return defaultVal, nil
	}
	return value, nil
}

// promptPassword prompts for a password input (hidden characters) using huh TUI.
func promptPassword(title, description string) (string, error) {
	var value string
	inp := huh.NewInput().
		Title(title).
		EchoMode(huh.EchoModePassword).
		Value(&value)

	if description != "" {
		inp = inp.Description(description)
	}

	if err := runWithHelp(inp); err != nil {
		return "", err
	}
	return value, nil
}

// filterThreshold: enable type-to-filter only when there are more than this many options.
const filterThreshold = 5

const scrollableThreshold = 15 // enable scrollbars when there are more than this many options

// promptSelect shows a single-select list using huh TUI.
// Returns the value of the selected option.
func promptSelect[T comparable](title string, options []SelectOption[T], defaultIdx int) (T, error) {
	var value T

	huhOpts := make([]huh.Option[T], len(options))
	for i, opt := range options {
		huhOpts[i] = huh.NewOption(opt.Label, opt.Value)
	}
	if defaultIdx >= 0 && defaultIdx < len(options) {
		huhOpts[defaultIdx] = huhOpts[defaultIdx].Selected(true)
	}

	sel := huh.NewSelect[T]().
		Options(huhOpts...).
		Value(&value)

	if len(options) > scrollableThreshold {
		sel.Height(scrollableThreshold) // show scrollbar if too many options
		title += " (scroll for more)"
	}
	sel.Title(title)

	if err := runWithHelp(sel); err != nil {
		var zero T
		return zero, err
	}
	return value, nil
}

// promptMultiSelect shows a multi-select list using huh TUI.
// Returns the values of all selected options.
func promptMultiSelect[T comparable](title, description string, options []SelectOption[T], preselected []T) ([]T, error) {
	var values []T

	// Build pre-selected set for fast lookup
	preSet := make(map[T]bool, len(preselected))
	for _, v := range preselected {
		preSet[v] = true
	}

	huhOpts := make([]huh.Option[T], len(options))
	for i, opt := range options {
		o := huh.NewOption(opt.Label, opt.Value)
		if preSet[opt.Value] {
			o = o.Selected(true)
		}
		huhOpts[i] = o
	}

	ms := huh.NewMultiSelect[T]().
		Title(title).
		Options(huhOpts...).
		Value(&values)

	if description != "" {
		ms = ms.Description(description)
	}
	if len(options) > filterThreshold {
		ms = ms.Filtering(true)
	}

	if err := runWithHelp(ms); err != nil {
		return nil, err
	}
	return values, nil
}

// promptConfirm asks a yes/no question using huh TUI. Returns true for yes.
func promptConfirm(title string, defaultYes bool) (bool, error) {
	value := defaultYes

	c := huh.NewConfirm().
		Title(title).
		Affirmative("Yes").
		Negative("No").
		Value(&value)

	if err := runWithHelp(c); err != nil {
		return false, err
	}
	return value, nil
}

// SelectOption represents a single option in a select prompt.
type SelectOption[T any] struct {
	Label string
	Value T
}
