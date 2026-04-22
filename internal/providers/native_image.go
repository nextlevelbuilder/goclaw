package providers

import "context"

// NativeImageProvider is implemented by OAuth-backed providers whose upstream
// exposes an image_generation native tool (ChatGPT Responses API style).
// create_image routes through this interface when the chain resolves to such
// a provider, bypassing the credentialProvider (APIKey/APIBase) path.
type NativeImageProvider interface {
	GenerateImage(ctx context.Context, req NativeImageRequest) (*NativeImageResult, error)
}

// NativeImageRequest describes a single image generation request.
type NativeImageRequest struct {
	// Model to use for image generation (e.g. "gpt-image-2").
	// If empty, the provider uses its own default.
	Model string

	// Prompt is the text description of the image.
	Prompt string

	// AspectRatio is the desired aspect ratio, e.g. "16:9", "1:1", "9:16".
	// Converted to a concrete pixel size by the provider implementation.
	// Defaults to "1:1" if empty.
	AspectRatio string

	// OutputFormat is the desired image format: "png" (default), "jpg", "webp".
	OutputFormat string
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
