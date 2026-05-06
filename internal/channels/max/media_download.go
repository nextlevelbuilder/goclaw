package max

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// downloadDefaults bound media downloads to prevent abuse and tame slow
// origins. They are not configurable per-instance in Day 4 — promote to
// instanceConfig if production traffic shows we need finer tuning.
const (
	// downloadMaxBytes caps any single inbound media file. 25 MiB matches
	// the largest practical agent input today (long voice messages, large
	// images). Files exceeding this are reported as media errors but the
	// surrounding text is still delivered to the agent.
	downloadMaxBytes int64 = 25 << 20

	// downloadTimeout bounds an individual file download. Long-poll
	// receives a fresh batch every ~30s, so 60s headroom is plenty.
	downloadTimeout = 60 * time.Second

	// downloadMaxRetries is the number of attempts for transient
	// (network/5xx) errors. Constant backoff between retries.
	downloadMaxRetries = 3
)

// errMediaTooLarge is returned when a file exceeds downloadMaxBytes. It is
// distinguishable so callers can surface a friendlier message to the agent
// (vs. a generic "download failed").
var errMediaTooLarge = errors.New("max: media file too large")

// downloadInboundMedia fetches files for image/video/audio/file/sticker
// attachments. Returns the local file paths in the same order as the input
// attachments (skipping non-media types and failed downloads).
//
// Failures are logged and continue — partial success is preferable to
// silently dropping the entire message. The agent will still receive the
// text content and any successfully-downloaded files.
//
// Files are written to os.TempDir() with prefix "goclaw_max_*" matching
// other goclaw channels. Ownership transfers to BaseChannel.HandleMessage,
// which is responsible for cleanup downstream.
func (c *Channel) downloadInboundMedia(ctx context.Context, atts []Attachment) []string {
	if len(atts) == 0 {
		return nil
	}

	var paths []string
	for i, a := range atts {
		switch a.Type {
		case AttachmentTypeImage, AttachmentTypeVideo, AttachmentTypeAudio,
			AttachmentTypeFile, AttachmentTypeSticker:
			path, err := c.downloadOneAttachment(ctx, a)
			if err != nil {
				if errors.Is(err, errMediaTooLarge) {
					slog.Warn("max: media too large, skipping",
						"channel", c.Name(),
						"index", i,
						"type", a.Type,
						"limit_bytes", downloadMaxBytes)
				} else {
					slog.Warn("max: media download failed",
						"channel", c.Name(),
						"index", i,
						"type", a.Type,
						"error", err)
				}
				continue
			}
			paths = append(paths, path)

		case AttachmentTypeContact, AttachmentTypeShare,
			AttachmentTypeLocation, AttachmentTypeInlineKeyboard:
			// Non-file attachments — content is already in the metadata
			// or encoded in the message text. Skip silently.

		default:
			slog.Debug("max: unknown attachment type",
				"channel", c.Name(), "type", a.Type)
		}
	}
	return paths
}

