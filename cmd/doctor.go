package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/upgrade"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check system environment and configuration health",
		Run: func(cmd *cobra.Command, args []string) {
			runDoctor()
		},
	}
}

func runDoctor() {
	fmt.Println("goclaw doctor")
	fmt.Printf("  Version:  %s (protocol %d)\n", Version, protocol.ProtocolVersion)
	fmt.Printf("  OS:       %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  Go:       %s\n", runtime.Version())
	fmt.Println()

	// Config
	cfgPath := resolveConfigPath()
	fmt.Printf("  Config:   %s", cfgPath)
	if _, err := os.Stat(cfgPath); err != nil {
		fmt.Println(" (NOT FOUND)")
	} else {
		fmt.Println(" (OK)")
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Printf("  Config load error: %s\n", err)
		return
	}

	// Database
	var db *sql.DB
	if cfg.Database.PostgresDSN == "" {
		fmt.Println()
		fmt.Println("  Database:")
		fmt.Printf("    %-12s NOT CONFIGURED (set GOCLAW_POSTGRES_DSN)\n", "Status:")
	} else {
		fmt.Println()
		fmt.Println("  Database:")
		var dbErr error
		db, dbErr = sql.Open("pgx", cfg.Database.PostgresDSN)
		if dbErr != nil {
			fmt.Printf("    %-12s CONNECT FAILED (%s)\n", "Status:", dbErr)
			db = nil
		} else if pingErr := db.Ping(); pingErr != nil {
			fmt.Printf("    %-12s CONNECT FAILED (%s)\n", "Status:", pingErr)
			db.Close()
			db = nil
		} else {
			defer db.Close()
			s, schemaErr := upgrade.CheckSchema(db)
			if schemaErr != nil {
				fmt.Printf("    %-12s CHECK FAILED (%s)\n", "Schema:", schemaErr)
			} else if s.Dirty {
				fmt.Printf("    %-12s v%d (DIRTY — run: goclaw migrate force %d)\n", "Schema:", s.CurrentVersion, s.CurrentVersion-1)
			} else if s.Compatible {
				fmt.Printf("    %-12s v%d (up to date)\n", "Schema:", s.CurrentVersion)
			} else if s.CurrentVersion > s.RequiredVersion {
				fmt.Printf("    %-12s v%d (binary too old, requires v%d)\n", "Schema:", s.CurrentVersion, s.RequiredVersion)
			} else {
				fmt.Printf("    %-12s v%d (upgrade needed — run: goclaw upgrade)\n", "Schema:", s.CurrentVersion)
			}

			pending, hookErr := upgrade.PendingHooks(context.Background(), db)
			if hookErr == nil && len(pending) > 0 {
				fmt.Printf("    %-12s %d pending\n", "Data hooks:", len(pending))
			} else if hookErr == nil {
				fmt.Printf("    %-12s all applied\n", "Data hooks:")
			}
		}
	}

	// Providers — show DB providers, plus config-only providers (env vars).
	fmt.Println()
	fmt.Println("  Providers:")
	if db != nil {
		checkDBProviders(db)
		checkProvider("Anthropic (env)", cfg.Providers.Anthropic.APIKey)
		checkProvider("OpenAI (env)", cfg.Providers.OpenAI.APIKey)
		checkProvider("OpenRouter (env)", cfg.Providers.OpenRouter.APIKey)
	} else {
		checkProvider("Anthropic", cfg.Providers.Anthropic.APIKey)
		checkProvider("OpenAI", cfg.Providers.OpenAI.APIKey)
		checkProvider("OpenRouter", cfg.Providers.OpenRouter.APIKey)
		checkProvider("Gemini", cfg.Providers.Gemini.APIKey)
		checkProvider("Groq", cfg.Providers.Groq.APIKey)
		checkProvider("DeepSeek", cfg.Providers.DeepSeek.APIKey)
		checkProvider("Mistral", cfg.Providers.Mistral.APIKey)
		checkProvider("XAI", cfg.Providers.XAI.APIKey)
	}

	// Channels — show DB channels, fallback to config channels.
	fmt.Println()
	fmt.Println("  Channels:")
	if db != nil {
		checkDBChannels(db)
	} else {
		checkChannel("Telegram", cfg.Channels.Telegram.Enabled, cfg.Channels.Telegram.Token != "")
		checkChannel("Discord", cfg.Channels.Discord.Enabled, cfg.Channels.Discord.Token != "")
		checkChannel("Zalo", cfg.Channels.Zalo.Enabled, cfg.Channels.Zalo.Token != "")
		checkChannel("Feishu", cfg.Channels.Feishu.Enabled, cfg.Channels.Feishu.AppID != "")
		checkChannel("WhatsApp", cfg.Channels.WhatsApp.Enabled, cfg.Channels.WhatsApp.Enabled)
	}

	// External tools
	fmt.Println()
	fmt.Println("  External Tools:")
	checkBinary("docker")
	checkBinary("curl")
	checkBinary("git")

	// Workspace
	fmt.Println()
	ws := config.ExpandHome(cfg.Agents.Defaults.Workspace)
	fmt.Printf("  Workspace: %s", ws)
	if _, err := os.Stat(ws); err != nil {
		fmt.Println(" (NOT FOUND)")
	} else {
		fmt.Println(" (OK)")
	}

	fmt.Println()
	fmt.Println("Doctor check complete.")
}

