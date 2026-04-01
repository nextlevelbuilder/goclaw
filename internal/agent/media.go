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
	// Verify .uploads is a real directory (not symlink) to prevent symlink-based attacks.
	// os.Lstat does NOT follow symlinks — rejects if attacker replaced .uploads with a symlink.
	if fi, err := os.Lstat(uploadsDir); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		slog.Warn("media: .uploads is a symlink, refusing to use", "dir", uploadsDir)
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
		pathAttr := fmt.Sprintf(" path=%q", p)

		// Match first <media:document> without a path — covers bare, named, and file= variants.
		content, _ = replaceFirstMediaTag(content, "<media:document", func(tag string) bool {
			return !tagHasAttr(tag, "path")
		}, func(tag string) string {
			return appendTagAttrs(tag, pathAttr)
		})
	}
	messages[lastIdx].Content = content
}

// enrichAudioIDs updates the last user message to embed persisted media IDs
// in <media:audio> and <media:voice> tags so the LLM can reference them.
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

		var replaced bool
		content, replaced = replaceFirstMediaTag(content, "<media:audio", func(tag string) bool {
			return !tagHasAttr(tag, "id")
		}, func(tag string) string {
			return appendTagAttrs(tag, idAttr)
		})
		if replaced {
			continue
		}

		content, _ = replaceFirstMediaTag(content, "<media:voice", func(tag string) bool {
			return !tagHasAttr(tag, "id")
		}, func(tag string) string {
			return appendTagAttrs(tag, idAttr)
		})
	}
	messages[lastIdx].Content = content
}

// enrichVideoIDs updates the last user message to embed persisted media IDs
// in <media:video> tags so the LLM can reference them via read_video tool.
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

		content, _ = replaceFirstMediaTag(content, "<media:video", func(tag string) bool {
			return !tagHasAttr(tag, "id")
		}, func(tag string) string {
			return appendTagAttrs(tag, idAttr)
		})
	}
	messages[lastIdx].Content = content
}

// enrichImageIDs updates the last user message to embed persisted media IDs
// and file paths in <media:image> tags so the LLM knows images were received
// and stored. The path attribute allows tools called via MCP bridge (e.g.
// claude-cli) to access images via read_image(path=...) even though the
// bridge context does not carry WithMediaImages.
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
	for _, ref := range refs {
		if ref.Kind != "image" {
			continue
		}
		idAttr := fmt.Sprintf(" id=%q", ref.ID)
		pathAttr := ""
		if ref.Path != "" {
			pathAttr = fmt.Sprintf(" path=%q", ref.Path)
		}

		content, _ = replaceFirstMediaTag(content, "<media:image", func(tag string) bool {
			return !tagHasAttr(tag, "id")
		}, func(tag string) string {
			attrs := []string{idAttr}
			if pathAttr != "" {
				attrs = append(attrs, pathAttr)
			}
			return appendTagAttrs(tag, attrs...)
		})
	}
	messages[lastIdx].Content = content
}

// enrichImagePaths updates ALL user messages to include persisted file paths
// in <media:image> tags. This enables the LLM to call read_image(path=...)
// to analyze images without inline base64 (saving context tokens).
// Unlike enrichImageIDs (last user message only), this enriches ALL messages
// so historical images from prior turns are also accessible via file path.
func (l *Loop) enrichImagePaths(messages []providers.Message) {
	if l.mediaStore == nil {
		return
	}
	for i := range messages {
		if messages[i].Role != "user" || len(messages[i].MediaRefs) == 0 {
			continue
		}
		content := messages[i].Content
		changed := false
		for _, ref := range messages[i].MediaRefs {
			if ref.Kind != "image" {
				continue
			}
			p := ref.Path
			if p == "" {
				var err error
				p, err = l.mediaStore.LoadPath(ref.ID)
				if err != nil {
					continue
				}
			}
			if p == "" {
				continue
			}
			pathAttr := fmt.Sprintf(" path=%q", p)

			// Prefer tags that already carry the matching media ID.
			var replaced bool
			content, replaced = replaceFirstMediaTag(content, "<media:image", func(tag string) bool {
				return tagHasAttrValue(tag, "id", ref.ID) && !tagHasAttr(tag, "path")
			}, func(tag string) string {
				return appendTagAttrs(tag, pathAttr)
			})
			if replaced {
				changed = true
				continue
			}

			// Fallback: first image tag without an ID — attach both id and path.
			content, replaced = replaceFirstMediaTag(content, "<media:image", func(tag string) bool {
				return !tagHasAttr(tag, "id")
			}, func(tag string) string {
				return appendTagAttrs(tag, fmt.Sprintf(` id=%q`, ref.ID), pathAttr)
			})
			if replaced {
				changed = true
			}
		}
		if changed {
			messages[i].Content = content
		}
	}
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

// replaceFirstMediaTag finds the first tag in content starting with prefix
// whose full text satisfies match, and replaces it using replace.
// Forward scanning ensures natural positional pairing when iterating refs in order.
func replaceFirstMediaTag(content, prefix string, match func(tag string) bool, replace func(tag string) string) (string, bool) {
	pos := 0
	for pos < len(content) {
		idx := strings.Index(content[pos:], prefix)
		if idx < 0 {
			return content, false
		}
		idx += pos
		endRel := strings.IndexByte(content[idx:], '>')
		if endRel < 0 {
			return content, false
		}
		end := idx + endRel + 1
		tag := content[idx:end]
		if match(tag) {
			return content[:idx] + replace(tag) + content[end:], true
		}
		pos = end
	}
	return content, false
}

func tagHasAttr(tag, attr string) bool {
	return strings.Contains(tag, " "+attr+"=")
}

func tagHasAttrValue(tag, attr, value string) bool {
	return strings.Contains(tag, fmt.Sprintf(` %s=%q`, attr, value))
}

func appendTagAttrs(tag string, attrs ...string) string {
	if len(attrs) == 0 {
		return tag
	}
	return strings.TrimSuffix(tag, ">") + strings.Join(attrs, "") + ">"
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
