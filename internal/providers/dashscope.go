package providers

import (
	"context"
	"log/slog"
	"maps"
)

const (
	dashscopeDefaultBase  = "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"
	dashscopeDefaultModel = "qwen3-max"
)

// dashscopeThinkingModels lists DashScope models that accept the
// enable_thinking / thinking_budget parameters (Qwen3 open-weight and Qwen3.5 series).
// Models NOT in this set (e.g. qwen3-plus, qwen3-turbo) will silently
// skip thinking injection to avoid API "model not supported" errors.
var dashscopeThinkingModels = map[string]bool{
	// Qwen3.5 series — thinking + vision
	"qwen3.5-plus":    true,
	"qwen3.5-turbo":   true,
	// Qwen3 hosted
	"qwen3-max":       true,
	// Qwen3 open-weight (available as hosted inference)
	"qwen3-235b-a22b": true,
	"qwen3-32b":       true,
	"qwen3-14b":       true,
	"qwen3-8b":        true,
}

// DashScopeProvider wraps OpenAIProvider to handle DashScope-specific behaviors.
// Critical: DashScope does NOT support tools + streaming simultaneously.
// When tools are present, ChatStream falls back to non-streaming Chat().
type DashScopeProvider struct {
	*OpenAIProvider
}

func NewDashScopeProvider(name, apiKey, apiBase, defaultModel string) *DashScopeProvider {
	if apiBase == "" {
		apiBase = dashscopeDefaultBase
	}
	if defaultModel == "" {
		defaultModel = dashscopeDefaultModel
	}
	return &DashScopeProvider{
		OpenAIProvider: NewOpenAIProvider(name, apiKey, apiBase, defaultModel),
	}
}

// Name is inherited from the embedded OpenAIProvider (returns the user-specified name).
func (p *DashScopeProvider) SupportsThinking() bool { return true }

// Capabilities implements CapabilitiesAware for pipeline code-path selection.
// StreamWithTools=false is THE critical difference: DashScope falls back to
// non-streaming when tools are present.
func (p *DashScopeProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		Streaming:        true,
		ToolCalling:      true,
		StreamWithTools:  false,
		Thinking:         true,
		Vision:           true,
		CacheControl:     false,
		MaxContextWindow: 128_000,
		TokenizerID:      "cl100k_base",
	}
}

// ModelSupportsThinking implements ModelThinkingCapable.
// Returns true only for models that accept enable_thinking / thinking_budget.
func (p *DashScopeProvider) ModelSupportsThinking(model string) bool {
	return dashscopeThinkingModels[p.resolveModel(model)]
}

// applyThinkingGuard maps thinking_level to DashScope-specific params
// (enable_thinking / thinking_budget) only when the model supports it.
// Returns the (possibly mutated) request. Shared by Chat and ChatStream.
func (p *DashScopeProvider) applyThinkingGuard(req ChatRequest) ChatRequest {
	level, ok := req.Options[OptThinkingLevel].(string)
	if !ok || level == "" || level == "off" {
		return req
	}

	if p.ModelSupportsThinking(req.Model) {
		// Clone Options to avoid mutating caller's map
		opts := make(map[string]any, len(req.Options)+2)
		maps.Copy(opts, req.Options)
		opts[OptEnableThinking] = true
		opts[OptThinkingBudget] = dashscopeThinkingBudget(level)
		delete(opts, OptThinkingLevel) // don't pass generic key to OpenAI buildRequestBody
		req.Options = opts
	} else {
		slog.Debug("dashscope: model does not support thinking, skipping enable_thinking",
			"model", p.resolveModel(req.Model), "requested_level", level)
	}

	return req
}

// Chat overrides OpenAIProvider.Chat to apply the per-model thinking guard.
func (p *DashScopeProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return p.OpenAIProvider.Chat(ctx, p.applyThinkingGuard(req))
}

// ChatStream handles DashScope's limitation: tools + streaming cannot coexist.
// When tools are present, falls back to non-streaming Chat() and synthesizes
// chunk callbacks for the caller.
func (p *DashScopeProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	req = p.applyThinkingGuard(req)

	if len(req.Tools) > 0 {
		slog.Debug("dashscope: tools present, falling back to non-streaming Chat")
		resp, err := p.OpenAIProvider.Chat(ctx, req)
		if err != nil {
			return nil, err
		}
		if onChunk != nil {
			if resp.Thinking != "" {
				onChunk(StreamChunk{Thinking: resp.Thinking})
			}
			if resp.Content != "" {
				onChunk(StreamChunk{Content: resp.Content})
			}
			onChunk(StreamChunk{Done: true})
		}
		return resp, nil
	}
	return p.OpenAIProvider.ChatStream(ctx, req, onChunk)
}

// dashscopeThinkingBudget maps a thinking level to a DashScope thinking_budget value.
func dashscopeThinkingBudget(level string) int {
	switch level {
	case "low":
		return 4096
	case "medium":
		return 16384
	case "high":
		return 32768
	default:
		return 16384
	}
}
