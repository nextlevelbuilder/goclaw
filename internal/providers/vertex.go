package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// VertexProvider is a native provider for Google Vertex AI's OpenAI-compatible chat API.
// It keeps request/response shape aligned with OpenAI Chat Completions while using
// Google access tokens (ADC or explicit bearer token) for authentication.
type VertexProvider struct {
	name         string
	apiBase      string
	defaultModel string
	tokenSource  TokenSource
	client       *http.Client
	retryConfig  RetryConfig
	middlewares  RequestMiddleware
	base         *OpenAIProvider
}

func NewVertexProvider(name, apiBase, defaultModel string, tokenSource TokenSource) *VertexProvider {
	baseURL := strings.TrimRight(apiBase, "/")
	if name == "" {
		name = "vertex"
	}
	if defaultModel == "" {
		defaultModel = "google/gemini-2.5-flash"
	}
	openaiBase := NewOpenAIProvider(name, "", baseURL, defaultModel)
	openaiBase.WithProviderType("vertex")

	return &VertexProvider{
		name:         name,
		apiBase:      baseURL,
		defaultModel: defaultModel,
		tokenSource:  tokenSource,
		client:       &http.Client{Timeout: DefaultHTTPTimeout},
		retryConfig:  DefaultRetryConfig(),
		middlewares:  ComposeMiddlewares(FastModeMiddleware, ServiceTierMiddleware, CacheMiddleware),
		base:         openaiBase,
	}
}

func (p *VertexProvider) Name() string           { return p.name }
func (p *VertexProvider) DefaultModel() string   { return p.defaultModel }
func (p *VertexProvider) SupportsThinking() bool { return true }

