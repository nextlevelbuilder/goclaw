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

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// credentialProvider is a narrow interface for providers that expose API credentials.
type credentialProvider interface {
	APIKey() string
	APIBase() string
}

// imageGenProviderPriority is the default order for image generation providers.
var imageGenProviderPriority = []string{"openrouter", "gemini", "openai"}

// imageGenModelDefaults maps provider names to default image generation models.
var imageGenModelDefaults = map[string]string{
	"openrouter": "google/gemini-2.5-flash-image",
	"openai":     "dall-e-3",
	"gemini":     "gemini-2.5-flash-image",
}

// CreateImageTool generates images using an image generation API.
// Uses OpenRouter (Gemini image model) or OpenAI (DALL-E) via per-agent ImageGenConfig.
type CreateImageTool struct {
	registry *providers.Registry
}

func NewCreateImageTool(registry *providers.Registry) *CreateImageTool {
	return &CreateImageTool{registry: registry}
}

func (t *CreateImageTool) Name() string { return "create_image" }

func (t *CreateImageTool) Description() string {
	return "Generate an image from a text description using an image generation model. Returns a MEDIA: path to the generated image file."
}

func (t *CreateImageTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"prompt": map[string]interface{}{
				"type":        "string",
				"description": "Text description of the image to generate.",
			},
			"aspect_ratio": map[string]interface{}{
				"type":        "string",
				"description": "Aspect ratio: '1:1' (default), '3:4', '4:3', '9:16', '16:9'.",
			},
		},
		"required": []string{"prompt"},
	}
}

func (t *CreateImageTool) Execute(ctx context.Context, args map[string]interface{}) *Result {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		return ErrorResult("prompt is required")
	}
	aspectRatio, _ := args["aspect_ratio"].(string)
	if aspectRatio == "" {
		aspectRatio = "1:1"
	}

	// Resolve provider from per-agent config or defaults
	providerName, model := t.resolveConfig(ctx)

	p, err := t.registry.Get(providerName)
	if err != nil {
		return ErrorResult(fmt.Sprintf("image generation provider %q not available", providerName))
	}

	cp, ok := p.(credentialProvider)
	if !ok {
		return ErrorResult(fmt.Sprintf("provider %q does not expose API credentials for image generation", providerName))
	}

	slog.Info("create_image: calling image generation API",
		"provider", providerName, "model", model, "aspect_ratio", aspectRatio)

	// OpenRouter supports modalities on /chat/completions; Gemini, OpenAI and others use /images/generations
	var imageBytes []byte
	var usage *providers.Usage
	if providerName == "openrouter" {
		var genErr error
		imageBytes, usage, genErr = t.callImageGenAPI(ctx, cp.APIKey(), cp.APIBase(), model, prompt, aspectRatio)
		err = genErr
	} else {
		var genErr error
		imageBytes, usage, genErr = t.callStandardImageGenAPI(ctx, cp.APIKey(), cp.APIBase(), model, prompt)
		err = genErr
	}
	if err != nil {
		return ErrorResult(fmt.Sprintf("image generation failed: %v", err))
	}

	// Save to temp file
	imagePath := filepath.Join(os.TempDir(), fmt.Sprintf("goclaw_gen_%d.png", time.Now().UnixNano()))
	if err := os.WriteFile(imagePath, imageBytes, 0644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to save generated image: %v", err))
	}

	result := &Result{ForLLM: fmt.Sprintf("MEDIA:%s", imagePath)}
	result.Provider = providerName
	result.Model = model
	if usage != nil {
		result.Usage = usage
	}
	return result
}

// resolveConfig returns the provider name and model to use for image generation.
func (t *CreateImageTool) resolveConfig(ctx context.Context) (providerName, model string) {
	// 1. Check per-agent ImageGenConfig from context (highest priority)
	if cfg := ImageGenConfigFromCtx(ctx); cfg != nil {
		if cfg.Provider != "" {
			providerName = cfg.Provider
		}
		if cfg.Model != "" {
			model = cfg.Model
		}
	}

	// 2. Check global builtin_tools.settings (DB defaults)
	if providerName == "" || model == "" {
		if settings := BuiltinToolSettingsFromCtx(ctx); settings != nil {
			if raw, ok := settings["create_image"]; ok && len(raw) > 0 {
				var cfg struct {
					Provider string `json:"provider"`
					Model    string `json:"model"`
				}
				if json.Unmarshal(raw, &cfg) == nil && cfg.Provider != "" {
					// DB settings are a provider+model pair — only use if provider is available
					if _, err := t.registry.Get(cfg.Provider); err == nil {
						if providerName == "" {
							providerName = cfg.Provider
						}
						if model == "" && cfg.Model != "" {
							model = cfg.Model
						}
					}
				}
			}
		}
	}

	// 3. If provider not set, find first available from priority list
	if providerName == "" {
		for _, name := range imageGenProviderPriority {
			if _, err := t.registry.Get(name); err == nil {
				providerName = name
				break
			}
		}
	}
	if providerName == "" {
		providerName = "openrouter" // fallback even if unavailable (error handled later)
	}

	// 4. If model not set, use default for this provider
	if model == "" {
		if m, ok := imageGenModelDefaults[providerName]; ok {
			model = m
		}
	}

	return providerName, model
}

// callImageGenAPI calls the OpenAI-compatible image generation endpoint.
// Works with OpenRouter (modalities: ["image","text"]) and OpenAI (/images/generations).
func (t *CreateImageTool) callImageGenAPI(ctx context.Context, apiKey, apiBase, model, prompt, aspectRatio string) ([]byte, *providers.Usage, error) {
	// OpenRouter / OpenAI-compat: use chat completions with modalities
	body := map[string]interface{}{
		"model": model,
		"messages": []map[string]interface{}{
			{"role": "user", "content": prompt},
		},
		"modalities": []string{"image", "text"},
	}
	if aspectRatio != "" && aspectRatio != "1:1" {
		body["image_config"] = map[string]interface{}{
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

	client := &http.Client{Timeout: 120 * time.Second}
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

// callStandardImageGenAPI uses the /images/generations endpoint (Gemini, OpenAI, and compatible providers).
// This is the standard OpenAI-compatible image generation endpoint that returns b64_json data.
func (t *CreateImageTool) callStandardImageGenAPI(ctx context.Context, apiKey, apiBase, model, prompt string) ([]byte, *providers.Usage, error) {
	body := map[string]interface{}{
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

	client := &http.Client{Timeout: 120 * time.Second}
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

	// Parse OpenAI-compat images/generations response: {data: [{b64_json: "..."}]}
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

// parseImageResponse extracts base64 image data from the OpenAI-compat chat response.
// Looks for images in choices[0].message.content (multipart) or choices[0].message.images.
func (t *CreateImageTool) parseImageResponse(respBody []byte) ([]byte, *providers.Usage, error) {
	var resp struct {
		Choices []struct {
			Message struct {
				Content interface{} `json:"content"`
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
	if parts, ok := msg.Content.([]interface{}); ok {
		for _, part := range parts {
			if m, ok := part.(map[string]interface{}); ok {
				if m["type"] == "image_url" {
					if imgURL, ok := m["image_url"].(map[string]interface{}); ok {
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
	// Format: data:image/png;base64,iVBORw0KGgo...
	idx := strings.Index(dataURL, ";base64,")
	if idx < 0 {
		return nil, fmt.Errorf("not a base64 data URL")
	}
	b64 := dataURL[idx+8:]
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
