package providers

import "os"

// wrapSystemForDashScopeCache transforms a system message string content into
// Anthropic-style content blocks with cache_control:ephemeral markers.
//
// DashScope verified 2026-05-08 to accept and process this wire format on
// coding-intl.dashscope.aliyuncs.com. Result: 90% discount on cached prefix
// tokens, 5min sliding TTL.
//
// Non-system messages and non-string content pass through unchanged
// (idempotent, supports already-blocked input).
func wrapSystemForDashScopeCache(msg map[string]any) map[string]any {
	if msg["role"] != "system" {
		return msg
	}
	content, ok := msg["content"].(string)
	if !ok {
		return msg
	}
	msg["content"] = SplitSystemPromptForCache(content)
	return msg
}

// applyDashScopeToolPrefixCache adds cache_control:ephemeral to the last tool
// definition, caching the entire tool prefix (descriptions, schemas).
//
// alreadyMarked: cache markers already consumed by system message blocks.
// DashScope limits 4 markers/request; skip tool marker if limit reached.
func applyDashScopeToolPrefixCache(tools []map[string]any, alreadyMarked int) []map[string]any {
	if len(tools) == 0 || alreadyMarked >= 4 {
		return tools
	}
	last := tools[len(tools)-1]
	last["cache_control"] = map[string]any{"type": "ephemeral"}
	return tools
}

// countCacheControlMarkers counts cache_control fields in a message's content
// blocks. Used to track marker budget across system + tools.
func countCacheControlMarkers(msg map[string]any) int {
	blocks, ok := msg["content"].([]map[string]any)
	if !ok {
		return 0
	}
	count := 0
	for _, b := range blocks {
		if b["cache_control"] != nil {
			count++
		}
	}
	return count
}

// dashScopeCacheDisabled returns true when env var GOCLAW_DISABLE_DASHSCOPE_CACHE
// is set to a truthy value. Provides runtime escape hatch without requiring
// code redeploy or config change.
func dashScopeCacheDisabled() bool {
	v := os.Getenv("GOCLAW_DISABLE_DASHSCOPE_CACHE")
	return v == "true" || v == "1" || v == "yes"
}
