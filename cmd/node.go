package cmd

import "github.com/spf13/cobra"

func nodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Manage node-host remote execution",
		Long:  "Run and manage the node-host service that executes system commands for the gateway.",
	}

	cmd.AddCommand(nodeRunCmd())
	cmd.AddCommand(nodeConfigCmd())

	return cmd
}
