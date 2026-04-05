package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// --- Context helpers for media images ---

const ctxMediaImages toolContextKey = "tool_media_images"

// WithMediaImages stores base64-encoded images in context for read_image tool access.
func WithMediaImages(ctx context.Context, images []providers.ImageContent) context.Context {
	return context.WithValue(ctx, ctxMediaImages, images)
}

// MediaImagesFromCtx retrieves stored images from context.
func MediaImagesFromCtx(ctx context.Context) []providers.ImageContent {
	v, _ := ctx.Value(ctxMediaImages).([]providers.ImageContent)
	return v
}

// --- ReadImageTool ---

// visionProviderPriority is the order in which providers are tried for vision.
var visionProviderPriority = []string{"openrouter", "gemini", "anthropic", "dashscope"}

// visionModelDefaults maps provider names to preferred vision models.
var visionModelDefaults = map[string]string{
	"openrouter": "google/gemini-2.5-flash-image",
	"gemini":     "gemini-2.5-flash",
	"anthropic":  "",
	"dashscope":  "qwen3-vl",
}

// ReadImageTool uses a vision-capable provider to describe images attached to the current message.
type ReadImageTool struct {
	registry *providers.Registry
}

func NewReadImageTool(registry *providers.Registry) *ReadImageTool {
	return &ReadImageTool{registry: registry}
}

func (t *ReadImageTool) Name() string { return "read_image" }

func (t *ReadImageTool) Description() string {
	return "Analyze images using vision AI. Works with: (1) images sent by the user (<media:image> tags), (2) workspace/generated image files (pass a file path)."
}

func (t *ReadImageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "What you want to know about the image(s). E.g. 'Describe this image in detail' or 'What text is in this image?'",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Optional file path to an image in the workspace. Use this for generated images or attachments. If omitted, analyzes images from the conversation.",
			},
		},
		"required": []string{"prompt"},
	}
}

// maxImageFileBytes is the max size for loading workspace images (10MB).
const maxImageFileBytes = 10 * 1024 * 1024

func (t *ReadImageTool) Execute(ctx context.Context, args map[string]any) *Result {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		prompt = "Describe this image in detail."
	}

	// If path is provided, load image from workspace file
	images := MediaImagesFromCtx(ctx)
	if imgPath, _ := args["path"].(string); imgPath != "" {
		fileImages, err := t.loadImageFromPath(ctx, imgPath)
		if err != nil {
			return ErrorResult(err.Error())
		}
		images = fileImages
	}

	if len(images) == 0 {
		return ErrorResult("No images available. Either send an image in the chat or provide a file path with the 'path' parameter.")
	}

	chain := ResolveMediaProviderChain(ctx, "read_image", "", "",
		visionProviderPriority, visionModelDefaults, t.registry)

	// Inject prompt and images into each chain entry's params
	for i := range chain {
		if chain[i].Params == nil {
			chain[i].Params = make(map[string]any)
		}
		chain[i].Params["prompt"] = prompt
		chain[i].Params["images"] = images
	}

	if len(chain) == 0 {
		return ErrorResult("No vision provider configured. Ask the user to add a vision-capable provider (e.g. Gemini, Anthropic, OpenRouter) in the system settings.")
	}

	chainResult, err := ExecuteWithChain(ctx, chain, t.registry, t.callProvider)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Image analysis failed — all vision providers returned errors: %v. The user may need to check their provider API keys or configuration.", err))
	}

	result := NewResult(string(chainResult.Data))
	result.Usage = chainResult.Usage
	result.Provider = chainResult.Provider
	result.Model = chainResult.Model
	return result
}

// callProvider dispatches the vision call using provider.Chat().
func (t *ReadImageTool) callProvider(ctx context.Context, cp credentialProvider, providerName, model string, params map[string]any) ([]byte, *providers.Usage, error) {
	prompt := GetParamString(params, "prompt", "Describe this image in detail.")
	images, _ := params["images"].([]providers.ImageContent)

	// Get the full provider for Chat() access
	p, err := t.registry.Get(ctx, providerName)
	if err != nil {
		return nil, nil, fmt.Errorf("provider %q not available: %w", providerName, err)
	}

	slog.Info("read_image: calling vision provider", "provider", providerName, "model", model, "images", len(images))

	resp, err := p.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{
			{
				Role:    "user",
				Content: prompt,
				Images:  images,
			},
		},
		Model: model,
		Options: map[string]any{
			"max_tokens":  1024,
			"temperature": 0.3,
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("vision provider error: %w", err)
	}

	return []byte(resp.Content), resp.Usage, nil
}

// loadImageFromPath reads an image file from the workspace and returns it as ImageContent.
func (t *ReadImageTool) loadImageFromPath(ctx context.Context, path string) ([]providers.ImageContent, error) {
	// Infer MIME type from extension
	ext := strings.ToLower(filepath.Ext(path))
	mimeTypes := map[string]string{
		".jpg": "image/jpeg", ".jpeg": "image/jpeg",
		".png": "image/png", ".gif": "image/gif",
		".webp": "image/webp", ".bmp": "image/bmp",
	}
	mime, ok := mimeTypes[ext]
	if !ok {
		return nil, fmt.Errorf("unsupported image format: %s (supported: jpg, png, gif, webp, bmp)", ext)
	}

	// Resolve path within workspace (respect workspace restriction).
	workspace := ToolWorkspaceFromCtx(ctx)
	resolved, err := resolvePathWithAllowed(path, workspace, effectiveRestrict(ctx, true), allowedWithTeamWorkspace(ctx, nil))
	if err != nil {
		return nil, fmt.Errorf("invalid image path: %w", err)
	}
	if err := checkDeniedPath(resolved, workspace, nil); err != nil {
		return nil, err
	}

	// Pre-check file size before loading into memory.
	fi, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to stat image file: %w", err)
	}
	if fi.Size() > maxImageFileBytes {
		return nil, fmt.Errorf("image file too large (%d bytes, max %d)", fi.Size(), maxImageFileBytes)
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("failed to read image file: %w", err)
	}

	return []providers.ImageContent{{
		MimeType: mime,
		Data:     base64.StdEncoding.EncodeToString(data),
	}}, nil
}
