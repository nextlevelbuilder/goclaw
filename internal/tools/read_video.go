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

// --- Context helpers for media video ---

const ctxMediaVideoRefs toolContextKey = "tool_media_video_refs"

// WithMediaVideoRefs stores video MediaRefs in context for read_video tool access.
func WithMediaVideoRefs(ctx context.Context, refs []providers.MediaRef) context.Context {
	return context.WithValue(ctx, ctxMediaVideoRefs, refs)
}

// MediaVideoRefsFromCtx retrieves stored video MediaRefs from context.
func MediaVideoRefsFromCtx(ctx context.Context) []providers.MediaRef {
	v, _ := ctx.Value(ctxMediaVideoRefs).([]providers.MediaRef)
	return v
}

// --- ReadVideoTool ---

// videoMaxBytes is the max file size for video analysis (100MB).
const videoMaxBytes = 100 * 1024 * 1024

// videoProviderPriority is the order in which providers are tried for video analysis.
// OpenAI excluded — no native video upload in chat completions.
var videoProviderPriority = []string{"gemini", "openrouter"}

// videoModelDefaults maps provider names to preferred video-capable models.
var videoModelDefaults = map[string]string{
	"gemini":     "gemini-2.5-flash",
	"openrouter": "google/gemini-2.5-flash",
}

// ReadVideoTool uses a video-capable provider to analyze video files
// attached to the current conversation.
type ReadVideoTool struct {
	registry    *providers.Registry
	mediaLoader MediaPathLoader
	usageCaps   *usagecaps.Service
}

func NewReadVideoTool(registry *providers.Registry, mediaLoader MediaPathLoader) *ReadVideoTool {
	return &ReadVideoTool{registry: registry, mediaLoader: mediaLoader}
}

func (t *ReadVideoTool) SetUsageCapService(svc *usagecaps.Service) {
	t.usageCaps = svc
}

func (t *ReadVideoTool) Name() string { return "read_video" }

func (t *ReadVideoTool) Description() string {
	return "Analyze video files attached to the conversation. " +
		"Use when you see <media:video> tags and need to describe, summarize, or analyze video content. " +
		"Specify what you want to extract or analyze."
}

func (t *ReadVideoTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "What to analyze. E.g. 'Describe what happens in this video', 'Summarize the key scenes', 'What text appears on screen?'",
			},
			"media_id": map[string]any{
				"type":        "string",
				"description": "Optional: specific media_id from <media:video> tag. If omitted, uses most recent video.",
			},
		},
		"required": []string{"prompt"},
	}
}

func (t *ReadVideoTool) Execute(ctx context.Context, args map[string]any) *Result {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		prompt = "Analyze this video and describe its contents."
	}
	mediaID, _ := args["media_id"].(string)

	videoPath, videoMime, err := t.resolveVideoFile(ctx, mediaID)
	if err != nil {
		return ErrorResult(err.Error())
	}

	slog.Info("read_video: resolved file", "path", videoPath, "mime", videoMime, "media_id", mediaID)

	data, err := os.ReadFile(videoPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to read video file: %v", err))
	}
	slog.Info("read_video: file loaded", "size_bytes", len(data))
	if len(data) > videoMaxBytes {
		return ErrorResult(fmt.Sprintf("Video too large: %d bytes (max %d)", len(data), videoMaxBytes))
	}

	chain := ResolveMediaProviderChain(ctx, "read_video", "", "",
		videoProviderPriority, videoModelDefaults, t.registry)

	for i := range chain {
		if chain[i].Params == nil {
			chain[i].Params = make(map[string]any)
		}
		chain[i].Params["prompt"] = prompt
		chain[i].Params["data"] = data
		chain[i].Params["mime"] = videoMime
	}

	chainResult, err := ExecuteWithChain(ctx, chain, t.registry, t.callProvider)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Video analysis failed: %v", err))
	}

	result := NewResult(string(chainResult.Data))
	result.Usage = chainResult.Usage
	result.Provider = chainResult.Provider
	result.Model = chainResult.Model
	return result
}

// mimeFromVideoExt returns MIME type for video file extensions.
func mimeFromVideoExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".mkv":
		return "video/x-matroska"
	case ".wmv":
		return "video/x-ms-wmv"
	case ".flv":
		return "video/x-flv"
	case ".3gp":
		return "video/3gpp"
	case ".mpeg", ".mpg":
		return "video/mpeg"
	default:
		return "video/mp4"
	}
}
