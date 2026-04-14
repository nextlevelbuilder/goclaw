package providers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

func parseQwenJSONResponse(data []byte) (*ChatResponse, error) {
	if resp := parseQwenJSONArray(data); resp != nil {
		return resp, nil
	}

	for line := range bytes.SplitSeq(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if resp := parseQwenSingleJSONResult(line); resp != nil {
			return resp, nil
		}
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, fmt.Errorf("qwen-cli: empty response")
	}
	return &ChatResponse{
		Content:      trimmed,
		FinishReason: "stop",
	}, nil
}

func parseQwenJSONArray(data []byte) *ChatResponse {
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
			Usage   *qwenUsage      `json:"usage,omitempty"`
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
					TotalTokens:      ev.Usage.TotalTokens,
					CacheReadTokens:  ev.Usage.CacheReadInputTokens,
				}
			}

		case "assistant":
			if ev.Message != nil {
				var msg qwenStreamMsg
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

func parseQwenSingleJSONResult(line []byte) *ChatResponse {
	var resp qwenJSONResponse
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
			TotalTokens:      resp.Usage.TotalTokens,
			CacheReadTokens:  resp.Usage.CacheReadInputTokens,
		}
	}
	return cr
}

func extractQwenStreamContent(msg *qwenStreamMsg) (text, thinking string) {
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
