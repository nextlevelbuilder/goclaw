package agent

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// maxImageBytes is the safety limit for reading image files (10MB).
const maxImageBytes = 10 * 1024 * 1024

// loadImages reads local image files and returns base64-encoded ImageContent slices.
// Non-image files and files that fail to read are skipped with a warning log.
func loadImages(files []bus.MediaFile) []providers.ImageContent {
	if len(files) == 0 {
		return nil
	}

	var images []providers.ImageContent
	for _, f := range files {
		mime := f.MimeType
		if mime == "" {
			mime = inferImageMime(f.Path)
		}
		if !strings.HasPrefix(mime, "image/") {
			continue
		}

		data, err := os.ReadFile(f.Path)
		if err != nil {
			slog.Warn("vision: failed to read image file", "path", f.Path, "error", err)
			continue
		}
		if len(data) > maxImageBytes {
			slog.Warn("vision: image file too large, skipping", "path", f.Path, "size", len(data))
			continue
		}

		images = append(images, providers.ImageContent{
			MimeType: mime,
			Data:     base64.StdEncoding.EncodeToString(data),
		})
	}
	return images
}

// persistMedia sanitizes images, saves all media files to persistent storage,
// and returns lightweight MediaRefs. Non-image files are saved as-is.
// If mediaStore is nil, falls back to legacy behavior (no persistence, delete temp files).
func (l *Loop) persistMedia(sessionKey string, files []bus.MediaFile) []providers.MediaRef {
	if l.mediaStore == nil {
		// Fallback: no persistent storage configured — delete temp files after loading.
		slog.Warn("media: no media store configured, temp files will be deleted", "agent", l.id)
		defer func() {
			for _, f := range files {
				_ = os.Remove(f.Path)
			}
		}()
		return nil
	}

	var refs []providers.MediaRef
	for _, f := range files {
		mime := f.MimeType
		if mime == "" {
			mime = mimeFromExt(filepath.Ext(f.Path)) // fallback for legacy callers
		}
		kind := mediaKindFromMime(mime)

		// Sanitize images before persistent storage.
		srcPath := f.Path
		if kind == "image" {
			sanitized, err := SanitizeImage(f.Path)
			if err != nil {
				slog.Warn("media: sanitize image failed, using original", "path", f.Path, "error", err)
			} else {
				srcPath = sanitized
				mime = "image/jpeg" // sanitized output is always JPEG
				// Clean up original if sanitized to a different file.
				if sanitized != f.Path {
					_ = os.Remove(f.Path)
				}
			}
		}

		id, _, err := l.mediaStore.SaveFile(sessionKey, srcPath, mime)
		if err != nil {
			slog.Warn("media: failed to persist file", "path", f.Path, "error", err)
			_ = os.Remove(srcPath)
			continue
		}

		refs = append(refs, providers.MediaRef{
			ID:       id,
			MimeType: mime,
			Kind:     kind,
		})
		slog.Debug("media: persisted file", "id", id, "kind", kind, "mime", mime, "agent", l.id)
	}
	return refs
}

