package providers

import "strings"

// isOpenAINativeEndpoint returns true for endpoints confirmed to be native OpenAI
// infrastructure that accepts the "developer" message role.
// Azure OpenAI, proxies, and other OpenAI-compatible backends only support "system".
// Matching OpenClaw TS: model-compat.ts → isOpenAINativeEndpoint().
func isOpenAINativeEndpoint(apiBase string) bool {
	// Extract hostname from the API base URL.
	lower := strings.ToLower(apiBase)
	return strings.Contains(lower, "api.openai.com")
}

// isFireworksEndpoint returns true for Fireworks AI endpoints.
// Fireworks requires stream=true for max_tokens > 4096.
func (p *OpenAIProvider) isFireworksEndpoint() bool {
	return strings.Contains(strings.ToLower(p.apiBase), "fireworks.ai")
}

// isTogetherEndpoint returns true for Together AI inference hosts.
// Together rejects some OpenAI extensions (e.g. stream_options, reasoning_effort) with HTTP 400.
// Uses URL, provider_type, and name so reverse-proxied Together endpoints are also detected.
func (p *OpenAIProvider) isTogetherEndpoint() bool {
	b := strings.ToLower(p.apiBase)
	if strings.Contains(b, "together.xyz") || strings.Contains(b, "together.ai") {
		return true
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(p.providerType)), "together") {
		return true
	}
	if strings.Contains(strings.ToLower(p.name), "together") {
		return true
	}
	return false
}

// isDashScopeAPIBase returns true for Alibaba DashScope OpenAI-compatible endpoints.
func isDashScopeAPIBase(apiBase string) bool {
	return strings.Contains(strings.ToLower(apiBase), "dashscope")
}

// dashScopePassthroughKeys is true when enable_thinking / thinking_budget may be added to the JSON body.
// Uses the same DashScope/Bailian route detection as prompt-cache wrapping.
func (p *OpenAIProvider) dashScopePassthroughKeys() bool {
	return p.isDashScope()
}

// isOllamaEndpoint returns true for local or cloud Ollama inference endpoints.
// Ollama requires options.num_ctx in the request body to control the context window
// (prevents context-window errors on long conversations).
// Uses 3-source detection (URL + providerType + name) to handle reverse-proxied endpoints.
func (p *OpenAIProvider) isOllamaEndpoint() bool {
	if strings.Contains(strings.ToLower(p.apiBase), "ollama") {
		return true
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(p.providerType)), "ollama") {
		return true
	}
	if strings.Contains(strings.ToLower(p.name), "ollama") {
		return true
	}
	return false
}

// ollamaNativeURL returns the full URL for Ollama's native /api/chat endpoint.
// Ollama's OpenAI-compat shim at /v1/chat/completions silently ignores options.num_ctx,
// while the native /api/chat endpoint honors it. The apiBase may include a /v1 suffix
// (e.g. "http://localhost:11434/v1") — it is stripped before appending /api/chat.
func (p *OpenAIProvider) ollamaNativeURL() string {
	base := strings.TrimRight(strings.TrimSuffix(strings.TrimRight(p.apiBase, "/"), "/v1"), "/")
	return base + "/api/chat"
}

// isDashScope returns true when this provider routes requests to DashScope/Bailian
// (supports cache_control:ephemeral wire format - verified live 2026-05-08).
// Uses 3-source detection (URL + providerType + name) to handle reverse-proxied
// DashScope endpoints. Includes "bailian" because live qwen-richard provider has
// provider_type=bailian.
//
// Used by buildRequestBody to wrap system content with Anthropic-style
// cache_control blocks for prompt caching (90% discount on cached prefix tokens).
func (p *OpenAIProvider) isDashScope() bool {
	if isDashScopeAPIBase(p.apiBase) {
		return true
	}
	pt := strings.ToLower(strings.TrimSpace(p.providerType))
	if strings.Contains(pt, "dashscope") || strings.Contains(pt, "bailian") {
		return true
	}
	name := strings.ToLower(p.name)
	if strings.Contains(name, "dashscope") || strings.Contains(name, "bailian") {
		return true
	}
	return false
}
