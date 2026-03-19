package providers

import "strings"

// collapseToolCallsWithoutSig rewrites tool_call cycles that lack thought_signature
// (required by Gemini 2.5+). Gemini requires thought_signature echoed back on every
// tool_call; models that don't return it cause HTTP 400 if sent as-is.
//
// The assistant's tool_calls are stripped, and the corresponding tool-result messages
// are folded into a single user message with the tool output content. This preserves
// context for the model without using a format that triggers tool-call imitation.
func collapseToolCallsWithoutSig(msgs []Message) []Message {
	// Collect tool_call IDs that need collapsing.
	collapseIDs := make(map[string]bool)
	for _, m := range msgs {
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		for _, tc := range m.ToolCalls {
			// If meta is nil or signature is empty/whitespace, collapse the whole message's tool cycle.
			// Checks both snake_case and camelCase for cross-proxy reliability.
			sig := ""
			if tc.Metadata != nil {
				sig = tc.Metadata["thought_signature"]
				if sig == "" {
					sig = tc.Metadata["thoughtSignature"]
				}
			}

			if strings.TrimSpace(sig) == "" {
				for _, tc2 := range m.ToolCalls {
					collapseIDs[tc2.ID] = true
				}
				break
			}
		}
	}
	if len(collapseIDs) == 0 {
		return msgs
	}

	result := make([]Message, 0, len(msgs))
	for i := 0; i < len(msgs); i++ {
		m := msgs[i]

		// Strip tool_calls from assistant message, keep original content only.
		if m.Role == "assistant" && len(m.ToolCalls) > 0 && collapseIDs[m.ToolCalls[0].ID] {
			if m.Content != "" {
				result = append(result, Message{
					Role:    "assistant",
					Content: m.Content,
				})
			}

			// Collect consecutive tool results → fold into one user message.
			var parts []string
			for i+1 < len(msgs) && msgs[i+1].Role == "tool" && collapseIDs[msgs[i+1].ToolCallID] {
				i++
				if content := strings.TrimSpace(msgs[i].Content); content != "" {
					parts = append(parts, content)
				}
			}
			if len(parts) > 0 {
				result = append(result, Message{
					Role:    "user",
					Content: strings.Join(parts, "\n\n"),
				})
			}
			continue
		}

		// Skip orphaned tool results whose assistant was already collapsed.
		if m.Role == "tool" && collapseIDs[m.ToolCallID] {
			continue
		}

		result = append(result, m)
	}
	return result
}
