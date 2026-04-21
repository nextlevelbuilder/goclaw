package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// MediaAnalyzer analyzes persisted media files using LLM providers
// to generate text descriptions suitable for KG extraction.
// Delegates to tools.AnalyzeMediaFile which reuses the same provider
// chains as the built-in read_image/read_document/read_audio/read_video tools.
type MediaAnalyzer struct {
	registry     *providers.Registry
	systemCfg    store.SystemConfigStore
	builtinTools store.BuiltinToolStore
	tenantID     uuid.UUID
}

// NewMediaAnalyzer creates a new MediaAnalyzer.
func NewMediaAnalyzer(registry *providers.Registry, systemCfg store.SystemConfigStore, builtinTools store.BuiltinToolStore, tenantID uuid.UUID) *MediaAnalyzer {
	return &MediaAnalyzer{
		registry:     registry,
		systemCfg:    systemCfg,
		builtinTools: builtinTools,
		tenantID:     tenantID,
	}
}

// mediaToolNames maps media types to their corresponding builtin tool names.
var mediaToolNames = map[string]string{
	"image":    "read_image",
	"document": "read_document",
	"audio":    "read_audio",
	"video":    "read_video",
}

// contextWithToolSettings loads user-configured provider chains from builtin_tools
// and injects them into the context so ResolveMediaProviderChain can find them.
func (a *MediaAnalyzer) contextWithToolSettings(ctx context.Context, mediaType string) context.Context {
	if a.builtinTools == nil {
		return ctx
	}
	toolName, ok := mediaToolNames[mediaType]
	if !ok {
		return ctx
	}
	settings, err := a.builtinTools.GetSettings(store.WithTenantID(ctx, a.tenantID), toolName)
	if err != nil || len(settings) == 0 {
		return ctx
	}
	return tools.WithBuiltinToolSettings(ctx, tools.BuiltinToolSettings{
		toolName: settings,
	})
}

// Analyze processes a list of media references and returns a concatenated
// description of all media content. Each file is analyzed independently;
// failures are logged and skipped (graceful degradation).
// Returns ("", nil) if media analysis is disabled via system config.
func (a *MediaAnalyzer) Analyze(ctx context.Context, refs []store.RawMediaRef) (string, error) {
	if a.registry == nil || len(refs) == 0 {
		return "", nil
	}

	lim := a.loadLimits(ctx)
	if !lim.enabled {
		return "", nil
	}

	var descs []string
	for _, ref := range refs {
		desc, err := a.analyzeOne(ctx, ref, lim)
		if err != nil {
			slog.Warn("whatsapp media analyzer: failed to analyze",
				"media_type", ref.MediaType, "file", ref.FileName, "error", err)
			descs = append(descs, fmt.Sprintf("[Media: %s — analysis failed]", ref.MediaType))
			continue
		}
		descs = append(descs, desc)
	}
	return strings.Join(descs, "\n"), nil
}

// analyzeOne analyzes a single media file by delegating to tools.AnalyzeMediaFile.
func (a *MediaAnalyzer) analyzeOne(ctx context.Context, ref store.RawMediaRef, lim mediaLimits) (string, error) {
	// Check file exists and size.
	fi, err := os.Stat(ref.FilePath)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", ref.FilePath, err)
	}

	mediaType := ref.MediaType
	if mediaType == "" {
		mediaType = mediaTypeFromMime(ref.ContentType)
	}

	// Check size limit.
	sizeLimit := lim.sizeLimitForType(mediaType)
	if fi.Size() > sizeLimit {
		return fmt.Sprintf("[Media: %s %q — too large (%d bytes)]", mediaType, ref.FileName, fi.Size()), nil
	}

	// Read file.
	data, err := os.ReadFile(ref.FilePath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	mime := ref.ContentType
	if mime == "" {
		mime = mimeFromExt(ref.FilePath)
	}

	prompt := mediaPromptForType(mediaType, ref.FileName)

	// Inject user-configured tool settings into context so the provider
	// chain resolver can find the user's configured provider for this media type.
	analyzeCtx := a.contextWithToolSettings(ctx, mediaType)

	// Delegate to tools package — uses built-in tool provider chains
	// with proper provider-specific dispatch (Gemini native for PDFs, etc.).
	result, err := tools.AnalyzeMediaFile(analyzeCtx, a.registry, tools.MediaFileRequest{
		MediaType: mediaType,
		Data:      data,
		MimeType:  mime,
		FileName:  ref.FileName,
		Prompt:    prompt,
		Timeout:   lim.timeout,
	})
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("[Media: %s — %s]", mediaType, result.Content), nil
}

// Default size limits (in MB) — used when system config is not set.
const (
	defaultMaxImageMB   = 10
	defaultMaxAudioMB   = 50
	defaultMaxDocMB     = 20
	defaultMaxVideoMB   = 100
	defaultMaxDefaultMB = 20

	defaultMediaTimeoutSec = 30
)

// mediaLimits holds size limits loaded from system configs.
type mediaLimits struct {
	enabled         bool
	maxImageBytes   int64
	maxAudioBytes   int64
	maxDocBytes     int64
	maxVideoBytes   int64
	maxDefaultBytes int64
	timeout         time.Duration
}

// sizeLimitForType returns the size limit for the given media type.
func (l mediaLimits) sizeLimitForType(mediaType string) int64 {
	switch mediaType {
	case "image", "sticker":
		return l.maxImageBytes
	case "audio", "voice":
		return l.maxAudioBytes
	case "document":
		return l.maxDocBytes
	case "video", "animation":
		return l.maxVideoBytes
	default:
		return l.maxDefaultBytes
	}
}

