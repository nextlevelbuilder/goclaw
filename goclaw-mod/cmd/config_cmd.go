package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View and manage configuration",
	}
	cmd.AddCommand(configShowCmd())
	cmd.AddCommand(configPathCmd())
	cmd.AddCommand(configValidateCmd())
	return cmd
}

func configShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Display current configuration (secrets redacted)",
		Run: func(cmd *cobra.Command, args []string) {
			cfgPath := resolveConfigPath()
			cfg, err := config.Load(cfgPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error loading config: %s\n", err)
				os.Exit(1)
			}

			// Redact secrets before display
			redacted := redactConfig(cfg)
			data, _ := json.MarshalIndent(redacted, "", "  ")
			fmt.Println(string(data))
		},
	}
}

func configPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the config file path",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(resolveConfigPath())
		},
	}
}

func configValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration file",
		Run: func(cmd *cobra.Command, args []string) {
			cfgPath := resolveConfigPath()
			_, err := config.Load(cfgPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Invalid config: %s\n", err)
				os.Exit(1)
			}
			fmt.Printf("Config at %s is valid.\n", cfgPath)
		},
	}
}

// redactConfig returns a JSON-safe copy with secrets masked.
func redactConfig(cfg *config.Config) any {
	data, _ := json.Marshal(cfg)
	var raw map[string]any
	json.Unmarshal(data, &raw)
	redactMap(raw)
	return raw
}

func redactMap(m map[string]any) {
	secretKeys := map[string]bool{
		"apiKey": true, "api_key": true, "token": true,
		"botToken": true, "bot_token": true, "secret": true,
		"appSecret": true, "encryptKey": true, "verificationToken": true,
	}
	for k, v := range m {
		if secretKeys[k] {
			if s, ok := v.(string); ok && len(s) > 8 {
				m[k] = s[:4] + "****" + s[len(s)-4:]
			} else if s, ok := v.(string); ok && s != "" {
				m[k] = "****"
			}
		} else if sub, ok := v.(map[string]any); ok {
			redactMap(sub)
		}
	}
}
