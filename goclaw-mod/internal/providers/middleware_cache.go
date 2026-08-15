package providers

// CacheMiddleware injects OpenAI prompt caching params (prompt_cache_key,
// prompt_cache_retention) for native OpenAI endpoints only.
// Silently passes through for proxies and non-OpenAI providers.
func CacheMiddleware(body map[string]any, cfg MiddlewareConfig) map[string]any {
	cacheKey, hasKey := cfg.Options[OptPromptCacheKey]
	retention, hasRetention := cfg.Options[OptPromptCacheRetention]

	if !hasKey && !hasRetention {
		return body // no cache options requested
	}

	// Only inject for native OpenAI endpoints
	if !isOpenAINativeEndpoint(cfg.APIBase) {
		return body
	}

	if hasKey {
		body["prompt_cache_key"] = cacheKey
	}
	if hasRetention {
		body["prompt_cache_retention"] = retention
	}

	return body
}
