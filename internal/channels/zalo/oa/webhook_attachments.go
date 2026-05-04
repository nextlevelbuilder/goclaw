package oa

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

type oaAttachment struct {
	Type    string              `json:"type"`
	Payload oaAttachmentPayload `json:"payload"`
}

type oaAttachmentPayload struct {
	URL         string `json:"url,omitempty"`
	Thumbnail   string `json:"thumbnail,omitempty"`
	Name        string `json:"name,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

func firstAttachmentURL(atts []oaAttachment) string {
	for _, a := range atts {
		if a.Payload.URL != "" {
			return a.Payload.URL
		}
	}
	return ""
}

func firstAttachment(atts []oaAttachment) *oaAttachment {
	if len(atts) == 0 {
		return nil
	}
	return &atts[0]
}

// dispatchWebhookMedia downloads the attachment URL and forwards it as a
// MediaInfo-tagged inbound. forceImageKind classifies stickers/gifs as
// image regardless of detected MIME so the agent treats them visually.
// The parent ctx (router's inst.ctx) cancels on UnregisterInstance, so
// downloads are aborted on Stop and Unregister can drain dispatchWG.
func (c *Channel) dispatchWebhookMedia(parent context.Context, e *oaInboundEvent, forceImageKind bool) {
	if e.Sender.ID == "" {
		return
	}
	url := firstAttachmentURL(e.Message.Attachments)
	if url == "" {
		slog.Warn("zalo_oa.webhook.attachment_missing_url",
			"event", e.EventName, "message_id", e.messageID())
		return
	}

	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	dl := c.downloadMediaFn
	if dl == nil {
		dl = downloadOAMedia
	}
	path, err := dl(ctx, url)
	if err != nil {
		slog.Warn("zalo_oa.webhook.attachment_download_failed",
			"event", e.EventName, "message_id", e.messageID(), "url", url, "error", err)
		return
	}

	mimeType := media.DetectMIMEType(path)
	kind := media.MediaKindFromMime(mimeType)
	if forceImageKind {
		kind = media.TypeImage
	}

	att := firstAttachment(e.Message.Attachments)
	fileName := ""
	if att != nil {
		fileName = att.Payload.Name
		if fileName == "" {
			fileName = att.Payload.Title
		}
	}

	tag := media.BuildMediaTags([]media.MediaInfo{{
		Type:        kind,
		FilePath:    path,
		ContentType: mimeType,
		FileName:    fileName,
		SourceURL:   url,
	}})

	content := strings.TrimSpace(e.Message.Text)
	if content == "" {
		content = tag
	} else {
		content = content + "\n" + tag
	}

	metadata := common.InboundMeta{
		MessageID:         e.messageID(),
		Platform:          common.PlatformZaloOA,
		SenderDisplayName: e.Sender.DisplayName,
	}.ToMap()
	c.BaseChannel.HandleMessage(e.Sender.ID, e.Sender.ID, content, []string{path}, metadata, "direct")
}

// dispatchWebhookLink forwards a shared link as plain text. We don't fetch
// the URL — arbitrary user-shared links would risk SSRF.
func (c *Channel) dispatchWebhookLink(e *oaInboundEvent) {
	if e.Sender.ID == "" {
		return
	}
	att := firstAttachment(e.Message.Attachments)
	if att == nil || att.Payload.URL == "" {
		if strings.TrimSpace(e.Message.Text) != "" {
			c.dispatchWebhookText(e)
		}
		return
	}

	var b strings.Builder
	if t := strings.TrimSpace(e.Message.Text); t != "" {
		b.WriteString(t)
		b.WriteString("\n\n")
	}
	b.WriteString("[link] ")
	if att.Payload.Title != "" {
		b.WriteString(att.Payload.Title)
		b.WriteString(" — ")
	}
	b.WriteString(att.Payload.URL)
	if att.Payload.Description != "" {
		b.WriteString("\n")
		b.WriteString(att.Payload.Description)
	}

	metadata := common.InboundMeta{
		MessageID:         e.messageID(),
		Platform:          common.PlatformZaloOA,
		SenderDisplayName: e.Sender.DisplayName,
	}.ToMap()
	c.BaseChannel.HandleMessage(e.Sender.ID, e.Sender.ID, b.String(), nil, metadata, "direct")
}

const oaWebhookMaxMediaBytes = 20 * 1024 * 1024

func downloadOAMedia(ctx context.Context, fileURL string) (string, error) {
	if err := tools.CheckSSRF(fileURL); err != nil {
		return "", fmt.Errorf("ssrf check: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	client := tools.NewSSRFSafeClient(0) // ctx governs deadline
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download status %d", resp.StatusCode)
	}

	ext := extFromURL(fileURL)
	tmpFile, err := os.CreateTemp("", "goclaw_zoa_*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	defer tmpFile.Close()

	written, err := io.Copy(tmpFile, io.LimitReader(resp.Body, oaWebhookMaxMediaBytes+1))
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("save: %w", err)
	}
	if written > oaWebhookMaxMediaBytes {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("attachment too large: %d bytes (cap %d)", written, oaWebhookMaxMediaBytes)
	}
	return tmpFile.Name(), nil
}

func extFromURL(fileURL string) string {
	path := fileURL
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" || len(ext) > 8 || !isSafeExt(ext) {
		return ".bin"
	}
	return ext
}

func isSafeExt(ext string) bool {
	for _, r := range ext[1:] {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
