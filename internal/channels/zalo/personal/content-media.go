package personal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// mediaFetcher downloads one URL and returns a local path. Tests inject a fake.
type mediaFetcher func(context.Context, string) (string, error)

// extractContentAndMedia routes a non-text payload by msgType. Every branch
// returns a non-empty description so the model always has something to reason
// about, even for unsupported payloads.
func extractContentAndMedia(content protocol.Content, msgType, notify string) (string, []string) {
	return extractContentAndMediaWithFetcher(content, msgType, notify, downloadFile)
}

func extractContentAndMediaWithFetcher(content protocol.Content, msgType, notify string, fetch mediaFetcher) (string, []string) {
	if text := content.Text(); text != "" {
		return text, nil
	}

	att := content.ParseAttachment()
	if att == nil {
		switch msgType {
		case "chat.sticker":
			return "[sticker]", nil
		case "chat.voice":
			return "[voice]", nil
		case "chat.video", "chat.video.msg":
			return "[video]", nil
		case "share.link", "chat.link":
			return "[link]", nil
		case "chat.location.new":
			return "[location]", nil
		case "chat.poll", "chat.todo", "chat.card", "chat.bankcard",
			"chat.doodle", "chat.gif", "chat.recommended", "share.file":
			return formatTaggedJSON(content, msgType), nil
		}
		return describeUnknown(content, msgType, notify), nil
	}

	switch msgType {
	case "chat.photo", "":
		if att.IsImage() || att.URLFor(msgType) != "" {
			return downloadPhoto(att, msgType, fetch)
		}
	case "chat.sticker":
		return formatSticker(att), nil
	case "chat.voice":
		return fmt.Sprintf("[voice %ss]", formatDuration(att.Duration)), nil
	case "chat.video", "chat.video.msg":
		text := fmt.Sprintf("[video %ss]", formatDuration(att.Duration))
		if thumb := firstNonEmpty(att.ThumbURL, att.Thumb); thumb != "" {
			text += "\n[thumbnail] " + thumb
		}
		return text, nil
	case "share.link", "chat.link":
		return formatLink(att), nil
	case "chat.location.new":
		return fmt.Sprintf("[location %s,%s] %s", formatFloat(att.Lat), formatFloat(att.Lng), strings.TrimSpace(att.Desc)), nil
	case "chat.poll", "chat.todo", "chat.card", "chat.bankcard",
		"chat.doodle", "chat.gif", "chat.recommended", "share.file":
		return formatTaggedJSON(content, msgType), nil
	}

	if notify = strings.TrimSpace(notify); notify != "" {
		return notify, nil
	}
	return describeUnknown(content, msgType, notify), nil
}

