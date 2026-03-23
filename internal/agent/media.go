package agent

import (
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/media"
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

// persistMedia sanitizes images, saves all media files to the per-user workspace
// .uploads/ directory, and returns lightweight MediaRefs with persisted paths.
// All media types (images, documents, audio, video) are stored within the user's
// workspace for filesystem-level tenant isolation.
// workspace is the per-user workspace path from ToolWorkspaceFromCtx(ctx).
func (l *Loop) persistMedia(sessionKey string, files []bus.MediaFile, workspace string) []providers.MediaRef {
	if workspace == "" {
		slog.Warn("media: no workspace, cannot persist media")
		return nil
	}

	uploadsDir := filepath.Join(workspace, ".uploads")
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		slog.Warn("media: failed to create .uploads dir", "dir", uploadsDir, "error", err)
		return nil
	}

	var refs []providers.MediaRef
	for _, f := range files {
		mime := f.MimeType
		if mime == "" {
			mime = mimeFromExt(filepath.Ext(f.Path))
		}
		kind := mediaKindFromMime(mime)

		// Sanitize images before persistent storage.
		srcPath := f.Path
		var sanitizedTemp string // track temp file for cleanup
		if kind == "image" {
			sanitized, err := SanitizeImage(f.Path)
			if err != nil {
				slog.Warn("media: sanitize image failed, using original", "path", f.Path, "error", err)
			} else {
				srcPath = sanitized
				sanitizedTemp = sanitized
				mime = "image/jpeg" // sanitized output is always JPEG
			}
		}

		id := uuid.New().String()
		ext := media.ExtFromMime(mime)
		if ext == "" {
			ext = filepath.Ext(srcPath) // fallback to source extension
		}
		dstPath := filepath.Join(uploadsDir, id+ext)

		if err := copyMediaFile(srcPath, dstPath); err != nil {
			slog.Warn("media: failed to persist file", "path", f.Path, "error", err)
			if sanitizedTemp != "" {
				os.Remove(sanitizedTemp)
			}
			continue
		}
		if sanitizedTemp != "" {
			os.Remove(sanitizedTemp) // cleanup sanitized temp file
		}

		refs = append(refs, providers.MediaRef{
			ID:       id,
			MimeType: mime,
			Kind:     kind,
			Path:     dstPath,
		})
		slog.Debug("media: persisted file", "id", id, "kind", kind, "path", dstPath, "agent", l.id)
	}
	return refs
}

// copyMediaFile copies src to dst using buffered I/O.
// Removes partial dst file on failure.
func copyMediaFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst) // cleanup partial file
		return err
	}
	return out.Close()
}

// enrichDocumentPaths updates the last user message to include persisted file paths
// in <media:document> tags. This allows skills (e.g. pdf skill via exec) to access
// the file directly, matching how Claude Code skills work with file paths.
func (l *Loop) enrichDocumentPaths(messages []providers.Message, refs []providers.MediaRef) {
	if len(messages) == 0 {
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
		// Use persisted workspace path; fall back to legacy .media/ lookup.
		p := ref.Path
		if p == "" && l.mediaStore != nil {
			var err error
			p, err = l.mediaStore.LoadPath(ref.ID)
			if err != nil {
				continue
			}
		}
		if p == "" {
			continue
		}
		// Replace <media:document> or <media:document name="X"> with version that includes path.
		// The hint tells the agent the file is directly accessible (no copy needed).
		pathAttr := fmt.Sprintf(" path=%q", p)
		old1 := "<media:document>"
		new1 := "<media:document" + pathAttr + ">"
		// Replace the LAST bare tag (current message, not group history).
		if idx := strings.LastIndex(content, old1); idx >= 0 {
			content = content[:idx] + new1 + content[idx+len(old1):]
			continue
		}
		// For named variant, inject path attribute (last occurrence)
		if idx := strings.LastIndex(content, "<media:document name="); idx >= 0 {
			closeIdx := strings.Index(content[idx:], ">")
			if closeIdx >= 0 {
				tag := content[idx : idx+closeIdx]
				content = content[:idx] + tag + pathAttr + ">" + content[idx+closeIdx+1:]
			}
		}
		// For Slack variant with file= attribute (last occurrence)
		if idx := strings.LastIndex(content, "<media:document file="); idx >= 0 {
			closeIdx := strings.Index(content[idx:], ">")
			if closeIdx >= 0 {
				tag := content[idx : idx+closeIdx]
				content = content[:idx] + tag + pathAttr + ">" + content[idx+closeIdx+1:]
			}
		}
	}
	messages[lastIdx].Content = content
}

