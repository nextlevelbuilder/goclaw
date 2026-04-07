package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// AnthropicAdapter implements ProviderAdapter for the Anthropic Messages API.
// Delegates to AnthropicProvider's buildRequestBody/parseResponse for DRY.
// Used by Pipeline to separate serialization from transport.
//
// Note: FromStreamChunk handles text/thinking deltas only. Tool call argument
// accumulation (input_json_delta) and signature tracking (signature_delta) are
// stateful — Pipeline must handle those externally when wiring adapters.
type AnthropicAdapter struct {
	provider *AnthropicProvider
}

// NewAnthropicAdapter creates an adapter from ProviderConfig.
func NewAnthropicAdapter(cfg ProviderConfig) (ProviderAdapter, error) {
	var opts []AnthropicOption
	if cfg.BaseURL != "" {
		opts = append(opts, WithAnthropicBaseURL(cfg.BaseURL))
	}
	if cfg.Model != "" {
		opts = append(opts, WithAnthropicModel(cfg.Model))
	}
	p := NewAnthropicProvider(cfg.APIKey, opts...)
	return &AnthropicAdapter{provider: p}, nil
}

func (a *AnthropicAdapter) Name() string { return "anthropic" }

// Capabilities delegates to the wrapped provider for single source of truth.
func (a *AnthropicAdapter) Capabilities() ProviderCapabilities {
	return a.provider.Capabilities()
}

// ToRequest converts ChatRequest to Anthropic Messages API JSON + headers.
// Defaults to stream=true; override via req.Options["stream"]=false.
func (a *AnthropicAdapter) ToRequest(req ChatRequest) ([]byte, http.Header, error) {
	stream := true
	if v, ok := req.Options["stream"].(bool); ok {
		stream = v
	}

	model := resolveAnthropicModel(req.Model, a.provider.defaultModel, a.provider.registry)
	body := a.provider.buildRequestBody(model, req, stream)

	data, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("anthropic adapter: marshal: %w", err)
	}

	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	h.Set("x-api-key", a.provider.apiKey)
	h.Set("anthropic-version", anthropicAPIVersion)

	// Add beta header for interleaved thinking
	if _, hasThinking := body["thinking"]; hasThinking {
		h.Set("anthropic-beta", "interleaved-thinking-2025-05-14")
	}

	return data, h, nil
}

// FromResponse parses Anthropic Messages API response JSON into ChatResponse.
func (a *AnthropicAdapter) FromResponse(data []byte) (*ChatResponse, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("anthropic adapter: decode: %w", err)
	}
	return a.provider.parseResponse(&resp), nil
}

// FromStreamChunk parses a single Anthropic SSE event payload.
// Returns content/thinking for delta events, Done for message_stop.
// Returns nil for non-content events (message_start, content_block_start, etc.).
func (a *AnthropicAdapter) FromStreamChunk(data []byte) (*StreamChunk, error) {
	// Anthropic SSE data includes a "type" field for event dispatch
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, nil
	}

	switch envelope.Type {
	case "content_block_delta":
		var ev anthropicContentBlockDeltaEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			return nil, nil
		}
		switch ev.Delta.Type {
		case "text_delta":
			return &StreamChunk{Content: ev.Delta.Text}, nil
		case "thinking_delta":
			return &StreamChunk{Thinking: ev.Delta.Thinking}, nil
		// input_json_delta and signature_delta are stateful (accumulate across chunks).
		// Pipeline must track these externally; adapter only handles atomic deltas.
		}

	case "message_stop":
		return &StreamChunk{Done: true}, nil

	case "error":
		var ev anthropicErrorEvent
		if err := json.Unmarshal(data, &ev); err == nil {
			return nil, fmt.Errorf("anthropic stream: %s: %s", ev.Error.Type, ev.Error.Message)
		}
	}

	return nil, nil
}
