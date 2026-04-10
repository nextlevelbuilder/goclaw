package cmd

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// syncSystemConfigs upserts non-secret config values into the system_configs table.
// Seeds per-tenant entries for all known tenants using real tenant IDs.
// When onlyMissing is true, existing keys are preserved (used at startup).
// When onlyMissing is false, all keys are upserted (used after config.apply/patch).
func syncSystemConfigs(sc store.SystemConfigStore, ts store.TenantStore, cfg *config.Config, onlyMissing bool) {
	if sc == nil {
		return
	}

	// Enumerate tenants and seed each one
	if ts != nil {
		tenants, err := ts.ListTenants(context.Background())
		if err != nil {
			slog.Warn("failed to list tenants for system config seed", "error", err)
			// Fall back to master tenant only
			masterCtx := store.WithTenantID(context.Background(), store.MasterTenantID)
			seedConfigForContext(masterCtx, sc, cfg, onlyMissing)
			return
		}

		if len(tenants) == 0 {
			// No tenants yet (fresh install before onboard) → seed master tenant
			masterCtx := store.WithTenantID(context.Background(), store.MasterTenantID)
			seedConfigForContext(masterCtx, sc, cfg, onlyMissing)
			return
		}

		for _, t := range tenants {
			tenantCtx := store.WithTenantID(context.Background(), t.ID)
			seedConfigForContext(tenantCtx, sc, cfg, onlyMissing)
		}
	} else {
		// No tenant store → seed master tenant
		masterCtx := store.WithTenantID(context.Background(), store.MasterTenantID)
		seedConfigForContext(masterCtx, sc, cfg, onlyMissing)
	}
}

// seedConfigForContext writes config keys for a specific tenant context.
func seedConfigForContext(ctx context.Context, sc store.SystemConfigStore, cfg *config.Config, onlyMissing bool) {
	var existing map[string]string
	if onlyMissing {
		var err error
		existing, err = sc.List(ctx)
		if err != nil {
			slog.Warn("failed to list system_configs for seed", "error", err)
			return
		}
	}

	set := func(key, val string) {
		if val == "" {
			return
		}
		if onlyMissing {
			if _, ok := existing[key]; ok {
				return
			}
		}
		if err := sc.Set(ctx, key, val); err != nil {
			slog.Warn("failed to sync system config", "key", key, "error", err)
		}
	}
	setInt := func(key string, val int) {
		if val != 0 {
			set(key, fmt.Sprintf("%d", val))
		}
	}
	setBool := func(key string, val *bool) {
		if val != nil {
			set(key, fmt.Sprintf("%t", *val))
		}
	}

	// Embedding
	if m := cfg.Agents.Defaults.Memory; m != nil {
		set("embedding.provider", m.EmbeddingProvider)
		set("embedding.model", m.EmbeddingModel)
		setInt("embedding.max_chunk_len", m.MaxChunkLen)
		setInt("embedding.chunk_overlap", m.ChunkOverlap)
	}

	// Agent defaults
	set("agent.default_provider", cfg.Agents.Defaults.Provider)
	set("agent.default_model", cfg.Agents.Defaults.Model)
	setInt("agent.context_window", cfg.Agents.Defaults.ContextWindow)
	setInt("agent.max_tool_iterations", cfg.Agents.Defaults.MaxToolIterations)

	// Gateway behavior (host/port are infra — env/file only, not DB)
	setInt("gateway.rate_limit_rpm", cfg.Gateway.RateLimitRPM)
	setInt("gateway.max_message_chars", cfg.Gateway.MaxMessageChars)
	set("gateway.injection_action", cfg.Gateway.InjectionAction)
	setInt("gateway.inbound_debounce_ms", cfg.Gateway.InboundDebounceMs)
	setBool("gateway.block_reply", cfg.Gateway.BlockReply)
	setBool("gateway.tool_status", cfg.Gateway.ToolStatus)
	setInt("gateway.task_recovery_interval_sec", cfg.Gateway.TaskRecoveryIntervalSec)

	// Background workers
	set("background.provider", cfg.Gateway.BackgroundProvider)
	set("background.model", cfg.Gateway.BackgroundModel)

	// Tools
	set("tools.profile", cfg.Tools.Profile)
	setInt("tools.rate_limit_per_hour", cfg.Tools.RateLimitPerHour)
	setBool("tools.scrub_credentials", cfg.Tools.ScrubCredentials)

	// TTS
	set("tts.provider", cfg.Tts.Provider)
	set("tts.auto", cfg.Tts.Auto)
	set("tts.mode", cfg.Tts.Mode)
	setInt("tts.max_length", cfg.Tts.MaxLength)

	// Cron
	setInt("cron.max_retries", cfg.Cron.MaxRetries)
	set("cron.default_timezone", cfg.Cron.DefaultTimezone)

	// Pending message compaction
	if pc := cfg.Channels.PendingCompaction; pc != nil {
		setInt("compaction.threshold", pc.Threshold)
		setInt("compaction.keep_recent", pc.KeepRecent)
		setInt("compaction.max_tokens", pc.MaxTokens)
		set("compaction.provider", pc.Provider)
		set("compaction.model", pc.Model)
	}
}
