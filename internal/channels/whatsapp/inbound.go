package whatsapp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const emptyMessageSentinel = "[empty message]"

// handleIncomingMessage processes an incoming WhatsApp message.
func (c *Channel) handleIncomingMessage(evt *events.Message) {
	ctx := context.Background()
	ctx = store.WithTenantID(ctx, c.TenantID())

	if evt.Info.IsFromMe {
		peerKind := "direct"
		if evt.Info.Chat.Server == types.GroupServer {
			peerKind = "group"
		}
		slog.Debug("whatsapp inbound dropped",
			c.inboundLogAttrs(peerKind, evt.Info.Sender.String(), evt.Info.Chat.String(),
				"reason", "from_self",
				"message_id", string(evt.Info.ID))...)
		return
	}

	senderJID := evt.Info.Sender
	chatJID := evt.Info.Chat

	// WhatsApp uses dual identity: phone JID (@s.whatsapp.net) and LID (@lid).
	// Groups may use LID addressing. Normalize to phone JID for consistent
	// policy checks, pairing lookups, allowlists, and contact collection.
	if evt.Info.AddressingMode == types.AddressingModeLID && !evt.Info.SenderAlt.IsEmpty() {
		senderJID = evt.Info.SenderAlt
	}

	senderID := senderJID.String()
	chatID := chatJID.String()

	peerKind := "direct"
	if chatJID.Server == types.GroupServer {
		peerKind = "group"
	}

	slog.Debug("whatsapp inbound received",
		c.inboundLogAttrs(peerKind, senderID, chatID,
			"message_id", string(evt.Info.ID),
			"addressing", evt.Info.AddressingMode,
			"dm_policy", effectiveWhatsAppDMPolicy(c.config.DMPolicy),
			"group_policy", effectiveWhatsAppGroupPolicy(c.config.GroupPolicy))...)

	// DM/Group policy check.
	if peerKind == "direct" {
		if !c.checkDMPolicy(ctx, senderID, chatID) {
			slog.Info("whatsapp inbound dropped",
				c.inboundLogAttrs(peerKind, senderID, chatID,
					"reason", "dm_policy",
					"policy", effectiveWhatsAppDMPolicy(c.config.DMPolicy),
					"message_id", string(evt.Info.ID))...)
			return
		}
	} else {
		if !c.checkGroupPolicy(ctx, senderID, chatID) {
			slog.Info("whatsapp inbound dropped",
				c.inboundLogAttrs(peerKind, senderID, chatID,
					"reason", "group_policy",
					"policy", effectiveWhatsAppGroupPolicy(c.config.GroupPolicy),
					"message_id", string(evt.Info.ID))...)
			return
		}
	}

	if peerKind == "direct" && !c.openDMAllowlistAllows(senderID) {
		slog.Info("whatsapp inbound dropped",
			c.inboundLogAttrs(peerKind, senderID, chatID,
				"reason", "dm_allowlist",
				"policy", effectiveWhatsAppDMPolicy(c.config.DMPolicy),
				"message_id", string(evt.Info.ID))...)
		return
	}

	content := extractTextContent(evt.Message)

	// Command interception: check for slash commands before the normal pipeline.
	if handled := c.handleCommand(ctx, content, senderID, chatID, peerKind, chatJID); handled {
		return
	}

	var mediaList []media.MediaInfo
	mediaList = c.downloadMedia(evt)

	if content == "" && len(mediaList) == 0 {
		slog.Info("whatsapp inbound dropped",
			c.inboundLogAttrs(peerKind, senderID, chatID,
				"reason", "empty_message",
				"message_id", string(evt.Info.ID))...)
		return
	}
	if content == "" {
		content = emptyMessageSentinel
	}

	// Group history + mention detection.
	historyLimit := c.config.HistoryLimit
	if historyLimit == 0 {
		historyLimit = channels.DefaultGroupHistoryLimit
	}
	if peerKind == "group" && c.config.RequireMention != nil && *c.config.RequireMention {
		if !c.isMentioned(evt) {
			slog.Info("whatsapp inbound dropped",
				c.inboundLogAttrs(peerKind, senderID, chatID,
					"reason", "mention_required",
					"message_id", string(evt.Info.ID))...)
			// Not mentioned — record for context and skip.
			senderLabel := evt.Info.PushName
			if senderLabel == "" {
				senderLabel = senderID
			}
			c.GroupHistory().Record(chatID, channels.HistoryEntry{
				Sender:    senderLabel,
				SenderID:  senderID,
				Body:      content,
				Timestamp: evt.Info.Timestamp,
				MessageID: string(evt.Info.ID),
			}, historyLimit)
			return
		}
		// Mentioned — prepend accumulated group context.
		content = c.GroupHistory().BuildContext(chatID, content, historyLimit)
		c.GroupHistory().Clear(chatID)
	}

	metadata := map[string]string{
		"message_id": string(evt.Info.ID),
	}
	if evt.Info.PushName != "" {
		metadata["user_name"] = evt.Info.PushName
	}

	// STT: transcribe audio items (opt-in via builtin_tools[stt].settings.whatsapp_enabled,
	// default false per Decision 6 — enabling breaks E2E encryption for voice messages).
	waSttSettings := c.loadSTTSettings(ctx)
	locale := "" // i18n.T falls back to English when locale is empty
	for i := range mediaList {
		m := &mediaList[i]
		if m.Type == media.TypeAudio || m.Type == media.TypeVoice {
			mimeType := m.ContentType
			if mimeType == "" {
				mimeType = "audio/ogg"
			}
			m.Transcript = c.transcribeVoice(ctx, m.FilePath, mimeType, locale, waSttSettings)
		}
	}

	// Build media tags and bus.MediaFile list.
	var mediaFiles []bus.MediaFile
	if len(mediaList) > 0 {
		mediaTags := media.BuildMediaTags(mediaList)
		if mediaTags != "" {
			if content != emptyMessageSentinel {
				content = mediaTags + "\n\n" + content
			} else {
				content = mediaTags
			}
		}
		for _, m := range mediaList {
			if m.FilePath != "" {
				mediaFiles = append(mediaFiles, bus.MediaFile{
					Path: m.FilePath, MimeType: m.ContentType, Filename: m.FileName,
				})
			}
		}
	}

	// Annotate with sender identity.
	if senderName := metadata["user_name"]; senderName != "" {
		content = fmt.Sprintf("[From: %s]\n%s", senderName, content)
	}

	// Collect contact.
	if cc := c.ContactCollector(); cc != nil {
		cc.EnsureContact(ctx, c.Type(), c.Name(), senderID, senderID,
			metadata["user_name"], "", peerKind, "user", "", "")
	}

	// Typing indicator.
	if prevCancel, ok := c.typingCancel.LoadAndDelete(chatID); ok {
		if fn, ok := prevCancel.(context.CancelFunc); ok {
			fn()
		}
	}
	typingCtx, typingCancel := context.WithCancel(context.Background())
	c.typingCancel.Store(chatID, typingCancel)
	go c.keepTyping(typingCtx, chatJID)

	// Derive userID from senderID.
	userID := senderID
	if idx := strings.IndexByte(senderID, '|'); idx > 0 {
		userID = senderID[:idx]
	}

	if c.AgentID() == "" {
		slog.Warn("whatsapp inbound accepted without configured agent",
			c.inboundLogAttrs(peerKind, senderID, chatID, "message_id", string(evt.Info.ID))...)
	} else {
		slog.Debug("whatsapp inbound accepted",
			c.inboundLogAttrs(peerKind, senderID, chatID, "message_id", string(evt.Info.ID))...)
	}

	c.Bus().PublishInbound(bus.InboundMessage{
		Channel:  c.Name(),
		SenderID: senderID,
		ChatID:   chatID,
		Content:  content,
		Media:    mediaFiles,
		PeerKind: peerKind,
		UserID:   userID,
		AgentID:  c.AgentID(),
		TenantID: c.TenantID(),
		Metadata: metadata,
	})

	// Schedule temp media file cleanup after agent pipeline has had time to process.
	var tmpPaths []string
	for _, mf := range mediaFiles {
		tmpPaths = append(tmpPaths, mf.Path)
	}
	scheduleMediaCleanup(tmpPaths, 5*time.Minute)
}

