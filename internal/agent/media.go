package agent

import (
	"encoding/base64"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// maxImageBytes is the safety limit for reading image files (10MB).
const maxImageBytes = 10 * 1024 * 1024

// MediaResult represents a media file produced by a tool during the agent run.
type MediaResult struct {
	Path        string `json:"path"`                   // local file path
	ContentType string `json:"content_type,omitempty"` // MIME type
	AsVoice     bool   `json:"as_voice,omitempty"`     // send as voice message (Telegram OGG)
}

// loadImages reads local image files and returns base64-encoded ImageContent slices.
// Non-image files and files that fail to read are skipped with a warning log.
func loadImages(paths []string) []providers.ImageContent {
	if len(paths) == 0 {
		return nil
	}

	var images []providers.ImageContent
	for _, p := range paths {
		mime := inferImageMime(p)
		if mime == "" {
			continue
		}

		data, err := os.ReadFile(p)
		if err != nil {
			slog.Warn("vision: failed to read image file", "path", p, "error", err)
			continue
		}
		if len(data) > maxImageBytes {
			slog.Warn("vision: image file too large, skipping", "path", p, "size", len(data))
			continue
		}

		images = append(images, providers.ImageContent{
			MimeType: mime,
			Data:     base64.StdEncoding.EncodeToString(data),
		})
	}
	return images
}

// inferImageMime returns the MIME type for supported image extensions, or "" if not an image.
func inferImageMime(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}

// parseMediaResult extracts a MediaResult from a tool result string containing "MEDIA:" prefix.
// Handles formats: "MEDIA:/path/to/file" and "[[audio_as_voice]]\nMEDIA:/path/to/file".
// Returns nil if no MEDIA: prefix is found.
func parseMediaResult(toolOutput string) *MediaResult {
	s := toolOutput
	asVoice := false

	// Check for [[audio_as_voice]] tag (TTS voice messages)
	if strings.Contains(s, "[[audio_as_voice]]") {
		asVoice = true
		s = strings.ReplaceAll(s, "[[audio_as_voice]]", "")
		s = strings.TrimSpace(s)
	}

	// Find MEDIA: prefix
	idx := strings.Index(s, "MEDIA:")
	if idx < 0 {
		return nil
	}
	path := strings.TrimSpace(s[idx+6:])
	if path == "" {
		return nil
	}
	// Take only the first line (in case there's trailing text)
	if nl := strings.IndexByte(path, '\n'); nl >= 0 {
		path = strings.TrimSpace(path[:nl])
	}

	return &MediaResult{
		Path:        path,
		ContentType: mimeFromExt(filepath.Ext(path)),
		AsVoice:     asVoice,
	}
}

// mimeFromExt returns a MIME type for common media file extensions.
func mimeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
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
	default:
		return "application/octet-stream"
	}
}
