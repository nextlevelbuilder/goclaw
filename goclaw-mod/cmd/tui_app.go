//go:build tui

package cmd

import "fmt"

// tuiProgressBar renders a text-based progress bar: [●●●○○] 3/5
func tuiProgressBar(current, total int) string {
	bar := ""
	for i := 0; i < total; i++ {
		if i < current {
			bar += tuiStepDone + " "
		} else if i == current {
			bar += tuiStepCurrent + " "
		} else {
			bar += tuiStepPending + " "
		}
	}
	return fmt.Sprintf("%s %d/%d", bar, current, total)
}

// tuiHeader renders a styled header with progress.
func tuiHeader(title string, step, total int) string {
	header := tuiTitleStyle.Render(title)
	progress := tuiProgressBar(step, total)
	return fmt.Sprintf("\n%s  %s\n", header, progress)
}

// tuiResult renders a success/fail line.
func tuiResult(ok bool, msg string) string {
	if ok {
		return tuiSuccessStyle.Render("  ✓ ") + msg
	}
	return tuiErrorStyle.Render("  ✗ ") + msg
}