// loadLimits reads media analysis limits from system configs.
// Falls back to hardcoded defaults for any missing key.
// Media analysis is enabled by default unless explicitly set to "false".
func (a *MediaAnalyzer) loadLimits(ctx context.Context) mediaLimits {
	lim := mediaLimits{
		enabled:         true,
		maxImageBytes:   int64(defaultMaxImageMB) * 1024 * 1024,
		maxAudioBytes:   int64(defaultMaxAudioMB) * 1024 * 1024,
		maxDocBytes:     int64(defaultMaxDocMB) * 1024 * 1024,
		maxVideoBytes:   int64(defaultMaxVideoMB) * 1024 * 1024,
		maxDefaultBytes: int64(defaultMaxDefaultMB) * 1024 * 1024,
		timeout:         time.Duration(defaultMediaTimeoutSec) * time.Second,
	}
	if a.systemCfg == nil {
		return lim
	}
	configs, err := a.systemCfg.List(store.WithTenantID(ctx, a.tenantID))
	if err != nil {
		return lim
	}
	if v := configs["listen.media_analysis.max_image_mb"]; v != "" {
		if mb, err := strconv.ParseInt(v, 10, 64); err == nil && mb > 0 {
			lim.maxImageBytes = mb * 1024 * 1024
		}
	}
	if v := configs["listen.media_analysis.max_audio_mb"]; v != "" {
		if mb, err := strconv.ParseInt(v, 10, 64); err == nil && mb > 0 {
			lim.maxAudioBytes = mb * 1024 * 1024
		}
	}
	if v := configs["listen.media_analysis.max_document_mb"]; v != "" {
		if mb, err := strconv.ParseInt(v, 10, 64); err == nil && mb > 0 {
			lim.maxDocBytes = mb * 1024 * 1024
		}
	}
	if v := configs["listen.media_analysis.max_video_mb"]; v != "" {
		if mb, err := strconv.ParseInt(v, 10, 64); err == nil && mb > 0 {
			lim.maxVideoBytes = mb * 1024 * 1024
		}
	}
	if v := configs["listen.media_analysis.max_default_mb"]; v != "" {
		if mb, err := strconv.ParseInt(v, 10, 64); err == nil && mb > 0 {
			lim.maxDefaultBytes = mb * 1024 * 1024
		}
	}
	if v := configs["listen.media_analysis.timeout_sec"]; v != "" {
		if sec, err := strconv.ParseInt(v, 10, 64); err == nil && sec > 0 {
			lim.timeout = time.Duration(sec) * time.Second
		}
	}
	if v := configs["listen.media_analysis.enabled"]; v == "false" || v == "0" {
		lim.enabled = false
	}
	return lim
}

// mediaPromptForType returns a default analysis prompt for the given media type.
func mediaPromptForType(mediaType, fileName string) string {
	switch mediaType {
	case "image", "sticker":
		return "Describe this image in detail. If there is text visible, include it. If people, objects, or locations are identifiable, describe them."
	case "audio", "voice":
		return "Transcribe and describe this audio. If it's speech, provide the transcript. If it's music, describe what you hear."
	case "document":
		return fmt.Sprintf("Analyze this document (%s). Extract key information: projects, tasks, deadlines, people, organizations, and any other notable content.", fileName)
	case "video", "animation":
		return "Analyze this video. Describe what happens, any text on screen, people, objects, and locations visible."
	default:
		return "Describe this media file in detail."
	}
}

// mediaTypeFromMime infers a media type from MIME type.
func mediaTypeFromMime(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	case mime == "application/pdf":
		return "document"
	default:
		return ""
	}
}

// mimeFromExt returns a MIME type based on file extension.
func mimeFromExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}

// analyzeMediaAttachments processes media attachments for a batch of messages.
// Returns a map of message ID → media description text.
func analyzeMediaAttachments(ctx context.Context, msgs []store.ListenRawMessage, analyzer *MediaAnalyzer) map[uuid.UUID]string {
	if analyzer == nil {
		return nil
	}

	result := make(map[uuid.UUID]string)
	for _, m := range msgs {
		if len(m.MediaRefs) == 0 {
			continue
		}
		desc, err := analyzer.Analyze(ctx, m.MediaRefs)
		if err != nil {
			slog.Warn("whatsapp extraction: media analysis failed",
				"msg_id", m.ID, "error", err)
			continue
		}
		if desc != "" {
			result[m.ID] = desc
		}
	}
	return result
}

// mediaRefsSummary returns a compact summary of media refs for logging.
func mediaRefsSummary(msgs []store.ListenRawMessage) string {
	var counts [5]int // image, video, audio, document, other
	for _, m := range msgs {
		for _, r := range m.MediaRefs {
			switch r.MediaType {
			case "image", "sticker":
				counts[0]++
			case "video", "animation":
				counts[1]++
			case "audio", "voice":
				counts[2]++
			case "document":
				counts[3]++
			default:
				counts[4]++
			}
		}
	}
	total := counts[0] + counts[1] + counts[2] + counts[3] + counts[4]
	if total == 0 {
		return ""
	}

	var parts []string
	if counts[0] > 0 {
		parts = append(parts, fmt.Sprintf("%d images", counts[0]))
	}
	if counts[1] > 0 {
		parts = append(parts, fmt.Sprintf("%d videos", counts[1]))
	}
	if counts[2] > 0 {
		parts = append(parts, fmt.Sprintf("%d audio", counts[2]))
	}
	if counts[3] > 0 {
		parts = append(parts, fmt.Sprintf("%d docs", counts[3]))
	}
	if counts[4] > 0 {
		parts = append(parts, fmt.Sprintf("%d other", counts[4]))
	}

	summary, _ := json.Marshal(parts)
	return string(summary)
}
