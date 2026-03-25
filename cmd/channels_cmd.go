package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func channelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channels",
		Short: "List and manage messaging channels",
	}
	cmd.AddCommand(channelsListCmd())
	return cmd
}

type channelEntry struct {
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	HasCredentials bool   `json:"hasCredentials"`
}

func channelsListCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured channels and their status",
		Run: func(cmd *cobra.Command, args []string) {
			cfgPath := resolveConfigPath()
			cfg, err := config.Load(cfgPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error loading config: %s\n", err)
				os.Exit(1)
			}

			entries := []channelEntry{
				{"telegram", cfg.Channels.Telegram.Enabled, cfg.Channels.Telegram.Token != ""},
				{"discord", cfg.Channels.Discord.Enabled, cfg.Channels.Discord.Token != ""},
				{"zalo", cfg.Channels.Zalo.Enabled, cfg.Channels.Zalo.Token != ""},
				{"feishu", cfg.Channels.Feishu.Enabled, cfg.Channels.Feishu.AppID != ""},
				{"whatsapp", cfg.Channels.WhatsApp.Enabled, cfg.Channels.WhatsApp.BridgeURL != ""},
			}

			if jsonOutput {
				data, _ := json.MarshalIndent(entries, "", "  ")
				fmt.Println(string(data))
				return
			}

			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "CHANNEL\tENABLED\tCREDENTIALS\n")
			for _, e := range entries {
				creds := "missing"
				if e.HasCredentials {
					creds = "ok"
				}
				fmt.Fprintf(tw, "%s\t%v\t%s\n", e.Name, e.Enabled, creds)
			}
			tw.Flush()

			// Security alert: warn on "Open Pairing" configuration (matching openclaw #48813)
			if cfg.Channels.Telegram.Enabled && (cfg.Channels.Telegram.DMPolicy == "" || cfg.Channels.Telegram.DMPolicy == "pairing") && len(cfg.Channels.Telegram.AllowFrom) == 0 {
				fmt.Println("\n⚠️  Telegram DM access warning")
				fmt.Println("   Your bot is using the default DM policy (pairing) with no allowlist.")
				fmt.Println("   Any Telegram user who discovers the bot can send pairing requests.")
				fmt.Println("   For security, consider adding your user id to 'allow_from' in config.json.")
			}
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}
