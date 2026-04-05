package providers

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
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

// isOpenAINativeEndpoint returns true for endpoints confirmed to be native OpenAI
// infrastructure that accepts the "developer" message role.
// Azure OpenAI, proxies, and other OpenAI-compatible backends only support "system".
// Matching OpenClaw TS: model-compat.ts → isOpenAINativeEndpoint().
func isOpenAINativeEndpoint(apiBase string) bool {
	// Extract hostname from the API base URL.
	lower := strings.ToLower(apiBase)
	return strings.Contains(lower, "api.openai.com")
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
		client:       &http.Client{Timeout: DefaultHTTPTimeout},
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

// schemaProviderName returns the most specific provider identifier for schema normalization.
// Prefers providerType (from DB) over name for accurate profile matching.
func (p *OpenAIProvider) schemaProviderName() string {
	if p.providerType != "" {
		return p.providerType
	}
	return p.name
}

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

	chatFn := p.chatRequestFn(ctx, body)

	resp, err := RetryDo(ctx, p.retryConfig, chatFn)

	// Auto-clamp max_tokens and retry once if the model rejects the value
	if err != nil {
		if clamped := clampMaxTokensFromError(err, body); clamped {
			slog.Info("max_tokens clamped, retrying", "model", model, "limit", clampedLimit(body))
			return RetryDo(ctx, p.retryConfig, chatFn)
		}
	}

	return resp, err
}

// chatRequestFn returns a closure that performs a single non-streaming chat request.
// Shared between initial attempt and post-clamp retry to avoid duplication.
func (p *OpenAIProvider) chatRequestFn(ctx context.Context, body map[string]any) func() (*ChatResponse, error) {
	return func() (*ChatResponse, error) {
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
	}
}

