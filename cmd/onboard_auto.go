package cmd

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// providerPriority defines the order in which providers are auto-detected
// from environment variables. First match wins.
var providerPriority = []string{
	"openrouter", "anthropic", "openai", "groq", "deepseek",
	"gemini", "mistral", "xai", "minimax", "cohere", "perplexity",
}

// canAutoOnboard returns true if any GOCLAW_*_API_KEY env var is set,
// indicating the user wants non-interactive configuration (e.g. Docker).
func canAutoOnboard() bool {
	for _, name := range providerPriority {
		pi, ok := providerMap[name]
		if !ok || pi.envKey == "" {
			continue
		}
		if os.Getenv(pi.envKey) != "" {
			return true
		}
	}
	return false
}

// runAutoOnboard performs non-interactive setup from environment variables.
// Returns true on success, false on fatal error.
func runAutoOnboard(cfgPath string) bool {
	fmt.Println("Auto-onboard: environment variables detected, running non-interactive setup...")

	cfg := config.Default()
	cfg.ApplyEnvOverrides()

	// 1. Resolve provider: respect GOCLAW_PROVIDER if set, otherwise auto-detect.
	provider := cfg.Agents.Defaults.Provider // may be set by GOCLAW_PROVIDER via ApplyEnvOverrides
	apiKey := ""
	if provider != "" {
		apiKey = resolveProviderAPIKey(cfg, provider)
	}
	if apiKey == "" {
		// No explicit provider or no API key for it — auto-detect from available keys
		provider, apiKey = detectProvider(cfg)
	}
	if provider == "" {
		fmt.Println("Auto-onboard: no provider API key found in environment")
		return false
	}
	cfg.Agents.Defaults.Provider = provider

	// Use model hint if no model override set via GOCLAW_MODEL
	if cfg.Agents.Defaults.Model == "" || cfg.Agents.Defaults.Model == config.Default().Agents.Defaults.Model {
		if pi, ok := providerMap[provider]; ok && pi.modelHint != "" {
			cfg.Agents.Defaults.Model = pi.modelHint
		}
	}

	fmt.Printf("  Provider: %s (model: %s)\n", provider, cfg.Agents.Defaults.Model)

	// 2. Auto-enable memory: detect embedding-capable API keys from env.
	// Embedding providers: openai, openrouter, gemini (same order as resolveEmbeddingProvider).
	embProvider := autoDetectEmbeddingProvider(cfg)
	if embProvider != "" {
		enabled := true
		cfg.Agents.Defaults.Memory = &config.MemoryConfig{
			Enabled:           &enabled,
			EmbeddingProvider: embProvider,
		}
		fmt.Printf("  Memory:   enabled (embedding: %s)\n", embProvider)
	} else {
		fmt.Println("  Memory:   enabled (FTS-only, no embedding API key)")
		enabled := true
		cfg.Agents.Defaults.Memory = &config.MemoryConfig{Enabled: &enabled}
	}

	// 3. Gateway token
	if cfg.Gateway.Token == "" {
		cfg.Gateway.Token = onboardGenerateToken(16)
		slog.Info("auto-onboard: generated gateway token")
	}

	// 3. Database: Postgres setup
	if cfg.Database.PostgresDSN != "" {
		fmt.Print("  Testing Postgres connection...")

		// Retry loop: database container may still be starting
		var pgErr error
		for attempt := 1; attempt <= 5; attempt++ {
			pgErr = testPostgresConnection(cfg.Database.PostgresDSN)
			if pgErr == nil {
				break
			}
			if attempt < 5 {
				fmt.Printf(" retry %d/5...", attempt)
				time.Sleep(2 * time.Second)
			}
		}

		if pgErr != nil {
			fmt.Println(" FAILED")
			fmt.Printf("  Error: %v\n", pgErr)
			return false
		}
		fmt.Println(" OK")

		// Generate encryption key if not set
		if os.Getenv("GOCLAW_ENCRYPTION_KEY") == "" {
			encKey := onboardGenerateToken(32)
			os.Setenv("GOCLAW_ENCRYPTION_KEY", encKey)
			slog.Info("auto-onboard: generated encryption key")
		}

		// Run migrations (idempotent)
		fmt.Print("  Running migrations...")
		m, err := newMigrator(cfg.Database.PostgresDSN)
		if err != nil {
			fmt.Printf(" error: %v\n", err)
			fmt.Println("  Continuing without migration (run manually: goclaw migrate up)")
		} else {
			if err := m.Up(); err != nil && err.Error() != "no change" {
				fmt.Printf(" error: %v\n", err)
				fmt.Println("  Continuing without migration (run manually: goclaw migrate up)")
			} else {
				v, _, _ := m.Version()
				fmt.Printf(" OK (version: %d)\n", v)
			}
			m.Close()
		}

		// Verify provider connectivity for all configured providers before seeding.
		// Only the primary provider's auth failure blocks bootstrap.
		fmt.Println("  Verifying provider connectivity...")
		if fatalErrors := verifyAllProviders(cfg, provider); len(fatalErrors) > 0 {
			slog.Error("auto-onboard: primary provider verification failed", "errors", fatalErrors)
			fmt.Printf("  Provider verification FAILED: primary provider %q has invalid API key\n", provider)
			return false
		}

		// Seed default data (non-fatal if already exists)
		fmt.Print("  Seeding default agent/provider...")
		if err := seedManagedData(cfg.Database.PostgresDSN, cfg); err != nil {
			fmt.Printf(" skipped: %v\n", err)
		} else {
			fmt.Println(" OK")
		}
	}

	// 4. Save config (clean, minimal — secrets stripped, unused sections omitted)
	savedDSN := cfg.Database.PostgresDSN
	if err := saveCleanConfig(cfgPath, cfg); err != nil {
		fmt.Printf("  Warning: could not save config: %v\n", err)
	} else {
		fmt.Printf("  Config saved to %s\n", cfgPath)
	}

	// Restore DSN and re-apply env overrides so the runtime config has secrets
	cfg.Database.PostgresDSN = savedDSN
	cfg.ApplyEnvOverrides()
	_ = apiKey // apiKey is already applied via ApplyEnvOverrides

	fmt.Println("Auto-onboard complete.")
	return true
}

