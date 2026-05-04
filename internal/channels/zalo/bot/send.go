package bot

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

const maxMediaBytes = 10 * 1024 * 1024 // 10MB

// isHTTPURL gates sendPhoto inputs — Zalo Bot's sendPhoto only accepts
// remote URLs.
func isHTTPURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

// mergeTrailingText joins caption + content with a blank line.
func mergeTrailingText(caption, content string) string {
	caption = strings.TrimSpace(caption)
	content = strings.TrimSpace(content)
	switch {
	case caption == "" && content == "":
		return ""
	case caption == "":
		return content
	case content == "":
		return caption
	default:
		return caption + "\n\n" + content
	}
}

func (c *Channel) sendChunkedText(chatID, text string) error {
	for _, chunk := range channels.ChunkMarkdown(text, maxTextLength) {
		if err := c.sendMessage(chatID, chunk); err != nil {
			return err
		}
	}
	return nil
}

// downloadMedia fetches a photo from Zalo's CDN to a local temp file.
// PhotoURL originates in Zalo's getUpdates JSON (untrusted) — uses the
// SSRF-safe client to close the DNS-rebind window between CheckSSRF and
// the actual dial.
func (c *Channel) downloadMedia(url string) (string, error) {
	resp, err := c.mediaClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}

	ext := ".jpg"
	ct := resp.Header.Get("Content-Type")
	switch {
	case strings.Contains(ct, "png"):
		ext = ".png"
	case strings.Contains(ct, "gif"):
		ext = ".gif"
	case strings.Contains(ct, "webp"):
		ext = ".webp"
	}

	f, err := os.CreateTemp("", "goclaw_zalo_*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	defer f.Close()

	// cap+1 distinguishes fits from truncated; bare LimitReader chops silently.
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxMediaBytes+1))
	if err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("write: %w", err)
	}
	if n == 0 {
		os.Remove(f.Name())
		return "", fmt.Errorf("empty response")
	}
	if n > maxMediaBytes {
		os.Remove(f.Name())
		return "", fmt.Errorf("media exceeds %d byte cap", maxMediaBytes)
	}

	slog.Debug("zalo media downloaded", "path", f.Name(), "size", n)
	return f.Name(), nil
}
