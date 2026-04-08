package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/nodehost"
)

func nodeConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Show current node-host configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, err := nodehost.ResolveNodeHostConfigPath()
			if err != nil {
				return fmt.Errorf("resolve config path: %w", err)
			}

			cfg, err := nodehost.LoadNodeHostConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if cfg == nil {
				fmt.Fprintf(os.Stderr, "No node config found at %s\n", configPath)
				fmt.Println("Run 'goclaw node run' to create one.")
				return nil
			}

			fmt.Printf("Config: %s\n", configPath)
			data, _ := json.MarshalIndent(cfg, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}
}
