package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// GenerateImage implements NativeImageProvider for CodexProvider.
// Sends a minimal POST /codex/responses request with an image_generation tool
// and tool_choice forced to image_generation. Returns decoded image bytes.
func (p *CodexProvider) GenerateImage(ctx context.Context, req NativeImageRequest) (*NativeImageResult, error) {
	if req.Prompt == "" {
		return nil, fmt.Errorf("codex native image: prompt is required")
	}
	if req.OutputFormat == "" {
		req.OutputFormat = "png"
	}
	if req.AspectRatio == "" {
		req.AspectRatio = "1:1"
	}

	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	imageModel, err := ValidateImageModel(req.ImageModel)
	if err != nil {
		return nil, err
	}
	req.ImageModel = imageModel

	body := p.buildNativeImageRequestBody(model, req)

	respBody, err := RetryDo(ctx, p.retryConfig, func() (io.ReadCloser, error) {
		return p.doRequest(ctx, body)
	})
	if err != nil {
		return nil, fmt.Errorf("codex native image: request failed: %w", err)
	}
	defer respBody.Close()

	raw, err := io.ReadAll(respBody)
	if err != nil {
		return nil, fmt.Errorf("codex native image: read response: %w", err)
	}

	return parseNativeImageResponse(raw)
}

// buildNativeImageRequestBody constructs the minimal Responses API body for image generation.
// The Responses API rejects non-streaming requests with HTTP 400 "Stream must be set to true",
// so stream is always true. Final assembly happens in parseNativeImageSSE which scans the
// event stream for response.output_item.done (image item) or response.completed output walk.
//
// When req.InputImages is non-empty, the user turn is built as a multipart array
// (input_image parts followed by the input_text prompt) so the model receives the
// references as visual context for image-to-image editing.
func (p *CodexProvider) buildNativeImageRequestBody(model string, req NativeImageRequest) map[string]any {
	content := make([]map[string]any, 0, len(req.InputImages)+1)
	for _, img := range req.InputImages {
		if len(img.Data) == 0 {
			continue
		}
		mime := img.MimeType
		if mime == "" {
			mime = "image/png"
		}
		content = append(content, map[string]any{
			"type":      "input_image",
			"image_url": fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(img.Data)),
		})
	}
	content = append(content, map[string]any{
		"type": "input_text",
		"text": req.Prompt,
	})

	instructions := "Generate an image matching the user's description using the image_generation tool. Return only the image; do not describe it in text."
	if len(req.InputImages) > 0 {
		instructions = "The user has provided reference image(s) and a description. Use the image_generation tool to produce a single new image guided by both. Return only the image; do not describe it in text."
	}

	return map[string]any{
		"model":        model,
		"stream":       true,
		"store":        false,
		"instructions": instructions,
		"input": []any{
			map[string]any{
				"role":    "user",
				"content": content,
			},
		},
		"tools": []map[string]any{
			{
				"type":          "image_generation",
				"action":        "generate",
				"model":         req.ImageModel,
				"output_format": req.OutputFormat,
				"size":          SizeFromAspect(req.AspectRatio),
			},
		},
		"tool_choice": map[string]any{
			"type": "image_generation",
		},
	}
}

// parseNativeImageResponse extracts base64-encoded image bytes from a Responses API
// non-streaming body (single JSON object). Walks output[] for type == "image_generation_call".
func parseNativeImageResponse(data []byte) (*NativeImageResult, error) {
	// Non-streaming path returns a raw JSON object (not SSE lines).
	// If the response looks like SSE (starts with "data:"), fall back to SSE parse.
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] != '{' {
		return parseNativeImageSSE(data)
	}

	var resp codexAPIResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("codex native image: decode response: %w", err)
	}

	if resp.Error != nil {
		msg := resp.Error.Message
		if msg == "" {
			msg = resp.Error.Code
		}
		return nil, fmt.Errorf("codex native image: API error: %s", msg)
	}

	for i := range resp.Output {
		item := &resp.Output[i]
		if item.Type == "image_generation_call" && item.Result != "" {
			raw, err := base64.StdEncoding.DecodeString(item.Result)
			if err != nil {
				return nil, fmt.Errorf("codex native image: decode base64: %w", err)
			}
			mime := mimeFromFormat(item.OutputFormat)
			var usage *Usage
			if resp.Usage != nil {
				usage = &Usage{
					PromptTokens:     resp.Usage.InputTokens,
					CompletionTokens: resp.Usage.OutputTokens,
					TotalTokens:      resp.Usage.TotalTokens,
				}
			}
			return &NativeImageResult{MimeType: mime, Data: raw, Usage: usage}, nil
		}
	}

	// No image returned. The model produced only message/reasoning items —
	// most commonly a refusal or a "here is what I would do" text response.
	// Surface the assistant text so the upstream tool error tells the agent
	// exactly why no image came back, instead of a generic "no image" error.
	if msg := extractCodexAssistantText(resp.Output); msg != "" {
		return nil, fmt.Errorf("codex native image: model returned text instead of image: %s", msg)
	}
	return nil, fmt.Errorf("codex native image: no image_generation_call in response output")
}

