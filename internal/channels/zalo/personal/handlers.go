package personal

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/replycontext"
	"github.com/nextlevelbuilder/goclaw/internal/channels/typing"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
	"github.com/nextlevelbuilder/goclaw/internal/store"
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
	ctx := context.Background()
	ctx = store.WithTenantID(ctx, c.TenantID())
	senderID := msg.Data.UIDFrom
	threadID := msg.ThreadID()

	if !c.checkDMPolicy(ctx, senderID, threadID) {
		return
	}

	body, media := extractContentAndMedia(msg.Data.Content)
	// A reply to an image should carry the image itself, not just the
	// "[Quoted image]" placeholder — download it and attach as media.
	media = append(media, extractQuoteMedia(msg.Data.Quote)...)
	content := replycontext.Compose(c.buildReplyContext(threadID, msg.Data.Quote), body)
	if content == "" {
		return
	}

	// Annotate with sender display name so the agent knows who is messaging.
	senderName := msg.Data.DName
	if senderName != "" {
		content = fmt.Sprintf("[From: %s]\n%s", senderName, content)
	}
	c.rememberReplyContext(threadID, displayNameOrID(senderName, senderID), msg.Data, cacheBodyForContent(msg.Data.Content))

	slog.Debug("zalo_personal DM received",
		"sender", senderID,
		"dname", senderName,
		"thread", threadID,
		"preview", channels.Truncate(content, 50),
	)

	c.startTyping(threadID, protocol.ThreadTypeUser)

	// Collect contact for DM messages.
	if cc := c.ContactCollector(); cc != nil {
		cc.EnsureContact(ctx, c.Type(), c.Name(), senderID, senderID, senderName, "", "direct", "user", "", "")
	}

	metadata := map[string]string{
		"message_id":   msg.Data.MsgID,
		"platform":     channels.TypeZaloPersonal,
		"display_name": channels.SanitizeDisplayName(senderName),
	}
	c.HandleAuthorizedMessage(senderID, threadID, content, media, metadata, "direct")
}

func (c *Channel) handleGroupMessage(msg protocol.GroupMessage) {
	ctx := context.Background()
	ctx = store.WithTenantID(ctx, c.TenantID())
	senderID := msg.Data.UIDFrom
	threadID := msg.ThreadID()

	// Step 1: enforce access policy (allowlist/pairing). Hard reject — don't record history.
	if !c.checkGroupPolicy(ctx, senderID, threadID) {
		return
	}

	senderName := msg.Data.DName
	if senderName == "" {
		senderName = senderID
	}

	body, media := extractContentAndMedia(msg.Data.Content)
	// A reply to an image should carry the image itself, not just the
	// "[Quoted image]" placeholder — download it and attach as media.
	media = append(media, extractQuoteMedia(msg.Data.Quote)...)
	content := replycontext.Compose(c.buildReplyContext(threadID, msg.Data.Quote), body)
	if content == "" {
		return
	}
	c.rememberReplyContext(threadID, senderName, msg.Data.TMessage, cacheBodyForContent(msg.Data.Content))

	// Step 2: @mention gating — record non-mentioned messages in history and return.
	if c.RequireMention() {
		wasMentioned := c.checkBotMentioned(msg.Data.Mentions)
		if !wasMentioned {
			c.GroupHistory().Record(threadID, channels.HistoryEntry{
				Sender:    senderName,
				SenderID:  senderID,
				Body:      content,
				Media:     media,
				Timestamp: time.Now(),
				MessageID: msg.Data.MsgID,
			}, c.HistoryLimit())

			// Collect contact even when bot is not mentioned (cache prevents DB spam).
			if cc := c.ContactCollector(); cc != nil {
				cc.EnsureContact(ctx, c.Type(), c.Name(), senderID, senderID, senderName, "", "group", "user", "", "")
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
	if c.HistoryLimit() > 0 {
		finalContent = c.GroupHistory().BuildContext(threadID, annotated, c.HistoryLimit())
	}

	c.startTyping(threadID, protocol.ThreadTypeGroup)

	// Collect media from pending history entries (images sent before this @mention).
	// Must come after BuildContext — CollectMedia nulls out Media fields to prevent double-cleanup.
	histMedia := c.GroupHistory().CollectMedia(threadID)
	allMedia := append(histMedia, media...)

	// Collect contact for group-mentioned messages.
	if cc := c.ContactCollector(); cc != nil {
		cc.EnsureContact(ctx, c.Type(), c.Name(), senderID, senderID, senderName, "", "group", "user", "", "")
	}

	metadata := map[string]string{
		"message_id":   msg.Data.MsgID,
		"platform":     channels.TypeZaloPersonal,
		"group_id":     threadID,
		"display_name": channels.SanitizeDisplayName(senderName),
	}
	c.HandleMessage(senderID, threadID, finalContent, allMedia, metadata, "group")

	// Clear pending history after sending to agent (matches Telegram/Discord/Slack/Feishu pattern).
	c.GroupHistory().Clear(threadID)
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

func extractContentAndMediaWithQuote(content protocol.Content, quote *protocol.TQuote) (string, []string) {
	text, media := extractContentAndMedia(content)
	media = append(media, extractQuoteMedia(quote)...)
	return replycontext.Compose(formatQuoteContext(quote), text), media
}