// embeddingCapable lists providers that support text embeddings.
// Only these three have embedding provider implementations in resolveEmbeddingProvider.
var embeddingCapable = map[string]bool{
	"openai":     true,
	"openrouter": true,
	"gemini":     true,
}

// autoDetectEmbeddingProvider picks an embedding provider from available API keys.
// Priority: primary provider (GOCLAW_PROVIDER) if embedding-capable, then openai → openrouter → gemini.
func autoDetectEmbeddingProvider(cfg *config.Config) string {
	// Prioritize the primary provider if it supports embeddings.
	primary := cfg.Agents.Defaults.Provider
	if embeddingCapable[primary] && resolveProviderAPIKey(cfg, primary) != "" {
		return primary
	}

	// Fallback: first available embedding-capable key.
	if cfg.Providers.OpenAI.APIKey != "" {
		return "openai"
	}
	if cfg.Providers.OpenRouter.APIKey != "" {
		return "openrouter"
	}
	if cfg.Providers.Gemini.APIKey != "" {
		return "gemini"
	}
	return ""
}

// detectProvider finds the first provider with an API key in the environment.
func detectProvider(cfg *config.Config) (string, string) {
	for _, name := range providerPriority {
		key := resolveProviderAPIKey(cfg, name)
		if key != "" {
			return name, key
		}
	}
	return "", ""
}

// saveCleanConfig saves a minimal config.json without noise (empty providers,
// disabled channels, stripped secrets). Only includes sections relevant to
// the active configuration so the file serves as clean documentation.
// Channels are stored in the DB (channel_instances table),
// so they are omitted from config.json to avoid dual-connection.
func saveCleanConfig(cfgPath string, cfg *config.Config) error {
	// Channels are stored in the DB (channel_instances table),
	// so they are omitted from config.json to avoid dual-connection.
	channels := make(map[string]interface{})

	// Build tools section.
	tools := map[string]interface{}{
		"web": map[string]interface{}{
			"duckduckgo": map[string]interface{}{
				"enabled":     cfg.Tools.Web.DuckDuckGo.Enabled,
				"max_results": nonZero(cfg.Tools.Web.DuckDuckGo.MaxResults, 5),
			},
		},
		"browser": map[string]interface{}{
			"enabled":    cfg.Tools.Browser.Enabled,
			"headless":   cfg.Tools.Browser.Headless,
			"remote_url": cfg.Tools.Browser.RemoteURL,
		},
		"execApproval": map[string]interface{}{
			"security": nonEmpty(cfg.Tools.ExecApproval.Security, "full"),
			"ask":      nonEmpty(cfg.Tools.ExecApproval.Ask, "off"),
		},
	}

	// Build agents section.
	agents := map[string]interface{}{
		"defaults": map[string]interface{}{
			"workspace":            cfg.Agents.Defaults.Workspace,
			"restrict_to_workspace": cfg.Agents.Defaults.RestrictToWorkspace,
			"provider":             cfg.Agents.Defaults.Provider,
			"model":                cfg.Agents.Defaults.Model,
			"max_tokens":           cfg.Agents.Defaults.MaxTokens,
			"temperature":          cfg.Agents.Defaults.Temperature,
			"max_tool_iterations":  cfg.Agents.Defaults.MaxToolIterations,
			"context_window":       cfg.Agents.Defaults.ContextWindow,
		},
	}

	if cfg.Agents.Defaults.Subagents != nil {
		agents["defaults"].(map[string]interface{})["subagents"] = cfg.Agents.Defaults.Subagents
	}

	if mc := cfg.Agents.Defaults.Memory; mc != nil {
		mem := map[string]interface{}{
			"enabled": mc.Enabled == nil || *mc.Enabled,
		}
		if mc.EmbeddingProvider != "" {
			mem["embedding_provider"] = mc.EmbeddingProvider
		}
		if mc.EmbeddingModel != "" {
			mem["embedding_model"] = mc.EmbeddingModel
		}
		agents["defaults"].(map[string]interface{})["memory"] = mem
	}

	// Build gateway section (no token — secret).
	gateway := map[string]interface{}{
		"host":                cfg.Gateway.Host,
		"port":                cfg.Gateway.Port,
		"max_message_chars":   nonZero(cfg.Gateway.MaxMessageChars, 32000),
		"rate_limit_rpm":      nonZero(cfg.Gateway.RateLimitRPM, 20),
		"inbound_debounce_ms": nonZero(cfg.Gateway.InboundDebounceMs, 1000),
	}

	// Build root config map.
	root := map[string]interface{}{
		"agents":   agents,
		"gateway":  gateway,
		"tools":    tools,
	}

	if len(channels) > 0 {
		root["channels"] = channels
	}


	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(cfgPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(cfgPath, data, 0600)
}

// nonEmpty returns val if non-empty, otherwise fallback.
func nonEmpty(val, fallback string) string {
	if val != "" {
		return val
	}
	return fallback
}

// nonZero returns val if non-zero, otherwise fallback.
func nonZero(val, fallback int) int {
	if val != 0 {
		return val
	}
	return fallback
}
