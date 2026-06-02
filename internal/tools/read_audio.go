package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

// --- Context helpers for media audio ---

const ctxMediaAudioRefs toolContextKey = "tool_media_audio_refs"

// WithMediaAudioRefs stores audio MediaRefs in context for read_audio tool access.
func WithMediaAudioRefs(ctx context.Context, refs []providers.MediaRef) context.Context {
	return context.WithValue(ctx, ctxMediaAudioRefs, refs)
}

// MediaAudioRefsFromCtx retrieves stored audio MediaRefs from context.
func MediaAudioRefsFromCtx(ctx context.Context) []providers.MediaRef {
	v, _ := ctx.Value(ctxMediaAudioRefs).([]providers.MediaRef)
	return v
}

// --- ReadAudioTool ---

// audioMaxBytes is the max file size for audio analysis (50MB).
const audioMaxBytes = 50 * 1024 * 1024

// audioProviderPriority is the order in which providers are tried for audio analysis.
var audioProviderPriority = []string{"gemini", "openai", "openrouter"}

// audioModelDefaults maps provider names to preferred audio-capable models.
var audioModelDefaults = map[string]string{
	"gemini":     "gemini-2.5-flash",
	"openai":     "gpt-4o-audio-preview",
	"openrouter": "google/gemini-2.5-flash",
}

// ReadAudioTool uses an audio-capable provider to analyze audio files
// attached to the current conversation.
type ReadAudioTool struct {
	registry    *providers.Registry
	mediaLoader MediaPathLoader
	usageCaps   *usagecaps.Service
}

func NewReadAudioTool(registry *providers.Registry, mediaLoader MediaPathLoader) *ReadAudioTool {
	return &ReadAudioTool{registry: registry, mediaLoader: mediaLoader}
}

func (t *ReadAudioTool) SetUsageCapService(svc *usagecaps.Service) {
	t.usageCaps = svc
}

func (t *ReadAudioTool) Name() string { return "read_audio" }

func (t *ReadAudioTool) Description() string {
	return "Analyze audio files (speech, music, sounds) attached to the conversation. " +
		"Use when you see <media:audio> tags and need to transcribe, summarize, or analyze audio content. " +
		"Specify what you want to extract or analyze."
}

func (t *ReadAudioTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "What to analyze. E.g. 'Transcribe this audio', 'Summarize the conversation', 'What language is spoken?'",
			},
			"media_id": map[string]any{
				"type":        "string",
				"description": "Optional: specific media_id from <media:audio> tag. If omitted, uses most recent audio.",
			},
		},
		"required": []string{"prompt"},
	}
}

func (t *ReadAudioTool) Execute(ctx context.Context, args map[string]any) *Result {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		prompt = "Analyze this audio and describe its contents."
	}
	mediaID, _ := args["media_id"].(string)

	audioPath, audioMime, err := t.resolveAudioFile(ctx, mediaID)
	if err != nil {
		return ErrorResult(err.Error())
	}

	slog.Info("read_audio: resolved file", "path", audioPath, "mime", audioMime, "media_id", mediaID)

	data, err := os.ReadFile(audioPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to read audio file: %v", err))
	}
	slog.Info("read_audio: file loaded", "size_bytes", len(data))
	if len(data) > audioMaxBytes {
		return ErrorResult(fmt.Sprintf("Audio too large: %d bytes (max %d)", len(data), audioMaxBytes))
	}

	chain := ResolveMediaProviderChain(ctx, "read_audio", "", "",
		audioProviderPriority, audioModelDefaults, t.registry)

	for i := range chain {
		if chain[i].Params == nil {
			chain[i].Params = make(map[string]any)
		}
		chain[i].Params["prompt"] = prompt
		chain[i].Params["data"] = data
		chain[i].Params["mime"] = audioMime
	}

	chainResult, err := ExecuteWithChain(ctx, chain, t.registry, t.callProvider)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Audio analysis failed: %v", err))
	}

	result := NewResult(string(chainResult.Data))
	result.Usage = chainResult.Usage
	result.Provider = chainResult.Provider
	result.Model = chainResult.Model
	return result
}

// mimeFromAudioExt returns MIME type for audio file extensions.
func mimeFromAudioExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".m4a":
		return "audio/mp4"
	case ".aac":
		return "audio/aac"
	case ".flac":
		return "audio/flac"
	case ".aiff", ".aif":
		return "audio/aiff"
	case ".wma":
		return "audio/x-ms-wma"
	case ".opus":
		return "audio/opus"
	default:
		return "audio/mpeg"
	}
}
