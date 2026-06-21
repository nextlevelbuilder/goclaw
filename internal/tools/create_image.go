package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/security"
)

// Reference-image downloads are gateway-side fetches of caller-supplied URLs, so
// they must go through the SSRF guard with a bounded read.
const refImageDownloadTimeout = 30 * time.Second

// refImageMaxBytes caps a single reference-image download. Declared as a var so
// tests can shrink it to exercise the overflow path cheaply.
var refImageMaxBytes int64 = 20 * 1024 * 1024 // 20 MB

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

type referenceImage struct {
	Data        []byte
	Base64      string
	URL         string
	MimeType    string
	Strength    float64
	Description string
}

func (t *CreateImageTool) resolveReferenceImages(ctx context.Context, args map[string]any) ([]*referenceImage, error) {
	var results []*referenceImage

	seenPaths := make(map[string]bool)
	seenURLs := make(map[string]bool)
	seenIDs := make(map[string]bool)

	resolveSingle := func(path, url, id string, strength float64, description string) (*referenceImage, error) {
		if path != "" {
			if seenPaths[path] {
				return nil, nil
			}
			seenPaths[path] = true

			ext := strings.ToLower(filepath.Ext(path))
			mimeTypes := map[string]string{
				".jpg": "image/jpeg", ".jpeg": "image/jpeg",
				".png": "image/png", ".gif": "image/gif",
				".webp": "image/webp", ".bmp": "image/bmp",
			}
			mime, ok := mimeTypes[ext]
			if !ok {
				mime = "image/png"
			}
			workspace := ToolWorkspaceFromCtx(ctx)
			resolved, err := resolvePathWithAllowed(path, workspace, effectiveRestrict(ctx, true), allowedWithTeamWorkspace(ctx, nil))
			if err != nil {
				return nil, fmt.Errorf("invalid reference image path: %w", err)
			}
			if err := checkDeniedPath(resolved, workspace, nil); err != nil {
				return nil, err
			}
			data, err := os.ReadFile(resolved)
			if err != nil {
				return nil, fmt.Errorf("failed to read reference image file: %w", err)
			}
			return &referenceImage{
				Data:        data,
				Base64:      base64.StdEncoding.EncodeToString(data),
				MimeType:    mime,
				Strength:    strength,
				Description: description,
			}, nil
		}

		if url != "" {
			// Trust boundary: a reference URL is either forwarded to the image
			// provider (provider fetches it) or fetched gateway-side via
			// downloadImageBytes (SSRF-guarded there). Either way it must be a
			// plain HTTP(S) URL — reject file://, gopher://, data:, etc. up front.
			if !isHTTPURL(url) {
				return nil, fmt.Errorf("reference image url must be http(s): %q", url)
			}
			if seenURLs[url] {
				return nil, nil
			}
			seenURLs[url] = true

			return &referenceImage{
				URL:         url,
				Strength:    strength,
				Description: description,
			}, nil
		}

		if id != "" {
			if seenIDs[id] {
				return nil, nil
			}
			seenIDs[id] = true

			images := MediaImagesFromCtx(ctx)
			if len(images) == 0 {
				return nil, fmt.Errorf("no images available in conversation context")
			}
			var img providers.ImageContent
			if id == "latest" {
				img = images[len(images)-1]
			} else {
				var idx int
				if _, err := fmt.Sscanf(id, "%d", &idx); err == nil && idx >= 0 && idx < len(images) {
					img = images[idx]
				} else {
					img = images[len(images)-1]
				}
			}
			dataBytes, _ := base64.StdEncoding.DecodeString(img.Data)
			return &referenceImage{
				Data:        dataBytes,
				Base64:      img.Data,
				MimeType:    img.MimeType,
				URL:         img.URL,
				Strength:    strength,
				Description: description,
			}, nil
		}

		return nil, nil
	}

	// 1. Resolve complex ref_images array
	if refImagesRaw, ok := args["ref_images"]; ok {
		if refImagesList, ok := refImagesRaw.([]any); ok {
			for _, itemRaw := range refImagesList {
				if item, ok := itemRaw.(map[string]any); ok {
					path, _ := item["path"].(string)
					url, _ := item["url"].(string)
					id, _ := item["id"].(string)
					description, _ := item["description"].(string)
					strength := 0.6
					if strRaw, has := item["strength"]; has {
						if s, ok := strRaw.(float64); ok {
							strength = s
						}
					}
					refImg, err := resolveSingle(path, url, id, strength, description)
					if err != nil {
						return nil, err
					}
					if refImg != nil {
						results = append(results, refImg)
					}
				}
			}
		}
	}

	return results, nil
}

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
			"ref_images": map[string]any{
				"type":        "array",
				"description": "Optional array of reference images with custom properties.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":        map[string]any{"type": "string", "description": "Workspace file path to a reference image."},
						"url":         map[string]any{"type": "string", "description": "HTTP/HTTPS URL of a reference image."},
						"id":          map[string]any{"type": "string", "description": "Media ID of a reference image from the chat."},
						"strength":    map[string]any{"type": "number", "description": "Reference strength (0.0 to 1.0) specific to this image."},
						"description": map[string]any{"type": "string", "description": "Description of the role or content of this reference image (e.g. 'Lâm', 'Quân')."},
					},
				},
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

	refImgs, err := t.resolveReferenceImages(ctx, args)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to resolve reference images: %v", err))
	}

	chain := ResolveMediaProviderChain(ctx, "create_image", "", "",
		imageGenProviderPriority, imageGenModelDefaults, t.registry)

	// Inject prompt, aspect_ratio, and ref_images into each chain entry's params
	for i := range chain {
		if chain[i].Params == nil {
			chain[i].Params = make(map[string]any)
		}
		chain[i].Params["prompt"] = prompt
		chain[i].Params["aspect_ratio"] = aspectRatio
		if len(refImgs) > 0 {
			chain[i].Params["ref_images"] = refImgs
		}
	}

	chainResult, err := ExecuteWithChain(ctx, chain, t.registry, t.callProvider)
	if err != nil {
		return ErrorResult(fmt.Sprintf("image generation failed: %v", err))
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
	var refImgs []*referenceImage
	if rawImgs, ok := params["ref_images"]; ok {
		refImgs, _ = rawImgs.([]*referenceImage)
	}

	// Native path: provider implements the image_generation tool natively (e.g. Codex/OAuth).
	// The raw provider object is injected into params["_native_provider"] by ExecuteWithChain.
	// Must check before the cp==nil guard — these providers intentionally have no APIKey/APIBase.
	if rawProvider, ok := params["_native_provider"]; ok {
		if np, ok := rawProvider.(providers.NativeImageProvider); ok {
			prompt := GetParamString(params, "prompt", "")
			aspectRatio := GetParamString(params, "aspect_ratio", "1:1")
			imageModel := GetParamString(params, "image_model", "")

			req := providers.NativeImageRequest{
				Model:        model,
				ImageModel:   imageModel,
				Prompt:       prompt,
				AspectRatio:  aspectRatio,
				OutputFormat: "png",
			}
			if len(refImgs) > 0 {
				req.RefImages = make([]providers.RefImage, len(refImgs))
				for idx, r := range refImgs {
					req.RefImages[idx] = providers.RefImage{
						Data:     r.Data,
						Base64:   r.Base64,
						MimeType: r.MimeType,
						URL:      r.URL,
						Strength: r.Strength,
					}
				}
			}

			result, err := np.GenerateImage(ctx, req)
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

	ptype := GetParamString(params, "_provider_type", providerTypeFromName(providerName))

	// OpenAI image-to-image (Edits)
	if len(refImgs) > 0 && (ptype == "openai" || providerName == "openai" || ptype == "openai_compat") {
		isEditModel := model == "gpt-image-2" ||
			model == "gpt-image-1.5" ||
			model == "gpt-image-1" ||
			model == "gpt-image-1-mini" ||
			model == "chatgpt-image-latest" ||
			model == "dall-e-2" ||
			ptype == "openai_compat"
		if isEditModel {
			if model == "dall-e-2" {
				if len(refImgs) > 1 {
					slog.Warn("openai dall-e-2 only supports 1 reference image, using the first one", "count", len(refImgs))
				}
				return t.callOpenAIImageEditMultipart(ctx, cp.APIKey(), cp.APIBase(), model, prompt, refImgs[:1])
			}
			return t.callOpenAIImageEditJSON(ctx, cp.APIKey(), cp.APIBase(), model, prompt, refImgs)
		}
		slog.Warn("create_image: model does not support reference images, ignoring reference", "model", model, "provider", providerName)
	}

	switch ptype {
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
	var messages []map[string]any
	if rawImgs, ok := params["ref_images"]; ok {
		if refImgs, ok := rawImgs.([]*referenceImage); ok && len(refImgs) > 0 {
			contentParts := []map[string]any{
				{"type": "text", "text": prompt},
			}
			for _, refImg := range refImgs {
				// Trust boundary: this JSON path forwards the reference URL to the
				// image provider downstream (the provider fetches it), so the gateway
				// does NOT dial it here — only HTTP(S) URLs reach this point. The
				// gateway-side fetch path (OpenAI multipart) goes through
				// downloadImageBytes, which is SSRF-guarded. Keep these distinct: if a
				// provider URL ever becomes a gateway-side fetch, route it through
				// downloadImageBytes.
				var refURL string
				if refImg.URL != "" {
					refURL = refImg.URL
				} else {
					refMime := refImg.MimeType
					if refMime == "" {
						refMime = "image/png"
					}
					refURL = fmt.Sprintf("data:%s;base64,%s", refMime, refImg.Base64)
				}
				contentParts = append(contentParts, map[string]any{
					"type": "image_url",
					"image_url": map[string]any{
						"url": refURL,
					},
				})
			}
			messages = []map[string]any{
				{
					"role":    "user",
					"content": contentParts,
				},
			}
		}
	}
	if len(messages) == 0 {
		messages = []map[string]any{
			{"role": "user", "content": prompt},
		}
	}

	body := map[string]any{
		"model":      model,
		"messages":   messages,
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

// isHTTPURL reports whether s is a plain http(s) URL. Reference URLs must be
// HTTP(S) only — this blocks file://, data:, gopher://, etc. before a URL is
// either forwarded to a provider or fetched gateway-side.
func isHTTPURL(s string) bool {
	l := strings.ToLower(s)
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
}

// downloadImageBytes downloads a caller-supplied reference image URL server-side
// and returns its raw bytes and content type. The URL is attacker-controlled
// (agent/user-provided ref_images[].url), so it is validated against the SSRF
// guard and the resolved IP is pinned for the dial; the shared SafeClient also
// refuses redirects. The response body is read with a hard size cap.
func (t *CreateImageTool) downloadImageBytes(ctx context.Context, rawURL string) ([]byte, string, error) {
	// SSRF guard: rejects loopback/private/link-local (incl. cloud metadata
	// 169.254.169.254) and returns the resolved IP to pin for the dial.
	_, pinnedIP, err := security.Validate(rawURL)
	if err != nil {
		return nil, "", fmt.Errorf("invalid reference image URL: %w", err)
	}
	reqCtx := security.WithPinnedIP(ctx, pinnedIP)
	req, err := http.NewRequestWithContext(reqCtx, "GET", rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	// SafeClient dials only the pinned IP and never follows redirects (a 3xx is
	// returned as-is and rejected by the status check below).
	client := security.NewSafeClient(refImageDownloadTimeout)
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP error %d", resp.StatusCode)
	}
	// Bounded read: cap the download to avoid memory exhaustion from a hostile
	// or oversized response. Read one extra byte to detect overflow.
	data, err := io.ReadAll(io.LimitReader(resp.Body, refImageMaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > refImageMaxBytes {
		return nil, "", fmt.Errorf("reference image exceeds maximum size of %d bytes", refImageMaxBytes)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// callOpenAIImageEditMultipart calls the OpenAI /v1/images/edits API using multipart/form-data.
func (t *CreateImageTool) callOpenAIImageEditMultipart(ctx context.Context, apiKey, apiBase, model, prompt string, refImgs []*referenceImage) ([]byte, *providers.Usage, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	hasImages := false

	for idx, refImg := range refImgs {
		var imageData []byte
		var err error

		if len(refImg.Data) > 0 {
			imageData = refImg.Data
		} else if refImg.URL != "" {
			slog.Info("openai multipart: downloading reference image from URL", "url", refImg.URL)
			imageData, _, err = t.downloadImageBytes(ctx, refImg.URL)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to download reference image %d: %w", idx, err)
			}
		}

		if len(imageData) == 0 {
			continue
		}
		hasImages = true

		fieldName := "image"
		if len(refImgs) > 1 {
			fieldName = "images"
		}

		filename := fmt.Sprintf("image_%d.png", idx)
		if refImg.MimeType != "" {
			parts := strings.Split(refImg.MimeType, "/")
			if len(parts) == 2 {
				ext := parts[1]
				if ext == "jpeg" {
					ext = "jpg"
				}
				filename = fmt.Sprintf("image_%d.%s", idx, ext)
			}
		}

		part, err := writer.CreateFormFile(fieldName, filename)
		if err != nil {
			return nil, nil, fmt.Errorf("create form file image %d: %w", idx, err)
		}
		if _, err := part.Write(imageData); err != nil {
			return nil, nil, fmt.Errorf("write image %d to form: %w", idx, err)
		}
	}

	if !hasImages {
		return nil, nil, fmt.Errorf("no reference image data available")
	}

	// Build reference image descriptions if available
	var descParts []string
	for idx, refImg := range refImgs {
		if refImg.Description != "" {
			descParts = append(descParts, fmt.Sprintf("- image_%d.png: %s", idx+1, refImg.Description))
		}
	}
	finalPrompt := prompt
	if len(descParts) > 0 {
		finalPrompt = fmt.Sprintf("%s\n\n[Reference Image Roles]\n%s", prompt, strings.Join(descParts, "\n"))
		slog.Info("openai multipart: appended image descriptions to prompt", "desc_count", len(descParts))
	}

	// Add other fields
	if err := writer.WriteField("prompt", finalPrompt); err != nil {
		return nil, nil, fmt.Errorf("write field prompt: %w", err)
	}
	if err := writer.WriteField("model", model); err != nil {
		return nil, nil, fmt.Errorf("write field model: %w", err)
	}
	if err := writer.WriteField("response_format", "b64_json"); err != nil {
		return nil, nil, fmt.Errorf("write field response_format: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, nil, fmt.Errorf("close multipart writer: %w", err)
	}

	url := strings.TrimRight(apiBase, "/") + "/images/edits"
	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{}
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

// callOpenAIImageEditJSON calls the OpenAI /v1/images/edits API using a JSON payload.
func (t *CreateImageTool) callOpenAIImageEditJSON(ctx context.Context, apiKey, apiBase, model, prompt string, refImgs []*referenceImage) ([]byte, *providers.Usage, error) {
	type ImageRef struct {
		ImageURL string `json:"image_url,omitempty"`
		FileID   string `json:"file_id,omitempty"`
	}

	var images []ImageRef

	for _, refImg := range refImgs {
		var refURL string
		if refImg.URL != "" {
			refURL = refImg.URL
		} else {
			// Convert local image data to Base64 Data URL
			var imageData []byte
			if len(refImg.Data) > 0 {
				imageData = refImg.Data
			} else {
				// No data available
				continue
			}

			mime := refImg.MimeType
			if mime == "" {
				mime = "image/png"
			}
			refURL = fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(imageData))
		}

		images = append(images, ImageRef{ImageURL: refURL})
	}

	if len(images) == 0 {
		return nil, nil, fmt.Errorf("no reference images available")
	}

	// Build reference image descriptions if available
	var descParts []string
	for idx, refImg := range refImgs {
		if refImg.Description != "" {
			descParts = append(descParts, fmt.Sprintf("- image_%d.png: %s", idx+1, refImg.Description))
		}
	}
	finalPrompt := prompt
	if len(descParts) > 0 {
		finalPrompt = fmt.Sprintf("%s\n\n[Reference Image Roles]\n%s", prompt, strings.Join(descParts, "\n"))
		slog.Info("openai json edits: appended image descriptions to prompt", "desc_count", len(descParts))
	}

	body := map[string]any{
		"model":           model,
		"prompt":          finalPrompt,
		"images":          images,
		"response_format": "b64_json",
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(apiBase, "/") + "/images/edits"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{}
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

	parts := []map[string]any{
		{"text": prompt},
	}
	if rawImgs, ok := params["ref_images"]; ok {
		if refImgs, ok := rawImgs.([]*referenceImage); ok {
			for _, refImg := range refImgs {
				var dataB64 string
				var mime string
				if refImg.Base64 != "" {
					dataB64 = refImg.Base64
					mime = refImg.MimeType
				} else if refImg.URL != "" {
					dataBytes, contentType, err := t.downloadImageBytes(ctx, refImg.URL)
					if err == nil {
						dataB64 = base64.StdEncoding.EncodeToString(dataBytes)
						mime = contentType
					} else {
						slog.Warn("gemini native image gen: failed to download reference image from URL", "url", refImg.URL, "error", err)
					}
				}
				if dataB64 != "" {
					if mime == "" {
						mime = "image/png"
					}
					parts = append(parts, map[string]any{
						"inlineData": map[string]any{
							"mimeType": mime,
							"data":     dataB64,
						},
					})
				}
			}
		}
	}

	body := map[string]any{
		"contents": []map[string]any{
			{"parts": parts},
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
