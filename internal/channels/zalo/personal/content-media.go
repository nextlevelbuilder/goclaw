package personal

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// extractContentAndMedia returns text content with media tags plus local media paths.
func extractContentAndMedia(content protocol.Content) (string, []string) {
	if text := content.Text(); text != "" {
		return text, nil
	}
	att := content.ParseAttachment()
	if att == nil || att.URL() == "" {
		return "", nil
	}

	filePath, err := downloadFile(context.Background(), att.URL())
	if err != nil {
		slog.Warn("zalo_personal: failed to download attachment", "url", att.URL(), "error", err)
		if text := content.AttachmentText(); text != "" {
			return text, nil
		}
		return "", nil
	}

	mimeType := media.DetectMIMEType(filePath)
	mediaKind := media.MediaKindFromMime(mimeType)
	if mediaKind != media.TypeImage && att.IsImage() {
		mediaKind = media.TypeImage
	}

	tag := media.BuildMediaTags([]media.MediaInfo{{
		Type:        mediaKind,
		FilePath:    filePath,
		ContentType: mimeType,
		FileName:    att.Title,
	}})
	// Zalo photo messages carry the user's caption in the attachment title.
	// Keep it next to the media tag — dropping it loses the actual request text.
	if att.IsImage() {
		caption := strings.TrimSpace(att.Title)
		if caption == "" {
			caption = strings.TrimSpace(att.Description)
		}
		if caption != "" {
			tag = tag + "\n" + caption
		}
	}
	return tag, []string{filePath}
}

func cacheBodyForContent(content protocol.Content) string {
	if text := strings.TrimSpace(content.Text()); text != "" {
		return text
	}
	return content.AttachmentText()
}

func displayNameOrID(displayName, id string) string {
	if strings.TrimSpace(displayName) != "" {
		return displayName
	}
	return id
}

const maxMediaBytes = 20 * 1024 * 1024

// downloadFile validates URL safety and stores the attachment in a bounded temp file.
func downloadFile(ctx context.Context, fileURL string) (string, error) {
	if err := tools.CheckSSRF(fileURL); err != nil {
		return "", fmt.Errorf("ssrf check: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download status %d", resp.StatusCode)
	}

	path := fileURL
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	ext := filepath.Ext(path)
	if ext == "" || len(ext) > 5 {
		ext = ".bin"
	}

	tmpFile, err := os.CreateTemp("", "goclaw_zca_*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	defer tmpFile.Close()

	written, err := io.Copy(tmpFile, io.LimitReader(resp.Body, maxMediaBytes+1))
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("save: %w", err)
	}
	if written > maxMediaBytes {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("file too large: %d bytes (max %d)", written, maxMediaBytes)
	}
	return fixDownloadedExt(tmpFile.Name()), nil
}

// fixDownloadedExt renames a downloaded temp file to match its sniffed content
// type when the URL-derived extension misclassifies it. Zalo CDN URLs often
// carry no usable file extension, so downloads land as .bin — downstream media
// classification (persistMedia infers MIME from extension) would then treat an
// image as a document and the agent's vision input would silently skip it.
func fixDownloadedExt(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return path
	}
	buf := make([]byte, 512)
	n, _ := io.ReadFull(f, buf)
	f.Close()
	if n == 0 {
		return path
	}
	sniffed := http.DetectContentType(buf[:n])
	if !strings.HasPrefix(sniffed, "image/") {
		return path
	}
	if strings.HasPrefix(media.DetectMIMEType(path), "image/") {
		return path // extension already says image — nothing to fix
	}
	var ext string
	switch sniffed {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/gif":
		ext = ".gif"
	case "image/webp":
		ext = ".webp"
	default:
		return path
	}
	newPath := strings.TrimSuffix(path, filepath.Ext(path)) + ext
	if err := os.Rename(path, newPath); err != nil {
		return path
	}
	return newPath
}

// extractQuoteMedia downloads the image attached to a quoted message, so a
// reply like “make a poster from this photo” actually carries the photo.
// Non-image quotes (files, stickers) stay text-only placeholders.
func extractQuoteMedia(quote *protocol.TQuote) []string {
	if quote == nil {
		return nil
	}
	att := quote.Attach.ParseAttachment()
	if att == nil || !att.IsImage() || att.URL() == "" {
		return nil
	}
	filePath, err := downloadFile(context.Background(), att.URL())
	if err != nil {
		slog.Warn("zalo_personal: failed to download quoted attachment", "url", att.URL(), "error", err)
		return nil
	}
	return []string{filePath}
}

// buildQuoteMediaTag renders a bare <media:image> content tag for each image
// downloaded from a quoted message (extractQuoteMedia).
//
// Without this, a quote-forwarded image has NO reference anywhere in the
// text content — it's only reachable through the Media list, which lets the
// model see it via vision but gives it no path to hand back to
// message(MEDIA:<path>) to forward the file elsewhere. The later agent-side
// enrichment (enrichImageIDs/enrichImagePaths in internal/agent/media.go)
// only fills in id/path attributes on a bare tag it finds already present in
// content — it has nothing to enrich if no tag exists at all, which was the
// actual bug (the model reported "no direct file/path" even though the file
// was sitting on disk). Order matters: this must be appended to content
// AFTER any tag from the current message's own attachment, matching the
// order paths are appended in the media slice (own attachment first, quoted
// image second), so positional id/path enrichment lines up correctly.
func buildQuoteMediaTag(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	tags := make([]string, len(paths))
	for i := range paths {
		tags[i] = "<media:image>"
	}
	return strings.Join(tags, "\n")
}
