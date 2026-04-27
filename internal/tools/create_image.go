package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// credentialProvider is a narrow interface for providers that expose API credentials.
type credentialProvider interface {
	APIKey() string
	APIBase() string
}

// oauthImageProvider is implemented by OAuth-based providers (e.g. CodexProvider)
// that can generate images via the ChatGPT conversation API.
type oauthImageProvider interface {
	OAuthToken() (string, error)
	OAuthAPIBase() string
}

// imageGenProviderPriority is the default order for image generation providers.
var imageGenProviderPriority = []string{"chatgpt_oauth", "openrouter", "gemini", "openai", "minimax", "dashscope", "byteplus"}

// imageGenModelDefaults maps provider names to default image generation models.
var imageGenModelDefaults = map[string]string{
	"chatgpt_oauth": "gpt-image-2",
	"openrouter":    "google/gemini-2.5-flash-image",
	"openai":        "gpt-image-1.5",
	"gemini":        "gemini-2.5-flash-image",
	"minimax":       "image-01",
	"dashscope":     "wan2.6-image",
	"byteplus":      "seedream-5-0-260128",
}

// CreateImageTool generates images using an image generation API.
type CreateImageTool struct {
	registry  *providers.Registry
	vaultIntc *VaultInterceptor
}

func (t *CreateImageTool) SetVaultInterceptor(v *VaultInterceptor) { t.vaultIntc = v }

func NewCreateImageTool(registry *providers.Registry) *CreateImageTool {
	return &CreateImageTool{registry: registry}
}

func (t *CreateImageTool) Name() string { return "create_image" }

func (t *CreateImageTool) Description() string {
	return "Generate an image from a text description using an image generation model. Returns a MEDIA: path to the generated image file."
}

func (t *CreateImageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "Text description of the image to generate.",
			},
			"aspect_ratio": map[string]any{
				"type":        "string",
				"description": "Aspect ratio: '1:1' (default), '3:4', '4:3', '9:16', '16:9'.",
			},
			"filename_hint": map[string]any{
				"type":        "string",
				"description": "Short descriptive filename (no extension). Example: 'sunset-beach', 'company-logo'.",
			},
		},
		"required": []string{"prompt"},
	}
}

func (t *CreateImageTool) Execute(ctx context.Context, args map[string]any) *Result {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		return ErrorResult("prompt is required")
	}
	aspectRatio, _ := args["aspect_ratio"].(string)
	if aspectRatio == "" {
		aspectRatio = "1:1"
	}
	filenameHint, _ := args["filename_hint"].(string)

	chain := ResolveMediaProviderChain(ctx, "create_image", "", "",
		imageGenProviderPriority, imageGenModelDefaults, t.registry)

	// Inject prompt and aspect_ratio into each chain entry's params
	for i := range chain {
		if chain[i].Params == nil {
			chain[i].Params = make(map[string]any)
		}
		chain[i].Params["prompt"] = prompt
		chain[i].Params["aspect_ratio"] = aspectRatio
	}

	chainResult, err := ExecuteWithChain(ctx, chain, t.registry, t.callProvider)
	if err != nil {
		return ErrorResult(fmt.Sprintf("image generation failed: %v", err))
	}

	// Save to workspace under date-based folder (e.g. generated/2026-03-02/)
	workspace := ToolWorkspaceFromCtx(ctx)
	if workspace == "" {
		workspace = os.TempDir()
	}
	dateDir := filepath.Join(workspace, "generated", time.Now().Format("2006-01-02"))
	if err := os.MkdirAll(dateDir, 0755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create output directory: %v", err))
	}
	imagePath := filepath.Join(dateDir, mediaFileName(ctx, "image", filenameHint, "png"))
	if err := os.WriteFile(imagePath, chainResult.Data, 0644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to save generated image: %v", err))
	}

	// Verify file was persisted (diagnostic for disappearing files).
	if fi, err := os.Stat(imagePath); err != nil {
		slog.Warn("create_image: file missing immediately after write", "path", imagePath, "error", err)
		return ErrorResult(fmt.Sprintf("generated image file missing after write: %v", err))
	} else {
		slog.Info("create_image: file saved", "path", imagePath, "size", fi.Size(), "data_len", len(chainResult.Data))
	}

	result := &Result{ForLLM: fmt.Sprintf("MEDIA:%s\nUse the EXACT filename when referencing: %s", imagePath, filepath.Base(imagePath))}
	result.Media = []bus.MediaFile{{Path: imagePath, MimeType: "image/png"}}
	result.Deliverable = fmt.Sprintf("[Generated image: %s]\nPrompt: %s", filepath.Base(imagePath), prompt)
	if t.vaultIntc != nil {
		go t.vaultIntc.AfterWriteMedia(context.WithoutCancel(ctx), imagePath, prompt, "image/png")
	}
	result.Provider = chainResult.Provider
	result.Model = chainResult.Model
	if chainResult.Usage != nil {
		result.Usage = chainResult.Usage
	}
	return result
}

