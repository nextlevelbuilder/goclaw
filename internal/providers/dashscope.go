package providers

import (
	"context"
	"log/slog"
)

const (
	dashscopeDefaultBase  = "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"
	dashscopeDefaultModel = "qwen3-max"
)

// DashScopeProvider wraps OpenAIProvider to handle DashScope-specific behaviors.
// Critical: DashScope does NOT support tools + streaming simultaneously.
// When tools are present, ChatStream falls back to non-streaming Chat().
type DashScopeProvider struct {
	*OpenAIProvider
}

func NewDashScopeProvider(apiKey, apiBase, defaultModel string) *DashScopeProvider {
	if apiBase == "" {
		apiBase = dashscopeDefaultBase
	}
	if defaultModel == "" {
		defaultModel = dashscopeDefaultModel
	}
	return &DashScopeProvider{
		OpenAIProvider: NewOpenAIProvider("dashscope", apiKey, apiBase, defaultModel),
	}
}

func (p *DashScopeProvider) Name() string           { return "dashscope" }
func (p *DashScopeProvider) SupportsThinking() bool { return true }

// ChatStream handles DashScope's limitation: tools + streaming cannot coexist.
// When tools are present, falls back to non-streaming Chat() and synthesizes
// chunk callbacks for the caller.
func (p *DashScopeProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	// Map thinking_level to DashScope-specific params before passing to OpenAI base
	if level, ok := req.Options[OptThinkingLevel].(string); ok && level != "" && level != "off" {
		// Clone Options to avoid mutating caller's map
		opts := make(map[string]interface{}, len(req.Options)+2)
		for k, v := range req.Options {
			opts[k] = v
		}
		opts[OptEnableThinking] = true
		opts[OptThinkingBudget] = dashscopeThinkingBudget(level)
		delete(opts, OptThinkingLevel) // don't pass generic key to OpenAI buildRequestBody
		req.Options = opts
	}

	if len(req.Tools) > 0 {
		slog.Debug("dashscope: tools present, falling back to non-streaming Chat")
		resp, err := p.Chat(ctx, req)
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
