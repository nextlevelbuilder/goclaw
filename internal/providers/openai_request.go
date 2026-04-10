package providers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

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

		// Echo reasoning_content only for APIs/models that accept it on assistant history.
		// Together Qwen and many OpenAI-compat gateways reject unknown message fields → HTTP 400.
		if m.Thinking != "" && m.Role == "assistant" && openAIWireAssistantReasoningContent(model) {
			msg["reasoning_content"] = m.Thinking
		}

		// Include content; omit empty content for assistant messages with tool_calls
		// (Gemini rejects empty content → "must include at least one parts field").
		if m.Role == "user" && len(m.Images) > 0 {
			var parts []map[string]any
			// Text before images — Together / Qwen vision examples use this order; OpenAI accepts both.
			if m.Content != "" {
				parts = append(parts, map[string]any{
					"type": "text",
					"text": m.Content,
				})
			}
			for _, img := range m.Images {
				parts = append(parts, map[string]any{
					"type": "image_url",
					"image_url": map[string]any{
						"url": fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Data),
					},
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
					"id":       p.wireToolCallID(tc.ID),
					"type":     "function",
					"function": fn,
				}
			}
			msg["tool_calls"] = toolCalls
		}

		if m.ToolCallID != "" {
			msg["tool_call_id"] = p.wireToolCallID(m.ToolCallID)
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

	// Together returns HTTP 400 on some requests when stream_options is present.
	if stream && !p.isTogetherEndpoint() {
		body["stream_options"] = map[string]any{
			"include_usage": true,
		}
	}

	// Merge options
	capabilityModel := modelFamily(model)
	if v, ok := req.Options[OptMaxTokens]; ok {
		// Fireworks requires stream=true for max_tokens > 4096.
		// Clamp proactively to avoid a 400 round-trip (their error format
		// doesn't match the generic clampMaxTokensFromError regex).
		if !stream && p.isFireworksEndpoint() {
			if maxTokens, isInt := v.(int); isInt && maxTokens > 4096 {
				v = 4096
				slog.Debug("max_tokens clamped to 4096 for Fireworks non-streaming request", "provider", p.name, "model", model)
			}
		}
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

	// reasoning_effort is OpenAI-specific; do not send to third-party OpenAI-compatible APIs.
	if level, ok := req.Options[OptThinkingLevel].(string); ok && level != "" && level != "off" {
		if openAIModelSupportsReasoningEffort(model) {
			body[OptReasoningEffort] = level
		}
	}

	// DashScope-specific passthrough keys — never send to other OpenAI-compat hosts.
	if p.dashScopePassthroughKeys() {
		if v, ok := req.Options[OptEnableThinking]; ok {
			body[OptEnableThinking] = v
		}
		if v, ok := req.Options[OptThinkingBudget]; ok {
			body[OptThinkingBudget] = v
		}
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

// openAIModelSupportsReasoningEffort is true when the Chat Completions request may include
// the top-level "reasoning_effort" field (OpenAI o-series / GPT-5 family).
// Other OpenAI-compatible hosts (Together, Groq, vLLM, etc.) often reject unknown fields with HTTP 400.
func openAIModelSupportsReasoningEffort(model string) bool {
	if LookupReasoningCapability(model) != nil {
		return true
	}
	fam := strings.ToLower(modelFamily(model))
	for _, prefix := range []string{"gpt-5", "o1", "o3", "o4"} {
		if strings.HasPrefix(fam, prefix) {
			return true
		}
	}
	return false
}

// openAIWireAssistantReasoningContent is true when assistant message objects may include
// "reasoning_content" (thinking replay). Narrow allowlist — most OpenAI-compat hosts reject it.
func openAIWireAssistantReasoningContent(model string) bool {
	if openAIModelSupportsReasoningEffort(model) {
		return true
	}
	fam := strings.ToLower(modelFamily(model))
	full := strings.ToLower(model)
	if strings.Contains(fam, "deepseek") || strings.Contains(full, "deepseek") {
		return true
	}
	if strings.Contains(fam, "kimi") || strings.Contains(full, "kimi") {
		return true
	}
	return false
}
