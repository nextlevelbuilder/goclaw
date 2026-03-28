package providers

import (
	"context"
	"maps"
)

const (
	minimaxDefaultBase  = "https://api.minimax.io/v1"
	minimaxDefaultModel = "MiniMax-M2.7"
)

// MiniMaxProvider wraps OpenAIProvider to handle MiniMax-specific behaviors:
//   - Temperature clamping: MiniMax requires temperature in (0.0, 1.0].
//     Values ≤ 0 are removed (server default), values > 1 are clamped to 1.0.
//   - Uses the standard OpenAI-compatible /chat/completions endpoint.
//   - Sets providerType to "minimax_native" for media tool routing.
type MiniMaxProvider struct {
	*OpenAIProvider
}

// NewMiniMaxProvider creates a new MiniMax LLM provider.
func NewMiniMaxProvider(name, apiKey, apiBase, defaultModel string) *MiniMaxProvider {
	if apiBase == "" {
		apiBase = minimaxDefaultBase
	}
	if defaultModel == "" {
		defaultModel = minimaxDefaultModel
	}
	p := &MiniMaxProvider{
		OpenAIProvider: NewOpenAIProvider(name, apiKey, apiBase, defaultModel),
	}
	p.OpenAIProvider.providerType = "minimax_native"
	return p
}

// SupportsThinking returns true — MiniMax M2.7 supports reasoning_effort.
func (p *MiniMaxProvider) SupportsThinking() bool { return true }

// clampTemperature applies MiniMax's temperature constraint.
// MiniMax accepts temperature in (0.0, 1.0]. Zero or negative values
// are removed (letting the server use its default), values > 1 are clamped.
func (p *MiniMaxProvider) clampTemperature(req ChatRequest) ChatRequest {
	temp, ok := req.Options[OptTemperature]
	if !ok {
		return req
	}

	var tempVal float64
	switch v := temp.(type) {
	case float64:
		tempVal = v
	case float32:
		tempVal = float64(v)
	case int:
		tempVal = float64(v)
	default:
		return req
	}

	opts := make(map[string]any, len(req.Options))
	maps.Copy(opts, req.Options)

	if tempVal <= 0 {
		// MiniMax rejects temperature=0; remove to use server default
		delete(opts, OptTemperature)
	} else if tempVal > 1.0 {
		opts[OptTemperature] = 1.0
	}

	req.Options = opts
	return req
}

// Chat overrides OpenAIProvider.Chat to apply temperature clamping.
func (p *MiniMaxProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return p.OpenAIProvider.Chat(ctx, p.clampTemperature(req))
}

// ChatStream overrides OpenAIProvider.ChatStream to apply temperature clamping.
func (p *MiniMaxProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	return p.OpenAIProvider.ChatStream(ctx, p.clampTemperature(req), onChunk)
}
