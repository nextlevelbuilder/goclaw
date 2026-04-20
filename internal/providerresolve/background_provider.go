package providerresolve

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ResolveBackgroundProvider resolves the LLM provider for background workers.
// Fallback chain: background.provider → agent.default_provider → first registered.
// Used by vault enrichment, episodic summarization, dreaming consolidation.
func ResolveBackgroundProvider(
	ctx context.Context,
	tenantID uuid.UUID,
	registry *providers.Registry,
	systemConfigs store.SystemConfigStore,
) (providers.Provider, string) {
	if registry == nil {
		return nil, ""
	}

	// Load system configs for the tenant
	var configs map[string]string
	if systemConfigs != nil {
		tctx := store.WithTenantID(ctx, tenantID)
		var err error
		configs, err = systemConfigs.List(tctx)
		if err != nil {
			slog.Warn("background: failed to load system_configs", "tenant", tenantID, "error", err)
		} else {
			slog.Debug("background: loaded system_configs",
				"tenant", tenantID,
				"background.provider", configs["background.provider"],
				"agent.default_provider", configs["agent.default_provider"])
		}
	}

	// tryResolve attempts to get a provider by name
	tryResolve := func(name, model, source string) (providers.Provider, string, bool) {
		if name == "" {
			return nil, "", false
		}
		p, err := registry.GetForTenant(tenantID, name)
		if err != nil || p == nil {
			slog.Debug("background: provider not found",
				"source", source, "name", name, "tenant", tenantID, "error", err)
			return nil, "", false
		}
		if model == "" {
			model = p.DefaultModel()
		}
		if model == "" {
			model = firstAvailableModel(ctx, p, source, tenantID)
		}
		if model == "" {
			slog.Warn("background: provider resolved but model is empty — skipping",
				"source", source, "name", name, "tenant", tenantID,
				"hint", "set background.model / agent.default_model in system_configs, or default_model on the provider")
			return nil, "", false
		}
		slog.Debug("background: resolved provider",
			"source", source, "name", name, "model", model, "tenant", tenantID)
		return p, model, true
	}

	// 1. Explicit background config
	if p, m, ok := tryResolve(configs["background.provider"], configs["background.model"], "background.provider"); ok {
		return p, m
	}
	// 2. Agent default provider
	if p, m, ok := tryResolve(configs["agent.default_provider"], configs["agent.default_model"], "agent.default_provider"); ok {
		return p, m
	}
	// 3. First registered provider (fallback)
	names := registry.ListForTenant(tenantID)
	if len(names) == 0 {
		slog.Warn("background: no providers available", "tenant", tenantID)
		return nil, ""
	}
	p, err := registry.GetForTenant(tenantID, names[0])
	if err != nil {
		slog.Warn("background: fallback provider failed", "tenant", tenantID, "name", names[0], "error", err)
		return nil, ""
	}
	model := p.DefaultModel()
	if model == "" {
		model = firstAvailableModel(ctx, p, "fallback", tenantID)
	}
	if model == "" {
		slog.Warn("background: fallback provider has empty default_model and no upstream models — skipping",
			"tenant", tenantID, "provider", names[0],
			"hint", "set background.model / agent.default_model in system_configs, or default_model on the provider")
		return nil, ""
	}
	slog.Warn("background: using fallback provider (no explicit config)",
		"tenant", tenantID, "provider", names[0], "model", model, "available", names,
		"background.provider", configs["background.provider"],
		"agent.default_provider", configs["agent.default_provider"])
	return p, model
}

// firstAvailableModel asks the provider to enumerate upstream models and
// returns the first ID, if the provider implements ModelLister. Logs and
// returns "" on any failure. Used as a last-resort fallback for background
// workers when no default_model is configured.
func firstAvailableModel(ctx context.Context, p providers.Provider, source string, tenantID uuid.UUID) string {
	lister, ok := p.(providers.ModelLister)
	if !ok {
		return ""
	}
	lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	models, err := lister.ListModels(lctx)
	if err != nil {
		slog.Warn("background: ListModels failed",
			"source", source, "provider", p.Name(), "tenant", tenantID, "error", err)
		return ""
	}
	if len(models) == 0 {
		return ""
	}
	slog.Info("background: auto-picked first available model",
		"source", source, "provider", p.Name(), "tenant", tenantID,
		"model", models[0], "total_available", len(models))
	return models[0]
}
