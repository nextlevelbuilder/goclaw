//go:build !tui

package cmd

// runSetupTUI delegates to the huh-based setup when built without tui tag.
func runSetupTUI() {
	runSetup()
}
