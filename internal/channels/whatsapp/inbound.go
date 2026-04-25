package whatsapp

import (
	"context"
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
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const emptyMessageSentinel = "[empty message]"

// handleIncomingMessage processes an incoming WhatsApp message.
func (c *Channel) handleIncomingMessage(evt *events.Message) {
	ctx := context.Background()
	ctx = store.WithTenantID(ctx, c.TenantID())

	if evt.Info.IsFromMe {
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

	slog.Debug("whatsapp incoming", "peer", peerKind, "sender", senderID, "chat", chatID,
		"addressing", evt.Info.AddressingMode, "policy", c.config.GroupPolicy)

	// DM/Group policy check.
	if peerKind == "direct" {
		if !c.checkDMPolicy(ctx, senderID, chatID) {
			return
		}
	} else {
		if !c.checkGroupPolicy(ctx, senderID, chatID) {
			slog.Info("whatsapp group message rejected by policy", "sender_id", senderID, "chat_id", chatID, "policy", c.config.GroupPolicy)
			return
		}
	}

	if !c.IsAllowed(senderID) {
		slog.Info("whatsapp message rejected by allowlist", "sender_id", senderID)
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
		return
	}
	if content == "" {
		content = emptyMessageSentinel
	}

	// Save original text content (before media tags overwrite it) for caption buffer logic.
	hasCaption := content != "" && content != emptyMessageSentinel

	// Group history + mention detection.
	historyLimit := c.config.HistoryLimit
	if historyLimit == 0 {
		historyLimit = channels.DefaultGroupHistoryLimit
	}
	requireMention := c.effectiveRequireMention(chatID, peerKind)
	if peerKind == "group" && requireMention != nil && *requireMention {
		if !c.isMentioned(evt) {
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

	// Save raw content before sender annotation (used by listen-only raw storage).
	rawContent := content

	// Annotate with sender identity.
	if senderName := metadata["user_name"]; senderName != "" {
		content = fmt.Sprintf("[From: %s]\n%s", senderName, content)
	}

	// Collect contact.
	if cc := c.ContactCollector(); cc != nil {
		cc.EnsureContact(ctx, c.Type(), c.Name(), senderID, senderID,
			metadata["user_name"], "", peerKind, "user", "", "")
	}

	// Collect group as a contact for UI group discovery.
	if cc := c.ContactCollector(); cc != nil && peerKind == "group" {
		cc.EnsureContact(ctx, c.Type(), c.Name(), chatID, "", "", "", "group", "group", "", "")
	}

	// Resolve group-specific agent override.
	targetAgentID := c.resolveAgentID(chatID, peerKind)
	if peerKind == "group" {
		slog.Info("whatsapp group routing", "chat_id", chatID, "default_agent", c.AgentID(),
			"groups_count", len(c.config.Groups))
		if c.config.Groups != nil {
			if grp, ok := c.config.Groups[chatID]; ok && grp != nil {
				if grp.Enabled != nil && !*grp.Enabled {
					slog.Info("whatsapp group message rejected: group disabled", "chat_id", chatID)
					return
				}
				if grp.AgentID != "" && grp.AgentID != "__default__" {
					slog.Info("whatsapp group agent override applied", "chat_id", chatID, "agent_id", targetAgentID)
				}
			} else {
				slog.Info("whatsapp group no override found", "chat_id", chatID, "available_keys", groupKeys(c.config.Groups))
			}
		}
	}

	// Final routing summary log (unconditional — always fires for group messages).
	if peerKind == "group" {
		slog.Info("whatsapp routing resolved",
			"chat_id", chatID,
			"default_agent", c.AgentID(),
			"final_agent", targetAgentID,
			"override_applied", targetAgentID != c.AgentID(),
			"groups_configured", len(c.config.Groups),
		)
	}

	// Listen-only mode: buffer message for raw storage instead of responding.
	// If require_mention is configured and bot IS mentioned, fall through to normal pipeline.
	slog.Debug("whatsapp listen-only check",
		"chat_id", chatID, "peer_kind", peerKind,
		"listen_only", c.isListenOnly(chatID, peerKind),
		"listenBuf_nil", c.listenBuf == nil)
	if c.isListenOnly(chatID, peerKind) {
		rm := c.effectiveRequireMention(chatID, peerKind)
		mentioned := peerKind == "group" && rm != nil && *rm && c.isMentioned(evt)
		if !mentioned {
			// Persist media files to durable storage before storing the raw message.
			var persistedRefs []store.RawMediaRef
			var failedPaths []string
			effectiveAgentUUID := c.groupAgentUUID(chatID)
			graphID := c.resolveGraphID(chatID, peerKind)
			if len(mediaList) > 0 && c.listenBuf != nil {
				persistedRefs, failedPaths = c.listenBuf.PersistMedia(
					effectiveAgentUUID, graphID, mediaList,
				)
			} else {
				// No listen buffer — all media goes to cleanup.
				for _, mi := range mediaList {
					if mi.FilePath != "" {
						failedPaths = append(failedPaths, mi.FilePath)
					}
				}
			}
			// Clean up only media that failed to persist.
			scheduleMediaCleanup(failedPaths, 5*time.Minute)

			if c.listenBuf != nil {
				senderName := metadata["user_name"]
				if senderName == "" {
					senderName = senderID
				}
				// Look up group name from config for richer context.
				chatName := ""
				if peerKind == "group" && c.config.Groups != nil {
					if grp, ok := c.config.Groups[chatID]; ok && grp != nil {
						chatName = grp.Name
					}
				}
				c.listenBuf.Add(graphID, listenEntry{
					Sender:    senderName,
					SenderID:  senderID,
					Content:   rawContent,
					Timestamp: evt.Info.Timestamp,
					ChatID:    chatID,
					ChatName:  chatName,
					AgentID:   effectiveAgentUUID,
					MediaRefs: persistedRefs,
				})
			}
			return
		}
		// Mentioned in listen-only group — fall through to normal response pipeline.
		slog.Info("whatsapp listen: bot mentioned, responding normally",
			"chat_id", chatID)
	}

	// --- Media caption buffer (opt-in via media_caption_delay_ms > 0). ---
	// Default: disabled. The gateway-level InboundDebouncer handles media+caption
	// merging with the standard debounce window. This buffer provides an additional
	// WhatsApp-specific merge window for longer gaps.
	if c.captionBuf != nil {
		// Case 1: Text-only message. Check if it's a caption for a buffered media message.
		if len(mediaList) == 0 && rawContent != "" && rawContent != emptyMessageSentinel {
			if _, matched := c.captionBuf.PushText(chatID, senderID, rawContent); matched {
				slog.Debug("whatsapp: caption merged into buffered media",
					"chat_id", chatID, "sender_id", senderID)
				return
			}
		}

		// Case 2: Media with no caption text. Buffer it to wait for a follow-up.
		if len(mediaList) > 0 && !hasCaption {
			// Start typing indicator before buffering (good UX).
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

			// Build full InboundMessage for deferred publish.
			inboundMsg := bus.InboundMessage{
				Channel:  c.Name(),
				SenderID: senderID,
				ChatID:   chatID,
				Content:  content,
				Media:    mediaFiles,
				PeerKind: peerKind,
				UserID:   userID,
				AgentID:  targetAgentID,
				TenantID: c.TenantID(),
				Metadata: metadata,
			}

			var tmpPaths []string
			for _, mf := range mediaFiles {
				tmpPaths = append(tmpPaths, mf.Path)
			}

			buffered := c.captionBuf.PushMedia(chatID, senderID, inboundMsg, tmpPaths)
			if buffered {
				slog.Debug("whatsapp: media buffered, waiting for caption",
					"chat_id", chatID, "sender_id", senderID)
				return
			}
		}
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

	c.Bus().PublishInbound(bus.InboundMessage{
		Channel:  c.Name(),
		SenderID: senderID,
		ChatID:   chatID,
		Content:  content,
		Media:    mediaFiles,
		PeerKind: peerKind,
		UserID:   userID,
		AgentID:  targetAgentID,
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
	if doc := msg.GetDocumentMessage(); doc != nil {
		caption := doc.GetCaption()
		if caption != "" {
			return caption
		}
		if name := doc.GetFileName(); name != "" {
			return fmt.Sprintf("[Document: %s]", name)
		}
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

// groupKeys returns the keys of the groups map for logging.
func groupKeys(m map[string]*config.WhatsAppGroupConfig) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}