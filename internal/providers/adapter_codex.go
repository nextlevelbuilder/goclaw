package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// CodexAdapter implements ProviderAdapter for the OpenAI Responses API (Codex flow).
// Wire format differs from Chat Completions: instructions + input items, not messages.
type CodexAdapter struct {
	apiBase      string
	defaultModel string
	tokenSource  TokenSource
}

// NewCodexAdapter creates a Codex adapter from ProviderConfig.
// Requires "token_source" in ExtraOpts (TokenSource interface).
func NewCodexAdapter(cfg ProviderConfig) (ProviderAdapter, error) {
	apiBase := cfg.BaseURL
	if apiBase == "" {
		apiBase = "https://chatgpt.com/backend-api"
	}
	apiBase = strings.TrimRight(apiBase, "/")

	model := cfg.Model
	if model == "" {
		model = "gpt-5.4"
	}

	var ts TokenSource
	if cfg.ExtraOpts != nil {
		ts, _ = cfg.ExtraOpts["token_source"].(TokenSource)
	}

	return &CodexAdapter{
		apiBase:      apiBase,
		defaultModel: model,
		tokenSource:  ts,
	}, nil
}

func (a *CodexAdapter) Name() string { return "codex" }

// Capabilities returns Codex capability declaration matching CodexProvider.Capabilities().
func (a *CodexAdapter) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		Streaming:        true,
		ToolCalling:      true,
		StreamWithTools:  true,
		Thinking:         true,
		Vision:           true,
		CacheControl:     false,
		MaxContextWindow: 1_000_000,
		TokenizerID:      "o200k_base",
	}
}

// ToRequest converts ChatRequest to Responses API JSON + headers.
// Uses a temporary CodexProvider to reuse existing buildRequestBody logic.
func (a *CodexAdapter) ToRequest(req ChatRequest) ([]byte, http.Header, error) {
	stream := true
	if v, ok := req.Options["stream"].(bool); ok {
		stream = v
	}

	// Build a minimal CodexProvider to delegate body construction.
	// buildRequestBody only reads defaultModel from the struct.
	p := &CodexProvider{
		name:         "codex",
		apiBase:      a.apiBase,
		defaultModel: a.defaultModel,
	}
	body := p.buildRequestBody(req, stream)

	data, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("codex adapter: marshal: %w", err)
	}

	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	h.Set("OpenAI-Beta", "responses=v1")

	if a.tokenSource != nil {
		token, err := a.tokenSource.Token()
		if err != nil {
			return nil, nil, fmt.Errorf("codex adapter: token: %w", err)
		}
		h.Set("Authorization", "Bearer "+token)
	}

	return data, h, nil
}

// FromResponse parses a Codex Responses API response JSON into ChatResponse.
func (a *CodexAdapter) FromResponse(data []byte) (*ChatResponse, error) {
	var resp codexAPIResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("codex adapter: decode: %w", err)
	}
	return parseCodexAPIResponse(&resp), nil
}

// FromStreamChunk parses a single Codex SSE event payload.
// Handles text deltas and completion events.
func (a *CodexAdapter) FromStreamChunk(data []byte) (*StreamChunk, error) {
	if string(data) == "[DONE]" {
		return &StreamChunk{Done: true}, nil
	}

	var event codexSSEEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, nil
	}

	switch event.Type {
	case "response.output_text.delta":
		if event.Delta != "" {
			return &StreamChunk{Content: event.Delta}, nil
		}
	case "response.completed", "response.incomplete", "response.failed":
		return &StreamChunk{Done: true}, nil
	}

	return nil, nil
}

// parseCodexAPIResponse converts a Codex API response to internal ChatResponse.
// Simple concatenation — does not handle phase-aware ordering like the streaming
// path (codex_stream_state.go). Sufficient for Pipeline non-streaming use.
func parseCodexAPIResponse(resp *codexAPIResponse) *ChatResponse {
	result := &ChatResponse{FinishReason: "stop"}

	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, c := range item.Content {
				if c.Type == "output_text" {
					result.Content += c.Text
				}
			}
			if item.Phase != "" {
				result.Phase = item.Phase
			}
		case "function_call":
			args := make(map[string]any)
			if item.Arguments != "" {
				_ = json.Unmarshal([]byte(item.Arguments), &args)
			}
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:        item.CallID,
				Name:      item.Name,
				Arguments: args,
			})
		case "reasoning":
			for _, s := range item.Summary {
				result.Thinking += s.Text
			}
		}
	}

	if len(result.ToolCalls) > 0 && result.FinishReason != "length" {
		result.FinishReason = "tool_calls"
	}
	if resp.Status == "incomplete" {
		result.FinishReason = "length"
	}

	if resp.Usage != nil {
		result.Usage = &Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
		if resp.Usage.OutputTokensDetails != nil {
			result.Usage.ThinkingTokens = resp.Usage.OutputTokensDetails.ReasoningTokens
		}
	}

	return result
}
