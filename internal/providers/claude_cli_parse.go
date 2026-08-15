package providers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// parseJSONResponse parses the CLI JSON output into a ChatResponse.
func parseJSONResponse(data []byte) (*ChatResponse, error) {
	// Try parsing as JSON array first (CLI may output all events as a single array).
	if resp := parseJSONArray(data); resp != nil {
		return resp, nil
	}

	// Fallback: CLI may output one JSON object per line.
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if resp := parseSingleJSONResult(line); resp != nil {
			return resp, nil
		}
	}

	// Last resort: treat entire output as text response
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, fmt.Errorf("claude-cli: empty response")
	}
	return &ChatResponse{
		Content:      trimmed,
		FinishReason: "stop",
	}, nil
}

// parseJSONArray tries to parse data as a JSON array of CLI events, extracting
// the "result" event's text content and "assistant" event's text blocks.
func parseJSONArray(data []byte) *ChatResponse {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil
	}

	var events []json.RawMessage
	if err := json.Unmarshal(trimmed, &events); err != nil {
		return nil
	}

	var resultText string
	var assistantText strings.Builder
	var usage *Usage
	finishReason := "stop"

	for _, raw := range events {
		var ev struct {
			Type    string          `json:"type"`
			Subtype string          `json:"subtype,omitempty"`
			Result  string          `json:"result,omitempty"`
			Message json.RawMessage `json:"message,omitempty"`
			Usage   *cliUsage       `json:"usage,omitempty"`
		}
		if err := json.Unmarshal(raw, &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "result":
			resultText = ev.Result
			if ev.Subtype == "error" {
				finishReason = "error"
			}
			if ev.Usage != nil {
				usage = &Usage{
					PromptTokens:     ev.Usage.InputTokens,
					CompletionTokens: ev.Usage.OutputTokens,
					TotalTokens:      ev.Usage.InputTokens + ev.Usage.OutputTokens,
				}
			}

		case "assistant":
			// Extract text from content blocks
			if ev.Message != nil {
				var msg cliStreamMsg
				if err := json.Unmarshal(ev.Message, &msg); err == nil {
					for _, block := range msg.Content {
						if block.Type == "text" {
							assistantText.WriteString(block.Text)
						}
					}
				}
			}
		}
	}

	// Prefer "result" text, fall back to concatenated assistant text blocks
	content := resultText
	if content == "" {
		content = assistantText.String()
	}
	if content == "" {
		return nil
	}

	return &ChatResponse{
		Content:      content,
		FinishReason: finishReason,
		Usage:        usage,
	}
}

// parseSingleJSONResult tries to parse a single JSON line as a "result" event.
func parseSingleJSONResult(line []byte) *ChatResponse {
	var resp cliJSONResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil
	}
	if resp.Type != "result" {
		return nil
	}
	cr := &ChatResponse{
		Content:      resp.Result,
		FinishReason: "stop",
	}
	if resp.Subtype == "error" {
		cr.FinishReason = "error"
	}
	if resp.Usage != nil {
		cr.Usage = &Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		}
	}
	return cr
}

// extractStreamContent extracts text and thinking from a stream message.
func extractStreamContent(msg *cliStreamMsg) (text, thinking string) {
	var textBuf, thinkBuf strings.Builder
	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			textBuf.WriteString(block.Text)
		case "thinking":
			thinkBuf.WriteString(block.Thinking)
		}
	}
	return textBuf.String(), thinkBuf.String()
}
