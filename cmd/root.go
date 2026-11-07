package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Version is set at build time via -ldflags "-X github.com/nextlevelbuilder/goclaw/cmd.Version=v1.0.0"
var Version = "dev"

// CommitSHA is set at build time via -ldflags "-X github.com/nextlevelbuilder/goclaw/cmd.CommitSHA=<sha>".
// It records the exact source commit the binary was built from, so a running
// image/binary can be lined up with a commit even when VCS metadata is
// unavailable (e.g. Docker builds, where .git is excluded from the context).
var CommitSHA = "unknown"

// commitSuffix returns " (commit <sha>)" when CommitSHA was injected at build
// time, so version/doctor/upgrade output can carry release provenance.
func commitSuffix() string {
	if CommitSHA == "" || CommitSHA == "unknown" {
		return ""
	}
	return " (commit " + CommitSHA + ")"
}

var (
	cfgFile string
	verbose bool

	gatewayServerOverride string
	gatewayTokenOverride  string
	gatewayOutputFormat   string
)

var rootCmd = &cobra.Command{
	Use:   "goclaw",
	Short: "GoClaw — AI agent gateway",
	Long:  "GoClaw: multi-agent AI platform with WebSocket RPC, tool execution, and channel integration. A Go port of OpenClaw with enhanced security and multi-tenant support.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		switch cmd.Name() {
		case "version", "help", "completion":
			return nil
		}
		return validateEncryptionEnv()
	},
	Run: func(cmd *cobra.Command, args []string) {
		runGateway()
	},
}

// validateEncryptionEnv installs the keyring described by the environment and
// fails on a malformed configuration, so a bad key list stops the process at
// startup instead of surfacing as a decrypt error later.
func validateEncryptionEnv() error {
	_, err := crypto.ConfigureKeyringFromEnv()
	return err
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: config.json or $GOCLAW_CONFIG)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug logging")
	rootCmd.PersistentFlags().StringVar(&gatewayServerOverride, "server", "", "gateway server URL override")
	rootCmd.PersistentFlags().StringVar(&gatewayTokenOverride, "token", "", "gateway bearer token override")

	rootCmd.AddCommand(onboardCmd())
	rootCmd.AddCommand(versionCmd())
	rootCmd.AddCommand(pairingCmd())
	rootCmd.AddCommand(agentCmd())
	rootCmd.AddCommand(doctorCmd())
	rootCmd.AddCommand(configCmd())
	rootCmd.AddCommand(providersCmd())
	rootCmd.AddCommand(channelsCmd())
	rootCmd.AddCommand(bitrixPortalCmd())
	rootCmd.AddCommand(secretVaultCmd())
	rootCmd.AddCommand(keysCmd())
	rootCmd.AddCommand(cronCmd())
	rootCmd.AddCommand(skillsCmd())
	rootCmd.AddCommand(sessionsCmd())
	rootCmd.AddCommand(tracesCmd())
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
			fmt.Printf("goclaw %s%s (protocol %d)\n", Version, commitSuffix(), protocol.ProtocolVersion)
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
