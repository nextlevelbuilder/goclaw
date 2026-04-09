package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Version is set at build time via -ldflags "-X github.com/nextlevelbuilder/goclaw/cmd.Version=v1.0.0"
var Version = "dev"

var (
	cfgFile string
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:   "goclaw",
	Short: "GoClaw — AI agent gateway",
	Long:  "GoClaw: multi-agent AI platform with WebSocket RPC, tool execution, and channel integration. A Go port of OpenClaw with enhanced security and multi-tenant support.",
	Run: func(cmd *cobra.Command, args []string) {
		runGateway()
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: config.json or $GOCLAW_CONFIG)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug logging")

	rootCmd.AddCommand(onboardCmd())
	rootCmd.AddCommand(versionCmd())
	rootCmd.AddCommand(pairingCmd())
	rootCmd.AddCommand(agentCmd())
	rootCmd.AddCommand(doctorCmd())
	rootCmd.AddCommand(configCmd())
	rootCmd.AddCommand(providersCmd())
	rootCmd.AddCommand(channelsCmd())
	rootCmd.AddCommand(cronCmd())
	rootCmd.AddCommand(skillsCmd())
	rootCmd.AddCommand(sessionsCmd())
	rootCmd.AddCommand(migrateCmd())
	rootCmd.AddCommand(upgradeCmd())
	rootCmd.AddCommand(backupCmd())
	rootCmd.AddCommand(restoreCmd())
	rootCmd.AddCommand(tenantBackupCmd())
	rootCmd.AddCommand(tenantRestoreCmd())
	rootCmd.AddCommand(authCmd())
	rootCmd.AddCommand(setupCmd())
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("goclaw %s (protocol %d)\n", Version, protocol.ProtocolVersion)
		},
	}
}

func resolveConfigPath() string {
	if cfgFile != "" {
		return cfgFile
	}
	if v := os.Getenv("GOCLAW_CONFIG"); v != "" {
		return v
	}
	return "config.json"
}

// Execute runs the root cobra command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