// callProvider dispatches to the correct image generation implementation based on provider type.
func (t *CreateImageTool) callProvider(ctx context.Context, cp credentialProvider, providerName, model string, params map[string]any) ([]byte, *providers.Usage, error) {
	prompt := GetParamString(params, "prompt", "")
	aspectRatio := GetParamString(params, "aspect_ratio", "1:1")

	slog.Info("create_image: calling image generation API",
		"provider", providerName, "model", model, "aspect_ratio", aspectRatio)

	providerType := GetParamString(params, "_provider_type", providerTypeFromName(providerName))

	// OAuth-based providers (ChatGPT) use conversation API — no static credentials needed.
	if providerType == "chatgpt_oauth" {
		oap, _ := params["_oauth_provider"].(oauthImageProvider)
		if oap == nil {
			return nil, nil, fmt.Errorf("provider %q: OAuth token source not available", providerName)
		}
		return t.callChatGPTImageGen(ctx, oap, model, prompt, aspectRatio)
	}

	if cp == nil {
		return nil, nil, fmt.Errorf("provider %q does not expose API credentials required for image generation", providerName)
	}

	switch providerType {
	case "gemini":
		return t.callGeminiNativeImageGen(ctx, cp.APIKey(), cp.APIBase(), model, prompt, params)
	case "openrouter":
		return t.callImageGenAPI(ctx, cp.APIKey(), cp.APIBase(), model, prompt, aspectRatio, params)
	case "minimax":
		return callMinimaxImageGen(ctx, cp.APIKey(), cp.APIBase(), model, prompt, params)
	case "dashscope":
		return callDashScopeImageGen(ctx, cp.APIKey(), cp.APIBase(), model, prompt, params)
	case "byteplus":
		return callBytePlusImageGen(ctx, cp.APIKey(), cp.APIBase(), model, prompt, params)
	default:
		return t.callStandardImageGenAPI(ctx, cp.APIKey(), cp.APIBase(), model, prompt, params)
	}
}

