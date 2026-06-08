package bitrix24

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// maxOutboundMediaBytesFallback caps a single outbound file when the channel
// config doesn't specify one. Bitrix imbot.v2.File.upload accepts up to 100 MB
// of Base64 content, but we keep a tighter ceiling so the POST body (Base64
// inflates the payload by ~33%) stays reasonable. Normally cfg.MediaMaxMB wins.
const maxOutboundMediaBytesFallback = 20 * 1024 * 1024

// outboundMediaCap returns the per-file outbound size limit in bytes, honoring
// the same cfg.MediaMaxMB knob used for inbound so the two directions stay
// symmetric (default 20 MB via applyConfigDefaults).
func (c *Channel) outboundMediaCap() int64 {
	if mb := int64(c.cfg.MediaMaxMB); mb > 0 {
		return mb * 1024 * 1024
	}
	return maxOutboundMediaBytesFallback
}

// sendMedia uploads each attachment on an outbound message to the Bitrix24 chat
// via imbot.v2.File.upload (a single call uploads the file to Drive, attaches it
// to the chat, and posts it). The text body is deliberately NOT sent here —
// Send() delivers it separately through the normal text path, so a media failure
// never drops the text and we never double-post.
//
// Best-effort: a file that fails to read or upload is logged and skipped.
// Returns the first error encountered for caller visibility; the text path runs
// regardless of what this returns.
func (c *Channel) sendMedia(ctx context.Context, msg bus.OutboundMessage) error {
	client := c.Client()
	botID := c.BotID()
	if client == nil || botID <= 0 {
		return fmt.Errorf("bitrix24: channel not initialised for media upload")
	}

	var firstErr error
	for _, m := range msg.Media {
		if err := c.uploadOneFile(ctx, client, botID, msg.ChatID, m); err != nil {
			slog.Warn("bitrix24: media upload failed, skipping file",
				"chat_id", msg.ChatID, "path", m.URL, "err", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// uploadOneFile reads a local file, Base64-encodes it, and posts it to the chat
// via imbot.v2.File.upload. msg.Media[].URL holds a local filesystem path (set
// by appendMediaToOutbound in the gateway consumer), not a remote URL.
func (c *Channel) uploadOneFile(ctx context.Context, client *Client, botID int, dialogID string, m bus.MediaAttachment) error {
	if m.URL == "" {
		return fmt.Errorf("empty media path")
	}
	info, err := os.Stat(m.URL)
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	if cap := c.outboundMediaCap(); info.Size() > cap {
		return fmt.Errorf("file %d bytes exceeds cap %d", info.Size(), cap)
	}
	data, err := os.ReadFile(m.URL)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	// Base64 content must NOT carry a data:*/*;base64, prefix (Bitrix requirement).
	// botToken is omitted intentionally — not needed under OAuth.
	if _, err := client.Call(ctx, "imbot.v2.File.upload", map[string]any{
		"botId":    botID,
		"dialogId": dialogID,
		"fields": map[string]any{
			"FILE": map[string]any{
				"name":    filepath.Base(m.URL),
				"content": base64.StdEncoding.EncodeToString(data),
			},
		},
	}); err != nil {
		return fmt.Errorf("imbot.v2.File.upload: %w", err)
	}
	slog.Info("bitrix24: uploaded outbound file",
		"chat_id", dialogID, "name", filepath.Base(m.URL), "bytes", info.Size())
	return nil
}
