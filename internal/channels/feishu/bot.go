package feishu

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
)

// messageContext holds parsed information from a Feishu message event.
type messageContext struct {
	ChatID      string
	MessageID   string
	SenderID    string // sender_id.open_id
	ChatType    string // "p2p" or "group"
	Content     string
	ContentType string // "text", "post", "image", etc.
	MentionedBot bool
	RootID      string // thread root message ID
	ParentID    string // parent message ID
	Mentions    []mentionInfo
}

type mentionInfo struct {
	Key    string // @_user_N placeholder
	OpenID string
	Name   string
}

// handleMessageEvent processes an incoming Feishu message event.
func (c *Channel) handleMessageEvent(ctx context.Context, event *MessageEvent) {
	if event == nil {
		return
	}

	msg := &event.Event.Message
	sender := &event.Event.Sender

	messageID := msg.MessageID
	if messageID == "" {
		return
	}

	// 1. Dedup check
	if c.isDuplicate(messageID) {
		slog.Debug("feishu message deduplicated", "message_id", messageID)
		return
	}

	// 2. Parse message
	mc := c.parseMessageEvent(event)
	if mc == nil {
		return
	}

	// 3. Resolve sender name (cached)
	senderName := c.resolveSenderName(ctx, mc.SenderID)

	// 4. Group policy
	if mc.ChatType == "group" {
		if !c.checkGroupPolicy(ctx, mc.SenderID, mc.ChatID) {
			slog.Debug("feishu group message rejected by policy", "sender_id", mc.SenderID, "chat_id", mc.ChatID)
			return
		}

		// 5. RequireMention check — record to history if not mentioned
		requireMention := true
		if c.cfg.RequireMention != nil {
			requireMention = *c.cfg.RequireMention
		}
		if requireMention && !mc.MentionedBot {
			historyKey := mc.ChatID
			if mc.RootID != "" && c.cfg.TopicSessionMode == "enabled" {
				historyKey = fmt.Sprintf("%s:topic:%s", mc.ChatID, mc.RootID)
			}
			c.groupHistory.Record(historyKey, channels.HistoryEntry{
				Sender:    senderName,
				SenderID:  mc.SenderID,
				Body:      mc.Content,
				Timestamp: time.Now(),
				MessageID: messageID,
			}, c.historyLimit)

			// Collect contact even when bot is not mentioned (cache prevents DB spam).
			if cc := c.ContactCollector(); cc != nil {
				cc.EnsureContact(ctx, c.Type(), c.Name(), mc.SenderID, mc.SenderID, senderName, "", "group")
			}

			slog.Debug("feishu group message recorded (no mention)",
				"chat_id", mc.ChatID, "sender", senderName,
			)
			return
		}
	}

	// 6. DM policy (pairing flow)
	if mc.ChatType == "p2p" {
		if !c.checkDMPolicy(ctx, mc.SenderID, mc.ChatID) {
			return
		}
	}

	// 7. Build content (strip bot mention from text)
	content := mc.Content
	if content == "" {
		content = "[empty message]"
	}

	// 7b. Fetch reply context + media if this is a reply to another message
	var replyMediaList []media.MediaInfo
	if mc.ParentID != "" {
		replyCtx, replyMedia := c.fetchReplyContext(ctx, mc.ParentID)
		if replyCtx != "" {
			content += "\n\n" + replyCtx
		}
		replyMediaList = replyMedia
	}

	// 8. Topic session
	chatID := mc.ChatID
	if mc.RootID != "" && c.cfg.TopicSessionMode == "enabled" {
		chatID = fmt.Sprintf("%s:topic:%s", mc.ChatID, mc.RootID)
	}

	slog.Debug("feishu message received",
		"sender_id", mc.SenderID,
		"sender_name", senderName,
		"chat_id", chatID,
		"chat_type", mc.ChatType,
		"mentioned_bot", mc.MentionedBot,
		"preview", channels.Truncate(content, 50),
	)

	// 9. Build metadata
	peerKind := "direct"
	if mc.ChatType == "group" {
		peerKind = "group"
	}

	// Collect contact for processed messages (DM + group-mentioned).
	if cc := c.ContactCollector(); cc != nil {
		cc.EnsureContact(ctx, c.Type(), c.Name(), mc.SenderID, mc.SenderID, senderName, "", peerKind)
	}

	metadata := map[string]string{
		"message_id":    messageID,
		"chat_type":     mc.ChatType,
		"sender_name":   senderName,
		"mentioned_bot": fmt.Sprintf("%t", mc.MentionedBot),
		"platform":      channels.TypeFeishu,
	}

	if sender != nil {
		metadata["sender_open_id"] = sender.SenderID.OpenID
	}

	// Annotate content with sender identity so the agent knows who is messaging.
	if senderName != "" {
		if mc.ChatType == "group" {
			annotated := fmt.Sprintf("[From: %s]\n%s", senderName, content)
			if c.historyLimit > 0 {
				content = c.groupHistory.BuildContext(chatID, annotated, c.historyLimit)
			} else {
				content = annotated
			}
		} else {
			// DM: annotate with sender identity so the agent knows who is messaging.
			content = fmt.Sprintf("[From: %s]\n%s", senderName, content)
		}
	}

	// 10. Resolve inbound media (image, file, audio, video, sticker)
	var mediaList []media.MediaInfo
	// Reply media first (context), current media second.
	if len(replyMediaList) > 0 {
		mediaList = append(mediaList, replyMediaList...)
	}
	switch mc.ContentType {
	case "image", "file", "audio", "video", "sticker":
		mediaList = append(mediaList, c.resolveMediaFromMessage(ctx, mc.MessageID, mc.ContentType, msg.Content)...)
	}

	// 11. Process media: STT transcription, document extraction, build tags
	var mediaFiles []bus.MediaFile
	if len(mediaList) > 0 {
		var extraContent string
		for i := range mediaList {
			m := &mediaList[i]

			switch m.Type {
			case media.TypeAudio, media.TypeVoice:
				transcript, sttErr := c.transcribeAudio(ctx, m.FilePath)
				if sttErr != nil {
					slog.Warn("feishu: STT transcription failed",
						"type", m.Type, "error", sttErr,
					)
				} else {
					m.Transcript = transcript
				}

			case media.TypeDocument:
				if m.FileName != "" && m.FilePath != "" {
					docContent, err := media.ExtractDocumentContent(m.FilePath, m.FileName)
					if err != nil {
						slog.Warn("feishu: document extraction failed", "file", m.FileName, "error", err)
					} else if docContent != "" {
						extraContent += "\n\n" + docContent
					}
				}
			}

			if m.FilePath != "" {
				mediaFiles = append(mediaFiles, bus.MediaFile{
					Path:     m.FilePath,
					MimeType: m.ContentType,
				})
			}
		}

		// Build media tags AFTER processing so transcript fields are populated.
		mediaTags := media.BuildMediaTags(mediaList)
		if mediaTags != "" {
			if content != "" {
				content = mediaTags + "\n\n" + content
			} else {
				content = mediaTags
			}
		}

		if extraContent != "" {
			content += extraContent
		}
	}

	// 12. Voice agent routing
	targetAgentID := c.AgentID()
	if c.cfg.VoiceAgentID != "" {
		for _, m := range mediaList {
			if m.Type == media.TypeAudio || m.Type == media.TypeVoice {
				targetAgentID = c.cfg.VoiceAgentID
				slog.Debug("feishu: routing voice inbound to speaking agent",
					"agent_id", targetAgentID, "media_type", m.Type,
				)
				break
			}
		}
	}

	// Derive userID from senderID (strip "|username" suffix if present).
	userID := mc.SenderID

	// 13. Publish to bus directly (to preserve MediaFile MIME types)
	c.Bus().PublishInbound(bus.InboundMessage{
		Channel:      c.Name(),
		SenderID:     mc.SenderID,
		ChatID:       chatID,
		Content:      content,
		Media:        mediaFiles,
		PeerKind:     peerKind,
		UserID:       userID,
		AgentID:      targetAgentID,
		HistoryLimit: c.historyLimit,
		Metadata:     metadata,
	})

	// Clear pending history after sending to agent.
	if mc.ChatType == "group" {
		c.groupHistory.Clear(chatID)
	}
}