// enrichDocumentPaths updates the last user message to include persisted file paths
// in <media:document> tags. This allows skills (e.g. pdf skill via exec) to access
// the file directly, matching how Claude Code skills work with file paths.
func (l *Loop) enrichDocumentPaths(messages []providers.Message, refs []providers.MediaRef) {
	if l.mediaStore == nil || len(messages) == 0 {
		return
	}
	// Find last user message
	lastIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastIdx = i
			break
		}
	}
	if lastIdx < 0 {
		return
	}

	content := messages[lastIdx].Content
	for _, ref := range refs {
		if ref.Kind != "document" {
			continue
		}
		p, err := l.mediaStore.LoadPath(ref.ID)
		if err != nil {
			continue
		}
		// Replace <media:document> or <media:document name="X"> with version that includes path.
		// The hint tells the agent the file is directly accessible (no copy needed).
		pathAttr := fmt.Sprintf(" path=%q", p)
		old1 := "<media:document>"
		new1 := "<media:document" + pathAttr + ">"
		if strings.Contains(content, old1) {
			content = strings.Replace(content, old1, new1, 1)
			continue
		}
		// For named variant, inject path attribute
		if idx := strings.Index(content, "<media:document name="); idx >= 0 {
			closeIdx := strings.Index(content[idx:], ">")
			if closeIdx >= 0 {
				tag := content[idx : idx+closeIdx]
				content = strings.Replace(content, tag+">", tag+pathAttr+">", 1)
			}
		}
		// For Slack variant with file= attribute
		if idx := strings.Index(content, "<media:document file="); idx >= 0 {
			closeIdx := strings.Index(content[idx:], ">")
			if closeIdx >= 0 {
				tag := content[idx : idx+closeIdx]
				content = strings.Replace(content, tag+">", tag+pathAttr+">", 1)
			}
		}
	}
	messages[lastIdx].Content = content
}

// enrichAudioIDs updates the last user message to embed persisted media IDs
// in <media:audio> and <media:voice> tags so the LLM can reference them.
// Without this, the LLM sees plain <media:audio> and cannot pass a valid media_id.
func (l *Loop) enrichAudioIDs(messages []providers.Message, refs []providers.MediaRef) {
	if len(messages) == 0 {
		return
	}
	lastIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastIdx = i
			break
		}
	}
	if lastIdx < 0 {
		return
	}

	content := messages[lastIdx].Content
	for _, ref := range refs {
		if ref.Kind != "audio" {
			continue
		}
		idAttr := fmt.Sprintf(" id=%q", ref.ID)

		// Replace bare <media:audio> with <media:audio id="uuid">
		bare := "<media:audio>"
		if strings.Contains(content, bare) {
			content = strings.Replace(content, bare, "<media:audio"+idAttr+">", 1)
			continue
		}
		// Replace bare <media:voice> with <media:voice id="uuid">
		bareVoice := "<media:voice>"
		if strings.Contains(content, bareVoice) {
			content = strings.Replace(content, bareVoice, "<media:voice"+idAttr+">", 1)
			continue
		}
	}
	messages[lastIdx].Content = content
}

// mediaKindFromMime returns the media kind ("image", "video", "audio", "document")
// based on MIME type prefix.
func mediaKindFromMime(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	default:
		return "document"
	}
}

// maxMediaReloadMessages is the default number of recent messages with image MediaRefs
// to reload for LLM vision context.
const maxMediaReloadMessages = 5

// reloadMediaForMessages populates Images on historical messages that have image MediaRefs.
// Only reloads the last maxMessages messages with image refs (newest first) to limit context usage.
func (l *Loop) reloadMediaForMessages(msgs []providers.Message, maxMessages int) {
	if l.mediaStore == nil || maxMessages <= 0 {
		return
	}

	count := 0
	for i := len(msgs) - 1; i >= 0 && count < maxMessages; i-- {
		if len(msgs[i].MediaRefs) == 0 || len(msgs[i].Images) > 0 {
			continue // skip if no refs or already loaded
		}

		hasImageRef := false
		var imageFiles []bus.MediaFile
		for _, ref := range msgs[i].MediaRefs {
			if ref.Kind != "image" {
				continue
			}
			hasImageRef = true
			p, err := l.mediaStore.LoadPath(ref.ID)
			if err != nil {
				slog.Debug("media: reload skip missing file", "id", ref.ID, "error", err)
				continue
			}
			imageFiles = append(imageFiles, bus.MediaFile{Path: p, MimeType: ref.MimeType})
		}

		if !hasImageRef {
			continue
		}
		count++

		if images := loadImages(imageFiles); len(images) > 0 {
			msgs[i].Images = images
			slog.Debug("media: reloaded images for historical message", "index", i, "count", len(images))
		}
	}
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
