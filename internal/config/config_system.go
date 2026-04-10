package config

import (
	"strconv"
	"strings"
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
	booleanValue := func(key string, dst *bool) {
		if v, ok := configs[key]; ok && v != "" {
			*dst = v == "true" || v == "1"
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
	integer("gateway.task_recovery_interval_sec", &c.Gateway.TaskRecoveryIntervalSec)

	// Background workers (vault enrichment, consolidation)
	str("background.provider", &c.Gateway.BackgroundProvider)
	str("background.model", &c.Gateway.BackgroundModel)

	// Tools
	str("tools.profile", &c.Tools.Profile)
	integer("tools.rate_limit_per_hour", &c.Tools.RateLimitPerHour)
	boolean("tools.scrub_credentials", &c.Tools.ScrubCredentials)
	if raw, ok := configs["tools.web.provider_order"]; ok && raw != "" {
		parts := strings.Split(raw, ",")
		order := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.ToLower(strings.TrimSpace(part))
			if part == "" {
				continue
			}
			order = append(order, part)
		}
		c.Tools.Web.ProviderOrder = order
	}
	booleanValue("tools.web.exa.enabled", &c.Tools.Web.Exa.Enabled)
	integer("tools.web.exa.max_results", &c.Tools.Web.Exa.MaxResults)
	booleanValue("tools.web.tavily.enabled", &c.Tools.Web.Tavily.Enabled)
	integer("tools.web.tavily.max_results", &c.Tools.Web.Tavily.MaxResults)
	booleanValue("tools.web.brave.enabled", &c.Tools.Web.Brave.Enabled)
	integer("tools.web.brave.max_results", &c.Tools.Web.Brave.MaxResults)
	booleanValue("tools.web.duckduckgo.enabled", &c.Tools.Web.DuckDuckGo.Enabled)
	integer("tools.web.duckduckgo.max_results", &c.Tools.Web.DuckDuckGo.MaxResults)

	// TTS
	str("tts.provider", &c.Tts.Provider)
	str("tts.auto", &c.Tts.Auto)
	str("tts.mode", &c.Tts.Mode)
	integer("tts.max_length", &c.Tts.MaxLength)

	// Cron
	integer("cron.max_retries", &c.Cron.MaxRetries)
	str("cron.default_timezone", &c.Cron.DefaultTimezone)

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
}
