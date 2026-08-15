package pancake

import (
	"context"
	"fmt"
	"log/slog"
	"mime"
	"os"
	"path/filepath"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// handleMediaAttachments uploads media files from an OutboundMessage and returns attachment IDs.
// Returns an empty slice (not an error) when no media is attached.
// On upload failure the error is returned so the caller can decide to send text-only.
func (ch *Channel) handleMediaAttachments(ctx context.Context, msg bus.OutboundMessage) ([]string, error) {
	if len(msg.Media) == 0 {
		return nil, nil
	}

	var ids []string
	for _, att := range msg.Media {
		if att.URL == "" {
			continue
		}

		id, err := ch.uploadMediaFile(ctx, att.URL, att.ContentType)
		if err != nil {
			slog.Warn("pancake: skipping media attachment on upload error",
				"page_id", ch.pageID, "url", att.URL, "err", err)
			return ids, err
		}
		ids = append(ids, id)
	}

	return ids, nil
}

// uploadMediaFile opens a local file path and uploads it to the Pancake API.
// path must be absolute to prevent directory traversal via relative paths.
func (ch *Channel) uploadMediaFile(ctx context.Context, path string, contentType string) (string, error) {
	cleanPath := filepath.Clean(path)
	if !filepath.IsAbs(cleanPath) {
		return "", fmt.Errorf("pancake: media path must be absolute, got: %s", path)
	}
	f, err := os.Open(cleanPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	ct := contentType
	if ct == "" {
		ct = mimeTypeFromPath(cleanPath)
	}

	return ch.apiClient.UploadMedia(ctx, filepath.Base(cleanPath), f, ct)
}

// mimeTypeFromPath guesses the MIME type from the file extension.
func mimeTypeFromPath(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return "application/octet-stream"
	}
	mt := mime.TypeByExtension(ext)
	if mt == "" {
		return "application/octet-stream"
	}
	return mt
}
