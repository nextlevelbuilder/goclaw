package telegram

import (
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

const (
	telegramMediaGroupMinItems = 2
	telegramMediaGroupMaxItems = 10
)

type telegramMediaKind string

const (
	telegramMediaPhoto    telegramMediaKind = "photo"
	telegramMediaVideo    telegramMediaKind = "video"
	telegramMediaAudio    telegramMediaKind = "audio"
	telegramMediaDocument telegramMediaKind = "document"
)

type telegramMediaSendItem struct {
	media        bus.MediaAttachment
	kind         telegramMediaKind
	caption      string
	followUpText string
	groupable    bool
	asVoice      bool
}

type telegramMediaChunk struct {
	items   []telegramMediaSendItem
	grouped bool
}

func (c *Channel) prepareTelegramMediaItems(msg bus.OutboundMessage) ([]telegramMediaSendItem, error) {
	content := msg.Content
	items := make([]telegramMediaSendItem, 0, len(msg.Media))
	for _, media := range msg.Media {
		if err := c.validateOutboundMediaSize(media.URL); err != nil {
			return nil, err
		}

		caption := media.Caption
		if caption == "" && content != "" {
			caption = content
			content = ""
		}

		var followUpText string
		if caption != "" {
			caption = markdownToTelegramHTML(caption)
			if len(caption) > telegramCaptionMaxLen {
				followUpText = caption
				caption = ""
			}
		}

		kind, groupable, asVoice := classifyTelegramOutboundMedia(media, msg.Metadata)
		items = append(items, telegramMediaSendItem{
			media:        media,
			kind:         kind,
			caption:      caption,
			followUpText: followUpText,
			groupable:    groupable,
			asVoice:      asVoice,
		})
	}
	return items, nil
}

func classifyTelegramOutboundMedia(media bus.MediaAttachment, metadata map[string]string) (telegramMediaKind, bool, bool) {
	ct := strings.ToLower(media.ContentType)
	if ct == "" {
		ct = strings.ToLower(mime.TypeByExtension(filepath.Ext(media.URL)))
	}

	switch {
	case strings.HasPrefix(ct, "image/"):
		if info, statErr := os.Stat(media.URL); statErr == nil && info.Size() > photoSizeThreshold {
			slog.Info("large image, sending as document to preserve quality", "path", media.URL, "size", info.Size())
			return telegramMediaDocument, true, false
		}
		return telegramMediaPhoto, true, false
	case strings.HasPrefix(ct, "video/"):
		return telegramMediaVideo, true, false
	case strings.HasPrefix(ct, "audio/"):
		asVoice := metadata["audio_as_voice"] == "true" && isVoiceCompatible(ct)
		return telegramMediaAudio, !asVoice, asVoice
	default:
		return telegramMediaDocument, true, false
	}
}

func chunkTelegramMediaItems(items []telegramMediaSendItem) []telegramMediaChunk {
	chunks := make([]telegramMediaChunk, 0, len(items))
	for i := 0; i < len(items); {
		key := telegramMediaGroupKey(items[i])
		if key == "" {
			chunks = append(chunks, telegramMediaChunk{items: items[i : i+1]})
			i++
			continue
		}

		j := i + 1
		for j < len(items) && telegramMediaGroupKey(items[j]) == key {
			j++
		}

		for start := i; start < j; {
			size := min(j-start, telegramMediaGroupMaxItems)
			chunks = append(chunks, telegramMediaChunk{
				items:   items[start : start+size],
				grouped: size >= telegramMediaGroupMinItems,
			})
			start += size
		}
		i = j
	}
	return chunks
}

func telegramMediaGroupKey(item telegramMediaSendItem) string {
	if !item.groupable {
		return ""
	}
	switch item.kind {
	case telegramMediaPhoto, telegramMediaVideo:
		return "visual"
	case telegramMediaAudio:
		return "audio"
	case telegramMediaDocument:
		return "document"
	default:
		return ""
	}
}
