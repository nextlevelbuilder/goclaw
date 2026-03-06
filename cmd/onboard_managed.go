package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// testPostgresConnection verifies connectivity to Postgres with a 5s timeout.
func testPostgresConnection(dsn string) error {
	db, err := pg.OpenDB(dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return db.PingContext(ctx)
}

// seedManagedData inserts providers, default model, and default agent into Postgres
// so the gateway has something to work with on first start.
// All providers with API keys are seeded (not just the default one).
// Idempotent: duplicate entries are skipped on re-run.
func seedManagedData(dsn string, cfg *config.Config) error {
	storeCfg := store.StoreConfig{
		PostgresDSN:   dsn,
		EncryptionKey: os.Getenv("GOCLAW_ENCRYPTION_KEY"),
	}
	stores, err := pg.NewPGStores(storeCfg)
	if err != nil {
		return fmt.Errorf("open PG stores: %w", err)
	}

	ctx := context.Background()

	// Resolve owner: use first GOCLAW_OWNER_IDS entry if set, otherwise "system".
	ownerID := "system"
	if len(cfg.Gateway.OwnerIDs) > 0 && cfg.Gateway.OwnerIDs[0] != "" {
		ownerID = cfg.Gateway.OwnerIDs[0]
	}

	defaultProvider := cfg.Agents.Defaults.Provider
	if defaultProvider == "" {
		defaultProvider = "openrouter"
	}

	// 1. Seed all providers that have API keys.
	// Errors are non-fatal per provider (e.g. unique violation on re-run).
	var seededCount int
	for _, name := range providerPriority {
		apiKey := resolveProviderAPIKey(cfg, name)
		if apiKey == "" {
			continue
		}

		providerType := resolveProviderType(name)

		p := &store.LLMProviderData{
			Name:         name,
			DisplayName:  name,
			ProviderType: providerType,
			APIBase:      resolveProviderAPIBase(name),
			APIKey:       apiKey,
			Enabled:      true,
		}

		if err := stores.Providers.CreateProvider(ctx, p); err != nil {
			slog.Debug("seed provider skipped (may already exist)", "name", name, "error", err)
			continue
		}
		seededCount++
		slog.Info("seeded provider", "name", name)
	}

	if seededCount > 0 {
		fmt.Printf("  Seeded %d provider(s)\n", seededCount)
	}

	// 2. Find the default provider's ID from DB (handles both fresh seed and re-run).
	allProviders, err := stores.Providers.ListProviders(ctx)
	if err != nil {
		return fmt.Errorf("list providers: %w", err)
	}

	var defaultProviderID uuid.UUID
	for _, p := range allProviders {
		if p.Name == defaultProvider {
			defaultProviderID = p.ID
			break
		}
	}
	if defaultProviderID == uuid.Nil {
		return fmt.Errorf("default provider %q not found in DB (no API key?)", defaultProvider)
	}

	// 3. Resolve default model string (used for agent seed below).
	modelID := cfg.Agents.Defaults.Model
	if modelID == "" {
		modelID = "anthropic/claude-sonnet-4-5-20250929"
	}

	// 4. Seed default agent (skip if already exists)
	workspace := config.ExpandHome(cfg.Agents.Defaults.Workspace)
	agent := &store.AgentData{
		AgentKey:            "default",
		DisplayName:         "Default Agent",
		OwnerID:             ownerID,
		AgentType:           store.AgentTypeOpen,
		Provider:            defaultProvider,
		Model:               modelID,
		Workspace:           workspace,
		RestrictToWorkspace: true,
		IsDefault:           true,
		Status:              store.AgentStatusActive,
		SubagentsConfig:     json.RawMessage(`{"maxSpawnDepth":1,"maxConcurrent":20}`),
	}

	if err := stores.Agents.Create(ctx, agent); err != nil {
		slog.Debug("seed agent skipped (may already exist)", "error", err)
		return nil
	}

	// 5. Seed context files into agent_context_files (only for predefined agents;
	//    open agents get per-user files via SeedUserFiles on first chat)
	if _, err := bootstrap.SeedToStore(ctx, stores.Agents, agent.ID, agent.AgentType); err != nil {
		return fmt.Errorf("seed context files: %w", err)
	}

	// 6. Seed channel instances from env vars (if set)
	seedChannelInstances(ctx, stores, cfg, agent.ID, ownerID)

	// 7. Seed config_secrets from env vars
	seedConfigSecrets(ctx, stores, cfg)

	// 8. Seed placeholder providers for UI discoverability
	seedDefaultPlaceholders(ctx, stores)

	return nil
}

// seedChannelInstances creates channel_instances rows from env-var-provided credentials.
// Idempotent: skips if an instance with the same name already exists.
func seedChannelInstances(ctx context.Context, stores *store.Stores, cfg *config.Config, defaultAgentID uuid.UUID, ownerID string) {
	if stores.ChannelInstances == nil {
		return
	}

	type seed struct {
		name        string // channel name in the system
		channelType string
		display     string
		creds       map[string]string
		config      map[string]interface{}
	}

	var seeds []seed

	// Telegram: use legacy name "telegram" for backward compat with existing session keys.
	if cfg.Channels.Telegram.Token != "" {
		tgConfig := map[string]interface{}{
			"dm_policy":      nonEmpty(cfg.Channels.Telegram.DMPolicy, "pairing"),
			"group_policy":   nonEmpty(cfg.Channels.Telegram.GroupPolicy, "pairing"),
			"reaction_level": nonEmpty(cfg.Channels.Telegram.ReactionLevel, "full"),
			"history_limit":  nonZero(cfg.Channels.Telegram.HistoryLimit, 50),
		}
		if cfg.Channels.Telegram.RequireMention != nil {
			tgConfig["require_mention"] = *cfg.Channels.Telegram.RequireMention
		}
		if cfg.Channels.Telegram.MediaMaxBytes > 0 {
			tgConfig["media_max_bytes"] = cfg.Channels.Telegram.MediaMaxBytes
		}
		if cfg.Channels.Telegram.LinkPreview != nil {
			tgConfig["link_preview"] = *cfg.Channels.Telegram.LinkPreview
		}
		if len(cfg.Channels.Telegram.AllowFrom) > 0 {
			tgConfig["allow_from"] = cfg.Channels.Telegram.AllowFrom
		}

		seeds = append(seeds, seed{
			name: "telegram", channelType: "telegram", display: "Telegram Bot",
			creds:  map[string]string{"token": cfg.Channels.Telegram.Token, "proxy": cfg.Channels.Telegram.Proxy},
			config: tgConfig,
		})
	}

	// Other channels: use {type}/default format (no legacy data to preserve).
	if cfg.Channels.Discord.Token != "" {
		seeds = append(seeds, seed{
			name: "discord/default", channelType: "discord", display: "Discord Bot",
			creds:  map[string]string{"token": cfg.Channels.Discord.Token},
			config: map[string]interface{}{"dm_policy": cfg.Channels.Discord.DMPolicy, "group_policy": cfg.Channels.Discord.GroupPolicy},
		})
	}

	if cfg.Channels.Feishu.AppID != "" && cfg.Channels.Feishu.AppSecret != "" {
		seeds = append(seeds, seed{
			name: "feishu/default", channelType: "feishu", display: "Feishu/Lark Bot",
			creds: map[string]string{
				"app_id": cfg.Channels.Feishu.AppID, "app_secret": cfg.Channels.Feishu.AppSecret,
				"encrypt_key": cfg.Channels.Feishu.EncryptKey, "verification_token": cfg.Channels.Feishu.VerificationToken,
			},
			config: map[string]interface{}{"dm_policy": cfg.Channels.Feishu.DMPolicy, "domain": cfg.Channels.Feishu.Domain},
		})
	}

	if cfg.Channels.Zalo.Token != "" {
		seeds = append(seeds, seed{
			name: "zalo_oa/default", channelType: "zalo_oa", display: "Zalo OA",
			creds:  map[string]string{"token": cfg.Channels.Zalo.Token, "webhook_secret": cfg.Channels.Zalo.WebhookSecret},
			config: map[string]interface{}{"dm_policy": cfg.Channels.Zalo.DMPolicy},
		})
	}

	if cfg.Channels.WhatsApp.BridgeURL != "" {
		seeds = append(seeds, seed{
			name: "whatsapp/default", channelType: "whatsapp", display: "WhatsApp",
			creds:  map[string]string{"bridge_url": cfg.Channels.WhatsApp.BridgeURL},
			config: map[string]interface{}{"dm_policy": cfg.Channels.WhatsApp.DMPolicy, "group_policy": cfg.Channels.WhatsApp.GroupPolicy},
		})
	}

	seeded := 0
	for _, s := range seeds {
		credsJSON, _ := json.Marshal(s.creds)
		cfgJSON, _ := json.Marshal(s.config)

		inst := &store.ChannelInstanceData{
			Name:        s.name,
			DisplayName: s.display,
			ChannelType: s.channelType,
			AgentID:     defaultAgentID,
			Credentials: credsJSON,
			Config:      cfgJSON,
			Enabled:     true,
			CreatedBy:   ownerID,
		}

		if err := stores.ChannelInstances.Create(ctx, inst); err != nil {
			slog.Debug("seed channel instance skipped (may already exist)", "name", s.name, "error", err)
			continue
		}
		seeded++
		slog.Info("seeded channel instance", "name", s.name, "type", s.channelType)
	}

	if seeded > 0 {
		fmt.Printf("  Seeded %d channel instance(s)\n", seeded)
	}
}

// seedConfigSecrets saves non-LLM/non-channel secrets to the config_secrets table.
// These are secrets that don't belong in llm_providers or channel_instances tables.
func seedConfigSecrets(ctx context.Context, stores *store.Stores, cfg *config.Config) {
	if stores.ConfigSecrets == nil {
		return
	}

	secrets := cfg.ExtractDBSecrets()
	seeded := 0
	for key, value := range secrets {
		if err := stores.ConfigSecrets.Set(ctx, key, value); err != nil {
			slog.Debug("seed config secret failed", "key", key, "error", err)
			continue
		}
		seeded++
	}

	if seeded > 0 {
		slog.Info("seeded config secrets", "count", seeded)
	}
}

// defaultPlaceholderProviders defines disabled placeholder providers seeded for
// UI discoverability. Users can later enable and configure them via the dashboard.
var defaultPlaceholderProviders = []store.LLMProviderData{
	{Name: "openrouter", DisplayName: "OpenRouter", ProviderType: store.ProviderOpenRouter, APIBase: "https://openrouter.ai/api/v1", Enabled: false},
	{Name: "synthetic", DisplayName: "Synthetic", ProviderType: store.ProviderOpenAICompat, APIBase: "https://api.synthetic.new/openai/v1", Enabled: false},
	{Name: "alicloud-api", DisplayName: "AliCloud API", ProviderType: store.ProviderDashScope, APIBase: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", Enabled: false},
	{Name: "alicloud-sub", DisplayName: "AliCloud Sub", ProviderType: store.ProviderBailian, APIBase: "https://coding-intl.dashscope.aliyuncs.com/v1", Enabled: false},
}

// seedDefaultPlaceholders inserts disabled placeholder providers so they
// appear in the UI for easy configuration. Idempotent: UNIQUE(name) constraint
// skips duplicates, and providers whose api_base already exists are skipped
// to avoid overwriting user-configured entries.
func seedDefaultPlaceholders(ctx context.Context, stores *store.Stores) {
	if stores.Providers == nil {
		return
	}

	// Build a set of existing api_base values to avoid seeding a placeholder
	// when a user-configured provider already uses that base URL.
	existing, err := stores.Providers.ListProviders(ctx)
	if err != nil {
		slog.Debug("seedDefaultPlaceholders: list providers failed", "error", err)
		return
	}
	existingBases := make(map[string]bool, len(existing))
	for _, p := range existing {
		if p.APIBase != "" {
			existingBases[p.APIBase] = true
		}
	}

	seeded := 0
	for _, ph := range defaultPlaceholderProviders {
		// Skip if a provider with this api_base already exists
		if ph.APIBase != "" && existingBases[ph.APIBase] {
			continue
		}

		p := ph // copy
		if err := stores.Providers.CreateProvider(ctx, &p); err != nil {
			slog.Debug("seed placeholder skipped (may already exist)", "name", ph.Name, "error", err)
			continue
		}
		seeded++
	}

	if seeded > 0 {
		fmt.Printf("  Seeded %d placeholder provider(s)\n", seeded)
	}
}

// resolveProviderAPIKey extracts the API key for a provider from the config.
func resolveProviderAPIKey(cfg *config.Config, providerName string) string {
	switch providerName {
	case "openrouter":
		return cfg.Providers.OpenRouter.APIKey
	case "anthropic":
		return cfg.Providers.Anthropic.APIKey
	case "openai":
		return cfg.Providers.OpenAI.APIKey
	case "groq":
		return cfg.Providers.Groq.APIKey
	case "deepseek":
		return cfg.Providers.DeepSeek.APIKey
	case "gemini":
		return cfg.Providers.Gemini.APIKey
	case "mistral":
		return cfg.Providers.Mistral.APIKey
	case "xai":
		return cfg.Providers.XAI.APIKey
	case "minimax":
		return cfg.Providers.MiniMax.APIKey
	case "cohere":
		return cfg.Providers.Cohere.APIKey
	case "perplexity":
		return cfg.Providers.Perplexity.APIKey
	default:
		return ""
	}
}

// resolveProviderType maps a provider name to its store.Provider* type constant.
func resolveProviderType(name string) string {
	switch name {
	case "anthropic":
		return store.ProviderAnthropicNative
	case "gemini":
		return store.ProviderGeminiNative
	case "minimax":
		return store.ProviderMiniMax
	case "openrouter":
		return store.ProviderOpenRouter
	case "groq":
		return store.ProviderGroq
	case "deepseek":
		return store.ProviderDeepSeek
	case "mistral":
		return store.ProviderMistral
	case "xai":
		return store.ProviderXAI
	case "cohere":
		return store.ProviderCohere
	case "perplexity":
		return store.ProviderPerplexity
	default:
		return store.ProviderOpenAICompat
	}
}

// resolveProviderAPIBase returns the default API base URL for known providers.
func resolveProviderAPIBase(providerName string) string {
	switch providerName {
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "anthropic":
		return "https://api.anthropic.com"
	case "openai":
		return "https://api.openai.com/v1"
	case "groq":
		return "https://api.groq.com/openai/v1"
	case "deepseek":
		return "https://api.deepseek.com/v1"
	case "gemini":
		return "https://generativelanguage.googleapis.com/v1beta/openai"
	case "mistral":
		return "https://api.mistral.ai/v1"
	case "xai":
		return "https://api.x.ai/v1"
	case "minimax":
		return "https://api.minimax.io/v1"
	case "cohere":
		return "https://api.cohere.com/v2"
	case "perplexity":
		return "https://api.perplexity.ai"
	default:
		return ""
	}
}
