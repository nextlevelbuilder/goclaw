package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIProvider implements Provider for OpenAI-compatible APIs
// (OpenAI, Groq, OpenRouter, DeepSeek, VLLM, etc.)
type OpenAIProvider struct {
	name         string
	apiKey       string
	apiBase      string
	chatPath     string // defaults to "/chat/completions"
	defaultModel string
	providerType string // DB provider_type (e.g. "gemini_native", "openai", "minimax_native")
	client       *http.Client
	retryConfig  RetryConfig
}

func NewOpenAIProvider(name, apiKey, apiBase, defaultModel string) *OpenAIProvider {
	if apiBase == "" {
		apiBase = "https://api.openai.com/v1"
	}
	apiBase = strings.TrimRight(apiBase, "/")

	return &OpenAIProvider{
		name:         name,
		apiKey:       apiKey,
		apiBase:      apiBase,
		chatPath:     "/chat/completions",
		defaultModel: defaultModel,
		client:       &http.Client{Timeout: 300 * time.Second},
		retryConfig:  DefaultRetryConfig(),
	}
}

// WithChatPath returns a copy with a custom chat completions path (e.g. "/text/chatcompletion_v2" for MiniMax native API).
func (p *OpenAIProvider) WithChatPath(path string) *OpenAIProvider {
	p.chatPath = path
	return p
}

func (p *OpenAIProvider) Name() string           { return p.name }
func (p *OpenAIProvider) DefaultModel() string   { return p.defaultModel }
func (p *OpenAIProvider) SupportsThinking() bool { return true }
func (p *OpenAIProvider) APIKey() string         { return p.apiKey }
func (p *OpenAIProvider) APIBase() string        { return p.apiBase }
func (p *OpenAIProvider) ProviderType() string   { return p.providerType }

// WithProviderType sets the DB provider_type for correct API endpoint routing in media tools.
func (p *OpenAIProvider) WithProviderType(pt string) *OpenAIProvider {
	p.providerType = pt
	return p
}

// resolveModel returns the model ID to use for a request.
// For OpenRouter, model IDs require a provider prefix (e.g. "anthropic/claude-sonnet-4-5-20250929").
// If the caller passes an unprefixed model, fall back to the provider's default.
func (p *OpenAIProvider) resolveModel(model string) string {
	if model == "" {
		return p.defaultModel
	}
	if p.name == "openrouter" && !strings.Contains(model, "/") {
		return p.defaultModel
	}
	return model
}

func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := p.resolveModel(req.Model)
	body := p.buildRequestBody(model, req, false)

	return RetryDo(ctx, p.retryConfig, func() (*ChatResponse, error) {
		respBody, err := p.doRequest(ctx, body)
		if err != nil {
			return nil, err
		}
		defer respBody.Close()

		var oaiResp openAIResponse
		if err := json.NewDecoder(respBody).Decode(&oaiResp); err != nil {
			return nil, fmt.Errorf("%s: decode response: %w", p.name, err)
		}

		return p.parseResponse(&oaiResp), nil
	})
}

func (p *OpenAIProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	model := p.resolveModel(req.Model)
	body := p.buildRequestBody(model, req, true)

	// Retry only the connection phase; once streaming starts, no retry.
	respBody, err := RetryDo(ctx, p.retryConfig, func() (io.ReadCloser, error) {
		return p.doRequest(ctx, body)
	})
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	result := &ChatResponse{FinishReason: "stop"}
	accumulators := make(map[int]*toolCallAccumulator)

	scanner := bufio.NewScanner(respBody)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line for large tool call / thinking chunks
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		// SSE spec allows both "data: value" and "data:value" (space is optional).
		// Some providers (e.g. Kimi) omit the space after the colon.
		data := strings.TrimPrefix(line, "data:")
		data = strings.TrimPrefix(data, " ")
		if data == "[DONE]" {
			break
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		// Usage chunk often has empty choices — extract usage before skipping.
		// When stream_options.include_usage is true, the final chunk contains
		// usage data but choices is typically an empty array.
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
		if delta.ReasoningContent != "" {
			result.Thinking += delta.ReasoningContent
			if onChunk != nil {
				onChunk(StreamChunk{Thinking: delta.ReasoningContent})
			}
		}
		if delta.Content != "" {
			result.Content += delta.Content
			if onChunk != nil {
				onChunk(StreamChunk{Content: delta.Content})
			}
		}

		// Accumulate streamed tool calls
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

	// Check for scanner errors (timeout, connection reset, etc.)
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%s: stream read error: %w", p.name, err)
	}

	// Parse accumulated tool call arguments
	for i := 0; i < len(accumulators); i++ {
		acc := accumulators[i]
		args := make(map[string]any)
		_ = json.Unmarshal([]byte(acc.rawArgs), &args)
		acc.Arguments = args
		if acc.thoughtSig != "" {
			acc.Metadata = map[string]string{"thought_signature": acc.thoughtSig}
		}
		result.ToolCalls = append(result.ToolCalls, acc.ToolCall)
	}

	if len(result.ToolCalls) > 0 {
		result.FinishReason = "tool_calls"
	}

	if onChunk != nil {
		onChunk(StreamChunk{Done: true})
	}

	return result, nil
}

