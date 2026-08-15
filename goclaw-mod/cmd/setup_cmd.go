package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func setupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Configuration wizard — providers, agents, channels",
		Long:  "Interactive setup for providers, models, agents, and channels. Requires a running gateway.",
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			runSetup()
		},
	}
}

func runSetup() {
	fmt.Println()
	fmt.Println("╭──────────────────────────────────╮")
	fmt.Println("│     GoClaw — Setup Wizard        │")
	fmt.Println("╰──────────────────────────────────╯")
	fmt.Println()

	// Step 1: Providers
	setupProviderStep()

	// Step 2: Agent
	setupAgentStep()

	// Step 3: Channel (optional)
	setupChannelStep()

	// Summary
	printSetupSummary()
}

func printSetupSummary() {
	fmt.Println()
	fmt.Println("── Setup Complete ──")
	fmt.Println()

	// Show what was configured
	providers, _ := fetchProviders()
	agents, _ := fetchAgentList()

	if len(providers) > 0 {
		fmt.Printf("  Providers:  %d configured\n", len(providers))
		for _, p := range providers {
			fmt.Printf("    - %s (%s)\n", p.Name, p.ProviderType)
		}
	}

	if len(agents) > 0 {
		fmt.Printf("  Agents:     %d configured\n", len(agents))
		for _, a := range agents {
			fmt.Printf("    - %s (%s)\n", a.AgentKey, a.Model)
		}
	}

	fmt.Println()
	base := resolveGatewayBaseURL()
	fmt.Printf("  Dashboard: %s\n", base)
	fmt.Println()
	fmt.Println("Run 'goclaw setup' again anytime to add more.")
}