func checkProvider(name, apiKey string) {
	if apiKey != "" {
		maskedKey := apiKey[:4] + strings.Repeat("*", len(apiKey)-8) + apiKey[len(apiKey)-4:]
		fmt.Printf("    %-12s %s\n", name+":", maskedKey)
	} else {
		fmt.Printf("    %-12s (not configured)\n", name+":")
	}
}

func checkChannel(name string, enabled, hasCredentials bool) {
	status := "disabled"
	if enabled && hasCredentials {
		status = "enabled"
	} else if enabled {
		status = "enabled (missing credentials)"
	}
	fmt.Printf("    %-12s %s\n", name+":", status)
}

func checkDBChannels(db *sql.DB) {
	rows, err := db.QueryContext(context.Background(),
		"SELECT name, channel_type, enabled FROM channel_instances ORDER BY channel_type, name")
	if err != nil {
		fmt.Printf("    (could not query channels: %s)\n", err)
		return
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var name, channelType string
		var enabled bool
		if err := rows.Scan(&name, &channelType, &enabled); err != nil {
			continue
		}
		found = true
		status := "enabled"
		if !enabled {
			status = "disabled"
		}
		label := fmt.Sprintf("%s/%s", channelType, name)
		fmt.Printf("    %-24s %s\n", label+":", status)
	}
	if !found {
		fmt.Println("    (none configured in database)")
	}
}

func checkDBProviders(db *sql.DB) {
	rows, err := db.QueryContext(context.Background(),
		"SELECT name, COALESCE(NULLIF(display_name, ''), name), enabled, (api_key IS NOT NULL AND api_key != '') AS has_key FROM llm_providers ORDER BY name")
	if err != nil {
		fmt.Printf("    (could not query providers: %s)\n", err)
		return
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var name, displayName string
		var enabled, hasKey bool
		if err := rows.Scan(&name, &displayName, &enabled, &hasKey); err != nil {
			continue
		}
		found = true
		status := "enabled"
		if !enabled {
			status = "disabled"
		}
		if !hasKey {
			status += " (no API key)"
		}
		fmt.Printf("    %-16s %s\n", displayName+":", status)
	}
	if !found {
		fmt.Println("    (none configured in database)")
	}
}

func checkBinary(name string) {
	path, err := exec.LookPath(name)
	if err != nil {
		fmt.Printf("    %-12s NOT FOUND\n", name+":")
	} else {
		fmt.Printf("    %-12s %s\n", name+":", path)
	}
}