func (p *OpenAIProvider) buildRequestBody(model string, req ChatRequest, stream bool) map[string]any {
	// Gemini 2.5+: collapse tool_call cycles missing thought_signature.
	// Gemini requires thought_signature echoed back on every tool_call; models that
	// don't return it (e.g. gemini-3-flash) will cause HTTP 400 if sent as-is.
	// Tool results are folded into plain user messages to preserve context.
	inputMessages := req.Messages
	if strings.Contains(strings.ToLower(p.name), "gemini") {
		inputMessages = collapseToolCallsWithoutSig(inputMessages)
	}

	// Convert messages to proper OpenAI wire format.
	// This is necessary because our internal Message/ToolCall structs don't match
	// the OpenAI API format (tool_calls need type+function wrapper, arguments as JSON string).
	// Also omits empty content on assistant messages with tool_calls (Gemini compatibility).
	msgs := make([]map[string]any, 0, len(inputMessages))
	for _, m := range inputMessages {
		msg := map[string]any{
			"role": m.Role,
		}

		// Echo reasoning_content for assistant messages (required by Kimi, DeepSeek when thinking is enabled)
		if m.Thinking != "" && m.Role == "assistant" {
			msg["reasoning_content"] = m.Thinking
		}

		// Include content; omit empty content for assistant messages with tool_calls
		// (Gemini rejects empty content → "must include at least one parts field").
		if m.Role == "user" && len(m.Images) > 0 {
			var parts []map[string]any
			for _, img := range m.Images {
				parts = append(parts, map[string]any{
					"type": "image_url",
					"image_url": map[string]any{
						"url": fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Data),
					},
				})
			}
			if m.Content != "" {
				parts = append(parts, map[string]any{
					"type": "text",
					"text": m.Content,
				})
			}
			msg["content"] = parts
		} else if m.Content != "" || len(m.ToolCalls) == 0 {
			msg["content"] = m.Content
		}

		// Convert tool_calls to OpenAI wire format:
		// {id, type: "function", function: {name, arguments: "<json string>"}}
		if len(m.ToolCalls) > 0 {
			toolCalls := make([]map[string]any, len(m.ToolCalls))
			for i, tc := range m.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Arguments)
				fn := map[string]any{
					"name":      tc.Name,
					"arguments": string(argsJSON),
				}
				if sig := tc.Metadata["thought_signature"]; sig != "" {
					fn["thought_signature"] = sig
				}
				toolCalls[i] = map[string]any{
					"id":       tc.ID,
					"type":     "function",
					"function": fn,
				}
			}
			msg["tool_calls"] = toolCalls
		}

		if m.ToolCallID != "" {
			msg["tool_call_id"] = m.ToolCallID
		}

		msgs = append(msgs, msg)
	}

	body := map[string]any{
		"model":    model,
		"messages": msgs,
		"stream":   stream,
	}

	if len(req.Tools) > 0 {
		body["tools"] = CleanToolSchemas(p.name, req.Tools)
		body["tool_choice"] = "auto"
	}

	if stream {
		body["stream_options"] = map[string]any{
			"include_usage": true,
		}
	}

	// Merge options
	if v, ok := req.Options[OptMaxTokens]; ok {
		body["max_tokens"] = v
	}
	if v, ok := req.Options[OptTemperature]; ok {
		body["temperature"] = v
	}

	// Inject reasoning_effort for o-series models (ignored by models that don't support it)
	if level, ok := req.Options[OptThinkingLevel].(string); ok && level != "" && level != "off" {
		body[OptReasoningEffort] = level
	}

	// DashScope-specific passthrough keys
	if v, ok := req.Options[OptEnableThinking]; ok {
		body[OptEnableThinking] = v
	}
	if v, ok := req.Options[OptThinkingBudget]; ok {
		body[OptThinkingBudget] = v
	}

	return body
}

func (p *OpenAIProvider) doRequest(ctx context.Context, body any) (io.ReadCloser, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal request: %w", p.name, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.apiBase+p.chatPath, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", p.name, err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: request failed: %w", p.name, err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
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

func (p *OpenAIProvider) parseResponse(resp *openAIResponse) *ChatResponse {
	result := &ChatResponse{FinishReason: "stop"}

	if len(resp.Choices) > 0 {
		msg := resp.Choices[0].Message
		result.Content = msg.Content
		result.Thinking = msg.ReasoningContent
		result.FinishReason = resp.Choices[0].FinishReason

		for _, tc := range msg.ToolCalls {
			args := make(map[string]any)
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			call := ToolCall{
				ID:        tc.ID,
				Name:      strings.TrimSpace(tc.Function.Name),
				Arguments: args,
			}
			if tc.Function.ThoughtSignature != "" {
				call.Metadata = map[string]string{"thought_signature": tc.Function.ThoughtSignature}
			}
			result.ToolCalls = append(result.ToolCalls, call)
		}

		if len(result.ToolCalls) > 0 {
			result.FinishReason = "tool_calls"
		}
	}

	if resp.Usage != nil {
		result.Usage = &Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
		if resp.Usage.PromptTokensDetails != nil {
			result.Usage.CacheReadTokens = resp.Usage.PromptTokensDetails.CachedTokens
		}
		if resp.Usage.CompletionTokensDetails != nil && resp.Usage.CompletionTokensDetails.ReasoningTokens > 0 {
			result.Usage.ThinkingTokens = resp.Usage.CompletionTokensDetails.ReasoningTokens
		}
	}

	return result
}
