package feishu

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// messageContext holds parsed information from a Feishu message event.
type messageContext struct {
	ChatID       string
	MessageID    string
	SenderID     string // sender_id.open_id
	ChatType     string // "p2p" or "group"
	Content      string
	ContentType  string // "text", "post", "image", etc.
	MentionedBot bool
	RootID       string // reply-chain root (populated on ANY reply, incl. plain quote reply)
	ParentID     string // direct parent in reply chain
	ThreadID     string // set ONLY when message is inside an actual topic thread
	Mentions     []mentionInfo
}

type mentionInfo struct {
	Key       string // @_user_N placeholder
	OpenID    string
	UserID    string
	UnionID   string
	Name      string
	TenantKey string
}

// handleMessageEvent processes an incoming Feishu message event.
func (c *Channel) handleMessageEvent(ctx context.Context, event *MessageEvent) {
	c.handleMessageEventFrom(ctx, event, "direct")
}

func (c *Channel) handleMessageEventFrom(ctx context.Context, event *MessageEvent, source string) {
	// Inject tenant scope so store queries filter by the correct tenant_id.
	ctx = store.WithTenantID(ctx, c.TenantID())

	if event == nil {
		return
	}
	if !c.shouldProcessMessageEvent(event, source) {
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
	c.logParsedMessage(event, mc, source)

	// 2a. Slash commands in DMs are rejected early with a clear hint so
	// they never reach the agent pipeline (otherwise users typing
	// "/addwriter" in a DM would waste an LLM turn). The full writer
	// command router is gated behind group policy below at step 5a.
	if mc.ChatType != "group" && c.isWriterSlashCommand(mc) {
		c.sendCommandReply(ctx, mc, "This command only works in group chats.")
		return
	}

	if mc.ChatType == "group" && len(mc.Mentions) > 0 && !mc.MentionedBot {
		slog.Info("feishu group message skipped; explicit mention is not this bot",
			"source", source,
			"decision", "skip_non_target_mention",
			"channel", c.Name(),
			"event_id", event.Header.EventID,
			"message_id", mc.MessageID,
			"chat_id", mc.ChatID,
			"sender_id", mc.SenderID,
			"bot_open_id", c.botOpenID,
			"mention_count", len(mc.Mentions),
		)
		return
	}

	// 3. Resolve sender name (cached)
	senderName := c.resolveSenderName(ctx, mc.SenderID)

	// 4. Resolve media BEFORE mention gate so non-mentioned messages
	// also have their files downloaded and stored in pending history.
	var earlyMedia []media.MediaInfo
	switch mc.ContentType {
	case "image", "file", "audio", "video", "sticker":
		earlyMedia = c.resolveMediaFromMessage(ctx, mc.MessageID, mc.ContentType, msg.Content)
	case "post":
		if imageKeys := extractPostImageKeys(msg.Content); len(imageKeys) > 0 {
			earlyMedia = c.resolvePostImages(ctx, mc.MessageID, imageKeys)
		}
	}
	var earlyMediaPaths []string
	for _, m := range earlyMedia {
		if m.FilePath != "" {
			earlyMediaPaths = append(earlyMediaPaths, m.FilePath)
		}
	}

	// 5. Group policy
	if mc.ChatType == "group" {
		isWriterCommand := c.isWriterSlashCommand(mc)

		// 5a. RequireMention check runs before pairing for normal messages so
		// non-target bots stay silent in multi-agent groups. Do not create a
		// pairing request for messages that did not mention this bot.
		requireMention := true
		if c.cfg.RequireMention != nil {
			requireMention = *c.cfg.RequireMention
		}
		slog.Debug("feishu group mention gate",
			"source", source,
			"decision", "evaluate_group_gate",
			"channel", c.Name(),
			"event_id", event.Header.EventID,
			"message_id", mc.MessageID,
			"chat_id", mc.ChatID,
			"sender_id", mc.SenderID,
			"bot_open_id", c.botOpenID,
			"mentioned_bot", mc.MentionedBot,
			"mention_count", len(mc.Mentions),
			"mentions", formatMentionInfos(mc.Mentions),
			"require_mention", requireMention,
			"group_policy", c.cfg.GroupPolicy,
			"is_writer_command", isWriterCommand,
		)
		if !isWriterCommand && requireMention && !mc.MentionedBot {
			if !c.canRecordUnmentionedGroupMessage(ctx, mc.SenderID, mc.ChatID) {
				slog.Debug("feishu group message skipped; no bot mention and policy not approved",
					"source", source,
					"decision", "skip_no_mention_unapproved",
					"channel", c.Name(),
					"event_id", event.Header.EventID,
					"message_id", mc.MessageID,
					"chat_id", mc.ChatID,
					"sender_id", mc.SenderID,
				)
				return
			}

			historyKey := mc.ChatID
			if mc.RootID != "" && c.cfg.TopicSessionMode == "enabled" {
				historyKey = fmt.Sprintf("%s:topic:%s", mc.ChatID, mc.RootID)
			}
			c.GroupHistory().Record(historyKey, channels.HistoryEntry{
				Sender:    senderName,
				SenderID:  mc.SenderID,
				Body:      mc.Content,
				Media:     earlyMediaPaths,
				Timestamp: time.Now(),
				MessageID: messageID,
			}, c.HistoryLimit())

			// Collect contact even when bot is not mentioned (cache prevents DB spam).
			if cc := c.ContactCollector(); cc != nil {
				cc.EnsureContact(ctx, c.Type(), c.Name(), mc.SenderID, mc.SenderID, senderName, "", "group", "user", "", "")
			}

			slog.Debug("feishu group message recorded without bot mention",
				"source", source,
				"decision", "record_no_mention_history",
				"channel", c.Name(),
				"event_id", event.Header.EventID,
				"message_id", mc.MessageID,
				"chat_id", mc.ChatID,
				"sender", senderName,
			)
			return
		}

		if !c.checkGroupPolicy(ctx, mc.SenderID, mc.ChatID) {
			slog.Debug("feishu group message rejected by policy",
				"source", source,
				"decision", "reject_group_policy",
				"channel", c.Name(),
				"event_id", event.Header.EventID,
				"message_id", mc.MessageID,
				"sender_id", mc.SenderID,
				"chat_id", mc.ChatID,
			)
			return
		}

		// 5b. Writer management slash commands run AFTER the group policy
		// gate so commands cannot bypass allowlists or pairing. Commands
		// short-circuit the agent pipeline to avoid consuming LLM tokens.
		if isWriterCommand && c.maybeHandleWriterCommand(ctx, mc) {
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

	// 7a. Lark doc auto-fetch: expand any docx URLs in the message body into
	// inline context blocks so the agent can read linked docs without a tool
	// call. Cached per channel for the TTL window. Missing permission / dead
	// links fail soft with a marker string.
	content = c.resolveLarkDocs(ctx, content)

	// 7b. Fetch reply context + media if this is a reply to another message.
	// We intentionally do NOT recurse into resolveLarkDocs for the parent
	// message body — expanding doc URLs in older messages would bloat the
	// prompt unpredictably (one quote reply could drag in multiple docs the
	// user never intended to reference). Users must include the doc URL in
	// their own new message to get auto-fetch behavior.
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

	slog.Debug("feishu message accepted",
		"source", source,
		"decision", "publish_inbound",
		"channel", c.Name(),
		"event_id", event.Header.EventID,
		"message_id", mc.MessageID,
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
		cc.EnsureContact(ctx, c.Type(), c.Name(), mc.SenderID, mc.SenderID, senderName, "", peerKind, "user", "", "")
	}

	metadata := map[string]string{
		"message_id":    messageID,
		"chat_type":     mc.ChatType,
		"sender_name":   senderName,
		"display_name":  channels.SanitizeDisplayName(senderName),
		"mentioned_bot": fmt.Sprintf("%t", mc.MentionedBot),
		"platform":      channels.TypeFeishu,
	}

	// Thread routing: stamp the triggering message ID ONLY when the inbound
	// message is inside an actual topic thread (thread_id present per Lark
	// docs). We deliberately do NOT fire on mc.RootID — Lark populates root_id
	// on every reply including plain quote replies outside any thread, and
	// routing those through the reply endpoint would silently promote them to
	// new threads. thread_id is the definitive signal.
	//
	// Outbound Send() reads this key and, when non-empty, routes to the Lark
	// reply endpoint with reply_in_thread=true so the bot response lands
	// inside the same thread. Absent on non-thread messages — preserves
	// existing new-message endpoint behavior for DMs, plain groups, and quote
	// replies.
	if mc.ThreadID != "" {
		metadata["feishu_reply_target_id"] = messageID
	}

	if sender != nil {
		metadata["sender_open_id"] = sender.SenderID.OpenID
	}

	// Annotate content with sender identity so the agent knows who is messaging.
	if senderName != "" {
		if mc.ChatType == "group" {
			annotated := fmt.Sprintf("[From: %s]\n%s", senderName, content)
			if c.HistoryLimit() > 0 {
				content = c.GroupHistory().BuildContext(chatID, annotated, c.HistoryLimit())
			} else {
				content = annotated
			}
		} else {
			// DM: annotate with sender identity so the agent knows who is messaging.
			content = fmt.Sprintf("[From: %s]\n%s", senderName, content)
		}
	}

	// 10. Build media list from early-resolved media (step 4) + reply media.
	// Media was already downloaded before the mention gate — reuse results.
	var mediaList []media.MediaInfo
	// Reply media first (context), current-message media second.
	if len(replyMediaList) > 0 {
		mediaList = append(mediaList, replyMediaList...)
	}
	mediaList = append(mediaList, earlyMedia...)

	// 10b. Collect media from pending history (files downloaded by earlier non-mentioned messages).
	var mediaFiles []bus.MediaFile
	if mc.ChatType == "group" && c.HistoryLimit() > 0 {
		if histMediaPaths := c.GroupHistory().CollectMedia(chatID); len(histMediaPaths) > 0 {
			for _, p := range histMediaPaths {
				// Original filename not retained in pending-history paths; fall back to basename.
				mediaFiles = append(mediaFiles, bus.MediaFile{Path: p, Filename: filepath.Base(p)}) // cannot use append(slice, other...) — different types
			}
		}
	}

	// 11. Process media: STT transcription, document extraction, build tags
	if len(mediaList) > 0 {
		var extraContent string
		for i := range mediaList {
			m := &mediaList[i]

			switch m.Type {
			case media.TypeAudio, media.TypeVoice:
				var transcript string
				var sttErr error
				if c.audioMgr != nil {
					sttCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
					res, err := c.audioMgr.Transcribe(sttCtx, audio.STTInput{FilePath: m.FilePath, MimeType: "audio/ogg"}, audio.STTOptions{})
					cancel()
					if err == nil && res != nil {
						transcript = res.Text
					} else {
						sttErr = err
					}
				}
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
					Filename: m.FileName,
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
		HistoryLimit: c.HistoryLimit(),
		TenantID:     c.TenantID(),
		Metadata:     metadata,
	})

	// Clear pending history after sending to agent.
	if mc.ChatType == "group" {
		c.GroupHistory().Clear(chatID)
	}
}

const replyContextMaxLen = 2000

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

	// Download media from parent message (image, file, audio, video, sticker, post).
	var replyMedia []media.MediaInfo
	switch item.MsgType {
	case "image", "file", "audio", "video", "sticker":
		replyMedia = c.resolveMediaFromMessage(ctx, parentID, item.MsgType, item.Body.Content)
	case "post":
		if imageKeys := extractPostImageKeys(item.Body.Content); len(imageKeys) > 0 {
			replyMedia = c.resolvePostImages(ctx, parentID, imageKeys)
		}
	}
	for i := range replyMedia {
		replyMedia[i].FromReply = true
	}
	if len(replyMedia) > 0 {
		slog.Debug("feishu: resolved media from replied message",
			"parent_id", parentID, "media_count", len(replyMedia))
	}

	return replyCtx, replyMedia
}