// enrichAudioIDs updates the last user message to embed persisted media IDs
// in <media:audio> and <media:voice> tags so the LLM can reference them.
// Without this, the LLM sees plain <media:audio> and cannot pass a valid media_id.
// Replaces the LAST bare tag (current message) rather than the first (which may be
// in group history context), so the current turn's media gets the correct ID.
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

		// Replace the LAST bare <media:audio> with <media:audio id="uuid">
		bare := "<media:audio>"
		if idx := strings.LastIndex(content, bare); idx >= 0 {
			content = content[:idx] + "<media:audio" + idAttr + ">" + content[idx+len(bare):]
			continue
		}
		// Replace the LAST bare <media:voice> with <media:voice id="uuid">
		bareVoice := "<media:voice>"
		if idx := strings.LastIndex(content, bareVoice); idx >= 0 {
			content = content[:idx] + "<media:voice" + idAttr + ">" + content[idx+len(bareVoice):]
			continue
		}
	}
	messages[lastIdx].Content = content
}

// enrichVideoIDs updates the last user message to embed persisted media IDs
// in <media:video> tags so the LLM can reference them via read_video tool.
// Without this, the LLM sees plain <media:video> and hallucinates a media_id.
// Replaces the LAST bare tag (current message) rather than the first (which may be
// in group history context), so the current turn's media gets the correct ID.
func (l *Loop) enrichVideoIDs(messages []providers.Message, refs []providers.MediaRef) {
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
		if ref.Kind != "video" {
			continue
		}
		idAttr := fmt.Sprintf(" id=%q", ref.ID)

		// Replace the LAST bare <media:video> with <media:video id="uuid">
		bare := "<media:video>"
		if idx := strings.LastIndex(content, bare); idx >= 0 {
			content = content[:idx] + "<media:video" + idAttr + ">" + content[idx+len(bare):]
			continue
		}
	}
	messages[lastIdx].Content = content
}

// enrichImageIDs updates the last user message to embed persisted media IDs
// in <media:image> tags so the LLM knows images were received and stored.
// Without this, the LLM sees plain <media:image> and cannot confirm image availability.
// Iterates refs in reverse order so that when multiple images are present,
// each ref maps to the correct positional tag (last ref → last tag, etc.).
func (l *Loop) enrichImageIDs(messages []providers.Message, refs []providers.MediaRef) {
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
	// Iterate in reverse so first ref matches first tag when using LastIndex replacements.
	for i := len(refs) - 1; i >= 0; i-- {
		ref := refs[i]
		if ref.Kind != "image" {
			continue
		}
		idAttr := fmt.Sprintf(" id=%q", ref.ID)

		// Replace the LAST bare <media:image> with <media:image id="uuid">
		bare := "<media:image>"
		if idx := strings.LastIndex(content, bare); idx >= 0 {
			content = content[:idx] + "<media:image" + idAttr + ">" + content[idx+len(bare):]
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
	if maxMessages <= 0 {
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
			p := ref.Path
			if p == "" && l.mediaStore != nil {
				var err error
				p, err = l.mediaStore.LoadPath(ref.ID)
				if err != nil {
					slog.Debug("media: reload skip missing file", "id", ref.ID, "error", err)
					continue
				}
			}
			if p == "" {
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