func downloadPhoto(att *protocol.Attachment, msgType string, fetch mediaFetcher) (string, []string) {
	url := att.URLFor(msgType)
	if url == "" {
		return contentFallback(att), nil
	}
	filePath, err := fetch(context.Background(), url)
	if err != nil {
		slog.Warn("zalo_personal: failed to download attachment", "url", url, "error", err)
		return contentFallback(att), nil
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

func (c *Channel) extractContentAndMedia(content protocol.Content, msgType, notify string) (string, []string) {
	return extractContentAndMediaWithFetcher(content, msgType, notify, c.downloadFile)
}

func cacheBodyForContent(content protocol.Content, msgType, notify string) string {
	if text := strings.TrimSpace(content.Text()); text != "" {
		return text
	}
	att := content.ParseAttachment()
	if att == nil {
		return describeUnknown(content, msgType, notify)
	}
	switch msgType {
	case "chat.sticker":
		return formatSticker(att)
	case "chat.voice":
		return fmt.Sprintf("[voice %ss]", formatDuration(att.Duration))
	case "chat.video", "chat.video.msg":
		return fmt.Sprintf("[video %ss]", formatDuration(att.Duration))
	case "share.link", "chat.link":
		return formatLink(att)
	case "chat.location.new":
		return fmt.Sprintf("[location %s,%s] %s", formatFloat(att.Lat), formatFloat(att.Lng), strings.TrimSpace(att.Desc))
	case "chat.poll", "chat.todo", "chat.card", "chat.bankcard",
		"chat.doodle", "chat.gif", "chat.recommended", "share.file":
		return formatTaggedJSON(content, msgType)
	}
	if notify = strings.TrimSpace(notify); notify != "" {
		return notify
	}
	return contentFallback(att)
}

func formatSticker(att *protocol.Attachment) string {
	id := firstNonEmpty(att.StickerID, att.ID)
	if id == "" {
		return "[sticker]"
	}
	return "[sticker id=" + id + "]"
}

func formatLink(att *protocol.Attachment) string {
	url := firstNonEmpty(att.Href, att.OriURL, att.NormalURL)
	if url == "" {
		return "[link]"
	}
	return "[link] " + url
}

func formatTaggedJSON(content protocol.Content, msgType string) string {
	raw := compactJSON(content.Raw)
	if raw == "" {
		return "[zalo:" + msgType + "]"
	}
	return "[zalo:" + msgType + "] " + raw
}

func describeUnknown(content protocol.Content, msgType, notify string) string {
	if notify = strings.TrimSpace(notify); notify != "" {
		return notify
	}
	if msgType == "" {
		return contentFallback(content.ParseAttachment())
	}
	if raw := compactJSON(content.Raw); raw != "" {
		return "[unsupported: " + msgType + "] " + raw
	}
	return "[unsupported: " + msgType + "]"
}

func contentFallback(att *protocol.Attachment) string {
	if att == nil {
		return "[User sent a non-text message]"
	}
	if att.IsImage() {
		if title := strings.TrimSpace(att.Title); title != "" {
			return "[User sent an image: " + title + "]"
		}
		return "[User sent an image]"
	}
	if title := strings.TrimSpace(att.Title); title != "" {
		return "[User sent a file: " + title + "]"
	}
	return "[User sent a file]"
}

func compactJSON(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return ""
	}
	out := buf.String()
	const maxJSONBytes = 512
	if len(out) > maxJSONBytes {
		out = out[:maxJSONBytes]
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func formatDuration(d float64) string {
	return strconv.FormatFloat(d, 'f', -1, 64)
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func displayNameOrID(displayName, id string) string {
	if strings.TrimSpace(displayName) != "" {
		return displayName
	}
	return id
}

const maxMediaBytes = 20 * 1024 * 1024

// checkMediaURL is a package var so tests can exercise the download path with
// an httptest server (which binds a private loopback address).
var checkMediaURL = tools.CheckSSRF

// downloadFile is the unauthenticated fallback used by legacy tests/helpers.
func downloadFile(ctx context.Context, fileURL string) (string, error) {
	return downloadFileWithClient(ctx, &http.Client{Timeout: 30 * time.Second}, "", maxMediaBytes, fileURL)
}

// downloadFile downloads inbound media with the authenticated session client so
// Zalo CDN cookies and the required Referer header are present.
func (c *Channel) downloadFile(ctx context.Context, fileURL string) (string, error) {
	sess := c.session()
	if sess == nil || sess.Client == nil {
		return "", fmt.Errorf("zalo_personal: not connected")
	}
	maxBytes := int64(c.config.EffectiveMediaMaxMB()) * 1024 * 1024
	return downloadFileWithClient(ctx, sess.Client, sess.UserAgent, maxBytes, fileURL)
}

func downloadFileWithClient(ctx context.Context, client *http.Client, userAgent string, maxBytes int64, fileURL string) (string, error) {
	if err := checkMediaURL(fileURL); err != nil {
		return "", fmt.Errorf("ssrf check: %w", err)
	}
	if client == nil {
		return "", fmt.Errorf("download: nil client")
	}
	if maxBytes <= 0 {
		maxBytes = maxMediaBytes
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	req.Header.Set("Referer", "https://chat.zalo.me/")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download status %d", resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return "", fmt.Errorf("file too large: %d bytes (max %d)", resp.ContentLength, maxBytes)
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

	written, err := io.Copy(tmpFile, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("save: %w", err)
	}
	if written > maxBytes {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("file too large: %d bytes (max %d)", written, maxBytes)
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
	return extractQuoteMediaWithFetcher(quote, downloadFile)
}

func extractQuoteMediaWithFetcher(quote *protocol.TQuote, fetch mediaFetcher) []string {
	if quote == nil {
		return nil
	}
	att := quote.Attach.ParseAttachment()
	if att == nil || !att.IsImage() || att.URL() == "" {
		return nil
	}
	filePath, err := fetch(context.Background(), att.URL())
	if err != nil {
		slog.Warn("zalo_personal: failed to download quoted attachment", "url", att.URL(), "error", err)
		return nil
	}
	return []string{filePath}
}

func (c *Channel) extractQuoteMedia(quote *protocol.TQuote) []string {
	return extractQuoteMediaWithFetcher(quote, c.downloadFile)
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
