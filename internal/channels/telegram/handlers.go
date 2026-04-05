package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/typing"
)

// handleMessage processes an incoming Telegram update.
func (c *Channel) handleMessage(ctx context.Context, update telego.Update) {
	message := update.Message
	if message == nil {
		return
	}

	// Skip service messages (member added/removed, title changed, etc.).
	// These have no text/caption and no meaningful media — processing them
	// pollutes mention gate and history with "[empty message]" entries.
	if isServiceMessage(message) {
		slog.Debug("telegram service message skipped",
			"chat_id", message.Chat.ID,
			"new_members", len(message.NewChatMembers),
			"left_member", message.LeftChatMember != nil,
		)
		return
	}

	user := message.From
	if user == nil {
		return
	}

	userID := fmt.Sprintf("%d", user.ID)
	senderID := userID

	isGroup := message.Chat.Type == "group" || message.Chat.Type == "supergroup"

	slog.Debug("telegram message received",
		"chat_type", message.Chat.Type,
		"chat_id", message.Chat.ID,
		"is_group", isGroup,
		"user_id", user.ID,
		"username", user.Username,
		"channel", c.Name(),
		"text_preview", channels.Truncate(message.Text, 60),
	)

	// Forum detection (matching TS: resolveTelegramForumThreadId in src/telegram/bot/helpers.ts).
	// For non-forum groups: ignore message_thread_id (it's reply context, not a topic).
	// For forum groups without message_thread_id: default to General topic (ID=1).
	isForum := isGroup && message.Chat.IsForum
	messageThreadID := 0
	if isForum {
		messageThreadID = message.MessageThreadID
		if messageThreadID == 0 {
			messageThreadID = telegramGeneralTopicID
		}
	}

	// DM thread detection: preserve message_thread_id in private chats for session isolation.
	// Telegram supports topics/threads in bot DMs.
	dmThreadID := 0
	if !isGroup && message.MessageThreadID > 0 {
		dmThreadID = message.MessageThreadID
	}

	chatID := message.Chat.ID
	chatIDStr := fmt.Sprintf("%d", chatID)

	// Resolve per-topic config (matching TS resolveTelegramGroupConfig).
	// Merges: global defaults → wildcard group ("*") → specific group → specific topic.
	var topicCfg resolvedTopicConfig
	if isGroup {
		topicCfg = resolveTopicConfig(c.config, chatIDStr, messageThreadID)
	}

	// Group policy + enabled check (matching TS: groupPolicy ?? "open").
	if isGroup {
		// Per-topic enabled gate: if explicitly disabled, reject.
		if !topicCfg.isEnabled() {
			slog.Debug("telegram group message rejected: topic disabled",
				"chat_id", chatID, "topic_id", messageThreadID)
			return
		}

		groupPolicy := topicCfg.groupPolicy
		if groupPolicy == "" {
			groupPolicy = "open"
		}

		switch groupPolicy {
		case "disabled":
			slog.Debug("telegram group message rejected: groups disabled", "chat_id", message.Chat.ID)
			return
		case "allowlist":
			allowed := false
			for _, a := range topicCfg.allowFrom {
				if a == userID || a == senderID {
					allowed = true
					break
				}
			}
			if !allowed {
				slog.Debug("telegram group message rejected by allowlist",
					"user_id", userID, "username", user.Username, "chat_id", chatID,
				)
				return
			}
		default: // "open"
		}
	}

	// DM access control (matching TS: default is "pairing").
	if !isGroup {
		dmPolicy := c.config.DMPolicy
		if dmPolicy == "" {
			dmPolicy = "pairing"
		}

		switch dmPolicy {
		case "disabled":
			slog.Debug("telegram message rejected: DMs disabled", "user_id", userID)
			return

		case "open":
			// Allow all senders.

		case "allowlist":
			if !c.IsAllowed(userID) && !c.IsAllowed(senderID) {
				slog.Debug("telegram message rejected by allowlist",
					"user_id", userID, "username", user.Username,
				)
				return
			}

		default: // "pairing" or unknown → secure default
			paired := false
			if c.pairingService != nil {
				p1, err1 := c.pairingService.IsPaired(ctx, userID, c.Name())
				p2, err2 := c.pairingService.IsPaired(ctx, senderID, c.Name())
				if err1 != nil || err2 != nil {
					slog.Warn("security.pairing_check_failed, assuming paired (fail-open)",
						"user_id", userID, "channel", c.Name(), "err1", err1, "err2", err2)
					paired = true
				} else {
					paired = p1 || p2
				}
			}
			inAllowList := c.HasAllowList() && (c.IsAllowed(userID) || c.IsAllowed(senderID))

			if !paired && !inAllowList {
				slog.Debug("telegram message rejected: sender not paired",
					"user_id", userID, "username", user.Username, "dm_policy", dmPolicy,
				)
				c.sendPairingReply(ctx, message.Chat.ID, userID, user.Username)
				return
			}
		}
	}

	// Build composite localKey for sync.Map operations.
	// Forum topics get separate state (placeholders, streams, reactions, history).
	// TS ref: buildTelegramGroupPeerId() in src/telegram/bot/helpers.ts.
	localKey := chatIDStr
	if isForum && messageThreadID > 0 {
		localKey = fmt.Sprintf("%s:topic:%d", chatIDStr, messageThreadID)
	} else if dmThreadID > 0 {
		localKey = fmt.Sprintf("%s:thread:%d", chatIDStr, dmThreadID)
	}

	// Store thread ID for streaming/send use (looked up by localKey later).
	if messageThreadID > 0 {
		c.threadIDs.Store(localKey, messageThreadID)
	} else if dmThreadID > 0 {
		c.threadIDs.Store(localKey, dmThreadID)
	}

	// Extract text content
	content := ""
	if message.Text != "" {
		content += message.Text
	}
	if message.Caption != "" {
		if content != "" {
			content += "\n"
		}
		content += message.Caption
	}

	// Build lightweight media tags from message metadata (no download).
	// Used for pending history recording and bot command handling.
	// Actual media download + processing is deferred until after mention gating.
	if tags := lightweightMediaTags(message); tags != "" {
		if content != "" {
			content = tags + "\n\n" + content
		} else {
			content = tags
		}
	}

	// Handle bot commands BEFORE enriching with reply/forward context.
	// Command parsing (SplitN on spaces) breaks when reply context is appended with newlines,
	// e.g. "/addwriter@bot\n\n[Replying to ...]" — the bot-username check fails.
	if handled := c.handleBotCommand(ctx, message, chatID, chatIDStr, localKey, content, senderID, isGroup, isForum, messageThreadID); handled {
		return
	}

	// Enrich content with forward/reply/location context
	msgCtx := buildMessageContext(message, c.bot.Username())
	content = enrichContentWithContext(content, msgCtx)

	if content == "" {
		content = "[empty message]"
	}

	// Compute sender label for group context (used in history + current message annotation)
	displayName := strings.TrimSpace(user.FirstName + " " + user.LastName)
	senderLabel := displayName
	if user.Username != "" {
		if displayName != "" {
			senderLabel = "@" + user.Username + " (" + displayName + ")"
		} else {
			senderLabel = "@" + user.Username
		}
	}

	// --- Group mention gating (matching TS mentionGate logic) ---
	// Also check implicit mention via reply-to-bot
	//
	// mention_mode controls multi-bot behavior in groups:
	//   "strict" (default): only respond when explicitly @mentioned (require_mention=true)
	//   "yield": respond to all messages UNLESS another bot/user is @mentioned (and not us)
	//            — enables "shared group" where all bots listen, but yield when someone is called by name
	mentionMode := topicCfg.effectiveMentionMode(c.mentionMode)
	if isGroup && (topicCfg.effectiveRequireMention(c.requireMention) || mentionMode == "yield") {
		botUsername := c.bot.Username()

		// In yield mode, skip messages from other bots to prevent infinite loops.
		// Bot A responds → Bot B sees it as "no specific mention" → responds → loop.
		// Only skip when our bot is NOT explicitly mentioned — allow cross-bot @commands.
		if mentionMode == "yield" && user.IsBot && user.Username != botUsername && !c.detectMention(message, botUsername) {
			// Respect pairing guard — don't record history in unpaired groups.
			if topicCfg.groupPolicy == "pairing" && c.pairingService != nil {
				if _, cached := c.approvedGroups.Load(chatIDStr); !cached {
					groupSenderID := fmt.Sprintf("group:%d", chatID)
					paired, pairErr := c.pairingService.IsPaired(ctx, groupSenderID, c.Name())
					if pairErr != nil {
						slog.Warn("security.pairing_check_failed, assuming paired (fail-open)",
							"group_sender", groupSenderID, "channel", c.Name(), "error", pairErr)
						paired = true
					}
					if paired {
						c.approvedGroups.Store(chatIDStr, true)
					} else {
						return
					}
				}
			}
			c.groupHistory.Record(localKey, channels.HistoryEntry{
				Sender:    senderLabel,
				SenderID:  senderID,
				Body:      content,
				MediaRefs: extractMediaRefs(message),
				Timestamp: time.Unix(int64(message.Date), 0),
				MessageID: fmt.Sprintf("%d", message.MessageID),
			}, c.historyLimit)
			return
		}

		wasMentioned := c.detectMention(message, botUsername)

		// Reply to bot's message counts as implicit mention
		if !wasMentioned && msgCtx.ReplyInfo != nil && msgCtx.ReplyInfo.IsBotReply {
			wasMentioned = true
		}

		// Yield mode: skip only if another bot/user is explicitly mentioned (not us).
		// If nobody is mentioned → respond. If we are mentioned → respond.
		if mentionMode == "yield" && !wasMentioned {
			// In yield mode, respond unless someone else was specifically called out
			if !c.hasOtherMention(message, botUsername) {
				wasMentioned = true // treat as mentioned — no specific target, all bots respond
			}
		}

		slog.Debug("telegram group mention gate",
			"chat_id", chatID,
			"bot_username", botUsername,
			"require_mention", c.requireMention,
			"mention_mode", mentionMode,
			"was_mentioned", wasMentioned,
			"text_preview", channels.Truncate(content, 60),
		)

		if !wasMentioned {
			// Guard: skip recording for unpaired groups — don't leak message data.
			// Uses approvedGroups cache (same pattern as the pairing gate below).
			if topicCfg.groupPolicy == "pairing" && c.pairingService != nil {
				if _, cached := c.approvedGroups.Load(chatIDStr); !cached {
					groupSenderID := fmt.Sprintf("group:%d", chatID)
					paired, pairErr := c.pairingService.IsPaired(ctx, groupSenderID, c.Name())
					if pairErr != nil {
						slog.Warn("security.pairing_check_failed, assuming paired (fail-open)",
							"group_sender", groupSenderID, "channel", c.Name(), "error", pairErr)
						paired = true
					}
					if paired {
						c.approvedGroups.Store(chatIDStr, true)
					} else {
						return // silently skip — no pending history, no contact
					}
				}
			}

			c.groupHistory.Record(localKey, channels.HistoryEntry{
				Sender:    senderLabel,
				SenderID:  senderID,
				Body:      content,
				MediaRefs: extractMediaRefs(message),
				Timestamp: time.Unix(int64(message.Date), 0),
				MessageID: fmt.Sprintf("%d", message.MessageID),
			}, c.historyLimit)

			// Collect contact even when bot is not mentioned (cache prevents DB spam).
			if cc := c.ContactCollector(); cc != nil {
				contactName := strings.TrimSpace(user.FirstName + " " + user.LastName)
				cc.EnsureContact(ctx, c.Type(), c.Name(), senderID, userID, contactName, user.Username, "group", "user", "", "")
				// Also collect group chat itself as a contact (for group permission / merge).
				cc.EnsureContact(ctx, c.Type(), c.Name(), chatIDStr, "", message.Chat.Title, "", "group", "group", "", "")
				// Collect forum topic as a distinct delivery target (including General).
				if isForum && messageThreadID > 0 {
					threadStr := fmt.Sprintf("%d", messageThreadID)
					cc.EnsureContact(ctx, c.Type(), c.Name(), chatIDStr, "", message.Chat.Title, "", "group", "topic", threadStr, "topic")
				}
			}

			slog.Debug("telegram group message recorded (no mention)",
				"chat_id", chatID, "sender", senderLabel,
			)
			return
		}
	}

	// --- Group pairing gate (only reached when bot is mentioned) ---
	if isGroup && topicCfg.groupPolicy == "pairing" && c.pairingService != nil {
		if _, cached := c.approvedGroups.Load(chatIDStr); !cached {
			groupSenderID := fmt.Sprintf("group:%d", chatID)
			paired, err := c.pairingService.IsPaired(ctx, groupSenderID, c.Name())
			if err != nil {
				slog.Warn("security.pairing_check_failed, assuming paired (fail-open)",
					"group_sender", groupSenderID, "channel", c.Name(), "error", err)
				paired = true
			}
			if paired {
				c.approvedGroups.Store(chatIDStr, true)
			} else {
				c.sendGroupPairingReply(ctx, chatID, chatIDStr, groupSenderID, localKey, messageThreadID, message.Chat.Title)
				return
			}
		}
	}

	// --- Media download (only when bot will process the message) ---
	// Deferred until after mention + pairing gates to avoid downloading
	// media for messages that only get recorded in pending history.
	mediaList, mediaErrors := c.resolveMedia(ctx, message)
	if message.ReplyToMessage != nil {
		replyMedia, replyErrors := c.resolveMedia(ctx, message.ReplyToMessage)
		if len(replyMedia) > 0 {
			// Tag reply media so LLM knows which images came from the replied-to message.
			for i := range replyMedia {
				replyMedia[i].FromReply = true
			}
			// Reply media first (context), current media second.
			mediaList = append(replyMedia, mediaList...)
			slog.Debug("telegram: resolved media from replied message",
				"reply_msg_id", message.ReplyToMessage.MessageID,
				"media_count", len(replyMedia),
			)
		}
		mediaErrors = append(mediaErrors, replyErrors...)
	}

	var mediaFiles []bus.MediaFile
	if len(mediaList) > 0 {
		var extraContent string
		for i := range mediaList {
			m := &mediaList[i]
			switch m.Type {
			case "audio", "voice":
				transcript, sttErr := c.transcribeAudio(ctx, m.FilePath)
				if sttErr != nil {
					slog.Warn("telegram: STT transcription failed",
						"type", m.Type, "error", sttErr)
				} else {
					m.Transcript = transcript
				}
			case "document":
				if m.FileName != "" && m.FilePath != "" {
					docContent, err := extractDocumentContent(m.FilePath, m.FileName)
					if err != nil {
						slog.Warn("document extraction failed", "file", m.FileName, "error", err)
					} else if docContent != "" {
						extraContent += "\n\n" + docContent
					}
				}
			case "video", "animation":
				// Handled by read_video tool via MediaRef pipeline.
			}
			if m.FilePath != "" {
				mediaFiles = append(mediaFiles, bus.MediaFile{
					Path:     m.FilePath,
					MimeType: m.ContentType,
				})
			}
		}

		// Replace lightweight media tags with full tags (includes transcripts).
		fullTags := buildMediaTags(mediaList)
		lightTags := lightweightMediaTags(message)
		if lightTags != "" && fullTags != "" {
			content = strings.Replace(content, lightTags, fullTags, 1)
		} else if fullTags != "" {
			if content != "" {
				content = fullTags + "\n\n" + content
			} else {
				content = fullTags
			}
		}
		if extraContent != "" {
			content += extraContent
		}
	}

	// Annotate content + notify user for any media download failures.
	// Replace the per-type lightweight tag with an error-annotated version so the
	// model knows the specific media was skipped and why.
	if len(mediaErrors) > 0 {
		for _, me := range mediaErrors {
			errTag := fmt.Sprintf("[sent media (%s) — skipped: %s]", me.Type, me.Reason)
			if lightTag := lightweightTagForType(me.Type, message); lightTag != "" {
				content = strings.Replace(content, lightTag, errTag, 1)
			} else {
				content = errTag + "\n" + content
			}
		}

		// Send a short reply so the user knows their file was skipped.
		for _, me := range mediaErrors {
			var errText string
			if me.MaxBytes > 0 {
				errText = fmt.Sprintf("⚠️ File too large (max %d MB). Skipped.", me.MaxBytes/(1024*1024))
			} else {
				errText = "⚠️ Failed to download the attached file. Skipped."
			}
			_ = c.sendHTML(ctx, chatID, errText, 0, messageThreadID)
		}
	}

	slog.Debug("telegram message received",
		"sender_id", senderID,
		"chat_id", fmt.Sprintf("%d", chatID),
		"preview", channels.Truncate(content, 50),
	)

	// Build context from pending group history (if any).
	// Annotate current message with sender name so LLM knows who is talking.
	finalContent := content
	if isGroup {
		annotated := fmt.Sprintf("[From: %s]\n%s", senderLabel, content)
		if c.historyLimit > 0 {
			// Resolve deferred media from history entries (lazy download).
			if histRefs := c.groupHistory.CollectMediaRefs(localKey); len(histRefs) > 0 {
				histMedia, histErrors := c.resolveMediaRefs(ctx, histRefs)
				for _, m := range histMedia {
					mediaFiles = append(mediaFiles, bus.MediaFile{
						Path:     m.FilePath,
						MimeType: m.ContentType,
					})
				}
				if len(histMedia) > 0 {
					histTags := buildMediaTags(histMedia)
					annotated = "[Media from recent group messages — only analyze if user asks about them]\n" + histTags + "\n[/Media]\n\n" + annotated
				}
				for _, e := range histErrors {
					slog.Warn("telegram: history media download failed",
						"type", e.Type, "reason", e.Reason)
				}
			}
			finalContent = c.groupHistory.BuildContext(localKey, annotated, c.historyLimit)
		} else {
			finalContent = annotated
		}
	} else {
		// DM: annotate with sender identity so the agent knows who is messaging.
		finalContent = fmt.Sprintf("[From: %s]\n%s", senderLabel, content)
	}

	// Send typing indicator with keepalive + TTL safety net.
	// Telegram typing expires after 5s, so keepalive every 4s.
	// TTL auto-stops after 60s to prevent stuck indicators.
	chatIDObj := tu.ID(chatID)
	typingCtrl := typing.New(typing.Options{
		MaxDuration:       60 * time.Second,
		KeepaliveInterval: 4 * time.Second,
		StartFn: func() error {
			action := tu.ChatAction(chatIDObj, telego.ChatActionTyping)
			if messageThreadID > 0 {
				action.MessageThreadID = messageThreadID
			}
			return c.bot.SendChatAction(ctx, action)
		},
	})
	// Stop previous typing controller for this chat/topic (if any)
	if prev, ok := c.typingCtrls.Load(localKey); ok {
		prev.(*typing.Controller).Stop()
	}
	c.typingCtrls.Store(localKey, typingCtrl)
	typingCtrl.Start()

	// Stop previous thinking animation for this chat/topic
	if prevStop, ok := c.stopThinking.Load(localKey); ok {
		if cf, ok := prevStop.(*thinkingCancel); ok {
			cf.Cancel()
		}
	}

	// Create thinking cancel for this chat/topic
	_, thinkCancel := context.WithCancel(ctx)
	c.stopThinking.Store(localKey, &thinkingCancel{fn: thinkCancel})

	// No "Thinking..." placeholder — the DraftStream creates its own message
	// on the first streaming chunk (sendMessage on first flush).
	// This avoids "reply to deleted message" artifacts and is cleaner UX:
	// user sees typing indicator → first content appears directly.

	metadata := map[string]string{
		"message_id": fmt.Sprintf("%d", message.MessageID),
		"user_id":    fmt.Sprintf("%d", user.ID),
		"username":   user.Username,
		"first_name": user.FirstName,
		"is_group":   fmt.Sprintf("%t", isGroup),
		"local_key":  localKey,
	}
	if message.Chat.Title != "" {
		metadata["chat_title"] = message.Chat.Title
	}
	if isForum {
		metadata["is_forum"] = "true"
		metadata["message_thread_id"] = fmt.Sprintf("%d", messageThreadID)
	}
	if dmThreadID > 0 {
		metadata["dm_thread_id"] = fmt.Sprintf("%d", dmThreadID)
		metadata["message_thread_id"] = fmt.Sprintf("%d", dmThreadID)
	}
	if topicCfg.systemPrompt != "" {
		metadata["topic_system_prompt"] = topicCfg.systemPrompt
	}
	if topicCfg.skills != nil {
		metadata["topic_skills"] = strings.Join(topicCfg.skills, ",")
	}

	peerKind := "direct"
	if isGroup {
		peerKind = "group"
	}

	// Audio-aware routing: if a voice/audio message was received and a dedicated speaking agent
	// is configured, route to that agent instead of the default channel agent.
	// This prevents voice turns from landing on a text-router agent that cannot handle audio.
	targetAgentID := c.AgentID()
	if c.config.VoiceAgentID != "" {
		for _, m := range mediaList {
			if m.Type == "audio" || m.Type == "voice" {
				targetAgentID = c.config.VoiceAgentID
				slog.Debug("telegram: routing voice inbound to speaking agent",
					"agent_id", targetAgentID, "media_type", m.Type,
				)
				break
			}
		}
	}

	// Collect contact for processed messages (DM + group-mentioned).
	if cc := c.ContactCollector(); cc != nil {
		contactName := strings.TrimSpace(user.FirstName + " " + user.LastName)
		cc.EnsureContact(ctx, c.Type(), c.Name(), senderID, userID, contactName, user.Username, peerKind, "user", "", "")
		// Also collect group chat itself as a contact (for group permission / merge).
		if isGroup {
			cc.EnsureContact(ctx, c.Type(), c.Name(), chatIDStr, "", message.Chat.Title, "", "group", "group", "", "")
			// Collect forum topic as a distinct delivery target (including General).
			if isForum && messageThreadID > 0 {
				threadStr := fmt.Sprintf("%d", messageThreadID)
				cc.EnsureContact(ctx, c.Type(), c.Name(), chatIDStr, "", message.Chat.Title, "", "group", "topic", threadStr, "topic")
			}
		}
	}

	c.Bus().PublishInbound(bus.InboundMessage{
		Channel:      c.Name(),
		SenderID:     senderID,
		ChatID:       chatIDStr,
		Content:      finalContent,
		Media:        mediaFiles,
		PeerKind:     peerKind,
		UserID:       userID,
		AgentID:      targetAgentID,
		HistoryLimit: c.historyLimit,
		ToolAllow:    topicCfg.tools,
		TenantID:     c.TenantID(),
		Metadata:     metadata,
	})

	// Clear pending history after sending to agent.
	if isGroup {
		c.groupHistory.Clear(localKey)
	}
}
