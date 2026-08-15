package pipeline

import "github.com/nextlevelbuilder/goclaw/internal/providers"

// InputContextTokens returns the provider-reported input occupancy for one
// request. Anthropic-style usage reports cached segments separately, while
// OpenAI-style prompt tokens already include them.
func InputContextTokens(usage providers.Usage) int {
	if usage.PromptTokensIncludeCachedSegments {
		return usage.PromptTokens
	}
	return usage.PromptTokens + usage.CacheReadTokens + usage.CacheCreationTokens
}

// AddUsage accumulates provider usage while preserving telemetry fields that
// are easy to drop when callers hand-roll partial sums.
func AddUsage(dst *providers.Usage, src providers.Usage) {
	if dst == nil {
		return
	}
	dst.PromptTokens += src.PromptTokens
	dst.CompletionTokens += src.CompletionTokens
	dst.TotalTokens += src.TotalTokens
	dst.CacheCreationTokens += src.CacheCreationTokens
	dst.CacheReadTokens += src.CacheReadTokens
	dst.PromptTokensIncludeCachedSegments = dst.PromptTokensIncludeCachedSegments || src.PromptTokensIncludeCachedSegments
	dst.ThinkingTokens += src.ThinkingTokens
	dst.RequestCount += src.RequestCount
	dst.ImageCount += src.ImageCount
	dst.WebSearchCount += src.WebSearchCount
}
