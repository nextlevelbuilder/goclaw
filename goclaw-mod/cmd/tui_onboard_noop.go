//go:build !tui

package cmd

// runOnboardTUI is a no-op when built without tui tag.
// The existing huh-based onboard flow in onboard.go is used directly.
func runOnboardTUI() {
	// no-op: onboard.go already handles the full flow with huh forms
}
