package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := p.resolveModel(req.Model)
	body := p.buildRequestBody(model, req, false)
	body = ApplyMiddlewares(body, p.middlewares, p.middlewareConfig(model, req))

	chatFn := p.chatRequestFn(ctx, body)

	resp, err := RetryDo(ctx, p.retryConfig, chatFn)

	// Auto-clamp max_tokens and retry once if the model rejects the value
	if err != nil {
		if clamped := clampMaxTokensFromError(err, body); clamped {
			slog.Info("max_tokens clamped, retrying", "model", model, "limit", clampedLimit(body))
			resp, err = RetryDo(ctx, p.retryConfig, chatFn)
		}
	}

	// Drop user-visible reasoning for models flagged as leakers (e.g. Kimi,
	// DeepSeek-Reasoner). Usage.ThinkingTokens is preserved so billing stays
	// correct (Phase 1 depends on this).
	if resp != nil {
		if strip, _ := req.Options[OptStripThinking].(bool); strip {
			resp.Thinking = ""
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
	// stripThinking suppresses user-visible reasoning while leaving
	// Usage.ThinkingTokens untouched (the usage chunk below still records it).
	stripThinking, _ := req.Options[OptStripThinking].(bool)
	body := p.buildRequestBody(model, req, true)
	body = ApplyMiddlewares(body, p.middlewares, p.middlewareConfig(model, req))

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

	sse := NewSSEScanner(respBody)
	for sse.Next() {
		data := sse.Data()

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
	if err := sse.Err(); err != nil {
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

const maxToolCallIDLen = 40

// normalizeMistralToolCallID deterministically maps any tool call ID to a
// 9-character alphanumeric string required by the Mistral API.
// Uses SHA-256 of the full ID to avoid prefix-dependent collisions.
func normalizeMistralToolCallID(id string) string {
	h := sha256.Sum256([]byte(id))
	return hex.EncodeToString(h[:])[:9]
}

// wireToolCallID dispatches to Mistral-specific normalization (9-char alnum)
// or the standard OpenAI truncation (40-char max) based on the provider.
func (p *OpenAIProvider) wireToolCallID(id string) string {
	if p.name == "mistral" || p.providerType == "mistral" {
		return normalizeMistralToolCallID(id)
	}
	return truncateToolCallID(id)
}

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