// extractCodexAssistantText concatenates any output_text content from message
// items. Used to surface refusal/explanation text when the model declines to
// generate an image. Returns an empty string when no message text is present.
func extractCodexAssistantText(items []codexItem) string {
	var parts []string
	for i := range items {
		item := &items[i]
		if item.Type != "message" {
			continue
		}
		for _, c := range item.Content {
			if c.Type == "output_text" && c.Text != "" {
				parts = append(parts, c.Text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// parseNativeImageSSE parses SSE-streamed lines when the server unexpectedly returns
// a stream despite stream:false. Looks for response.completed or output_item.done events.
func parseNativeImageSSE(data []byte) (*NativeImageResult, error) {
	// Scan lines for "data: {...}" frames.
	var b64 string
	var outputFormat string
	var usage *Usage
	// Track assistant text so we can surface a refusal/explanation when the
	// stream ends without an image. Text arrives either as incremental deltas
	// (response.output_text.delta) or in the completed item walk below.
	var textDeltas []string
	var completedText string

	for line := range bytes.SplitSeq(data, []byte("\n")) {
		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}
		payload := line[len("data: "):]
		if bytes.Equal(payload, []byte("[DONE]")) {
			break
		}

		var event codexSSEEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			continue
		}

		switch event.Type {
		case "response.output_item.done":
			if event.Item != nil && event.Item.Type == "image_generation_call" && event.Item.Result != "" {
				b64 = event.Item.Result
				outputFormat = event.Item.OutputFormat
			}
		case "response.output_text.delta":
			if event.Delta != "" {
				textDeltas = append(textDeltas, event.Delta)
			}
		case "response.completed":
			if event.Response != nil {
				for i := range event.Response.Output {
					item := &event.Response.Output[i]
					if item.Type == "image_generation_call" && item.Result != "" {
						b64 = item.Result
						outputFormat = item.OutputFormat
					}
				}
				if event.Response.Usage != nil {
					u := event.Response.Usage
					usage = &Usage{
						PromptTokens:     u.InputTokens,
						CompletionTokens: u.OutputTokens,
						TotalTokens:      u.TotalTokens,
					}
				}
				completedText = extractCodexAssistantText(event.Response.Output)
			}
		}
	}

	if b64 == "" {
		// Prefer the text gathered from the completed response walk; fall back
		// to concatenated deltas if the server didn't emit a final aggregate.
		msg := completedText
		if msg == "" {
			msg = strings.TrimSpace(strings.Join(textDeltas, ""))
		}
		if msg != "" {
			return nil, fmt.Errorf("codex native image: model returned text instead of image: %s", msg)
		}
		return nil, fmt.Errorf("codex native image: no image in SSE stream")
	}

	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("codex native image: decode base64 from SSE: %w", err)
	}

	return &NativeImageResult{
		MimeType: mimeFromFormat(outputFormat),
		Data:     raw,
		Usage:    usage,
	}, nil
}

// GenerateImage implements NativeImageProvider for CodexAdapter.
// Delegates to a temporary CodexProvider using the adapter's credentials.
func (a *CodexAdapter) GenerateImage(ctx context.Context, req NativeImageRequest) (*NativeImageResult, error) {
	p := &CodexProvider{
		name:         "codex",
		apiBase:      a.apiBase,
		defaultModel: a.defaultModel,
		client:       NewDefaultHTTPClient(),
		retryConfig:  DefaultRetryConfig(),
		tokenSource:  a.tokenSource,
	}
	return p.GenerateImage(ctx, req)
}
