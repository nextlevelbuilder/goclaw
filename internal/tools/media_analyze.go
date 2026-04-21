package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// MediaFileRequest describes a media file to analyze.
type MediaFileRequest struct {
	MediaType string        // "image", "document", "audio", "video", "sticker", "voice", "animation"
	Data      []byte        // file bytes (caller reads from disk)
	MimeType  string        // e.g. "application/pdf", "image/jpeg"
	FileName  string        // for logging only
	Prompt    string        // what to analyze
	Timeout   time.Duration // optional per-request timeout (0 = use provider chain defaults)
}

// MediaFileResult holds the analysis result.
type MediaFileResult struct {
	Content  string
	Provider string
	Model    string
}

// AnalyzeMediaFile analyzes a media file using the same provider chain
// and dispatch logic as the built-in tools (read_image, read_document,
// read_audio, read_video). This is the public entry point for media
// analysis outside the agent loop (e.g. background KG extraction).
func AnalyzeMediaFile(ctx context.Context, registry *providers.Registry, req MediaFileRequest) (*MediaFileResult, error) {
	if registry == nil {
		return nil, fmt.Errorf("no provider registry")
	}
	if len(req.Data) == 0 {
		return nil, fmt.Errorf("no data provided")
	}

	// Apply per-request timeout if specified.
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	// Normalize media type aliases.
	mediaType := normalizeMediaType(req.MediaType)

	switch mediaType {
	case "image":
		return analyzeMediaImage(ctx, registry, req)
	case "document":
		return analyzeMediaDocument(ctx, registry, req)
	case "audio":
		return analyzeMediaAudio(ctx, registry, req)
	case "video":
		return analyzeMediaVideo(ctx, registry, req)
	default:
		return nil, fmt.Errorf("unsupported media type: %s", req.MediaType)
	}
}

// normalizeMediaType maps media type aliases to canonical types.
func normalizeMediaType(t string) string {
	switch t {
	case "sticker":
		return "image"
	case "voice":
		return "audio"
	case "animation":
		return "video"
	default:
		return t
	}
}

// --- Image ---

func analyzeMediaImage(ctx context.Context, registry *providers.Registry, req MediaFileRequest) (*MediaFileResult, error) {
	mime := req.MimeType
	if mime == "" {
		mime = "image/jpeg"
	}
	b64 := base64.StdEncoding.EncodeToString(req.Data)
	images := []providers.ImageContent{{MimeType: mime, Data: b64}}

	chain := ResolveMediaProviderChain(ctx, "read_image", "", "",
		visionProviderPriority, visionModelDefaults, registry)

	for i := range chain {
		if chain[i].Params == nil {
			chain[i].Params = make(map[string]any)
		}
		chain[i].Params["prompt"] = req.Prompt
		chain[i].Params["images"] = images
	}

	if len(chain) == 0 {
		return nil, fmt.Errorf("no vision provider configured")
	}

	callFn := func(ctx context.Context, cp credentialProvider, providerName, model string, params map[string]any) ([]byte, *providers.Usage, error) {
		return mediaImageCallProvider(ctx, cp, providerName, model, params, registry)
	}

	chainResult, err := ExecuteWithChain(ctx, chain, registry, callFn)
	if err != nil {
		return nil, err
	}

	return &MediaFileResult{
		Content:  string(chainResult.Data),
		Provider: chainResult.Provider,
		Model:    chainResult.Model,
	}, nil
}

// mediaImageCallProvider dispatches vision calls via provider.Chat().
func mediaImageCallProvider(ctx context.Context, cp credentialProvider, providerName, model string, params map[string]any, registry *providers.Registry) ([]byte, *providers.Usage, error) {
	prompt := GetParamString(params, "prompt", "Describe this image in detail.")
	images, _ := params["images"].([]providers.ImageContent)

	p, err := registry.Get(ctx, providerName)
	if err != nil {
		return nil, nil, fmt.Errorf("provider %q not available: %w", providerName, err)
	}

	slog.Info("media_analyze: calling vision provider", "provider", providerName, "model", model, "images", len(images))

	opts := map[string]any{"max_tokens": 1024, "temperature": 0.3}
	if providerName == "claude-cli" {
		opts["disable_tools"] = true
	}

	resp, err := p.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "user", Content: prompt, Images: images},
		},
		Model:   model,
		Options: opts,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("vision provider error: %w", err)
	}
	return []byte(resp.Content), resp.Usage, nil
}

// --- Document ---

