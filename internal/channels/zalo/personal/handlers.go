package personal

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

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/channels/typing"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func (c *Channel) handleMessage(msg protocol.Message) {
	if msg.IsSelf() {
		return
	}

	switch m := msg.(type) {
	case protocol.UserMessage:
		c.handleDM(m)
	case protocol.GroupMessage:
		c.handleGroupMessage(m)
	}
}

func (c *Channel) handleDM(msg protocol.UserMessage) {
	senderID := msg.Data.UIDFrom
	threadID := msg.ThreadID()

	content, media := extractContentAndMedia(msg.Data.Content)
	if content == "" {
		return
	}

	if !c.checkDMPolicy(senderID, threadID) {
		return
	}

	// Annotate with sender display name so the agent knows who is messaging.
	senderName := msg.Data.DName
	if senderName != "" {
		content = fmt.Sprintf("[From: %s]\n%s", senderName, content)
	}

	slog.Debug("zalo_personal DM received",
		"sender", senderID,
		"dname", senderName,
		"thread", threadID,
		"preview", channels.Truncate(content, 50),
	)

	c.startTyping(threadID, protocol.ThreadTypeUser)

	// Collect contact for DM messages.
	if cc := c.ContactCollector(); cc != nil {
		cc.EnsureContact(context.Background(), c.Type(), c.Name(), senderID, senderID, senderName, "", "direct")
	}

	metadata := map[string]string{
		"message_id": msg.Data.MsgID,
		"platform":   channels.TypeZaloPersonal,
	}
	c.HandleMessage(senderID, threadID, content, media, metadata, "direct")
}

func (c *Channel) handleGroupMessage(msg protocol.GroupMessage) {
	senderID := msg.Data.UIDFrom
	threadID := msg.ThreadID()

	content, media := extractContentAndMedia(msg.Data.Content)
	if content == "" {
		return
	}

	// Step 1: enforce access policy (allowlist/pairing). Hard reject — don't record history.
	if !c.checkGroupPolicy(senderID, threadID) {
		return
	}

	senderName := msg.Data.DName
	if senderName == "" {
		senderName = senderID
	}

	// Step 2: @mention gating — record non-mentioned messages in history and return.
	if c.requireMention {
		wasMentioned := c.checkBotMentioned(msg.Data.Mentions)
		if !wasMentioned {
			c.groupHistory.Record(threadID, channels.HistoryEntry{
				Sender:    senderName,
				SenderID:  senderID,
				Body:      content,
				Media:     media,
				Timestamp: time.Now(),
				MessageID: msg.Data.MsgID,
			}, c.historyLimit)

			// Collect contact even when bot is not mentioned (cache prevents DB spam).
			if cc := c.ContactCollector(); cc != nil {
				cc.EnsureContact(context.Background(), c.Type(), c.Name(), senderID, senderID, senderName, "", "group")
			}

			slog.Debug("zalo_personal group message recorded (no mention)",
				"group_id", threadID,
				"sender", senderName,
			)
			return
		}
	}

	slog.Debug("zalo_personal group message received",
		"sender", senderID,
		"group", threadID,
		"preview", channels.Truncate(content, 50),
	)

	// Step 3: flush pending history + annotate current message with sender name.
	annotated := fmt.Sprintf("[From: %s]\n%s", senderName, content)
	finalContent := annotated
	if c.historyLimit > 0 {
		finalContent = c.groupHistory.BuildContext(threadID, annotated, c.historyLimit)
	}

	c.startTyping(threadID, protocol.ThreadTypeGroup)

	// Collect media from pending history entries (images sent before this @mention).
	// Must come after BuildContext — CollectMedia nulls out Media fields to prevent double-cleanup.
	histMedia := c.groupHistory.CollectMedia(threadID)
	allMedia := append(histMedia, media...)

	// Collect contact for group-mentioned messages.
	if cc := c.ContactCollector(); cc != nil {
		cc.EnsureContact(context.Background(), c.Type(), c.Name(), senderID, senderID, senderName, "", "group")
	}

	metadata := map[string]string{
		"message_id": msg.Data.MsgID,
		"platform":   channels.TypeZaloPersonal,
		"group_id":   threadID,
	}
	c.HandleMessage(senderID, threadID, finalContent, allMedia, metadata, "group")

	// Clear pending history after sending to agent (matches Telegram/Discord/Slack/Feishu pattern).
	c.groupHistory.Clear(threadID)
}