func (p *OpenAIProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	model := p.resolveModel(req.Model)
	body := p.buildRequestBody(model, req, true)

	// Retry only the connection phase; once streaming starts, no retry.
	respBody, err := RetryDo(ctx, p.retryConfig, func() (io.ReadCloser, error) {
		return p.doRequest(ctx, body)
	})

	// Auto-clamp max_tokens and retry once if the model rejects the value
	if err != nil {
		if clamped := clampMaxTokensFromError(err, body); clamped {
			slog.Info("max_tokens clamped, retrying stream", "model", model, "limit", clampedLimit(body))
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

	scanner := bufio.NewScanner(respBody)
	scanner.Buffer(make([]byte, 0, SSEScanBufInit), SSEScanBufMax)
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
		if err := json.Unmarshal([]byte(acc.rawArgs), &args); err != nil && acc.rawArgs != "" {
			slog.Warn("openai_stream: failed to parse tool call arguments",
				"tool", acc.Name, "raw_len", len(acc.rawArgs), "error", err)
			acc.ParseError = fmt.Sprintf("malformed JSON (%d chars): %v", len(acc.rawArgs), err)
		}
		acc.Arguments = args
		if acc.thoughtSig != "" {
			acc.Metadata = map[string]string{"thought_signature": acc.thoughtSig}
		}
		result.ToolCalls = append(result.ToolCalls, acc.ToolCall)
	}

	// Only override finish_reason when stream wasn't truncated.
	// Preserve "length" so agent loop can detect truncation and retry.
	if len(result.ToolCalls) > 0 && result.FinishReason != "length" {
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

	// Compute provider capability once: does this endpoint support Google's thought_signature?
	// We check providerType, name, apiBase, and the model string (robust detection for proxies/OpenRouter).
	supportsThoughtSignature := strings.Contains(strings.ToLower(p.providerType), "gemini") ||
		strings.Contains(strings.ToLower(p.name), "gemini") ||
		strings.Contains(strings.ToLower(p.apiBase), "generativelanguage") ||
		strings.Contains(strings.ToLower(model), "gemini")

	if supportsThoughtSignature {
		inputMessages = collapseToolCallsWithoutSig(inputMessages)
	}

	// Detect native OpenAI endpoint to enable developer role.
	// GPT-4o+ models prioritize "developer" messages over "system" for instruction
	// adherence. Non-OpenAI backends (proxies, Qwen, DeepSeek, etc.) reject "developer".
	// Matching OpenClaw TS: model-compat.ts → isOpenAINativeEndpoint().
	useDevRole := isOpenAINativeEndpoint(p.apiBase)

	// Convert messages to proper OpenAI wire format.
	// This is necessary because our internal Message/ToolCall structs don't match
	// the OpenAI API format (tool_calls need type+function wrapper, arguments as JSON string).
	// Also omits empty content on assistant messages with tool_calls (Gemini compatibility).
	msgs := make([]map[string]any, 0, len(inputMessages))
	for _, m := range inputMessages {
		role := m.Role
		// Map "system" → "developer" for native OpenAI endpoints (GPT-4o+).
		// The developer role has higher instruction priority than system role.
		if useDevRole && role == "system" {
			role = "developer"
		}
		msg := map[string]any{
			"role": role,
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
					// Only send thought_signature to providers that support it (Google/Gemini).
					// Non-Google providers will reject the unknown field with 422 Unprocessable Entity.
					if supportsThoughtSignature {
						fn["thought_signature"] = sig
					}
				}
				toolCalls[i] = map[string]any{
					"id":       truncateToolCallID(tc.ID),
					"type":     "function",
					"function": fn,
				}
			}
			msg["tool_calls"] = toolCalls
		}

		if m.ToolCallID != "" {
			msg["tool_call_id"] = truncateToolCallID(m.ToolCallID)
		}

		msgs = append(msgs, msg)
	}

	// Safety net: strip trailing assistant message to prevent HTTP 400 from
	// proxy providers (LiteLLM, OpenRouter) that don't support assistant prefill.
	// This should rarely trigger — the agent loop ensures user message is last.
	if len(msgs) > 0 {
		if role, _ := msgs[len(msgs)-1]["role"].(string); role == "assistant" {
			slog.Warn("openai: stripped trailing assistant message (unsupported prefill)",
				"provider", p.name, "model", model)
			msgs = msgs[:len(msgs)-1]
		}
	}

	body := map[string]any{
		"model":    model,
		"messages": msgs,
		"stream":   stream,
	}

	if len(req.Tools) > 0 {
		body["tools"] = CleanToolSchemas(p.schemaProviderName(), req.Tools)
		body["tool_choice"] = "auto"
	}

	if stream {
		body["stream_options"] = map[string]any{
			"include_usage": true,
		}
	}

	// Merge options
	capabilityModel := modelFamily(model)
	if v, ok := req.Options[OptMaxTokens]; ok {
		if strings.HasPrefix(capabilityModel, "gpt-5") || strings.HasPrefix(capabilityModel, "o1") || strings.HasPrefix(capabilityModel, "o3") || strings.HasPrefix(capabilityModel, "o4") {
			body["max_completion_tokens"] = v
		} else {
			body["max_tokens"] = v
		}
	}
	if v, ok := req.Options[OptTemperature]; ok {
		// Certain model families don't support custom temperature (locked to default).
		// This is a model-level constraint, not provider-specific — applies to both OpenAI and Azure.
		// Note: gpt-5.X flagship models (gpt-5.1, gpt-5.4) DO support temperature;
		// only the mini/nano reasoning variants reject it.
		skipTemp := strings.HasPrefix(capabilityModel, "gpt-5-mini") || strings.HasPrefix(capabilityModel, "gpt-5-nano") || strings.HasPrefix(capabilityModel, "o1") || strings.HasPrefix(capabilityModel, "o3") || strings.HasPrefix(capabilityModel, "o4")
		if !skipTemp {
			body["temperature"] = v
		}
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

// modelFamily strips provider prefixes (for example "openai/o3-mini") so capability
// gates apply to the actual model family rather than the transport-specific wrapper.
func modelFamily(model string) string {
	if idx := strings.LastIndex(model, "/"); idx >= 0 && idx < len(model)-1 {
		return model[idx+1:]
	}
	return model
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
	// Azure OpenAI/Foundry support for now atleast
	if strings.Contains(strings.ToLower(p.apiBase), "azure.com") {
		httpReq.Header.Set("api-key", p.apiKey)
	} else {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

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
			var parseErr string
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil && tc.Function.Arguments != "" {
				slog.Warn("openai: failed to parse tool call arguments",
					"tool", tc.Function.Name, "raw_len", len(tc.Function.Arguments), "error", err)
				parseErr = fmt.Sprintf("malformed JSON (%d chars): %v", len(tc.Function.Arguments), err)
			}
			call := ToolCall{
				ID:         tc.ID,
				Name:       strings.TrimSpace(tc.Function.Name),
				Arguments:  args,
				ParseError: parseErr,
			}
			if tc.Function.ThoughtSignature != "" {
				call.Metadata = map[string]string{"thought_signature": tc.Function.ThoughtSignature}
			}
			result.ToolCalls = append(result.ToolCalls, call)
		}

		// Only override finish_reason when response wasn't truncated.
		// Preserve "length" so agent loop can detect truncation and retry.
		if len(result.ToolCalls) > 0 && result.FinishReason != "length" {
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

// maxTokensLimitRe matches "supports at most N completion tokens" from OpenAI 400 errors.
var maxTokensLimitRe = regexp.MustCompile(`supports at most (\d+) completion tokens`)

// clampMaxTokensFromError checks if an error is a 400 "max_tokens is too large" rejection.
// If so, it parses the model's stated limit, clamps the body's max_tokens/max_completion_tokens,
// and returns true so the caller can retry.
func clampMaxTokensFromError(err error, body map[string]any) bool {
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusBadRequest {
		return false
	}
	if !strings.Contains(httpErr.Body, "max_tokens") || !strings.Contains(httpErr.Body, "too large") {
		return false
	}

	matches := maxTokensLimitRe.FindStringSubmatch(httpErr.Body)
	if len(matches) < 2 {
		return false
	}
	limit, parseErr := strconv.Atoi(matches[1])
	if parseErr != nil || limit <= 0 {
		return false
	}

	// Clamp whichever key is present
	if _, ok := body["max_completion_tokens"]; ok {
		body["max_completion_tokens"] = limit
	} else {
		body["max_tokens"] = limit
	}
	return true
}

// clampedLimit returns the clamped max_tokens or max_completion_tokens value for logging.
func clampedLimit(body map[string]any) any {
	if v, ok := body["max_completion_tokens"]; ok {
		return v
	}
	return body["max_tokens"]
}

const maxToolCallIDLen = 40

// truncateToolCallID deterministically fits tool call IDs into OpenAI's 40-char
// limit. Prefix truncation can alias distinct legacy IDs that only diverge after
// byte 40, so we hash the full original ID when shortening is needed.
//
// Fresh tool calls from the agent loop already go through uniquifyToolCallIDs
// (which produces 40-char hashed IDs), so this is a no-op for those. This
// function catches replayed/legacy history entries that bypassed uniquification.
func truncateToolCallID(id string) string {
	if len(id) <= maxToolCallIDLen {
		return id
	}
	hash := sha256.Sum256([]byte(id))
	return "call_" + hex.EncodeToString(hash[:])[:maxToolCallIDLen-len("call_")]
}
