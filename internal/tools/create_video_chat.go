package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// callChatVideoGen tries OpenAI-compatible chat completions with video modality.
// Image-to-video is not supported by chat providers — image data is ignored.
func (t *CreateVideoTool) callChatVideoGen(ctx context.Context, apiKey, apiBase, model, prompt string, duration int, aspectRatio string, params map[string]any) ([]byte, *providers.Usage, error) {
	if GetParamString(params, "image_base64", "") != "" {
		slog.Warn("create_video: image-to-video not supported by chat provider, falling back to text-to-video")
	}
	body := map[string]any{
		"model": model,
		"messages": []map[string]any{
			{"role": "user", "content": prompt},
		},
		"modalities":   []string{"video", "text"},
		"duration":     duration,
		"aspect_ratio": aspectRatio,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(apiBase, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{} // timeout governed by chain context
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("API error %d: %s", resp.StatusCode, truncateBytes(respBody, 500))
	}

	// Try to extract video from multipart content or data URL.
	var chatResp struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, nil, fmt.Errorf("parse response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return nil, nil, fmt.Errorf("no choices in response")
	}

	// Look for video data URL in multipart content.
	if parts, ok := chatResp.Choices[0].Message.Content.([]any); ok {
		for _, part := range parts {
			if m, ok := part.(map[string]any); ok {
				if m["type"] == "video_url" || m["type"] == "image_url" {
					if vidURL, ok := m["video_url"].(map[string]any); ok {
						if urlStr, ok := vidURL["url"].(string); ok {
							if videoBytes, err := decodeDataURL(urlStr); err == nil {
								return videoBytes, convertUsage(chatResp.Usage), nil
							}
						}
					}
				}
			}
		}
	}

	return nil, nil, fmt.Errorf("no video data found in response")
}