// callImageGenAPI calls the OpenAI-compatible chat completions endpoint with image modalities.
// Works with OpenRouter (modalities: ["image","text"]).
func (t *CreateImageTool) callImageGenAPI(ctx context.Context, apiKey, apiBase, model, prompt, aspectRatio string, params map[string]any) ([]byte, *providers.Usage, error) {
	body := map[string]any{
		"model": model,
		"messages": []map[string]any{
			{"role": "user", "content": prompt},
		},
		"modalities": []string{"image", "text"},
	}
	if aspectRatio != "" && aspectRatio != "1:1" {
		body["image_config"] = map[string]any{
			"aspect_ratio": aspectRatio,
		}
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

	return t.parseImageResponse(respBody)
}

// callStandardImageGenAPI uses the /images/generations endpoint (OpenAI and compatible providers).
func (t *CreateImageTool) callStandardImageGenAPI(ctx context.Context, apiKey, apiBase, model, prompt string, params map[string]any) ([]byte, *providers.Usage, error) {
	body := map[string]any{
		"model":           model,
		"prompt":          prompt,
		"n":               1,
		"response_format": "b64_json",
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(apiBase, "/") + "/images/generations"
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

	var imgResp struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &imgResp); err != nil {
		return nil, nil, fmt.Errorf("parse response: %w", err)
	}
	if len(imgResp.Data) == 0 || imgResp.Data[0].B64JSON == "" {
		return nil, nil, fmt.Errorf("no image data in response")
	}

	imageBytes, err := base64.StdEncoding.DecodeString(imgResp.Data[0].B64JSON)
	if err != nil {
		return nil, nil, fmt.Errorf("decode base64: %w", err)
	}

	return imageBytes, nil, nil
}

// callChatGPTImageGen uses the ChatGPT conversation API to generate images via OAuth.
// This calls /backend-api/conversation (not /codex/responses) which supports image models.
func (t *CreateImageTool) callChatGPTImageGen(ctx context.Context, oap oauthImageProvider, model, prompt, aspectRatio string) ([]byte, *providers.Usage, error) {
	token, err := oap.OAuthToken()
	if err != nil {
		return nil, nil, fmt.Errorf("get OAuth token: %w", err)
	}

	apiBase := strings.TrimRight(oap.OAuthAPIBase(), "/")

	// Fetch sentinel chat-requirements token (required by ChatGPT backend).
	sentinelToken := t.fetchSentinelToken(ctx, apiBase, token)
	slog.Info("create_image: chatgpt sentinel", "has_token", sentinelToken != "", "token_len", len(sentinelToken))

	msgID := uuid.New().String()
	parentID := uuid.New().String()

	fullPrompt := prompt
	if aspectRatio != "" && aspectRatio != "1:1" {
		fullPrompt += fmt.Sprintf(" (aspect ratio: %s)", aspectRatio)
	}

	now := float64(time.Now().Unix())
	body := map[string]any{
		"action": "next",
		"messages": []map[string]any{
			{
				"id":     msgID,
				"author": map[string]any{"role": "user"},
				"content": map[string]any{
					"content_type": "text",
					"parts":        []string{fullPrompt},
				},
				"metadata": map[string]any{
					"serialization_metadata": map[string]any{"custom_symbol_offsets": []any{}},
					"system_hints":           []string{"picture_v2"},
				},
				"create_time": now,
			},
		},
		"parent_message_id":                  parentID,
		"model":                              "auto",
		"conversation_mode":                  map[string]any{"kind": "primary_assistant"},
		"system_hints":                       []string{"picture_v2"},
		"timezone_offset_min":                -420,
		"timezone":                           "Asia/Ho_Chi_Minh",
		"enable_message_followups":           true,
		"supports_buffering":                 true,
		"supported_encodings":                []string{"v1"},
		"paragen_cot_summary_display_override": "allow",
		"client_contextual_info": map[string]any{
			"is_dark_mode":    false,
			"time_since_loaded": 30,
			"page_height":     900,
			"page_width":      1440,
			"pixel_ratio":     2,
			"screen_height":   900,
			"screen_width":    1440,
		},
		"history_and_training_disabled": true,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	url := apiBase + "/conversation"
	slog.Debug("create_image: chatgpt request body", "body", string(jsonBody))

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Origin", "https://chatgpt.com")
	req.Header.Set("Referer", "https://chatgpt.com/")
	if sentinelToken != "" {
		req.Header.Set("Openai-Sentinel-Chat-Requirements-Token", sentinelToken)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		slog.Warn("create_image: chatgpt conversation error", "status", resp.StatusCode, "body", string(respBody), "has_sentinel", sentinelToken != "")
		return nil, nil, fmt.Errorf("ChatGPT API error %d: %s", resp.StatusCode, string(respBody))
	}

	slog.Info("create_image: chatgpt conversation streaming started")
	return t.parseChatGPTImageSSE(ctx, resp.Body, token, apiBase)
}

// fetchSentinelToken retrieves the chat-requirements token from ChatGPT backend.
func (t *CreateImageTool) fetchSentinelToken(ctx context.Context, apiBase, token string) string {
	url := apiBase + "/sentinel/chat-requirements"
	body := []byte(`{"p":null}`)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		slog.Warn("create_image: sentinel request creation failed", "error", err)
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("create_image: sentinel request failed", "error", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}
	return result.Token
}

// parseChatGPTImageSSE reads the SSE stream from ChatGPT conversation API
// and extracts the generated image URL, then downloads it.
func (t *CreateImageTool) parseChatGPTImageSSE(ctx context.Context, body io.Reader, token, apiBase string) ([]byte, *providers.Usage, error) {
	scanner := providers.NewSSEScanner(body)
	var imageURL string

	for scanner.Next() {
		data := scanner.Data()
		if data == "[DONE]" {
			break
		}

		var event struct {
			Message *struct {
				Content struct {
					ContentType string `json:"content_type"`
					Parts       []any  `json:"parts"`
				} `json:"content"`
				Metadata *struct {
					Dalle *struct {
						PromptRevision string `json:"prompt_revision,omitempty"`
					} `json:"dalle,omitempty"`
				} `json:"metadata,omitempty"`
			} `json:"message,omitempty"`
		}

		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if event.Message == nil {
			continue
		}

		ct := event.Message.Content.ContentType
		if ct == "multimodal_text" || ct == "image_asset_pointer" {
			for _, part := range event.Message.Content.Parts {
				partMap, ok := part.(map[string]any)
				if !ok {
					continue
				}
				// Image asset pointer format
				if assetPointer, ok := partMap["asset_pointer"].(string); ok && strings.HasPrefix(assetPointer, "file-service://") {
					fileID := strings.TrimPrefix(assetPointer, "file-service://")
					imageURL = apiBase + "/files/" + fileID + "/download"
				}
			}
		}
	}

	if imageURL == "" {
		return nil, nil, fmt.Errorf("no image generated in ChatGPT response")
	}

	// Download the image using the same OAuth token
	slog.Info("create_image: downloading ChatGPT generated image", "url", imageURL)
	imgReq, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create download request: %w", err)
	}
	imgReq.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	imgResp, err := client.Do(imgReq)
	if err != nil {
		return nil, nil, fmt.Errorf("download image: %w", err)
	}
	defer imgResp.Body.Close()

	if imgResp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("image download error %d", imgResp.StatusCode)
	}

	imageBytes, err := limitedReadAll(imgResp.Body, maxMediaDownloadBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("read image: %w", err)
	}

	return imageBytes, nil, nil
}

