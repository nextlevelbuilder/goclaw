package http

import (
	"log/slog"
	"regexp"
)

var slugRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// isValidSlug checks whether s matches the slug format: lowercase alphanumeric + hyphens,
// cannot start or end with a hyphen.
func isValidSlug(s string) bool {
	return slugRe.MatchString(s)
}

// filterAllowedKeys returns a new map containing only keys present in the allowlist.
// Defense-in-depth: prevents column injection and unauthorized field updates.
func filterAllowedKeys(updates map[string]any, allowed map[string]bool) map[string]any {
	filtered := make(map[string]any, len(updates))
	for k, v := range updates {
		if allowed[k] {
			filtered[k] = v
		} else {
			slog.Warn("security.filtered_unknown_field", "field", k)
		}
	}
	return filtered
}

// --- Field allowlists for update endpoints ---
// Each map lists the columns that HTTP clients may update.
// Immutable fields (id, owner_id, created_at, deleted_at) are excluded.

var agentAllowedFields = map[string]bool{
	"agent_key": true, "agent_type": true, "display_name": true,
	"provider": true, "model": true, "status": true,
	"context_window": true, "max_tool_iterations": true,
	"workspace": true,
	"frontmatter": true, "compaction_config": true,
	"memory_config": true, "other_config": true, "tools_config": true,
	"sandbox_config": true, "context_pruning": true,
	"is_default": true, "budget_monthly_cents": true, "subagents_config": true,
	// Promoted from other_config
	"emoji": true, "agent_description": true, "thinking_level": true, "max_tokens": true,
	"self_evolve": true, "skill_evolve": true, "skill_nudge_interval": true,
	"reasoning_config": true, "workspace_sharing": true, "chatgpt_oauth_routing": true,
	"shell_deny_groups": true, "kg_dedup_config": true,
}

var providerAllowedFields = map[string]bool{
	"name": true, "provider_type": true, "api_key": true,
	"api_base": true, "base_url": true, "default_model": true,
	"extra_headers": true, "config": true, "enabled": true,
	"display_name": true, "display_order": true, "settings": true,
}

var customToolAllowedFields = map[string]bool{
	"name": true, "description": true, "command": true,
	"parameters": true, "agent_id": true, "env": true,
	"tags": true, "requires": true, "timeout_seconds": true,
	"enabled": true,
}

var mcpServerAllowedFields = map[string]bool{
	"name": true, "transport": true, "command": true, "args": true,
	"url": true, "api_key": true, "env": true, "headers": true,
	"enabled": true, "tool_prefix": true, "timeout_sec": true,
	"agent_id": true, "config": true, "settings": true,
}

var channelInstanceAllowedFields = map[string]bool{
	"name": true, "channel_type": true, "credentials": true, "agent_id": true,
	"enabled": true, "group_policy": true, "allow_from": true,
	"metadata": true, "webhook_secret": true, "config": true,
	"display_name": true,
}
