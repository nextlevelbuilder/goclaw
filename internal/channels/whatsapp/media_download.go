package whatsapp

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
)

// downloadMedia downloads media attachments from a WhatsApp message.
func (c *Channel) downloadMedia(evt *events.Message) []media.MediaInfo {
	msg := evt.Message
	if msg == nil {
		return nil
	}

	type mediaItem struct {
		mediaType string
		mimetype  string
		filename  string
		download  whatsmeow.DownloadableMessage
	}

	var items []mediaItem
	if img := msg.GetImageMessage(); img != nil {
		items = append(items, mediaItem{"image", img.GetMimetype(), "", img})
	}
	if vid := msg.GetVideoMessage(); vid != nil {
		items = append(items, mediaItem{"video", vid.GetMimetype(), "", vid})
	}
	if aud := msg.GetAudioMessage(); aud != nil {
		items = append(items, mediaItem{"audio", aud.GetMimetype(), "", aud})
	}
	if doc := msg.GetDocumentMessage(); doc != nil {
		items = append(items, mediaItem{"document", doc.GetMimetype(), doc.GetFileName(), doc})
	}
	if stk := msg.GetStickerMessage(); stk != nil {
		items = append(items, mediaItem{"sticker", stk.GetMimetype(), "", stk})
	}

	if len(items) == 0 {
		return nil
	}

	var result []media.MediaInfo
	for _, item := range items {
		data, err := c.client.Download(c.ctx, item.download)
		if err != nil {
			reason := classifyDownloadError(err)
			slog.Warn("whatsapp: media download failed", "type", item.mediaType, "reason", reason, "error", err)
			continue
		}
		if len(data) > 20*1024*1024 { // 20MB limit
			slog.Warn("whatsapp: media too large, skipping", "type", item.mediaType,
				"size_mb", len(data)/(1024*1024))
			continue
		}

		ext := mimeToExt(item.mimetype)
		tmpFile, err := os.CreateTemp("", "goclaw_wa_*"+ext)
		if err != nil {
			slog.Warn("whatsapp: temp file creation failed", "error", err)
			continue
		}
		if _, err := tmpFile.Write(data); err != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
			continue
		}
		tmpFile.Close()

		result = append(result, media.MediaInfo{
			Type:        item.mediaType,
			FilePath:    tmpFile.Name(),
			ContentType: item.mimetype,
			FileName:    item.filename,
		})
	}
	return result
}

// mimeToExt maps MIME types to file extensions.
func mimeToExt(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/jpeg"):
		return ".jpg"
	case strings.HasPrefix(mime, "image/png"):
		return ".png"
	case strings.HasPrefix(mime, "image/webp"):
		return ".webp"
	case strings.HasPrefix(mime, "video/mp4"):
		return ".mp4"
	case strings.HasPrefix(mime, "audio/ogg"):
		return ".ogg"
	case strings.HasPrefix(mime, "audio/mpeg"):
		return ".mp3"
	case strings.HasPrefix(mime, "application/pdf"):
		return ".pdf"
	default:
		return ".bin"
	}
}

// classifyDownloadError returns a human-readable reason for a media download failure.
func classifyDownloadError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline"):
		return "timeout"
	case strings.Contains(msg, "decrypt") || strings.Contains(msg, "cipher"):
		return "decrypt_error"
	case strings.Contains(msg, "404") || strings.Contains(msg, "not found"):
		return "expired"
	case strings.Contains(msg, "unsupported"):
		return "unsupported"
	default:
		return "unknown"
	}
}

// scheduleMediaCleanup removes temp media files after a delay.
// Uses time.AfterFunc so it does not block and respects the provided context for logging only.
func scheduleMediaCleanup(ctx context.Context, paths []string, delay time.Duration) {
	if len(paths) == 0 {
		return
	}
	time.AfterFunc(delay, func() {
		for _, p := range paths {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				slog.Debug("whatsapp: temp media cleanup failed", "path", p, "error", err)
			}
		}
	})
}