const replyContextMaxLen = 500

// fetchReplyContext fetches the parent message content and returns a formatted
// reply context string + any downloaded media from the parent message.
func (c *Channel) fetchReplyContext(ctx context.Context, parentID string) (string, []media.MediaInfo) {
	resp, err := c.client.GetMessage(ctx, parentID)
	if err != nil {
		slog.Debug("feishu: failed to fetch parent message", "parent_id", parentID, "error", err)
		return "", nil
	}
	if len(resp.Items) == 0 {
		return "", nil
	}

	item := &resp.Items[0]
	body := parseMessageContent(item.Body.Content, item.MsgType)

	// Resolve sender name
	senderName := "unknown"
	if item.Sender.ID != "" {
		if name := c.resolveSenderName(ctx, item.Sender.ID); name != "" {
			senderName = name
		}
	}

	// Build reply context text.
	var replyCtx string
	if body != "" {
		body = channels.Truncate(body, replyContextMaxLen)
		replyCtx = fmt.Sprintf("[Replying to %s]\n%s\n[/Replying]", senderName, body)
	}

	// Download media from parent message (image, file, audio, video, sticker).
	var replyMedia []media.MediaInfo
	switch item.MsgType {
	case "image", "file", "audio", "video", "sticker":
		replyMedia = c.resolveMediaFromMessage(ctx, parentID, item.MsgType, item.Body.Content)
		for i := range replyMedia {
			replyMedia[i].FromReply = true
		}
		if len(replyMedia) > 0 {
			slog.Debug("feishu: resolved media from replied message",
				"parent_id", parentID, "media_count", len(replyMedia))
		}
	}

	return replyCtx, replyMedia
}
