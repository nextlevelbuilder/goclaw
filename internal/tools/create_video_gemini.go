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
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// callGeminiVideoGen uses the Gemini predictLongRunning API for Veo video generation.
// Supports both text-to-video and image-to-video (Veo 3.1+).
// Flow: POST predictLongRunning → poll operation → download video from URI.
func (t *CreateVideoTool) callGeminiVideoGen(ctx context.Context, apiKey, apiBase, model, prompt string, duration int, aspectRatio string, params map[string]any) ([]byte, *providers.Usage, error) {
	nativeBase := strings.TrimRight(apiBase, "/")
	nativeBase = strings.TrimSuffix(nativeBase, "/openai")

	// 1. Build request body.
	predictURL := fmt.Sprintf("%s/models/%s:predictLongRunning", nativeBase, model)

	instance := map[string]any{"prompt": prompt}

	// Image-to-video: attach inline image data if provided.
	if imgB64 := GetParamString(params, "image_base64", ""); imgB64 != "" {
		instance["image"] = map[string]any{
			"inlineData": map[string]any{
				"mimeType": GetParamString(params, "image_mime", "image/jpeg"),
				"data":     imgB64,
			},
		}
	}

	// Parameters: resolution and generateAudio come from chain params (per-provider UI config).
	body := map[string]any{
		"instances": []map[string]any{instance},
		"parameters": map[string]any{
			"aspectRatio":      aspectRatio,
			"durationSeconds":  fmt.Sprintf("%d", duration), // Veo 3.1 expects string
			"resolution":       GetParamString(params, "resolution", "720p"),
			"generateAudio":    GetParamBool(params, "generate_audio", true),
			"personGeneration": GetParamString(params, "person_generation", "allow_all"),
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", predictURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", apiKey)

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

	var opResp struct {
		Name string `json:"name"`
		Done bool   `json:"done"`
	}
	if err := json.Unmarshal(respBody, &opResp); err != nil {
		return nil, nil, fmt.Errorf("parse operation response: %w", err)
	}
	if opResp.Name == "" {
		return nil, nil, fmt.Errorf("no operation name in response: %s", truncateBytes(respBody, 300))
	}

	slog.Info("create_video: operation started", "operation", opResp.Name)

	// 2. Poll operation until done (max ~6 minutes, poll every 10s).
	pollURL := fmt.Sprintf("%s/%s", nativeBase, opResp.Name)
	const maxPolls = 40
	const pollInterval = 10 * time.Second

	var doneBody []byte
	for i := range maxPolls {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(pollInterval):
		}

		pollReq, err := http.NewRequestWithContext(ctx, "GET", pollURL, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("create poll request: %w", err)
		}
		pollReq.Header.Set("x-goog-api-key", apiKey)

		pollResp, err := client.Do(pollReq)
		if err != nil {
			slog.Warn("create_video: poll error, retrying", "error", err, "attempt", i+1)
			continue
		}

		pollBody, _ := io.ReadAll(pollResp.Body)
		pollResp.Body.Close()

		if pollResp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("poll API error %d: %s", pollResp.StatusCode, truncateBytes(pollBody, 500))
		}

		var pollOp struct {
			Done  bool            `json:"done"`
			Error json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(pollBody, &pollOp); err != nil {
			return nil, nil, fmt.Errorf("parse poll response: %w", err)
		}

		if pollOp.Error != nil && string(pollOp.Error) != "null" {
			return nil, nil, fmt.Errorf("video generation error: %s", truncateBytes(pollOp.Error, 500))
		}

		if pollOp.Done {
			doneBody = pollBody
			break
		}

		slog.Info("create_video: polling", "attempt", i+1, "done", pollOp.Done)
	}

	if doneBody == nil {
		return nil, nil, fmt.Errorf("video generation timed out after %d polls", maxPolls)
	}

	// 3. Extract video URI and download.
	// Handle both Veo 3.1 (generatedVideos) and Veo 3.0 (generateVideoResponse.generatedSamples) formats.
	var result struct {
		Response struct {
			GeneratedVideos []struct {
				Video struct {
					URI string `json:"uri"`
				} `json:"video"`
			} `json:"generatedVideos"`
			GenerateVideoResponse struct {
				GeneratedSamples []struct {
					Video struct {
						URI      string `json:"uri"`
						MimeType string `json:"mimeType"`
					} `json:"video"`
				} `json:"generatedSamples"`
			} `json:"generateVideoResponse"`
		} `json:"response"`
	}
	if err := json.Unmarshal(doneBody, &result); err != nil {
		return nil, nil, fmt.Errorf("parse final response: %w", err)
	}

	// Try Veo 3.1 format first, fall back to Veo 3.0.
	var videoURI string
	if len(result.Response.GeneratedVideos) > 0 {
		videoURI = result.Response.GeneratedVideos[0].Video.URI
	}
	if videoURI == "" {
		samples := result.Response.GenerateVideoResponse.GeneratedSamples
		if len(samples) > 0 {
			videoURI = samples[0].Video.URI
		}
	}
	if videoURI == "" {
		return nil, nil, fmt.Errorf("no video in response: %s", truncateBytes(doneBody, 300))
	}

	slog.Info("create_video: downloading video", "uri", videoURI)

	dlReq, err := http.NewRequestWithContext(ctx, "GET", videoURI, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create download request: %w", err)
	}
	dlReq.Header.Set("x-goog-api-key", apiKey)

	dlClient := &http.Client{} // timeout governed by chain context
	dlResp, err := dlClient.Do(dlReq)
	if err != nil {
		return nil, nil, fmt.Errorf("download video: %w", err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		dlBody, _ := io.ReadAll(dlResp.Body)
		return nil, nil, fmt.Errorf("download error %d: %s", dlResp.StatusCode, truncateBytes(dlBody, 300))
	}

	videoBytes, err := limitedReadAll(dlResp.Body, maxMediaDownloadBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("read video data: %w", err)
	}

	return videoBytes, nil, nil
}
