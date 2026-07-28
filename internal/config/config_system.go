package config

import (
	"encoding/json"
	"strconv"
)

// ApplySystemConfigs overlays system_configs DB values onto the in-memory config.
// Called after startup seed and after config.apply/patch to keep cfg.* in sync with DB.
// Follows the same pattern as ApplyDBSecrets — non-empty DB values override config.json values.
// Keys must match those in cmd/gateway_system_config_sync.go seedConfigForContext().
func (c *Config) ApplySystemConfigs(configs map[string]string) {
	str := func(key string, dst *string) {
		if v, ok := configs[key]; ok && v != "" {
			*dst = v
		}
	}
	integer := func(key string, dst *int) {
		if v, ok := configs[key]; ok && v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				*dst = n
			}
		}
	}
	boolean := func(key string, dst **bool) {
		if v, ok := configs[key]; ok && v != "" {
			b := v == "true" || v == "1"
			*dst = &b
		}
	}

	// Embedding
	if c.Agents.Defaults.Memory == nil {
		c.Agents.Defaults.Memory = &MemoryConfig{}
	}
	str("embedding.provider", &c.Agents.Defaults.Memory.EmbeddingProvider)
	str("embedding.model", &c.Agents.Defaults.Memory.EmbeddingModel)
	integer("embedding.max_chunk_len", &c.Agents.Defaults.Memory.MaxChunkLen)
	integer("embedding.chunk_overlap", &c.Agents.Defaults.Memory.ChunkOverlap)

	// Agent defaults
	str("agent.default_provider", &c.Agents.Defaults.Provider)
	str("agent.default_model", &c.Agents.Defaults.Model)
	integer("agent.context_window", &c.Agents.Defaults.ContextWindow)
	integer("agent.max_tool_iterations", &c.Agents.Defaults.MaxToolIterations)

	// Gateway behavior
	integer("gateway.rate_limit_rpm", &c.Gateway.RateLimitRPM)
	integer("gateway.max_message_chars", &c.Gateway.MaxMessageChars)
	str("gateway.injection_action", &c.Gateway.InjectionAction)
	integer("gateway.inbound_debounce_ms", &c.Gateway.InboundDebounceMs)
	boolean("gateway.block_reply", &c.Gateway.BlockReply)
	boolean("gateway.tool_status", &c.Gateway.ToolStatus)
	// NOTE: gateway.team_work_classify / _provider / _model are intentionally
	// NOT overlaid onto the shared cfg here. They are resolved per-tenant at
	// request time by internal/teamworkconfig.Resolver — overlaying them onto the
	// process-wide *config.Config leaked one tenant's Team Work settings to all
	// others. See cmd/gateway.go (deps.teamWorkCfg) and the system-config-changed
	// subscriber's per-tenant Invalidate.
	//
	// gateway.task_recovery_interval_sec is ALSO not overlaid here (Phase 7
	// Decision 8): it configures a single process-wide recovery ticker built once
	// at startup (cmd/gateway_lifecycle.go), which never re-reads the field. This
	// dynamic path runs on EVERY system-config-changed event carrying the CHANGING
	// tenant's context (cmd/gateway_http_wiring.go), so overlaying it here let any
	// tenant's edit mutate the shared runtime value — an ineffective (ticker never
	// re-reads) cross-tenant write. It is a startup-only, master-managed key,
	// applied exactly once from the master-tenant seed via ApplyStartupSystemConfigs.
	integer("gateway.webhook_async_timeout_sec", &c.Gateway.WebhookAsyncTimeoutSec)
	integer("gateway.webhook_sync_timeout_sec", &c.Gateway.WebhookSyncTimeoutSec)
	boolean("gateway.webhook_stream", &c.Gateway.WebhookStream)

	// Background workers (vault enrichment, consolidation)
	str("background.provider", &c.Gateway.BackgroundProvider)
	str("background.model", &c.Gateway.BackgroundModel)

	// Tools
	str("tools.profile", &c.Tools.Profile)
	integer("tools.rate_limit_per_hour", &c.Tools.RateLimitPerHour)
	boolean("tools.scrub_credentials", &c.Tools.ScrubCredentials)
	boolValue := func(key string, dst *bool) {
		if v, ok := configs[key]; ok && v != "" {
			*dst = v == "true" || v == "1"
		}
	}
	boolValue("tools.browser.enabled", &c.Tools.Browser.Enabled)
	boolValue("tools.browser.headless", &c.Tools.Browser.Headless)
	str("tools.browser.remote_url", &c.Tools.Browser.RemoteURL)
	integer("tools.browser.action_timeout_ms", &c.Tools.Browser.ActionTimeoutMs)
	integer("tools.browser.idle_timeout_ms", &c.Tools.Browser.IdleTimeoutMs)
	integer("tools.browser.max_pages", &c.Tools.Browser.MaxPages)
	boolValue("tools.browser.cookie_sync_enabled", &c.Tools.Browser.CookieSyncEnabled)

	// Skills
	integer(SkillMaxUploadSizeSystemConfigKey, &c.Skills.MaxUploadSizeMB)
	c.Skills.MaxUploadSizeMB = ClampSkillMaxUploadSizeMB(c.Skills.MaxUploadSizeMB)
	boolean(SkillSlashCommandsEnabledSystemConfigKey, &c.Skills.SlashCommands.Enabled)
	boolean(SkillSlashSuggestNotFoundSystemConfigKey, &c.Skills.SlashCommands.SuggestNotFound)
	boolValue(SkillSlashPartialMatchingSystemConfigKey, &c.Skills.SlashCommands.PartialMatching)
	str(SkillSlashCommandPrefixSystemConfigKey, &c.Skills.SlashCommands.Prefix)

	// Providers
	integer("providers.request_timeout_sec", &c.Providers.RequestTimeoutSec)

	// TTS
	str("tts.provider", &c.Tts.Provider)
	str("tts.auto", &c.Tts.Auto)
	str("tts.mode", &c.Tts.Mode)
	integer("tts.max_length", &c.Tts.MaxLength)
	integer("tts.timeout_ms", &c.Tts.TimeoutMs)

	// Cron
	integer("cron.max_retries", &c.Cron.MaxRetries)
	str("cron.default_timezone", &c.Cron.DefaultTimezone)
	boolValue("cron.command_enabled", &c.Cron.CommandEnabled)

	// Pending message compaction
	if _, ok := configs["compaction.threshold"]; ok {
		if c.Channels.PendingCompaction == nil {
			c.Channels.PendingCompaction = &PendingCompactionConfig{}
		}
		pc := c.Channels.PendingCompaction
		integer("compaction.threshold", &pc.Threshold)
		integer("compaction.keep_recent", &pc.KeepRecent)
		integer("compaction.max_tokens", &pc.MaxTokens)
		str("compaction.provider", &pc.Provider)
		str("compaction.model", &pc.Model)
	}

	// Allowed paths (JSON array)
	if v, ok := configs["allowed_paths"]; ok && v != "" {
		var paths []string
		if err := json.Unmarshal([]byte(v), &paths); err == nil {
			c.Agents.Defaults.AllowedPaths = paths
		}
	}
}

// ApplyStartupSystemConfigs overlays the process-wide, startup-only system_configs
// keys — the ones read exactly once when a singleton is constructed and never
// re-read afterwards. It MUST be called only at startup, from the master-tenant
// seed, and MUST NOT be wired into the per-tenant system-config-changed subscriber
// (Phase 7 Decision 8).
//
// These keys are deliberately kept out of ApplySystemConfigs: that method runs on
// every system-config-changed event under the CHANGING tenant's context, so
// overlaying a startup-only key there let any tenant's edit mutate the shared
// runtime value — a write that is both ineffective (the consuming singleton never
// re-reads the field) and cross-tenant. Splitting them out makes "read once at
// startup, master-managed" explicit in the type, not just a comment.
//
// gateway.task_recovery_interval_sec — feeds the single team-task recovery ticker
// built once in cmd/gateway_lifecycle.go. Changing it takes effect on the next
// process restart; there is no live ticker reconfiguration in Phase 7.
func (c *Config) ApplyStartupSystemConfigs(configs map[string]string) {
	if v, ok := configs["gateway.task_recovery_interval_sec"]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Gateway.TaskRecoveryIntervalSec = n
		}
	}
}
