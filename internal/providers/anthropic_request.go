package providers

import "encoding/json"

// buildRawBlock reconstructs a complete content block from streaming data.
// This is needed to preserve thinking blocks (with signatures) for tool use passback.
func (p *AnthropicProvider) buildRawBlock(blockType string, result *ChatResponse, toolCallJSON map[int]string, _ int) json.RawMessage {
	switch blockType {
	case "thinking":
		block := map[string]any{
			"type":     "thinking",
			"thinking": result.Thinking,
		}
		if result.ThinkingSignature != "" {
			block["signature"] = result.ThinkingSignature
		}
		if b, err := json.Marshal(block); err == nil {
			return b
		}
	case "text":
		block := map[string]any{
			"type": "text",
			"text": result.Content,
		}
		if b, err := json.Marshal(block); err == nil {
			return b
		}
	case "tool_use":
		if len(result.ToolCalls) > 0 {
			tc := result.ToolCalls[len(result.ToolCalls)-1]
			// Parse accumulated JSON for this tool call
			args := make(map[string]any)
			for i, rawJSON := range toolCallJSON {
				if i == len(result.ToolCalls)-1 && rawJSON != "" {
					_ = json.Unmarshal([]byte(rawJSON), &args)
				}
			}
			block := map[string]any{
				"type":  "tool_use",
				"id":    tc.ID,
				"name":  tc.Name,
				"input": args,
			}
			if b, err := json.Marshal(block); err == nil {
				return b
			}
		}
	case "redacted_thinking":
		// Pass through as-is (we don't have the encrypted data in streaming)
		block := map[string]any{
			"type": "redacted_thinking",
		}
		if b, err := json.Marshal(block); err == nil {
			return b
		}
	}
	return nil
}

func (p *AnthropicProvider) buildRequestBody(model string, req ChatRequest, stream bool) map[string]any {
	// Separate system messages and build conversation messages
	var systemBlocks []map[string]any
	var messages []map[string]any

	for _, msg := range req.Messages {
		switch msg.Role {
		case "system":
			systemBlocks = append(systemBlocks, map[string]any{
				"type": "text",
				"text": msg.Content,
			})

		case "user":
			if len(msg.Images) > 0 {
				var blocks []map[string]any
				for _, img := range msg.Images {
					blocks = append(blocks, map[string]any{
						"type": "image",
						"source": map[string]any{
							"type":       "base64",
							"media_type": img.MimeType,
							"data":       img.Data,
						},
					})
				}
				if msg.Content != "" {
					blocks = append(blocks, map[string]any{
						"type": "text",
						"text": msg.Content,
					})
				}
				messages = append(messages, map[string]any{
					"role":    "user",
					"content": blocks,
				})
			} else {
				messages = append(messages, map[string]any{
					"role":    "user",
					"content": msg.Content,
				})
			}

		case "assistant":
			// If we have raw content blocks (from Anthropic thinking), use them directly
			// to preserve thinking blocks + signatures for tool use passback.
			if msg.RawAssistantContent != nil {
				var rawBlocks []json.RawMessage
				if json.Unmarshal(msg.RawAssistantContent, &rawBlocks) == nil && len(rawBlocks) > 0 {
					messages = append(messages, map[string]any{
						"role":    "assistant",
						"content": rawBlocks,
					})
					continue
				}
			}

			var blocks []map[string]any
			if msg.Content != "" {
				blocks = append(blocks, map[string]any{
					"type": "text",
					"text": msg.Content,
				})
			}
			for _, tc := range msg.ToolCalls {
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": tc.Arguments,
				})
			}
			messages = append(messages, map[string]any{
				"role":    "assistant",
				"content": blocks,
			})

		case "tool":
			messages = append(messages, map[string]any{
				"role": "user",
				"content": []map[string]any{
					{
						"type":        "tool_result",
						"tool_use_id": msg.ToolCallID,
						"content":     msg.Content,
					},
				},
			})
		}
	}

	// Add cache_control breakpoint to the last system block (caches system prompt prefix).
	if len(systemBlocks) > 0 {
		systemBlocks[len(systemBlocks)-1]["cache_control"] = map[string]any{"type": "ephemeral"}
	}

	body := map[string]any{
		"model":      model,
		"max_tokens": 4096,
		"messages":   messages,
	}

	if stream {
		body["stream"] = true
	}

	if len(systemBlocks) > 0 {
		body["system"] = systemBlocks
	}

	// Translate tools to Anthropic format
	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			cleanedParams := CleanSchemaForProvider("anthropic", t.Function.Parameters)
			tool := map[string]any{
				"name":         t.Function.Name,
				"description":  t.Function.Description,
				"input_schema": cleanedParams,
			}
			tools = append(tools, tool)
		}
		// Add cache_control breakpoint to the last tool (caches tool definitions prefix).
		if len(tools) > 0 {
			tools[len(tools)-1]["cache_control"] = map[string]any{"type": "ephemeral"}
		}
		body["tools"] = tools
	}

	// Merge options
	if v, ok := req.Options[OptMaxTokens]; ok {
		body["max_tokens"] = v
	}
	if v, ok := req.Options[OptTemperature]; ok {
		body["temperature"] = v
	}

	// Enable extended thinking if thinking_level is set
	if level, ok := req.Options[OptThinkingLevel].(string); ok && level != "" && level != "off" {
		budget := anthropicThinkingBudget(level)
		body["thinking"] = map[string]any{
			"type":          "enabled",
			"budget_tokens": budget,
		}
		// Anthropic requires no temperature when thinking is enabled
		delete(body, "temperature")
		// Ensure max_tokens accommodates thinking budget + response
		if maxTok, ok := body["max_tokens"].(int); !ok || maxTok < budget+4096 {
			body["max_tokens"] = budget + 8192
		}
	}

	return body
}

// anthropicThinkingBudget maps a thinking level to a token budget.
func anthropicThinkingBudget(level string) int {
	switch level {
	case "low":
		return 4096
	case "medium":
		return 10000
	case "high":
		return 32000
	default:
		return 10000
	}
}