func analyzeMediaDocument(ctx context.Context, registry *providers.Registry, req MediaFileRequest) (*MediaFileResult, error) {
	mime := req.MimeType
	if mime == "" {
		mime = "application/octet-stream"
	}

	// Fast path: text-readable files — return content directly without LLM.
	if textReadableMIMEs[mime] || strings.HasPrefix(mime, "text/") {
		content := string(req.Data)
		if len(content) > 50000 {
			content = content[:50000] + "\n... [truncated]"
		}
		return &MediaFileResult{Content: content}, nil
	}

	chain := ResolveMediaProviderChain(ctx, "read_document", "", "",
		documentProviderPriority, documentModelDefaults, registry)

	for i := range chain {
		if chain[i].Params == nil {
			chain[i].Params = make(map[string]any)
		}
		chain[i].Params["prompt"] = req.Prompt
		chain[i].Params["data"] = req.Data
		chain[i].Params["mime"] = mime
	}

	if len(chain) == 0 {
		return nil, fmt.Errorf("no document provider configured")
	}

	callFn := func(ctx context.Context, cp credentialProvider, providerName, model string, params map[string]any) ([]byte, *providers.Usage, error) {
		return mediaDocumentCallProvider(ctx, cp, providerName, model, params, registry)
	}

	chainResult, err := ExecuteWithChain(ctx, chain, registry, callFn)
	if err != nil {
		return nil, err
	}

	return &MediaFileResult{
		Content:  string(chainResult.Data),
		Provider: chainResult.Provider,
		Model:    chainResult.Model,
	}, nil
}

// mediaDocumentCallProvider dispatches document analysis to the appropriate provider API.
// Gemini: uses native generateContent API (supports PDF natively).
// Others: uses standard Chat API with base64 document.
func mediaDocumentCallProvider(ctx context.Context, cp credentialProvider, providerName, model string, params map[string]any, registry *providers.Registry) ([]byte, *providers.Usage, error) {
	prompt := GetParamString(params, "prompt", "Analyze this document and describe its contents.")
	data, _ := params["data"].([]byte)
	mime := GetParamString(params, "mime", "application/octet-stream")

	ptype := GetParamString(params, "_provider_type", providerTypeFromName(providerName))

	// Gemini: use native API (requires credentials).
	if cp != nil && ptype == "gemini" {
		slog.Info("media_analyze: using gemini native API for document",
			"provider", providerName, "model", model, "size", len(data), "mime", mime)
		resp, err := geminiNativeDocumentCall(ctx, cp.APIKey(), model, prompt, data, mime)
		if err != nil {
			return nil, nil, fmt.Errorf("gemini native call: %w", err)
		}
		return []byte(resp.Content), resp.Usage, nil
	}

	// Other providers: use standard Chat API with document as base64.
	p, err := registry.Get(ctx, providerName)
	if err != nil {
		return nil, nil, fmt.Errorf("provider %q not available: %w", providerName, err)
	}

	slog.Info("media_analyze: using chat API for document", "provider", providerName, "model", model, "size", len(data))

	opts := map[string]any{"max_tokens": 16384, "temperature": 0.2}
	if providerName == "claude-cli" {
		opts["disable_tools"] = true
	}

	resp, err := p.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{
			{
				Role:    "user",
				Content: prompt,
				Images:  []providers.ImageContent{{MimeType: mime, Data: base64.StdEncoding.EncodeToString(data)}},
			},
		},
		Model:   model,
		Options: opts,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("chat call: %w", err)
	}
	return []byte(resp.Content), resp.Usage, nil
}

// --- Audio ---

func analyzeMediaAudio(ctx context.Context, registry *providers.Registry, req MediaFileRequest) (*MediaFileResult, error) {
	mime := req.MimeType
	if mime == "" {
		mime = "audio/mpeg"
	}

	chain := ResolveMediaProviderChain(ctx, "read_audio", "", "",
		audioProviderPriority, audioModelDefaults, registry)

	for i := range chain {
		if chain[i].Params == nil {
			chain[i].Params = make(map[string]any)
		}
		chain[i].Params["prompt"] = req.Prompt
		chain[i].Params["data"] = req.Data
		chain[i].Params["mime"] = mime
	}

	if len(chain) == 0 {
		return nil, fmt.Errorf("no audio provider configured")
	}

	callFn := func(ctx context.Context, cp credentialProvider, providerName, model string, params map[string]any) ([]byte, *providers.Usage, error) {
		return mediaAudioCallProvider(ctx, cp, providerName, model, params, registry)
	}

	chainResult, err := ExecuteWithChain(ctx, chain, registry, callFn)
	if err != nil {
		return nil, err
	}

	return &MediaFileResult{
		Content:  string(chainResult.Data),
		Provider: chainResult.Provider,
		Model:    chainResult.Model,
	}, nil
}

