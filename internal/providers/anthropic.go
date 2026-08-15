package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// resolveAnthropicModel expands a short alias to a full model ID, or returns the input unchanged.
// If a registry is provided, triggers forward-compat resolution for unknown models.
func resolveAnthropicModel(model, defaultModel string, registry ModelRegistry) string {
	if model == "" {
		return defaultModel
	}
	if full, ok := claudeModelAliases[model]; ok {
		return full
	}
	// Trigger forward-compat resolution to cache specs for token counting
	if registry != nil {
		_ = registry.Resolve("anthropic", model)
	}
	return model
}

// AnthropicProvider implements Provider using the Anthropic Claude API via net/http.
type AnthropicProvider struct {
	name         string // provider name (default: "anthropic")
	apiKey       string
	baseURL      string
	defaultModel string
	client       *http.Client
	retryConfig  RetryConfig
	middlewares  RequestMiddleware // composed middleware chain (nil = no-op)
	registry     ModelRegistry    // model resolution registry (nil = skip)
}

// NewAnthropicProvider creates a new Anthropic provider.
func NewAnthropicProvider(apiKey string, opts ...AnthropicOption) *AnthropicProvider {
	p := &AnthropicProvider{
		name:         "anthropic",
		apiKey:       apiKey,
		baseURL:      anthropicAPIBase,
		defaultModel: defaultClaudeModel,
		client:       NewDefaultHTTPClient(),
		retryConfig:  DefaultRetryConfig(),
		// No CacheMiddleware: Anthropic uses block-level cache_control in buildRequestBody
		middlewares: ComposeMiddlewares(FastModeMiddleware, ServiceTierMiddleware),
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

type AnthropicOption func(*AnthropicProvider)

// WithAnthropicName overrides the provider name (default: "anthropic").
func WithAnthropicName(name string) AnthropicOption {
	return func(p *AnthropicProvider) {
		if name != "" {
			p.name = name
		}
	}
}

func WithAnthropicModel(model string) AnthropicOption {
	return func(p *AnthropicProvider) { p.defaultModel = model }
}

func WithAnthropicRegistry(r ModelRegistry) AnthropicOption {
	return func(p *AnthropicProvider) { p.registry = r }
}

func WithAnthropicMiddlewares(mws ...RequestMiddleware) AnthropicOption {
	return func(p *AnthropicProvider) { p.middlewares = ComposeMiddlewares(mws...) }
}

func WithAnthropicBaseURL(baseURL string) AnthropicOption {
	return func(p *AnthropicProvider) {
		if baseURL != "" {
			p.baseURL = strings.TrimRight(baseURL, "/")
		}
	}
}

func (p *AnthropicProvider) Name() string           { return p.name }
func (p *AnthropicProvider) DefaultModel() string   { return p.defaultModel }
func (p *AnthropicProvider) SupportsThinking() bool { return true }

// Capabilities implements CapabilitiesAware for pipeline code-path selection.
func (p *AnthropicProvider) Capabilities() ProviderCapabilities {
	return ProviderCapabilities{
		Streaming:        true,
		ToolCalling:      true,
		StreamWithTools:  true,
		Thinking:         true,
		Vision:           true,
		CacheControl:     true,
		MaxContextWindow: 200_000,
		TokenizerID:      "cl100k_base",
	}
}

// middlewareConfig builds a MiddlewareConfig for the current request.
func (p *AnthropicProvider) middlewareConfig(model string, req ChatRequest) MiddlewareConfig {
	return MiddlewareConfig{
		Provider: "anthropic",
		Model:    model,
		Caps:     p.Capabilities(),
		AuthType: "api_key",
		APIBase:  p.baseURL,
		Options:  req.Options,
	}
}

func (p *AnthropicProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := resolveAnthropicModel(req.Model, p.defaultModel, p.registry)

	body := p.buildRequestBody(model, req, false)
	body = ApplyMiddlewares(body, p.middlewares, p.middlewareConfig(model, req))

	resp, err := RetryDo(ctx, p.retryConfig, func() (*ChatResponse, error) {
		respBody, err := p.doRequest(ctx, body)
		if err != nil {
			return nil, err
		}
		defer respBody.Close()

		var parsed anthropicResponse
		if err := json.NewDecoder(respBody).Decode(&parsed); err != nil {
			return nil, fmt.Errorf("anthropic: decode response: %w", err)
		}

		return p.parseResponse(&parsed), nil
	})
	// Drop user-visible reasoning after parsing for models flagged as leakers.
	// Usage.ThinkingTokens and RawAssistantContent remain intact so billing
	// and Anthropic tool-use thinking passback continue to work.
	if resp != nil {
		if strip, _ := req.Options[OptStripThinking].(bool); strip {
			resp.Thinking = ""
		}
	}
	return resp, err
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
			var parseErr string
			if err := json.Unmarshal(block.Input, &args); err != nil && len(block.Input) > 0 {
				parseErr = fmt.Sprintf("malformed JSON (%d chars): %v", len(block.Input), err)
			}
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:         block.ID,
				Name:       strings.TrimSpace(block.Name),
				Arguments:  args,
				ParseError: parseErr,
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
