package providers

import (
	"context"
	"fmt"
	"strings"
)

// NativeImageProvider is implemented by OAuth-backed providers whose upstream
// exposes an image_generation native tool (ChatGPT Responses API style).
// create_image routes through this interface when the chain resolves to such
// a provider, bypassing the credentialProvider (APIKey/APIBase) path.
type NativeImageProvider interface {
	GenerateImage(ctx context.Context, req NativeImageRequest) (*NativeImageResult, error)
}

// DefaultImageModel is the image model used by the Responses API image_generation
// tool when the caller does not specify one. GPT Image 2.5 (2026-09-08) ships as
// two model IDs instead of one: "flare" tuned for speed, "sunburst" tuned for
// editing precision. Flare is the default because it beats gpt-image-2 on quality
// at roughly half the latency, which suits the one-shot create_image path.
const DefaultImageModel = "gpt-image-2.5-flare"

// allowedImageModels enumerates the image models the native ChatGPT Responses API
// image_generation tool will accept. Constraining to this whitelist prevents
// silent upstream rejections from arbitrary model names (e.g. "dall-e-3").
var allowedImageModels = map[string]bool{
	"gpt-image-2.5-flare":    true, // default - fastest at high quality
	"gpt-image-2.5-sunburst": true, // most capable at editing precision
	"gpt-image-2":            true, // previous generation
	"gpt-image-1.5":          true, // legacy fallback
}

// allowedImageModelList is the whitelist rendered for error messages, ordered
// newest first so the suggestion a caller sees leads with the recommended model.
var allowedImageModelList = []string{
	"gpt-image-2.5-flare (default)",
	"gpt-image-2.5-sunburst",
	"gpt-image-2",
	"gpt-image-1.5 (legacy)",
}

// ValidateImageModel returns the model to use, or an error if the caller
// supplied an unsupported value. Empty input returns DefaultImageModel.
func ValidateImageModel(model string) (string, error) {
	if model == "" {
		return DefaultImageModel, nil
	}
	if !allowedImageModels[model] {
		return "", fmt.Errorf("unsupported image model %q; allowed: %s", model, strings.Join(allowedImageModelList, ", "))
	}
	return model, nil
}

// NativeImageRequest describes a single image generation request.
type NativeImageRequest struct {
	// Model is the parent LLM model for the Responses API call (e.g. "gpt-5.5").
	// NOT the image model — see ImageModel below.
	// If empty, the provider uses its own default LLM model.
	Model string

	// ImageModel is the image-generation model attached to the image_generation
	// tool (e.g. "gpt-image-2.5-flare"). Must be a value accepted by ValidateImageModel;
	// empty falls back to DefaultImageModel.
	ImageModel string

	// Prompt is the text description of the image.
	Prompt string

	// AspectRatio is the desired aspect ratio, e.g. "16:9", "1:1", "9:16".
	// Converted to a concrete pixel size by the provider implementation.
	// Defaults to "1:1" if empty.
	AspectRatio string

	// OutputFormat is the desired image format: "png" (default), "jpg", "webp".
	OutputFormat string

	// RefImages contains the list of reference images.
	RefImages []RefImage
}

// RefImage represents a single reference image for image-to-image or styling tasks.
type RefImage struct {
	Data     []byte
	Base64   string
	MimeType string
	URL      string
	Strength float64
}

// NativeImageResult holds the result of a native image generation call.
type NativeImageResult struct {
	// MimeType is the detected MIME type of the generated image (e.g. "image/png").
	MimeType string

	// Data is the raw decoded image bytes (NOT base64).
	Data []byte

	// Usage is optional token usage if the provider reports it.
	Usage *Usage
}

// SizeFromAspect converts a common aspect ratio string to a pixel dimension
// string expected by image generation APIs (e.g. "1792x1024").
// Falls back to "1024x1024" for unrecognised ratios.
func SizeFromAspect(aspectRatio string) string {
	switch aspectRatio {
	case "16:9":
		return "1792x1024"
	case "9:16":
		return "1024x1792"
	case "3:4":
		return "1024x1365"
	case "4:3":
		return "1365x1024"
	default:
		return "1024x1024"
	}
}
