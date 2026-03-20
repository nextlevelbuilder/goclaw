package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// invalidFcIDChars matches characters not allowed in Responses API tool call IDs.
var invalidFcIDChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// buildRequestBody converts internal ChatRequest to Responses API format.
func (p *CodexProvider) buildRequestBody(req ChatRequest, stream bool) map[string]any {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	var instructions string
	var input []any

	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			if instructions == "" {
				instructions = m.Content
			} else {
				instructions += "\n\n" + m.Content
			}

		case "user":
			if len(m.Images) > 0 {
				var parts []map[string]any
				for _, img := range m.Images {
					parts = append(parts, map[string]any{
						"type":      "input_image",
						"image_url": fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Data),
					})
				}
				if m.Content != "" {
					parts = append(parts, map[string]any{
						"type": "input_text",
						"text": m.Content,
					})
				}
				input = append(input, map[string]any{
					"role":    "user",
					"content": parts,
				})
			} else {
				input = append(input, map[string]any{
					"role":    "user",
					"content": m.Content,
				})
			}

		case "assistant":
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					argsJSON, _ := json.Marshal(tc.Arguments)
					callID := toFcID(tc.ID)
					input = append(input, map[string]any{
						"type":      "function_call",
						"id":        callID,
						"call_id":   callID,
						"name":      tc.Name,
						"arguments": string(argsJSON),
					})
				}
			}
			if m.Content != "" {
				item := map[string]any{
					"type": "message",
					"role": "assistant",
					"content": []map[string]any{
						{"type": "output_text", "text": m.Content},
					},
				}
				if m.Phase != "" {
					item["phase"] = m.Phase
				}
				input = append(input, item)
			}

		case "tool":
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": toFcID(m.ToolCallID),
				"output":  m.Content,
			})
		}
	}

	body := map[string]any{
		"model":  model,
		"input":  input,
		"stream": stream,
		"store":  false,
	}

	if instructions == "" {
		instructions = "You are a helpful assistant."
	}
	body["instructions"] = instructions

	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"type":        "function",
				"name":        t.Function.Name,
				"description": t.Function.Description,
				"parameters":  t.Function.Parameters,
			})
		}
		body["tools"] = tools
	}

	if level, ok := req.Options[OptThinkingLevel].(string); ok && level != "" && level != "off" {
		body["reasoning"] = map[string]any{"effort": level}
	}

	return body
}

func (p *CodexProvider) doRequest(ctx context.Context, body any) (io.ReadCloser, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal request: %w", p.name, err)
	}

	endpoint := p.apiBase + "/codex/responses"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", p.name, err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	token, err := p.tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("%s: get auth token: %w", p.name, err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("OpenAI-Beta", "responses=v1")

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

// toFcID ensures a tool call ID starts with "fc_" and contains only
// letters, numbers, underscores, or dashes as required by the Responses API.
func toFcID(id string) string {
	if strings.HasPrefix(id, "tool_") {
		id = id[len("tool_"):]
	} else if strings.HasPrefix(id, "call_") {
		id = id[len("call_"):]
	} else if strings.HasPrefix(id, "fc_") {
		id = id[len("fc_"):]
	}
	// Replace invalid characters (e.g. colons from session keys) with underscores.
	id = invalidFcIDChars.ReplaceAllString(id, "_")
	return "fc_" + id
}
