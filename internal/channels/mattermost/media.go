package mattermost

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
)

// postFiles returns attachment metadata for a post. Posts delivered over the
// websocket normally embed it; otherwise it is fetched per file ID.
func (c *Channel) postFiles(ctx context.Context, post *mmPost) []mmFileInfo {
	if post.Metadata != nil && len(post.Metadata.Files) > 0 {
		return post.Metadata.Files
	}
	var files []mmFileInfo
	for _, id := range post.FileIDs {
		info, err := c.api.getFileInfo(ctx, id)
		if err != nil {
			slog.Warn("mattermost: file info lookup failed", "file_id", id, "error", err)
			continue
		}
		files = append(files, *info)
	}
	return files
}

// resolveMedia downloads a post's attachments into temp files and classifies
// them. Returns the media items and extracted text of document attachments.
func (c *Channel) resolveMedia(ctx context.Context, post *mmPost) (items []media.MediaInfo, docContent string) {
	for _, f := range c.postFiles(ctx, post) {
		if f.Size > mediaMaxBytes {
			slog.Warn("mattermost: file too large, skipping", "file", f.Name, "size", f.Size, "max", mediaMaxBytes)
			continue
		}

		path, err := c.downloadToTemp(ctx, f, mediaMaxBytes)
		if err != nil {
			slog.Warn("mattermost: file download failed", "file", f.Name, "error", err)
			continue
		}

		mime := f.MimeType
		if mime == "" {
			mime = media.DetectMIMEType(f.Name)
		}
		kind := media.MediaKindFromMime(mime)
		items = append(items, media.MediaInfo{
			Type:        kind,
			FilePath:    path,
			FileID:      f.ID,
			ContentType: mime,
			FileName:    f.Name,
			FileSize:    f.Size,
		})

		if kind == media.TypeDocument {
			text, err := media.ExtractDocumentContent(path, f.Name)
			if err != nil {
				slog.Warn("mattermost: document extraction failed", "file", f.Name, "error", err)
				continue
			}
			docContent = joinLines(docContent, text)
		}
	}
	return items, docContent
}

func (c *Channel) downloadToTemp(ctx context.Context, f mmFileInfo, maxBytes int64) (string, error) {
	ext := filepath.Ext(f.Name)
	if ext == "" {
		ext = ".dat"
	}
	tmp, err := os.CreateTemp("", "mattermost-file-*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	err = c.api.downloadFile(ctx, f.ID, tmp, maxBytes)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

func joinLines(a, b string) string {
	if a == "" {
		return b
	}
	return a + "\n" + b
}
