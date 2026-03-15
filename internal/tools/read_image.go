package tools

import (
	"context"
	"fmt"
	"log/slog"

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
	return "Analyze images sent by the user in the current message. Only use when you see <media:image> tags in the conversation — do NOT use for workspace files or generated images."
}

func (t *ReadImageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "What you want to know about the image(s). E.g. 'Describe this image in detail' or 'What text is in this image?'",
			},
		},
		"required": []string{"prompt"},
	}
}

func (t *ReadImageTool) Execute(ctx context.Context, args map[string]any) *Result {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		prompt = "Describe this image in detail."
	}

	images := MediaImagesFromCtx(ctx)
	if len(images) == 0 {
		return ErrorResult("No images available in this conversation. The user may not have sent an image.")
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

	chainResult, err := ExecuteWithChain(ctx, chain, t.registry, t.callProvider)
	if err != nil {
		return ErrorResult(fmt.Sprintf("image analysis failed: %v", err))
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
	p, err := t.registry.Get(providerName)
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