func (c *Channel) inboundLogAttrs(peerKind, senderID, chatID string, extra ...any) []any {
	attrs := []any{
		"channel", c.Name(),
		"tenant", c.TenantID(),
		"peer", peerKind,
		"sender_hash", hashWhatsAppIdentifier(senderID),
		"chat_hash", hashWhatsAppIdentifier(chatID),
		"agent_id_present", c.AgentID() != "",
	}
	return append(attrs, extra...)
}

func hashWhatsAppIdentifier(id string) string {
	if id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(id))
	return fmt.Sprintf("%x", sum[:6])
}

func effectiveWhatsAppDMPolicy(policy string) string {
	if policy == "" {
		return "pairing"
	}
	return policy
}

func effectiveWhatsAppGroupPolicy(policy string) string {
	if policy == "" {
		return "open"
	}
	return policy
}

func (c *Channel) openDMAllowlistAllows(senderID string) bool {
	if effectiveWhatsAppDMPolicy(c.config.DMPolicy) != "open" {
		return true
	}
	if !c.HasAllowList() {
		return true
	}
	return c.IsAllowed(senderID)
}

// extractTextContent extracts text from any WhatsApp message variant.
// Includes quoted message context when present (reply-to messages).
func extractTextContent(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}

	var text string
	var quotedText string

	if msg.GetConversation() != "" {
		text = msg.GetConversation()
	} else if ext := msg.GetExtendedTextMessage(); ext != nil {
		text = ext.GetText()
		// Extract quoted (replied-to) message text.
		if ci := ext.GetContextInfo(); ci != nil {
			if qm := ci.GetQuotedMessage(); qm != nil {
				quotedText = extractQuotedText(qm)
			}
		}
	} else if img := msg.GetImageMessage(); img != nil {
		text = img.GetCaption()
	} else if vid := msg.GetVideoMessage(); vid != nil {
		text = vid.GetCaption()
	} else if doc := msg.GetDocumentMessage(); doc != nil {
		text = doc.GetCaption()
	}

	if quotedText != "" && text != "" {
		return fmt.Sprintf("[Replying to: %s]\n%s", quotedText, text)
	}
	if quotedText != "" {
		return fmt.Sprintf("[Replying to: %s]", quotedText)
	}
	return text
}

