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

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// credentialProvider is a narrow interface for providers that expose API credentials.
type credentialProvider interface {
	APIKey() string
	APIBase() string
}

// imageGenProviderPriority is the default order for image generation providers.
var imageGenProviderPriority = []string{"openrouter", "gemini", "openai", "minimax", "dashscope", "byteplus"}

// imageGenModelDefaults maps provider names to default image generation models.
var imageGenModelDefaults = map[string]string{
	"openrouter": "google/gemini-2.5-flash-image",
	"openai":     "gpt-image-1.5",
	"gemini":     "gemini-2.5-flash-image",
	"minimax":    "image-01",
	"dashscope":  "wan2.6-image",
	"byteplus":   "seedream-5-0-260128",
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
	return "Generate an image from a text description using an image generation model. Optionally pass reference images via input_images to do image-to-image edits (face swap, restyle, composition). Returns a MEDIA: path to the generated image file."
}

func (t *CreateImageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "Text description of the image to generate (or, when input_images is set, an instruction describing the edit).",
			},
			"aspect_ratio": map[string]any{
				"type":        "string",
				"description": "Aspect ratio: '1:1' (default), '3:4', '4:3', '9:16', '16:9'.",
			},
			"filename_hint": map[string]any{
				"type":        "string",
				"description": "Short descriptive filename (no extension). Example: 'sunset-beach', 'company-logo'.",
			},
			"input_images": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional. Absolute paths to reference images (e.g. files in your workspace under .uploads/ or generated/) used as visual context for the model. Enables face swap, restyle, composition. Up to 4 images. Only honored when the resolved provider supports image input (gpt-image-2 via openai-codex).",
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

	// Optional reference images for image-to-image editing. Read bytes here
	// (single point of failure) and pass decoded structs through the chain.
	inputImages, err := loadInputImages(ctx, args["input_images"])
	if err != nil {
		return ErrorResult(err.Error())
	}

	chain := ResolveMediaProviderChain(ctx, "create_image", "", "",
		imageGenProviderPriority, imageGenModelDefaults, t.registry)

	// Inject prompt and aspect_ratio into each chain entry's params
	for i := range chain {
		if chain[i].Params == nil {
			chain[i].Params = make(map[string]any)
		}
		chain[i].Params["prompt"] = prompt
		chain[i].Params["aspect_ratio"] = aspectRatio
		if len(inputImages) > 0 {
			chain[i].Params["input_images"] = inputImages
		}
	}

	chainResult, err := ExecuteWithChain(ctx, chain, t.registry, t.callProvider)
	if err != nil {
		return ErrorResult(fmt.Sprintf("image generation failed: %v", err))
	}

	// Silent-failure guard: when input_images are passed, gpt-image-2 sometimes
	// returns one of the inputs unchanged instead of applying the requested edit.
	// Surface that as a tool error so the agent retries or asks for clarification
	// instead of replying "Đây anh." with the customer's own image.
	if err := verifyOutputDiffersFromInputs(chainResult.Data, inputImages, imageVerifyDefaultThreshold); err != nil {
		slog.Warn("create_image: output verification failed",
			"provider", chainResult.Provider, "model", chainResult.Model, "error", err)
		return ErrorResult(err.Error())
	}

	// Embed prompt into PNG tEXt metadata before writing to disk.
	// If embedding fails (malformed bytes, non-PNG) the original data is used unchanged.
	imageData := embedPromptIntoPNG(chainResult.Data, prompt)

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
	if err := os.WriteFile(imagePath, imageData, 0644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to save generated image: %v", err))
	}

	// Verify file was persisted (diagnostic for disappearing files).
	if fi, err := os.Stat(imagePath); err != nil {
		slog.Warn("create_image: file missing immediately after write", "path", imagePath, "error", err)
		return ErrorResult(fmt.Sprintf("generated image file missing after write: %v", err))
	} else {
		slog.Info("create_image: file saved", "path", imagePath, "size", fi.Size(), "data_len", len(imageData))
	}

	result := &Result{ForLLM: fmt.Sprintf("MEDIA:%s\nUse the EXACT filename when referencing: %s", imagePath, filepath.Base(imagePath))}
	result.Media = []bus.MediaFile{{Path: imagePath, MimeType: "image/png", Filename: filepath.Base(imagePath)}}
	result.MediaPrompts = map[int]string{0: prompt}
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

