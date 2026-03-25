package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// videoGenProviderPriority is the default order for video generation providers.
var videoGenProviderPriority = []string{"gemini", "minimax", "openrouter"}

// videoGenModelDefaults maps provider names to default video generation models.
var videoGenModelDefaults = map[string]string{
	"gemini":     "veo-3.0-generate-preview",
	"minimax":    "MiniMax-Hailuo-2.3",
	"openrouter": "google/veo-3.0-generate-preview",
}

// CreateVideoTool generates videos using a video generation API.
// Uses Gemini Veo via predictLongRunning API (async with polling).
type CreateVideoTool struct {
	registry *providers.Registry
}

func NewCreateVideoTool(registry *providers.Registry) *CreateVideoTool {
	return &CreateVideoTool{registry: registry}
}

func (t *CreateVideoTool) Name() string { return "create_video" }

func (t *CreateVideoTool) Description() string {
	return "Generate a video from a text description using a video generation model. Returns a MEDIA: path to the generated video file."
}

func (t *CreateVideoTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "Text description of the video to generate.",
			},
			"duration": map[string]any{
				"type":        "integer",
				"description": "Video duration in seconds: 4, 6, or 8 (default 8).",
			},
			"aspect_ratio": map[string]any{
				"type":        "string",
				"description": "Aspect ratio: '16:9' (default) or '9:16'.",
			},
			"filename_hint": map[string]any{
				"type":        "string",
				"description": "Short descriptive filename (no extension). Example: 'cat-playing-piano', 'product-demo'.",
			},
		},
		"required": []string{"prompt"},
	}
}

func (t *CreateVideoTool) Execute(ctx context.Context, args map[string]any) *Result {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		return ErrorResult("prompt is required")
	}
	filenameHint, _ := args["filename_hint"].(string)

	// Parse and enforce duration. Veo supports 4, 6, or 8 seconds.
	duration := 8
	if d, ok := args["duration"].(float64); ok {
		duration = int(d)
	}
	switch {
	case duration <= 4:
		duration = 4
	case duration <= 6:
		duration = 6
	default:
		duration = 8
	}

	// Parse and validate aspect ratio. Veo supports 16:9 and 9:16.
	aspectRatio := "16:9"
	if ar, _ := args["aspect_ratio"].(string); ar != "" {
		switch ar {
		case "16:9", "9:16":
			aspectRatio = ar
		default:
			return ErrorResult(fmt.Sprintf("unsupported aspect_ratio %q; use '16:9' or '9:16'", ar))
		}
	}

	chain := ResolveMediaProviderChain(ctx, "create_video", "", "",
		videoGenProviderPriority, videoGenModelDefaults, t.registry)

	// Inject prompt, duration, and aspect_ratio into each chain entry's params.
	for i := range chain {
		if chain[i].Params == nil {
			chain[i].Params = make(map[string]any)
		}
		chain[i].Params["prompt"] = prompt
		chain[i].Params["duration"] = duration
		chain[i].Params["aspect_ratio"] = aspectRatio
	}

	chainResult, err := ExecuteWithChain(ctx, chain, t.registry, t.callProvider)
	if err != nil {
		return ErrorResult(fmt.Sprintf("video generation failed: %v", err))
	}

	// Save to workspace under date-based folder.
	workspace := ToolWorkspaceFromCtx(ctx)
	if workspace == "" {
		workspace = os.TempDir()
	}
	dateDir := filepath.Join(workspace, "generated", time.Now().Format("2006-01-02"))
	if err := os.MkdirAll(dateDir, 0755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create output directory: %v", err))
	}
	videoPath := filepath.Join(dateDir, mediaFileName(ctx, "video", filenameHint, "mp4"))
	if err := os.WriteFile(videoPath, chainResult.Data, 0644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to save generated video: %v", err))
	}

	// Verify file was persisted.
	if fi, err := os.Stat(videoPath); err != nil {
		slog.Warn("create_video: file missing immediately after write", "path", videoPath, "error", err)
		return ErrorResult(fmt.Sprintf("generated video file missing after write: %v", err))
	} else {
		slog.Info("create_video: file saved", "path", videoPath, "size", fi.Size(), "data_len", len(chainResult.Data))
	}

	result := &Result{ForLLM: fmt.Sprintf("MEDIA:%s", videoPath)}
	result.Media = []bus.MediaFile{{Path: videoPath, MimeType: "video/mp4"}}
	result.Deliverable = fmt.Sprintf("[Generated video: %s]\nPrompt: %s", filepath.Base(videoPath), prompt)
	result.Provider = chainResult.Provider
	result.Model = chainResult.Model
	if chainResult.Usage != nil {
		result.Usage = chainResult.Usage
	}
	return result
}

// callProvider dispatches to the correct video generation implementation based on provider type.
func (t *CreateVideoTool) callProvider(ctx context.Context, cp credentialProvider, providerName, model string, params map[string]any) ([]byte, *providers.Usage, error) {
	if cp == nil {
		return nil, nil, fmt.Errorf("provider %q does not expose API credentials required for video generation", providerName)
	}
	prompt := GetParamString(params, "prompt", "")
	duration := GetParamInt(params, "duration", 8)
	aspectRatio := GetParamString(params, "aspect_ratio", "16:9")

	slog.Info("create_video: calling video generation API",
		"provider", providerName, "model", model, "duration", duration, "aspect_ratio", aspectRatio)

	switch GetParamString(params, "_provider_type", providerTypeFromName(providerName)) {
	case "gemini":
		return t.callGeminiVideoGen(ctx, cp.APIKey(), cp.APIBase(), model, prompt, duration, aspectRatio, params)
	case "minimax":
		return callMinimaxVideoGen(ctx, cp.APIKey(), cp.APIBase(), model, params)
	default:
		return t.callChatVideoGen(ctx, cp.APIKey(), cp.APIBase(), model, prompt, duration, aspectRatio)
	}
}

// callGeminiVideoGen uses the Gemini predictLongRunning API for Veo video generation.
// Flow: POST predictLongRunning → poll operation → download video from URI.
func (t *CreateVideoTool) callGeminiVideoGen(ctx context.Context, apiKey, apiBase, model, prompt string, duration int, aspectRatio string, params map[string]any) ([]byte, *providers.Usage, error) {
	nativeBase := strings.TrimRight(apiBase, "/")
	nativeBase = strings.TrimSuffix(nativeBase, "/openai")

	// 1. Start long-running video generation.
	predictURL := fmt.Sprintf("%s/models/%s:predictLongRunning", nativeBase, model)

	body := map[string]any{
		"instances": []map[string]any{
			{"prompt": prompt},
		},
		"parameters": map[string]any{
			"aspectRatio":      aspectRatio,
			"durationSeconds":  duration,
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
	var result struct {
		Response struct {
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

	samples := result.Response.GenerateVideoResponse.GeneratedSamples
	if len(samples) == 0 || samples[0].Video.URI == "" {
		return nil, nil, fmt.Errorf("no video in response: %s", truncateBytes(doneBody, 300))
	}

	videoURI := samples[0].Video.URI
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

// callChatVideoGen tries OpenAI-compatible chat completions with video modality.
func (t *CreateVideoTool) callChatVideoGen(ctx context.Context, apiKey, apiBase, model, prompt string, duration int, aspectRatio string) ([]byte, *providers.Usage, error) {
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
