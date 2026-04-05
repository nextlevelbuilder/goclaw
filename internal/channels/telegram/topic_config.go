package telegram

import (
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// resolvedTopicConfig holds the merged config for a specific group+topic combination.
// Fields are resolved in order: global TelegramConfig → wildcard group ("*") → specific group → specific topic.
// TS ref: resolveTelegramGroupConfig() in src/telegram/bot.ts + resolveTelegramGroupPromptSettings() in group-config-helpers.ts.
type resolvedTopicConfig struct {
	groupPolicy    string
	requireMention *bool
	mentionMode    string // "strict" (default) or "yield"
	allowFrom      []string
	enabled        *bool
	skills         []string // nil = inherit, non-nil = override (empty = no skills)
	tools          []string // nil = inherit (all tools), non-nil = override (supports "group:xxx")
	systemPrompt   string   // concatenated group + topic prompts
}

// resolveTopicConfig resolves the effective config for a chat/topic by merging layers.
// chatIDStr is the raw Telegram chat ID (e.g., "-100123456").
// topicID is the forum topic thread ID (0 = not a forum topic).
func resolveTopicConfig(cfg config.TelegramConfig, chatIDStr string, topicID int) resolvedTopicConfig {
	result := resolvedTopicConfig{
		groupPolicy:    cfg.GroupPolicy,
		requireMention: cfg.RequireMention,
		mentionMode:    cfg.MentionMode,
		allowFrom:      cfg.AllowFrom,
	}

	if cfg.Groups == nil {
		return result
	}

	// Layer 1: wildcard group config ("*") — applies to all groups unless overridden.
	if wildcard, ok := cfg.Groups["*"]; ok && wildcard != nil {
		mergeGroupInto(&result, wildcard)
	}

	// Layer 2: specific group config — overrides wildcard.
	groupCfg, ok := cfg.Groups[chatIDStr]
	if !ok || groupCfg == nil {
		return result
	}
	mergeGroupInto(&result, groupCfg)

	// Layer 3: specific topic config — overrides group.
	if topicID > 0 && groupCfg.Topics != nil {
		topicIDStr := fmt.Sprintf("%d", topicID)
		if topicCfg, ok := groupCfg.Topics[topicIDStr]; ok && topicCfg != nil {
			mergeTopicInto(&result, topicCfg, groupCfg)
		}
	}

	return result
}

// mergeGroupInto applies non-zero group config values over the current result.
func mergeGroupInto(dst *resolvedTopicConfig, src *config.TelegramGroupConfig) {
	if src.GroupPolicy != "" {
		dst.groupPolicy = src.GroupPolicy
	}
	if src.RequireMention != nil {
		dst.requireMention = src.RequireMention
	}
	if src.MentionMode != "" {
		dst.mentionMode = src.MentionMode
	}
	if len(src.AllowFrom) > 0 {
		dst.allowFrom = src.AllowFrom
	}
	if src.Enabled != nil {
		dst.enabled = src.Enabled
	}
	if src.Skills != nil {
		dst.skills = src.Skills
	}
	if src.Tools != nil {
		dst.tools = src.Tools
	}
	if src.SystemPrompt != "" {
		dst.systemPrompt = src.SystemPrompt
	}
}

// mergeTopicInto applies topic config values, with special handling for systemPrompt
// (concatenated: group + topic, matching TS resolveTelegramGroupPromptSettings).
func mergeTopicInto(dst *resolvedTopicConfig, src *config.TelegramTopicConfig, groupCfg *config.TelegramGroupConfig) {
	if src.GroupPolicy != "" {
		dst.groupPolicy = src.GroupPolicy
	}
	if src.RequireMention != nil {
		dst.requireMention = src.RequireMention
	}
	if src.MentionMode != "" {
		dst.mentionMode = src.MentionMode
	}
	if len(src.AllowFrom) > 0 {
		dst.allowFrom = src.AllowFrom
	}
	if src.Enabled != nil {
		dst.enabled = src.Enabled
	}
	// Skills: topic overrides group (firstDefined pattern, matching TS).
	if src.Skills != nil {
		dst.skills = src.Skills
	}
	// Tools: topic overrides group (same firstDefined pattern as skills).
	if src.Tools != nil {
		dst.tools = src.Tools
	}
	// SystemPrompt: concatenate group + topic (both may exist, matching TS).
	if src.SystemPrompt != "" {
		parts := []string{}
		if groupCfg.SystemPrompt != "" {
			parts = append(parts, strings.TrimSpace(groupCfg.SystemPrompt))
		}
		parts = append(parts, strings.TrimSpace(src.SystemPrompt))
		dst.systemPrompt = strings.Join(parts, "\n\n")
	}
}

// isEnabled returns whether the resolved config allows the bot to operate.
// nil = enabled (default), false = disabled.
func (r *resolvedTopicConfig) isEnabled() bool {
	return r.enabled == nil || *r.enabled
}

// effectiveMentionMode returns the resolved mention_mode value.
// Falls back to the channel default if not overridden at group/topic level.
func (r *resolvedTopicConfig) effectiveMentionMode(defaultVal string) string {
	if r.mentionMode != "" {
		return r.mentionMode
	}
	return defaultVal
}

// effectiveRequireMention returns the resolved require_mention value.
// Falls back to the provided default if not overridden.
func (r *resolvedTopicConfig) effectiveRequireMention(defaultVal bool) bool {
	if r.requireMention != nil {
		return *r.requireMention
	}
	return defaultVal
}