// extractQuotedText extracts plain text from a quoted message (no recursion).
func extractQuotedText(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	if msg.GetConversation() != "" {
		return msg.GetConversation()
	}
	if ext := msg.GetExtendedTextMessage(); ext != nil {
		return ext.GetText()
	}
	if img := msg.GetImageMessage(); img != nil && img.GetCaption() != "" {
		return img.GetCaption()
	}
	if vid := msg.GetVideoMessage(); vid != nil && vid.GetCaption() != "" {
		return vid.GetCaption()
	}
	return ""
}

// isMentioned checks if the linked account is @mentioned in a group message.
// WhatsApp uses dual identity: phone JID and LID. Mentions may use either format.
func (c *Channel) isMentioned(evt *events.Message) bool {
	c.lastQRMu.RLock()
	myJID := c.myJID
	myLID := c.myLID
	c.lastQRMu.RUnlock()

	if myJID.IsEmpty() && myLID.IsEmpty() {
		return false // fail closed: unknown identity = not mentioned
	}

	// Check mentioned JIDs from extended text.
	if ext := evt.Message.GetExtendedTextMessage(); ext != nil {
		if ci := ext.GetContextInfo(); ci != nil {
			for _, jidStr := range ci.GetMentionedJID() {
				mentioned, _ := types.ParseJID(jidStr)
				if !myJID.IsEmpty() && mentioned.User == myJID.User {
					return true
				}
				if !myLID.IsEmpty() && mentioned.User == myLID.User {
					return true
				}
			}
		}
	}
	return false
}
