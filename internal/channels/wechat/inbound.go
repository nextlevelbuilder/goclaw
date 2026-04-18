package wechat

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// hexKeyToBase64 converts a raw hex AES key (from image_item.aeskey) to base64,
// matching the TypeScript reference: Buffer.from(hexKey, "hex").toString("base64").
func hexKeyToBase64(hexKey string) (string, error) {
	raw, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", fmt.Errorf("hex decode aeskey: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// scheduleMediaCleanup removes temp media files after a delay.
func scheduleMediaCleanup(paths []string, delay time.Duration) {
	if len(paths) == 0 {
		return
	}
	time.AfterFunc(delay, func() {
		for _, path := range paths {
			if path == "" {
				continue
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				slog.Debug("failed to cleanup temp media", "path", path, "error", err)
			}
		}
	})
}

// isMediaItem returns true if the item is image, video, file, or voice.
func isMediaItem(item *MessageItem) bool {
	return item.Type == MessageItemTypeImage ||
		item.Type == MessageItemTypeVideo ||
		item.Type == MessageItemTypeFile ||
		item.Type == MessageItemTypeVoice
}

// bodyFromItemList extracts text body from a message's item list.
func bodyFromItemList(items []MessageItem) string {
	for _, item := range items {
		if item.Type == MessageItemTypeText && item.TextItem != nil && item.TextItem.Text != "" {
			text := item.TextItem.Text
			ref := item.RefMsg
			if ref == nil {
				return text
			}
			// Quoted media: only include current text
			if ref.MessageItem != nil && isMediaItem(ref.MessageItem) {
				return text
			}
			// Build quoted context
			var parts []string
			if ref.Title != "" {
				parts = append(parts, ref.Title)
			}
			if ref.MessageItem != nil {
				refBody := bodyFromItemList([]MessageItem{*ref.MessageItem})
				if refBody != "" {
					parts = append(parts, refBody)
				}
			}
			if len(parts) == 0 {
				return text
			}
			return fmt.Sprintf("[引用: %s]\n%s", strings.Join(parts, " | "), text)
		}
		// Voice-to-text: use text field from voice item
		if item.Type == MessageItemTypeVoice && item.VoiceItem != nil && item.VoiceItem.Text != "" {
			return item.VoiceItem.Text
		}
	}
	return ""
}

// weixinMessageToInbound converts a WeixinMessage to a bus.InboundMessage.
func weixinMessageToInbound(msg *WeixinMessage, channelName string) bus.InboundMessage {
	fromUserID := msg.FromUserID

	body := bodyFromItemList(msg.ItemList)

	inbound := bus.InboundMessage{
		Channel:  channelName,
		SenderID: fromUserID,
		ChatID:   fromUserID,
		Content:  body,
		PeerKind: "direct",
		Metadata: map[string]string{
			"message_sid": "goclaw-wechat-" + uuid.New().String(),
		},
	}

	if msg.ContextToken != "" {
		inbound.Metadata["context_token"] = msg.ContextToken
	}

	return inbound
}

// downloadMedia handles CDN download and AES decryption for incoming media attachments.
func (ch *Channel) downloadMedia(ctx context.Context, msg *WeixinMessage) []bus.MediaFile {
	var mediaFiles []bus.MediaFile

	for _, item := range msg.ItemList {
		if item.Type == MessageItemTypeImage && item.ImageItem != nil {
			img := item.ImageItem
			var cdnMedia *CDNMedia
			if img.Media != nil {
				cdnMedia = img.Media
			} else if img.ThumbMedia != nil {
				cdnMedia = img.ThumbMedia
			}
			if cdnMedia == nil {
				continue
			}

			// Resolve AES key: image_item.aeskey is raw hex, media.aes_key is already base64.
			// Match TS: Buffer.from(img.aeskey, "hex").toString("base64")
			var aesKeyBase64 string
			if img.AesKey != "" {
				b64, err := hexKeyToBase64(img.AesKey)
				if err != nil {
					slog.Error("wechat inbound image: invalid hex aeskey", "error", err)
					continue
				}
				aesKeyBase64 = b64
			} else if cdnMedia.AesKey != "" {
				aesKeyBase64 = cdnMedia.AesKey // already base64
			}

			if aesKeyBase64 == "" {
				slog.Debug("wechat inbound image: no AES key, skipping")
				continue
			}

			decrypted, err := downloadAndDecrypt(ctx, cdnMedia.EncryptQueryParam, aesKeyBase64, ch.cdnBaseURL, "inbound_image", cdnMedia.FullURL)
			if err != nil {
				slog.Error("wechat inbound image download failed", "error", err)
				continue
			}
			destDir := filepath.Join(os.TempDir(), "goclaw", "weixin", "media", "inbound")
			if err := os.MkdirAll(destDir, 0o755); err != nil {
				slog.Error("wechat inbound mkdir failed", "error", err)
				continue
			}

			tmpFile := filepath.Join(destDir, fmt.Sprintf("inbound_wechat_%d.jpg", time.Now().UnixNano()))
			if err := os.WriteFile(tmpFile, decrypted, 0o644); err != nil {
				slog.Error("wechat inbound file write failed", "error", err)
				continue
			}

			mediaFiles = append(mediaFiles, bus.MediaFile{
				Path:     tmpFile,
				MimeType: "image/jpeg",
				Filename: "image.jpg",
			})
		}
		// Future: implement MessageItemTypeVideo, MessageItemTypeVoice, MessageItemTypeFile
	}
	return mediaFiles
}
