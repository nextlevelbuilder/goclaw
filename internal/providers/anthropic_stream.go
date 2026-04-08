package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func (p *AnthropicProvider) ChatStream(ctx context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	model := resolveAnthropicModel(req.Model, p.defaultModel, p.registry)

	body := p.buildRequestBody(model, req, true)
	body = ApplyMiddlewares(body, p.middlewares, p.middlewareConfig(model, req))

	// Retry only the connection phase; once streaming starts, no retry.
	respBody, err := RetryDo(ctx, p.retryConfig, func() (io.ReadCloser, error) {
		return p.doRequest(ctx, body)
	})
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	result := &ChatResponse{FinishReason: "stop"}
	// Accumulate raw JSON fragments for each tool call by index
	toolCallJSON := make(map[int]string)

	// Track content blocks for RawAssistantContent (needed for thinking block passback)
	var rawContentBlocks []json.RawMessage
	var currentBlockType string
	// Track thinking token count by accumulated chunk size
	thinkingChars := 0
	var thinkingSignature strings.Builder

	sse := NewSSEScanner(respBody)
	for sse.Next() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		data := sse.Data()

		switch sse.EventType() {
		case "message_start":
			var ev anthropicMessageStartEvent
			if err := json.Unmarshal([]byte(data), &ev); err == nil {
				if result.Usage == nil {
					result.Usage = &Usage{}
				}
				if ev.Message.Usage.InputTokens > 0 {
					result.Usage.PromptTokens = ev.Message.Usage.InputTokens
				}
				result.Usage.CacheCreationTokens = ev.Message.Usage.CacheCreationInputTokens
				result.Usage.CacheReadTokens = ev.Message.Usage.CacheReadInputTokens
			}

		case "content_block_start":
			var ev anthropicContentBlockStartEvent
			if err := json.Unmarshal([]byte(data), &ev); err == nil {
				currentBlockType = ev.ContentBlock.Type
				if ev.ContentBlock.Type == "tool_use" {
					result.ToolCalls = append(result.ToolCalls, ToolCall{
						ID:        ev.ContentBlock.ID,
						Name:      strings.TrimSpace(ev.ContentBlock.Name),
						Arguments: make(map[string]any),
					})
				}
				// Store raw content_block for later reconstruction
				rawContentBlocks = append(rawContentBlocks, json.RawMessage(fmt.Sprintf(`{"type":"%s"`, ev.ContentBlock.Type)))
			}

		case "content_block_delta":
			var ev anthropicContentBlockDeltaEvent
			if err := json.Unmarshal([]byte(data), &ev); err == nil {
				switch ev.Delta.Type {
				case "text_delta":
					result.Content += ev.Delta.Text
					if onChunk != nil {
						onChunk(StreamChunk{Content: ev.Delta.Text})
					}
				case "thinking_delta":
					result.Thinking += ev.Delta.Thinking
					thinkingChars += len(ev.Delta.Thinking)
					if onChunk != nil {
						onChunk(StreamChunk{Thinking: ev.Delta.Thinking})
					}
				case "input_json_delta":
					if len(result.ToolCalls) > 0 {
						idx := len(result.ToolCalls) - 1
						toolCallJSON[idx] += ev.Delta.PartialJSON
					}
				case "signature_delta":
					thinkingSignature.WriteString(ev.Delta.Signature)
				}
			}

		case "content_block_stop":
			// Reconstruct the complete content block for RawAssistantContent
			if len(rawContentBlocks) > 0 {
				idx := len(rawContentBlocks) - 1
				block := p.buildRawBlock(currentBlockType, result, toolCallJSON, idx)
				if block != nil {
					rawContentBlocks[idx] = block
				}
			}
			currentBlockType = ""

		case "message_delta":
			var ev anthropicMessageDeltaEvent
			if err := json.Unmarshal([]byte(data), &ev); err == nil {
				if ev.Delta.StopReason != "" {
					switch ev.Delta.StopReason {
					case "tool_use":
						result.FinishReason = "tool_calls"
					case "max_tokens":
						result.FinishReason = "length"
					default:
						result.FinishReason = "stop"
					}
				}
				if ev.Usage.OutputTokens > 0 {
					if result.Usage == nil {
						result.Usage = &Usage{}
					}
					result.Usage.CompletionTokens = ev.Usage.OutputTokens
				}
			}

		case "error":
			var ev anthropicErrorEvent
			if err := json.Unmarshal([]byte(data), &ev); err == nil {
				return nil, fmt.Errorf("anthropic stream error: %s: %s", ev.Error.Type, ev.Error.Message)
			}

		case "message_stop":
			// Stream complete
		}
	}

	if err := sse.Err(); err != nil {
		return nil, fmt.Errorf("anthropic stream read error: %w", err)
	}

	// Parse accumulated tool call JSON arguments
	for i, rawJSON := range toolCallJSON {
		if rawJSON != "" && i < len(result.ToolCalls) {
			args := make(map[string]any)
			if err := json.Unmarshal([]byte(rawJSON), &args); err != nil {
				result.ToolCalls[i].ParseError = fmt.Sprintf("malformed JSON (%d chars): %v", len(rawJSON), err)
			}
			result.ToolCalls[i].Arguments = args
		}
	}

	if result.Usage != nil {
		result.Usage.TotalTokens = result.Usage.PromptTokens + result.Usage.CompletionTokens
		// Estimate thinking tokens from accumulated character count (~4 chars per token)
		if thinkingChars > 0 {
			result.Usage.ThinkingTokens = thinkingChars / 4
		}
	}

	// Preserve raw content blocks for tool use passback
	if len(rawContentBlocks) > 0 && len(result.ToolCalls) > 0 {
		if b, err := json.Marshal(rawContentBlocks); err == nil {
			result.RawAssistantContent = b
		}
	}

	result.ThinkingSignature = thinkingSignature.String()

	if onChunk != nil {
		onChunk(StreamChunk{Done: true})
	}

	return result, nil
}