// downloadOneAttachment fetches a single attachment URL to a temp file.
// Validates URL presence, applies size limit during streaming copy, and
// retries transient errors (network, 5xx) up to downloadMaxRetries.
//
// On any error, the partial temp file is cleaned up before returning.
func (c *Channel) downloadOneAttachment(ctx context.Context, a Attachment) (string, error) {
	rawURL := a.Payload.URL
	if rawURL == "" {
		return "", errors.New("attachment has no URL")
	}

	ext := guessExtension(a, rawURL)

	var lastErr error
	for attempt := 1; attempt <= downloadMaxRetries; attempt++ {
		path, err := c.fetchToTempFile(ctx, rawURL, ext, a.Type)
		if err == nil {
			return path, nil
		}

		// Don't retry size-limit errors — re-downloading won't help.
		if errors.Is(err, errMediaTooLarge) {
			return "", err
		}

		// Don't retry on context cancellation.
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		lastErr = err
		if attempt < downloadMaxRetries {
			slog.Debug("max: retrying media download",
				"attempt", attempt, "url_host", urlHost(rawURL), "error", err)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
	}
	return "", fmt.Errorf("after %d attempts: %w", downloadMaxRetries, lastErr)
}

// fetchToTempFile performs one HTTP GET attempt with size limit and timeout.
//
// Single attempt — caller is responsible for retry logic.
//
// Returned path is owned by the caller (must be cleaned up downstream).
// On error, the temp file is removed before returning — no partial files.
func (c *Channel) fetchToTempFile(ctx context.Context, rawURL, ext, mediaType string) (string, error) {
	dlCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	resp, err := c.client.DownloadFile(dlCtx, rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}

	// Reject obviously-too-large responses up front via Content-Length.
	if resp.ContentLength > downloadMaxBytes {
		return "", fmt.Errorf("%w: %d bytes", errMediaTooLarge, resp.ContentLength)
	}

	tmp, err := os.CreateTemp("", "goclaw_max_"+sanitizeMediaType(mediaType)+"_*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	// Copy with a hard cap (one extra byte to detect overrun).
	written, copyErr := io.Copy(tmp, io.LimitReader(resp.Body, downloadMaxBytes+1))
	closeErr := tmp.Close()

	if copyErr != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("copy: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("close: %w", closeErr)
	}
	if written > downloadMaxBytes {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("%w: %d bytes (limit %d)",
			errMediaTooLarge, written, downloadMaxBytes)
	}
	if written == 0 {
		_ = os.Remove(tmp.Name())
		return "", errors.New("empty response body")
	}

	return tmp.Name(), nil
}

// guessExtension picks a file extension for a downloaded media file based on
// the attachment type, payload metadata (filename for files), and URL hint.
//
// Returns "" if no good guess is available — the temp file will then have
// no extension, which is acceptable but reduces downstream tool quality.
func guessExtension(a Attachment, rawURL string) string {
	// Best signal: explicit filename in payload (typical for "file" type).
	if a.Payload.Filename != "" {
		ext := filepath.Ext(a.Payload.Filename)
		if ext != "" {
			return sanitizeExt(ext)
		}
	}

	// URL path hint: Max upload URLs sometimes include extensions.
	if i := strings.LastIndex(rawURL, "."); i > 0 {
		// Strip query string from extension candidate.
		ext := rawURL[i:]
		if q := strings.IndexAny(ext, "?#"); q > 0 {
			ext = ext[:q]
		}
		if len(ext) >= 2 && len(ext) <= 6 && !strings.ContainsAny(ext[1:], "/\\") {
			return sanitizeExt(ext)
		}
	}

	// Type-based fallback. Best-effort — actual format unknown without
	// content sniffing.
	switch a.Type {
	case AttachmentTypeImage:
		return ".jpg"
	case AttachmentTypeVideo:
		return ".mp4"
	case AttachmentTypeAudio:
		return ".ogg"
	case AttachmentTypeSticker:
		return ".webp"
	}
	return ""
}

// sanitizeExt returns ext lowercased with non-alphanumerics stripped (except
// the leading dot). Defends against path tricks if a hostile filename ever
// reaches us.
func sanitizeExt(ext string) string {
	if ext == "" || ext[0] != '.' {
		return ""
	}
	out := []byte{'.'}
	for i := 1; i < len(ext) && i < 6; i++ {
		ch := ext[i]
		switch {
		case ch >= 'a' && ch <= 'z',
			ch >= '0' && ch <= '9':
			out = append(out, ch)
		case ch >= 'A' && ch <= 'Z':
			out = append(out, ch+32) // lowercase
		}
	}
	if len(out) < 2 {
		return ""
	}
	return string(out)
}

// sanitizeMediaType returns a temp-filename-safe form of an attachment type.
// Used as a hint in the temp filename for easier debugging.
func sanitizeMediaType(t string) string {
	switch t {
	case AttachmentTypeImage, AttachmentTypeVideo, AttachmentTypeAudio,
		AttachmentTypeFile, AttachmentTypeSticker:
		return t
	}
	return "media"
}

// urlHost returns the host part of a URL for log lines, or "?" if parse fails.
// Used to avoid leaking signed query parameters in logs.
func urlHost(rawURL string) string {
	idx := strings.Index(rawURL, "://")
	if idx < 0 {
		return "?"
	}
	rest := rawURL[idx+3:]
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		return rest[:i]
	}
	return rest
}
