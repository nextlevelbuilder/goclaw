package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	mediapkg "github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// videoGenProviderPriority is the default order for video generation providers.
var videoGenProviderPriority = []string{"gemini", "minimax", "openrouter", "byteplus"}

// videoGenModelDefaults maps provider names to default video generation models.
var videoGenModelDefaults = map[string]string{
	"gemini":     "veo-3.1-lite-generate-preview",
	"minimax":    "MiniMax-Hailuo-2.3",
	"openrouter": "google/veo-3.1-lite-generate-preview",
	"byteplus":   "seedance-1-5-pro",
}

// maxImageToVideoBytes is the maximum image file size for image-to-video (20 MB).
const maxImageToVideoBytes = 20 * 1024 * 1024

// CreateVideoTool generates videos using a video generation API.
// Uses Gemini Veo via predictLongRunning API (async with polling).
type CreateVideoTool struct {
	registry  *providers.Registry
	vaultIntc *VaultInterceptor
}

func (t *CreateVideoTool) SetVaultInterceptor(v *VaultInterceptor) { t.vaultIntc = v }

func NewCreateVideoTool(registry *providers.Registry) *CreateVideoTool {
	return &CreateVideoTool{registry: registry}
}

func (t *CreateVideoTool) Name() string { return "create_video" }

func (t *CreateVideoTool) Description() string {
	return "Generate a video from a text description or image using a video generation model. Returns a MEDIA: path to the generated video file."
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
			"image_path": map[string]any{
				"type":        "string",
				"description": "Path to a workspace image to use as starting frame for video generation. Omit for text-to-video. Not all providers support this.",
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

	// Parse optional image_path for image-to-video generation.
	var imageBase64, imageMime string
	if imagePath, _ := args["image_path"].(string); imagePath != "" {
		workspace := ToolWorkspaceFromCtx(ctx)
		resolved, err := resolvePath(imagePath, workspace, workspace != "")
		if err != nil {
			return ErrorResult(fmt.Sprintf("invalid image_path: %v", err))
		}
		fi, err := os.Stat(resolved)
		if err != nil {
			return ErrorResult(fmt.Sprintf("image file not found: %v", err))
		}
		if fi.IsDir() {
			return ErrorResult("image_path is a directory, not a file")
		}
		if fi.Size() == 0 {
			return ErrorResult("image file is empty")
		}
		if fi.Size() > maxImageToVideoBytes {
			return ErrorResult(fmt.Sprintf("image file too large (%d bytes); max %d bytes", fi.Size(), maxImageToVideoBytes))
		}
		mime := mediapkg.DetectMIMEType(resolved)
		switch mime {
		case "image/png", "image/jpeg", "image/webp", "image/gif":
			// supported
		default:
			return ErrorResult(fmt.Sprintf("unsupported image type %q; use PNG, JPEG, WebP, or GIF", mime))
		}
		raw, err := os.ReadFile(resolved)
		if err != nil {
			return ErrorResult(fmt.Sprintf("failed to read image: %v", err))
		}
		imageBase64 = base64.StdEncoding.EncodeToString(raw)
		imageMime = mime
		// Veo 3.1 API constraint: image-to-video requires duration=8.
		duration = 8
		slog.Info("create_video: image-to-video mode", "image", resolved, "mime", mime, "size", fi.Size())
	}

	chain := ResolveMediaProviderChain(ctx, "create_video", "", "",
		videoGenProviderPriority, videoGenModelDefaults, t.registry)

	// Inject universal params into each chain entry's params.
	// Provider-specific params (resolution, generate_audio, person_generation) come
	// from chain config and are already in chain[i].Params.
	for i := range chain {
		if chain[i].Params == nil {
			chain[i].Params = make(map[string]any)
		}
		chain[i].Params["prompt"] = prompt
		chain[i].Params["duration"] = duration
		chain[i].Params["aspect_ratio"] = aspectRatio
		// Inject image data — each callProvider decides whether to use it.
		if imageBase64 != "" {
			chain[i].Params["image_base64"] = imageBase64
			chain[i].Params["image_mime"] = imageMime
		}
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

	result := &Result{ForLLM: fmt.Sprintf("MEDIA:%s\nUse the EXACT filename when referencing: %s", videoPath, filepath.Base(videoPath))}
	result.Media = []bus.MediaFile{{Path: videoPath, MimeType: "video/mp4"}}
	result.Deliverable = fmt.Sprintf("[Generated video: %s]\nPrompt: %s", filepath.Base(videoPath), prompt)
	if t.vaultIntc != nil {
		go t.vaultIntc.AfterWriteMedia(context.WithoutCancel(ctx), videoPath, prompt, "video/mp4")
	}
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
	case "byteplus":
		return callBytePlusVideoGen(ctx, cp.APIKey(), cp.APIBase(), model, prompt, duration, aspectRatio, params)
	default:
		return t.callChatVideoGen(ctx, cp.APIKey(), cp.APIBase(), model, prompt, duration, aspectRatio, params)
	}
}

