package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultClaudeModel  = "claude-sonnet-4-5-20250929"
	anthropicAPIBase    = "https://api.anthropic.com/v1"
	anthropicAPIVersion = "2023-06-01"
)

// claudeModelAliases maps short model aliases to full Anthropic model IDs.
// This allows agents configured with aliases (e.g. "opus") to work with the
// anthropic_native provider, consistent with the Claude CLI provider.
var claudeModelAliases = map[string]string{
	"opus":   "claude-opus-4-6",
	"sonnet": "claude-sonnet-4-6",
	"haiku":  "claude-haiku-4-5-20251001",
}

// resolveModel expands a short alias to a full model ID, or returns the input unchanged.
func resolveAnthropicModel(model, defaultModel string) string {
	if model == "" {
		return defaultModel
	}
	if full, ok := claudeModelAliases[model]; ok {
		return full
	}
	return model
}

// AnthropicProvider implements Provider using the Anthropic Claude API via net/http.
type AnthropicProvider struct {
	apiKey       string
	baseURL      string
	defaultModel string
	client       *http.Client
	retryConfig  RetryConfig
}

// NewAnthropicProvider creates a new Anthropic provider.
func NewAnthropicProvider(apiKey string, opts ...AnthropicOption) *AnthropicProvider {
	p := &AnthropicProvider{
		apiKey:       apiKey,
		baseURL:      anthropicAPIBase,
		defaultModel: defaultClaudeModel,
		client:       &http.Client{Timeout: 300 * time.Second},
		retryConfig:  DefaultRetryConfig(),
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

type AnthropicOption func(*AnthropicProvider)

func WithAnthropicModel(model string) AnthropicOption {
	return func(p *AnthropicProvider) { p.defaultModel = model }
}

func WithAnthropicBaseURL(baseURL string) AnthropicOption {
	return func(p *AnthropicProvider) {
		if baseURL != "" {
			p.baseURL = strings.TrimRight(baseURL, "/")
		}
	}
}

func (p *AnthropicProvider) Name() string           { return "anthropic" }
func (p *AnthropicProvider) DefaultModel() string   { return p.defaultModel }
func (p *AnthropicProvider) SupportsThinking() bool { return true }

func (p *AnthropicProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := resolveAnthropicModel(req.Model, p.defaultModel)

	body := p.buildRequestBody(model, req, false)

	return RetryDo(ctx, p.retryConfig, func() (*ChatResponse, error) {
		respBody, err := p.doRequest(ctx, body)
		if err != nil {
			return nil, err
		}
		defer respBody.Close()

		var resp anthropicResponse
		if err := json.NewDecoder(respBody).Decode(&resp); err != nil {
			return nil, fmt.Errorf("anthropic: decode response: %w", err)
		}

		return p.parseResponse(&resp), nil
	})
}

func (p *AnthropicProvider) doRequest(ctx context.Context, body any) (io.ReadCloser, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("anthropic: create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	// Add beta header for interleaved thinking when thinking is enabled
	if bodyMap, ok := body.(map[string]any); ok {
		if _, hasThinking := bodyMap["thinking"]; hasThinking {
			httpReq.Header.Set("anthropic-beta", "interleaved-thinking-2025-05-14")
		}
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		retryAfter := ParseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, &HTTPError{
			Status:     resp.StatusCode,
			Body:       fmt.Sprintf("anthropic: %s", string(respBody)),
			RetryAfter: retryAfter,
		}
	}

	return resp.Body, nil
}

func (p *AnthropicProvider) parseResponse(resp *anthropicResponse) *ChatResponse {
	result := &ChatResponse{}
	thinkingChars := 0

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			result.Content += block.Text
		case "thinking":
			result.Thinking += block.Thinking
			thinkingChars += len(block.Thinking)
		case "redacted_thinking":
			// Encrypted thinking — cannot display but must preserve for passback
		case "tool_use":
			args := make(map[string]any)
			_ = json.Unmarshal(block.Input, &args)
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:        block.ID,
				Name:      strings.TrimSpace(block.Name),
				Arguments: args,
			})
		}
	}

	switch resp.StopReason {
	case "tool_use":
		result.FinishReason = "tool_calls"
	case "max_tokens":
		result.FinishReason = "length"
	default:
		result.FinishReason = "stop"
	}

	result.Usage = &Usage{
		PromptTokens:        resp.Usage.InputTokens,
		CompletionTokens:    resp.Usage.OutputTokens,
		TotalTokens:         resp.Usage.InputTokens + resp.Usage.OutputTokens,
		CacheCreationTokens: resp.Usage.CacheCreationInputTokens,
		CacheReadTokens:     resp.Usage.CacheReadInputTokens,
	}
	if thinkingChars > 0 {
		result.Usage.ThinkingTokens = thinkingChars / 4
	}

	// Preserve raw content blocks for tool use passback
	if len(result.ToolCalls) > 0 {
		if b, err := json.Marshal(resp.Content); err == nil {
			result.RawAssistantContent = b
		}
	}

	return result
}

// --- Anthropic API types (internal) ---

type anthropicResponse struct {
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      anthropicUsage          `json:"usage"`
}

type anthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`  // for type="thinking"
	Signature string          `json:"signature,omitempty"` // encrypted thinking verification
	Data      string          `json:"data,omitempty"`      // for type="redacted_thinking"
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
}

type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// --- Streaming event types ---

type anthropicMessageStartEvent struct {
	Message struct {
		Usage anthropicUsage `json:"usage"`
	} `json:"message"`
}

type anthropicContentBlockStartEvent struct {
	Index        int                   `json:"index"`
	ContentBlock anthropicContentBlock `json:"content_block"`
}

type anthropicContentBlockDeltaEvent struct {
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text,omitempty"`
		Thinking    string `json:"thinking,omitempty"`  // for thinking_delta
		Signature   string `json:"signature,omitempty"` // for signature_delta
		PartialJSON string `json:"partial_json,omitempty"`
	} `json:"delta"`
}

type anthropicMessageDeltaEvent struct {
	Delta struct {
		StopReason string `json:"stop_reason,omitempty"`
	} `json:"delta"`
	Usage anthropicUsage `json:"usage"`
}

type anthropicErrorEvent struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}
