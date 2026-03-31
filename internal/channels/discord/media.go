package discord

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
)

const (
	// defaultMediaMaxBytes is the default max media download size (25MB, Discord default).
	defaultMediaMaxBytes int64 = 25 * 1024 * 1024

	// downloadMaxRetries is the number of download retry attempts.
	downloadMaxRetries = 3
)

// resolveMedia downloads Discord attachments and returns MediaInfo for each.
func resolveMedia(attachments []*discordgo.MessageAttachment, maxBytes int64) []media.MediaInfo {
	if len(attachments) == 0 {
		return nil
	}
	if maxBytes <= 0 {
		maxBytes = defaultMediaMaxBytes
	}

	var results []media.MediaInfo
	for _, att := range attachments {
		if att.URL == "" {
			continue
		}

		// Skip oversized files before downloading.
		if int64(att.Size) > maxBytes {
			slog.Debug("discord: attachment too large, skipping",
				"filename", att.Filename, "size", att.Size, "max", maxBytes,
			)
			continue
		}

		filePath, err := downloadFromURL(att.URL, maxBytes)
		if err != nil {
			slog.Warn("discord: failed to download attachment",
				"filename", att.Filename, "url", att.URL, "error", err,
			)
			continue
		}

		ct := att.ContentType
		if ct == "" {
			ct = media.DetectMIMEType(att.Filename)
		}

		results = append(results, media.MediaInfo{
			Type:        classifyMediaType(ct, att.Filename),
			FilePath:    filePath,
			SourceURL:   att.URL,
			ContentType: ct,
			FileName:    att.Filename,
			FileSize:    int64(att.Size),
		})
	}
	return results
}

// downloadFromURL downloads a file from a URL with retry logic and saves to a temp file.
func downloadFromURL(url string, maxBytes int64) (string, error) {
	var resp *http.Response
	var err error

	for attempt := 1; attempt <= downloadMaxRetries; attempt++ {
		resp, err = http.Get(url) //nolint:gosec // Discord CDN URLs are trusted
		if err == nil {
			break
		}
		if attempt < downloadMaxRetries {
			slog.Debug("discord: retrying download", "url", url, "attempt", attempt, "error", err)
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}
	if err != nil {
		return "", fmt.Errorf("download after %d attempts: %w", downloadMaxRetries, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Determine extension from URL or fallback
	ext := filepath.Ext(urlFileName(url))
	if ext == "" {
		ext = ".bin"
	}

	tmpFile, err := os.CreateTemp("", "discord_media_*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmpFile.Close()

	written, err := io.Copy(tmpFile, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("save file: %w", err)
	}
	if written > maxBytes {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("file exceeds max size: %d bytes", written)
	}

	return tmpFile.Name(), nil
}

// classifyMediaType returns the media type constant based on MIME type.
func classifyMediaType(contentType, filename string) string {
	ct := strings.ToLower(contentType)
	switch {
	case strings.HasPrefix(ct, "image/"):
		return media.TypeImage
	case strings.HasPrefix(ct, "video/"):
		return media.TypeVideo
	case strings.HasPrefix(ct, "audio/"):
		return media.TypeAudio
	default:
		return media.TypeDocument
	}
}

// urlFileName extracts the filename component from a URL path.
func urlFileName(url string) string {
	// Strip query params
	if idx := strings.IndexByte(url, '?'); idx >= 0 {
		url = url[:idx]
	}
	return filepath.Base(url)
}
