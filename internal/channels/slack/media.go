package slack

import (
	"fmt"
	"log/slog"
	"strings"

	slackapi "github.com/slack-go/slack"

	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
)

const defaultMediaMaxBytes int64 = 20 * 1024 * 1024 // 20MB

// mediaItem represents a downloaded file from a Slack message.
type mediaItem struct {
	Type        string // "image", "audio", "document"
	FilePath    string // local temp file path
	FileName    string // original filename
	ContentType string // MIME type
}

// resolveMedia downloads and classifies files attached to a Slack message.
// Returns media items and any extra content (extracted document text).
func (c *Channel) resolveMedia(files []slackapi.File) (items []mediaItem, extraContent string) {
	maxBytes := c.config.MediaMaxBytes
	if maxBytes == 0 {
		maxBytes = defaultMediaMaxBytes
	}

	for _, f := range files {
		// Skip files exceeding size limit before download
		if int64(f.Size) > maxBytes {
			slog.Warn("slack: file too large, skipping",
				"file", f.Name, "size", f.Size, "max", maxBytes)
			continue
		}

		mtype := classifyMime(f.Mimetype)

		filePath, err := c.downloadFile(f.Name, f.URLPrivate, f.URLPrivateDownload, maxBytes)
		if err != nil {
			slog.Warn("slack: file download failed",
				"file", f.Name, "error", err)
			continue
		}

		items = append(items, mediaItem{
			Type:        mtype,
			FilePath:    filePath,
			FileName:    f.Name,
			ContentType: f.Mimetype,
		})

		// Extract text from document files
		if mtype == "document" {
			docContent, err := media.ExtractDocumentContent(filePath, f.Name)
			if err != nil {
				slog.Warn("slack: document extraction failed",
					"file", f.Name, "error", err)
				continue
			}
			if extraContent != "" {
				extraContent += "\n"
			}
			extraContent += docContent
		}
	}

	return items, extraContent
}

// classifyMime maps a MIME type to a media category.
func classifyMime(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	default:
		return "document"
	}
}

// buildMediaTags generates content tags for media items.
func buildMediaTags(items []mediaItem) string {
	var tags []string
	for _, m := range items {
		switch m.Type {
		case "image":
			tags = append(tags, "<media:image>")
		case "audio":
			tags = append(tags, "<media:audio>")
		case "document":
			tags = append(tags, fmt.Sprintf("<media:document file=%q>", m.FileName))
		}
	}
	return strings.Join(tags, "\n")
}