// startTyping starts a typing indicator with keepalive for the given thread.
// Zalo typing expires after ~5s, so keepalive fires every 3s.
func (c *Channel) startTyping(threadID string, threadType protocol.ThreadType) {
	sess := c.session()
	ctrl := typing.New(typing.Options{
		MaxDuration:       60 * time.Second,
		KeepaliveInterval: 4 * time.Second,
		StartFn: func() error {
			return protocol.SendTypingEvent(context.Background(), sess, threadID, threadType)
		},
	})
	if prev, ok := c.typingCtrls.Load(threadID); ok {
		if ctrl, ok := prev.(*typing.Controller); ok {
			ctrl.Stop()
		}
	}
	c.typingCtrls.Store(threadID, ctrl)
	ctrl.Start()
}

// extractContentAndMedia returns text content (with <media:*> tags) and optional local media
// file paths from a Zalo message. For text messages, media is nil. For attachments, the file
// is downloaded and classified by MIME type, matching the pattern used by Telegram/Discord/Feishu.
func extractContentAndMedia(content protocol.Content) (string, []string) {
	if text := content.Text(); text != "" {
		return text, nil
	}
	att := content.ParseAttachment()
	if att == nil || att.Href == "" {
		return "", nil
	}

	// Download the attachment file.
	filePath, err := downloadFile(context.Background(), att.Href)
	if err != nil {
		slog.Warn("zalo_personal: failed to download attachment", "url", att.Href, "error", err)
		// Return human-readable fallback so the message isn't silently dropped.
		if text := content.AttachmentText(); text != "" {
			return text, nil
		}
		return "", nil
	}

	// Classify by MIME type (image, video, audio, document) — same as Discord/Feishu.
	mimeType := media.DetectMIMEType(filePath)
	mediaKind := media.MediaKindFromMime(mimeType)

	// For images, also check via Zalo CDN path patterns (e.g. /jpg/, /png/) since
	// temp files lose the original extension context.
	if mediaKind != media.TypeImage && att.IsImage() {
		mediaKind = media.TypeImage
	}

	// Build the <media:*> tag that the agent loop's enrichImageIDs/enrichMediaIDs expects.
	tag := media.BuildMediaTags([]media.MediaInfo{{
		Type:        mediaKind,
		FilePath:    filePath,
		ContentType: mimeType,
		FileName:    att.Title,
	}})

	return tag, []string{filePath}
}

const maxMediaBytes = 20 * 1024 * 1024 // 20MB (matches Telegram default)

// downloadFile downloads a URL to a temp file and returns the local path.
// Validates against SSRF and enforces timeout and size limits.
func downloadFile(ctx context.Context, fileURL string) (string, error) {
	if err := tools.CheckSSRF(fileURL); err != nil {
		return "", fmt.Errorf("ssrf check: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download status %d", resp.StatusCode)
	}

	// Strip query params before extracting extension.
	path := fileURL
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	ext := filepath.Ext(path)
	if ext == "" || len(ext) > 5 {
		ext = ".bin"
	}

	tmpFile, err := os.CreateTemp("", "goclaw_zca_*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	defer tmpFile.Close()

	written, err := io.Copy(tmpFile, io.LimitReader(resp.Body, maxMediaBytes+1))
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("save: %w", err)
	}
	if written > maxMediaBytes {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("file too large: %d bytes (max %d)", written, maxMediaBytes)
	}

	return tmpFile.Name(), nil
}