func (p *VertexProvider) Capabilities() ProviderCapabilities {
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

func (p *VertexProvider) middlewareConfig(model string, req ChatRequest) MiddlewareConfig {
	return MiddlewareConfig{
		Provider: p.name,
		Model:    model,
		Caps:     p.Capabilities(),
		AuthType: "oauth",
		APIBase:  p.apiBase,
		Options:  req.Options,
	}
}

func (p *VertexProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := p.base.resolveModel(req.Model)
	body := p.base.buildRequestBody(model, req, false)
	body = ApplyMiddlewares(body, p.middlewares, p.middlewareConfig(model, req))

	chatFn := func() (*ChatResponse, error) {
		respBody, err := p.doRequest(ctx, body)
		if err != nil {
			return nil, err
		}
		defer respBody.Close()

		var oaiResp openAIResponse
		if err := json.NewDecoder(respBody).Decode(&oaiResp); err != nil {
			return nil, fmt.Errorf("%s: decode response: %w", p.name, err)
		}
		return p.base.parseResponse(&oaiResp), nil
	}

	resp, err := RetryDo(ctx, p.retryConfig, chatFn)
	if err != nil {
		if clamped := clampMaxTokensFromError(err, body); clamped {
			slog.Info("vertex: max_tokens clamped, retrying", "model", model, "limit", clampedLimit(body))
			resp, err = RetryDo(ctx, p.retryConfig, chatFn)
		}
	}
	if resp != nil {
		if strip, _ := req.Options[OptStripThinking].(bool); strip {
			resp.Thinking = ""
		}
	}
	return resp, err
}

func (p *VertexProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	model := p.base.resolveModel(req.Model)
	stripThinking, _ := req.Options[OptStripThinking].(bool)
	body := p.base.buildRequestBody(model, req, true)
	body = ApplyMiddlewares(body, p.middlewares, p.middlewareConfig(model, req))

	respBody, err := RetryDo(ctx, p.retryConfig, func() (io.ReadCloser, error) {
		return p.doRequest(ctx, body)
	})
	if err != nil {
		if clamped := clampMaxTokensFromError(err, body); clamped {
			slog.Info("vertex: max_tokens clamped, retrying stream", "model", model, "limit", clampedLimit(body))
			respBody, err = RetryDo(ctx, p.retryConfig, func() (io.ReadCloser, error) {
				return p.doRequest(ctx, body)
			})
		}
	}
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	result := &ChatResponse{FinishReason: "stop"}
	accumulators := make(map[int]*toolCallAccumulator)

	sse := NewSSEScanner(respBody)
	for sse.Next() {
		data := sse.Data()

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			result.Usage = &Usage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
			if chunk.Usage.PromptTokensDetails != nil {
				result.Usage.CacheReadTokens = chunk.Usage.PromptTokensDetails.CachedTokens
			}
			if chunk.Usage.CompletionTokensDetails != nil && chunk.Usage.CompletionTokensDetails.ReasoningTokens > 0 {
				result.Usage.ThinkingTokens = chunk.Usage.CompletionTokensDetails.ReasoningTokens
			}
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta
		reasoning := delta.ReasoningContent
		if reasoning == "" {
			reasoning = delta.Reasoning
		}
		if reasoning != "" && !stripThinking {
			result.Thinking += reasoning
			if onChunk != nil {
				onChunk(StreamChunk{Thinking: reasoning})
			}
		}
		if delta.Content != "" {
			result.Content += delta.Content
			if onChunk != nil {
				onChunk(StreamChunk{Content: delta.Content})
			}
		}
		for _, tc := range delta.ToolCalls {
			acc, ok := accumulators[tc.Index]
			if !ok {
				acc = &toolCallAccumulator{
					ToolCall: ToolCall{ID: tc.ID, Name: strings.TrimSpace(tc.Function.Name)},
				}
				accumulators[tc.Index] = acc
			}
			if tc.Function.Name != "" {
				acc.Name = strings.TrimSpace(tc.Function.Name)
			}
			acc.rawArgs += tc.Function.Arguments
			if tc.Function.ThoughtSignature != "" {
				acc.thoughtSig = tc.Function.ThoughtSignature
			}
		}
		if chunk.Choices[0].FinishReason != "" {
			result.FinishReason = chunk.Choices[0].FinishReason
		}
	}

	if err := sse.Err(); err != nil {
		return nil, fmt.Errorf("%s: stream read error: %w", p.name, err)
	}
	for i := 0; i < len(accumulators); i++ {
		acc := accumulators[i]
		args := make(map[string]any)
		if err := json.Unmarshal([]byte(acc.rawArgs), &args); err != nil && acc.rawArgs != "" {
			slog.Warn("vertex_stream: failed to parse tool call arguments",
				"tool", acc.Name, "raw_len", len(acc.rawArgs), "error", err)
			acc.ParseError = fmt.Sprintf("malformed JSON (%d chars): %v", len(acc.rawArgs), err)
		}
		acc.Arguments = args
		if acc.thoughtSig != "" {
			acc.Metadata = map[string]string{"thought_signature": acc.thoughtSig}
		}
		result.ToolCalls = append(result.ToolCalls, acc.ToolCall)
	}
	if len(result.ToolCalls) > 0 && result.FinishReason != "length" {
		result.FinishReason = "tool_calls"
	}
	if onChunk != nil {
		onChunk(StreamChunk{Done: true})
	}
	return result, nil
}

func (p *VertexProvider) doRequest(ctx context.Context, body any) (io.ReadCloser, error) {
	if p.tokenSource == nil {
		return nil, fmt.Errorf("%s: missing token source", p.name)
	}

	token, err := p.tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("%s: get access token: %w", p.name, err)
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal request: %w", p.name, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.apiBase+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", p.name, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: request failed: %w", p.name, err)
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		retryAfter := ParseRetryAfter(resp.Header.Get("Retry-After"))
		return nil, &HTTPError{
			Status:     resp.StatusCode,
			Body:       fmt.Sprintf("%s: %s", p.name, string(respBody)),
			RetryAfter: retryAfter,
		}
	}
	return resp.Body, nil
}
