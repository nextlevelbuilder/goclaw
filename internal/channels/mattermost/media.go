package mattermost

import (
	"context"
	"fmt"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// maxMediaBytes is the default max media download size (50MB).
const maxMediaBytes int64 = 50 * 1024 * 1024

// resolveMedia downloads files from Mattermost and returns local file paths
// along with a cleanup function that removes all temp files.
// The caller MUST call cleanup() when done processing (typically via defer).
func (c *Channel) resolveMedia(ctx context.Context, fileIDs []string) ([]string, func()) {
	maxBytes := c.config.MediaMaxBytes
	if maxBytes <= 0 {
		maxBytes = maxMediaBytes
	}

	var paths []string
	for _, fileID := range fileIDs {
		info, err := c.client.GetFileInfo(ctx, fileID)
		if err != nil {
			slog.Debug("mattermost: failed to get file info", "file_id", fileID, "error", err)
			continue
		}

		// Size check
		if info.Size > maxBytes {
			slog.Debug("mattermost: file too large, skipping",
				"file_id", fileID, "size", info.Size, "max", maxBytes)
			continue
		}

		// Determine file extension
		ext := filepath.Ext(info.Name)
		if ext == "" {
			exts, _ := mime.ExtensionsByType(info.MimeType)
			if len(exts) > 0 {
				ext = exts[0]
			}
		}

		// Create temp file first, then stream directly to it (never holds full file in RAM)
		tmpFile, err := os.CreateTemp("", "mm-media-*"+ext)
		if err != nil {
			slog.Debug("mattermost: failed to create temp file", "error", err)
			continue
		}
		tmpFile.Close() // close so DownloadFileToPath can write to it

		if err := c.client.DownloadFileToPath(ctx, fileID, tmpFile.Name()); err != nil {
			os.Remove(tmpFile.Name())
			slog.Debug("mattermost: failed to download file", "file_id", fileID, "error", err)
			continue
		}

		paths = append(paths, tmpFile.Name())
	}

	// Return cleanup function that removes all downloaded temp files
	cleanup := func() {
		for _, p := range paths {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				slog.Debug("mattermost: failed to cleanup temp file", "path", p, "error", err)
			}
		}
	}

	return paths, cleanup
}

// uploadFile uploads a media file to a Mattermost channel using streaming I/O.
func (c *Channel) uploadFile(ctx context.Context, channelID, rootID string, m bus.MediaAttachment) error {
	filePath := m.URL
	if filePath == "" {
		return nil
	}

	// Upload using streaming (never loads full file into RAM)
	resp, err := c.client.UploadFileStreaming(ctx, filePath, channelID)
	if err != nil {
		return fmt.Errorf("upload file: %w", err)
	}

	if len(resp.FileInfos) == 0 {
		return fmt.Errorf("upload returned no file info")
	}

	// Create post with file attachment
	post := &MMPost{
		ChannelId: channelID,
		Message:   "",
		RootId:    rootID,
		FileIds:   []string{resp.FileInfos[0].Id},
	}

	if m.Caption != "" {
		post.Message = m.Caption
	}

	_, err = c.client.CreatePost(ctx, post)
	return err
}

// buildMediaTags creates inline media reference tags for content injection.
func buildMediaTags(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	var tags []string
	for _, p := range paths {
		ext := strings.ToLower(filepath.Ext(p))
		switch ext {
		case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg":
			tags = append(tags, fmt.Sprintf("[Attached image: %s]", filepath.Base(p)))
		case ".mp4", ".mov", ".webm", ".avi":
			tags = append(tags, fmt.Sprintf("[Attached video: %s]", filepath.Base(p)))
		case ".mp3", ".ogg", ".wav", ".flac", ".m4a":
			tags = append(tags, fmt.Sprintf("[Attached audio: %s]", filepath.Base(p)))
		case ".pdf":
			tags = append(tags, fmt.Sprintf("[Attached PDF: %s]", filepath.Base(p)))
		default:
			tags = append(tags, fmt.Sprintf("[Attached file: %s]", filepath.Base(p)))
		}
	}
	return strings.Join(tags, "\n")
}