// loadInputImages decodes the optional input_images argument into raw image
// bytes paired with detected MIME type. The argument may be a []any of file
// paths (LLM-provided strings). Each file must live inside the workspace
// (enforced by EnsureWorkspacePath) and decode as png/jpeg/webp/gif. Returns
// nil with no error when the argument is absent or empty.
func loadInputImages(ctx context.Context, raw any) ([]providers.NativeImageInput, error) {
	if raw == nil {
		return nil, nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("input_images must be an array of file paths")
	}
	if len(arr) == 0 {
		return nil, nil
	}
	const maxInputs = 4
	if len(arr) > maxInputs {
		return nil, fmt.Errorf("input_images supports at most %d images, got %d", maxInputs, len(arr))
	}
	workspace := ToolWorkspaceFromCtx(ctx)
	out := make([]providers.NativeImageInput, 0, len(arr))
	for i, item := range arr {
		path, _ := item.(string)
		if path == "" {
			return nil, fmt.Errorf("input_images[%d] is not a string path", i)
		}
		resolved, err := resolvePath(path, workspace, effectiveRestrict(ctx, false))
		if err != nil {
			return nil, fmt.Errorf("input_images[%d] (%s): %w", i, path, err)
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return nil, fmt.Errorf("input_images[%d] (%s): read failed: %w", i, path, err)
		}
		mime := http.DetectContentType(data)
		switch mime {
		case "image/png", "image/jpeg", "image/webp", "image/gif":
			// ok
		default:
			return nil, fmt.Errorf("input_images[%d] (%s): unsupported MIME %q (need png/jpeg/webp/gif)", i, path, mime)
		}
		out = append(out, providers.NativeImageInput{MimeType: mime, Data: data})
	}
	return out, nil
}

// embedPromptIntoPNG wraps agent.EmbedPNGPrompt for the tools package.
// Logs a warning on error but always returns usable bytes.
func embedPromptIntoPNG(data []byte, prompt string) []byte {
	if prompt == "" {
		return data
	}
	// Import cycle guard: tools → agent is not allowed. Use the local pngEmbed function.
	out, err := pngEmbedPrompt(data, prompt)
	if err != nil {
		slog.Warn("create_image: failed to embed prompt into PNG metadata", "error", err)
		return data
	}
	return out
}

// callProvider dispatches to the correct image generation implementation based on provider type.
// If the resolved provider implements NativeImageProvider (e.g. CodexProvider via OAuth),
// the native path is used and cp may be nil. The credentialProvider path is only reached
// for API-key-backed providers.
func (t *CreateImageTool) callProvider(ctx context.Context, cp credentialProvider, providerName, model string, params map[string]any) ([]byte, *providers.Usage, error) {
	// Native path: provider implements the image_generation tool natively (e.g. Codex/OAuth).
	// The raw provider object is injected into params["_native_provider"] by ExecuteWithChain.
	// Must check before the cp==nil guard — these providers intentionally have no APIKey/APIBase.
	if rawProvider, ok := params["_native_provider"]; ok {
		if np, ok := rawProvider.(providers.NativeImageProvider); ok {
			prompt := GetParamString(params, "prompt", "")
			aspectRatio := GetParamString(params, "aspect_ratio", "1:1")
			imageModel := GetParamString(params, "image_model", "")
			inputImages, _ := params["input_images"].([]providers.NativeImageInput)
			result, err := np.GenerateImage(ctx, providers.NativeImageRequest{
				Model:        model,
				ImageModel:   imageModel,
				Prompt:       prompt,
				AspectRatio:  aspectRatio,
				OutputFormat: "png",
				InputImages:  inputImages,
			})
			if err != nil {
				return nil, nil, fmt.Errorf("native image generation: %w", err)
			}
			return result.Data, result.Usage, nil
		}
	}

	if cp == nil {
		return nil, nil, fmt.Errorf("provider %q does not expose API credentials required for image generation", providerName)
	}
	prompt := GetParamString(params, "prompt", "")
	aspectRatio := GetParamString(params, "aspect_ratio", "1:1")

	slog.Info("create_image: calling image generation API",
		"provider", providerName, "model", model, "aspect_ratio", aspectRatio)

	switch GetParamString(params, "_provider_type", providerTypeFromName(providerName)) {
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
