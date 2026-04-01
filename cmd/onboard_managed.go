package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

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

// defaultPlaceholderProviders defines disabled placeholder providers seeded for
// UI discoverability. Users can later enable and configure them via the dashboard.
var defaultPlaceholderProviders = []store.LLMProviderData{
	{Name: "openrouter", DisplayName: "OpenRouter", ProviderType: store.ProviderOpenRouter, APIBase: "https://openrouter.ai/api/v1", Enabled: false},
	{Name: "xai", DisplayName: "xAI (Grok)", ProviderType: store.ProviderXAI, APIBase: "https://api.x.ai/v1", Enabled: false},
	{Name: "synthetic", DisplayName: "Synthetic", ProviderType: store.ProviderOpenAICompat, APIBase: "https://api.synthetic.new/openai/v1", Enabled: false},
	{Name: "alicloud-api", DisplayName: "AliCloud API", ProviderType: store.ProviderDashScope, APIBase: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", Enabled: false},
	{Name: "alicloud-sub", DisplayName: "AliCloud Sub", ProviderType: store.ProviderBailian, APIBase: "https://coding-intl.dashscope.aliyuncs.com/v1", Enabled: false},
	{Name: "zai", DisplayName: "Z.ai API", ProviderType: store.ProviderZai, APIBase: "https://api.z.ai/api/paas/v4", Enabled: false},
	{Name: "zai-coding", DisplayName: "Z.ai Coding Plan", ProviderType: store.ProviderZaiCoding, APIBase: "https://api.z.ai/api/coding/paas/v4", Enabled: false},
}

// seedOnboardPlaceholders opens a PG store and seeds disabled placeholder providers
// so they appear in the UI for easy configuration.
func seedOnboardPlaceholders(dsn string) error {
	storeCfg := store.StoreConfig{
		PostgresDSN:   dsn,
		EncryptionKey: os.Getenv("GOCLAW_ENCRYPTION_KEY"),
	}
	stores, err := pg.NewPGStores(storeCfg)
	if err != nil {
		return fmt.Errorf("open PG stores: %w", err)
	}

	if stores.Providers == nil {
		return nil
	}

	ctx := context.Background()

	// Build a set of existing api_base values to avoid overwriting user-configured entries.
	existing, err := stores.Providers.ListProviders(ctx)
	if err != nil {
		return fmt.Errorf("list providers: %w", err)
	}
	existingBases := make(map[string]bool, len(existing))
	for _, p := range existing {
		if p.APIBase != "" {
			existingBases[p.APIBase] = true
		}
	}

	seeded := 0
	for _, ph := range defaultPlaceholderProviders {
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
		slog.Info("seeded placeholder providers", "count", seeded)
	}
	return nil
}