// mediaAudioCallProvider dispatches audio analysis to the appropriate provider API.
// Gemini: uses File API. OpenAI: uses audio/transcription or input_audio.
// Others: falls back to base64 in image_url (best effort).
func mediaAudioCallProvider(ctx context.Context, cp credentialProvider, providerName, model string, params map[string]any, registry *providers.Registry) ([]byte, *providers.Usage, error) {
	prompt := GetParamString(params, "prompt", "Analyze this audio and describe its contents.")
	data, _ := params["data"].([]byte)
	mime := GetParamString(params, "mime", "audio/mpeg")

	ptype := GetParamString(params, "_provider_type", providerTypeFromName(providerName))

	if cp == nil && (ptype == "gemini" || ptype == "openai") {
		slog.Info("media_analyze: no API credentials for audio, falling back to Chat API", "provider", providerName)
	}
	if cp != nil {
		if ptype == "gemini" {
			slog.Info("media_analyze: using gemini file API for audio", "provider", providerName, "model", model, "size", len(data), "mime", mime)
			resp, err := geminiFileAPICall(ctx, cp.APIKey(), model, prompt, data, mime, 120*time.Second)
			if err != nil {
				return nil, nil, fmt.Errorf("gemini file API: %w", err)
			}
			return []byte(resp.Content), resp.Usage, nil
		}

		if ptype == "openai" {
			if isTranscriptionModel(model) {
				slog.Info("media_analyze: using openai transcription API", "provider", providerName, "model", model)
				resp, err := openaiTranscriptionCall(ctx, cp.APIKey(), cp.APIBase(), model, prompt, data, mime)
				if err != nil {
					return nil, nil, fmt.Errorf("openai transcription: %w", err)
				}
				return []byte(resp.Content), resp.Usage, nil
			}
			slog.Info("media_analyze: using openai input_audio API", "provider", providerName, "model", model)
			resp, err := openaiAudioCall(ctx, cp.APIKey(), cp.APIBase(), model, prompt, data, mime)
			if err != nil {
				return nil, nil, fmt.Errorf("openai audio call: %w", err)
			}
			return []byte(resp.Content), resp.Usage, nil
		}
	}

	// Fallback: standard Chat API with base64 as image_url.
	p, err := registry.Get(ctx, providerName)
	if err != nil {
		return nil, nil, fmt.Errorf("provider %q not available: %w", providerName, err)
	}

	slog.Info("media_analyze: using chat API fallback for audio", "provider", providerName, "model", model, "size", len(data))
	resp, err := p.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{
			{
				Role:    "user",
				Content: prompt,
				Images:  []providers.ImageContent{{MimeType: mime, Data: base64.StdEncoding.EncodeToString(data)}},
			},
		},
		Model:   model,
		Options: map[string]any{"max_tokens": 16384, "temperature": 0.2},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("chat API: %w", err)
	}
	return []byte(resp.Content), resp.Usage, nil
}

// --- Video ---

func analyzeMediaVideo(ctx context.Context, registry *providers.Registry, req MediaFileRequest) (*MediaFileResult, error) {
	mime := req.MimeType
	if mime == "" {
		mime = "video/mp4"
	}

	chain := ResolveMediaProviderChain(ctx, "read_video", "", "",
		videoProviderPriority, videoModelDefaults, registry)

	for i := range chain {
		if chain[i].Params == nil {
			chain[i].Params = make(map[string]any)
		}
		chain[i].Params["prompt"] = req.Prompt
		chain[i].Params["data"] = req.Data
		chain[i].Params["mime"] = mime
	}

	if len(chain) == 0 {
		return nil, fmt.Errorf("no video provider configured")
	}

	callFn := func(ctx context.Context, cp credentialProvider, providerName, model string, params map[string]any) ([]byte, *providers.Usage, error) {
		return mediaVideoCallProvider(ctx, cp, providerName, model, params, registry)
	}

	chainResult, err := ExecuteWithChain(ctx, chain, registry, callFn)
	if err != nil {
		return nil, err
	}

	return &MediaFileResult{
		Content:  string(chainResult.Data),
		Provider: chainResult.Provider,
		Model:    chainResult.Model,
	}, nil
}

// mediaVideoCallProvider dispatches video analysis to the appropriate provider API.
// Gemini: uses File API. Others: falls back to base64 in image_url.
func mediaVideoCallProvider(ctx context.Context, cp credentialProvider, providerName, model string, params map[string]any, registry *providers.Registry) ([]byte, *providers.Usage, error) {
	prompt := GetParamString(params, "prompt", "Analyze this video and describe its contents.")
	data, _ := params["data"].([]byte)
	mime := GetParamString(params, "mime", "video/mp4")

	ptype := GetParamString(params, "_provider_type", providerTypeFromName(providerName))

	if cp != nil && ptype == "gemini" {
		slog.Info("media_analyze: using gemini file API for video", "provider", providerName, "model", model, "size", len(data), "mime", mime)
		resp, err := geminiFileAPICall(ctx, cp.APIKey(), model, prompt, data, mime, 180*time.Second)
		if err != nil {
			return nil, nil, fmt.Errorf("gemini file API: %w", err)
		}
		return []byte(resp.Content), resp.Usage, nil
	}

	// Fallback: standard Chat API with base64.
	p, err := registry.Get(ctx, providerName)
	if err != nil {
		return nil, nil, fmt.Errorf("provider %q not available: %w", providerName, err)
	}

	slog.Info("media_analyze: using chat API fallback for video", "provider", providerName, "model", model, "size", len(data))
	resp, err := p.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{
			{
				Role:    "user",
				Content: prompt,
				Images:  []providers.ImageContent{{MimeType: mime, Data: base64.StdEncoding.EncodeToString(data)}},
			},
		},
		Model:   model,
		Options: map[string]any{"max_tokens": 16384, "temperature": 0.2},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("chat API: %w", err)
	}
	return []byte(resp.Content), resp.Usage, nil
}