// callGeminiNativeImageGen uses the native Gemini generateContent API with responseModalities.
// Gemini image models require this endpoint — they don't support OpenAI-compat endpoints.
func (t *CreateImageTool) callGeminiNativeImageGen(ctx context.Context, apiKey, apiBase, model, prompt string, params map[string]any) ([]byte, *providers.Usage, error) {
	// Derive native Gemini base from OpenAI-compat base (strip /openai suffix)
	nativeBase := strings.TrimRight(apiBase, "/")
	nativeBase = strings.TrimSuffix(nativeBase, "/openai")

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", nativeBase, model, apiKey)

	body := map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]any{{"text": prompt}}},
		},
		"generationConfig": map[string]any{
			"responseModalities": []string{"TEXT", "IMAGE"},
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

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

	// Parse native Gemini response: {candidates: [{content: {parts: [{inlineData: {mimeType, data}}]}}]}
	var gemResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData *struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata *struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(respBody, &gemResp); err != nil {
		return nil, nil, fmt.Errorf("parse response: %w", err)
	}

	// Extract first image from parts
	for _, cand := range gemResp.Candidates {
		for _, part := range cand.Content.Parts {
			if part.InlineData != nil && strings.HasPrefix(part.InlineData.MimeType, "image/") {
				imageBytes, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
				if err != nil {
					return nil, nil, fmt.Errorf("decode base64: %w", err)
				}
				var usage *providers.Usage
				if gemResp.UsageMetadata != nil {
					usage = &providers.Usage{
						PromptTokens:     gemResp.UsageMetadata.PromptTokenCount,
						CompletionTokens: gemResp.UsageMetadata.CandidatesTokenCount,
						TotalTokens:      gemResp.UsageMetadata.TotalTokenCount,
					}
				}
				return imageBytes, usage, nil
			}
		}
	}

	return nil, nil, fmt.Errorf("no image data in Gemini response")
}

// parseImageResponse extracts base64 image data from the OpenAI-compat chat response.
// Looks for images in choices[0].message.content (multipart) or choices[0].message.images.
func (t *CreateImageTool) parseImageResponse(respBody []byte) ([]byte, *providers.Usage, error) {
	var resp struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
				Images  []struct {
					ImageURL struct {
						URL string `json:"url"`
					} `json:"image_url"`
				} `json:"images"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, nil, fmt.Errorf("parse response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, nil, fmt.Errorf("no choices in response")
	}

	msg := resp.Choices[0].Message

	// Try images array first (OpenRouter format)
	for _, img := range msg.Images {
		if imageBytes, err := decodeDataURL(img.ImageURL.URL); err == nil {
			return imageBytes, convertUsage(resp.Usage), nil
		}
	}

	// Try multipart content array (some providers return content as array of parts)
	if parts, ok := msg.Content.([]any); ok {
		for _, part := range parts {
			if m, ok := part.(map[string]any); ok {
				if m["type"] == "image_url" {
					if imgURL, ok := m["image_url"].(map[string]any); ok {
						if url, ok := imgURL["url"].(string); ok {
							if imageBytes, err := decodeDataURL(url); err == nil {
								return imageBytes, convertUsage(resp.Usage), nil
							}
						}
					}
				}
			}
		}
	}

	return nil, nil, fmt.Errorf("no image data found in response")
}

// decodeDataURL decodes a data:image/...;base64,... URL into raw bytes.
func decodeDataURL(dataURL string) ([]byte, error) {
	_, after, ok := strings.Cut(dataURL, ";base64,")
	if !ok {
		return nil, fmt.Errorf("not a base64 data URL")
	}
	b64 := after
	return base64.StdEncoding.DecodeString(b64)
}

func convertUsage(u *struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}) *providers.Usage {
	if u == nil {
		return nil
	}
	return &providers.Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
	}
}

func truncateBytes(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}
