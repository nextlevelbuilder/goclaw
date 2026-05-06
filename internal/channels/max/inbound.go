package max

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// pollLoop runs GET /updates in a long-poll cycle, dispatching each Update
// to handleUpdate. Exits when ctx is cancelled.
//
// Errors are classified:
//   - context.Canceled / context.DeadlineExceeded → exit cleanly
//   - APIError (auth/4xx) → log + sleep + continue (could be transient config issue)
//   - Network errors → log + exponential backoff + continue
//
// Marker is advanced only on successful response, ensuring at-least-once delivery.
func (c *Channel) pollLoop(ctx context.Context) {
	defer close(c.pollDone)

	slog.Info("max: polling started", "channel", c.Name(), "timeout_s", c.cfg.PollingTimeout)

	// Backoff state for transient errors.
	const baseBackoff = 1 * time.Second
	const maxBackoff = 30 * time.Second
	backoff := baseBackoff

	for {
		select {
		case <-ctx.Done():
			slog.Info("max: polling stopped (context cancelled)", "channel", c.Name())
			return
		default:
		}

		resp, err := c.client.GetUpdates(ctx, GetUpdatesParams{
			Limit:   100,
			Timeout: c.cfg.PollingTimeout,
			Marker:  c.getMarker(),
			Types: []string{
				UpdateTypeMessageCreated,
				UpdateTypeMessageEdited,
				UpdateTypeMessageCallback,
				UpdateTypeBotAdded,
				UpdateTypeBotRemoved,
			},
		})

		if err != nil {
			if isCtxDone(ctx, err) {
				return
			}
			c.handlePollError(err, &backoff, maxBackoff)
			continue
		}

		// Reset backoff on success.
		backoff = baseBackoff

		// Dispatch each update; advance marker only after dispatch.
		for _, u := range resp.Updates {
			c.handleUpdate(ctx, u)
		}

		// Advance marker (server tells us next page; nil means "you've seen them all").
		c.setMarker(resp.Marker)
	}
}

// handlePollError logs and waits with bounded backoff before next poll attempt.
func (c *Channel) handlePollError(err error, backoff *time.Duration, maxBackoff time.Duration) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		slog.Warn("max: API error during poll",
			"channel", c.Name(), "code", apiErr.Code, "message", apiErr.Message)
	} else {
		slog.Warn("max: poll request failed", "channel", c.Name(), "error", err)
	}

	// Mark degraded so health endpoint reflects the issue.
	c.MarkDegraded(
		"Poll request failed",
		err.Error(),
		channels.ChannelFailureKindUnknown,
		true,
	)

	time.Sleep(*backoff)
	*backoff *= 2
	if *backoff > maxBackoff {
		*backoff = maxBackoff
	}
}

// handleUpdate switches on update_type and dispatches to the right handler.
// Spawns a goroutine bounded by handlerSem for message handling; callbacks
// and other types run inline because they are short-lived.
func (c *Channel) handleUpdate(ctx context.Context, u Update) {
	switch u.UpdateType {
	case UpdateTypeMessageCreated:
		if u.Message == nil {
			return
		}
		c.spawnMessageHandler(ctx, *u.Message, false)

	case UpdateTypeMessageEdited:
		// We treat edits like new messages with metadata.edited=true.
		// Agent loop can decide to ignore or to incorporate.
		if u.Message == nil {
			return
		}
		c.spawnMessageHandler(ctx, *u.Message, true)

	case UpdateTypeMessageCallback:
		if u.Callback == nil {
			return
		}
		c.handleCallback(ctx, *u.Callback)

	case UpdateTypeBotAdded:
		slog.Info("max: bot added to chat",
			"channel", c.Name(), "chat_id", chatIDFromUpdate(u))

	case UpdateTypeBotRemoved:
		slog.Info("max: bot removed from chat",
			"channel", c.Name(), "chat_id", chatIDFromUpdate(u))

	default:
		slog.Debug("max: unhandled update type",
			"channel", c.Name(), "type", u.UpdateType)
	}
}

// chatIDFromUpdate extracts a chat_id from various update_types for logging.
func chatIDFromUpdate(u Update) int64 {
	if u.Chat != nil {
		return u.Chat.ChatID
	}
	if u.ChatID != 0 {
		return u.ChatID
	}
	if u.Message != nil && u.Message.Recipient != nil {
		return u.Message.Recipient.ChatID
	}
	return 0
}

// spawnMessageHandler enqueues a message into the bounded handler pool.
func (c *Channel) spawnMessageHandler(ctx context.Context, msg Message, edited bool) {
	select {
	case c.handlerSem <- struct{}{}:
		c.handlerWg.Add(1)
		go func() {
			defer c.handlerWg.Done()
			defer func() { <-c.handlerSem }()
			c.handleMessage(ctx, msg, edited)
		}()
	case <-ctx.Done():
		return
	}
}

// handleMessage processes a single inbound message: extracts identity,
// detects DM vs group, applies mention gate, and forwards to the agent
// loop via BaseChannel.HandleMessage.
func (c *Channel) handleMessage(ctx context.Context, msg Message, edited bool) {
	if msg.Sender == nil || msg.Recipient == nil || msg.Body == nil {
		slog.Debug("max: message missing required fields, skipping",
			"channel", c.Name(),
			"has_sender", msg.Sender != nil,
			"has_recipient", msg.Recipient != nil,
			"has_body", msg.Body != nil)
		return
	}

	// Self-loop guard: ignore messages sent BY us.
	if c.creds.BotID != 0 && msg.Sender.UserID == c.creds.BotID {
		return
	}

	senderID := strconv.FormatInt(msg.Sender.UserID, 10)

	// Resolve chatID and peerKind from recipient.chat_type (authoritative).
	// In real Max API, both user_id and chat_id are populated for DMs:
	//   user_id = bot's id, chat_id = dialog thread ID, chat_type = "dialog"
	// For groups: chat_type = "chat", chat_id = group ID.
	var chatID, peerKind string
	if msg.Recipient.IsDialog() {
		// Use recipient.chat_id (the DM thread ID) — stable per-conversation
		// identifier that goclaw can use to construct session keys.
		chatID = strconv.FormatInt(msg.Recipient.ChatID, 10)
		peerKind = "direct"
	} else {
		chatID = strconv.FormatInt(msg.Recipient.ChatID, 10)
		peerKind = "group"
	}

	content := strings.TrimSpace(msg.Body.Text)

	slog.Debug("max: message received",
		"channel", c.Name(),
		"peer_kind", peerKind,
		"sender_id", senderID,
		"chat_id", chatID,
		"text_preview", channels.Truncate(content, 60),
		"attachments", len(msg.Body.Attachments),
		"edited", edited,
	)

	// Group mention gate: in pairing/strict mode, only respond when bot is mentioned.
	if peerKind == "group" && c.RequireMention() {
		if !c.detectMention(msg) {
			slog.Debug("max: group message without bot mention, skipping",
				"channel", c.Name(), "chat_id", chatID)
			return
		}
	}

	// Skip empty messages (no text and no media).
	if content == "" && len(msg.Body.Attachments) == 0 {
		return
	}

	// Build metadata from message link (reply/forward) and locale.
	metadata := buildMetadata(msg, edited)

	// Download inbound media to local files. Empty for messages without
	// media, errors are logged + skipped (we still deliver the text).
	mediaPaths := c.downloadInboundMedia(ctx, msg.Body.Attachments)

	// Strip bot mention from content for cleaner agent input.
	if peerKind == "group" && c.creds.BotID != 0 {
		content = stripBotMention(content, c.creds.Username, c.creds.BotID)
	}

	// Hand off to BaseChannel — this enforces allowlist + publishes to bus.
	c.HandleMessage(senderID, chatID, content, mediaPaths, metadata, peerKind)
}

// handleCallback responds to inline keyboard button clicks.
// Day 2: minimal — we acknowledge with empty answer to dismiss client toast,
// then forward the payload as a regular text message to the agent.
// Day 4 will add richer callback handling.
func (c *Channel) handleCallback(ctx context.Context, cb Callback) {
	if cb.User == nil {
		return
	}

	senderID := strconv.FormatInt(cb.User.UserID, 10)
	// Callbacks don't carry chat_id directly — for Day 2 we treat them as DMs
	// from the user. Day 4 will extend Update to carry chat context.
	chatID := senderID

	slog.Debug("max: callback received",
		"channel", c.Name(),
		"sender_id", senderID,
		"payload", cb.Payload)

	// Acknowledge so the user's client dismisses the loading state.
	if err := c.client.AnswerCallback(ctx, cb.CallbackID, "", nil); err != nil {
		slog.Warn("max: failed to answer callback",
			"channel", c.Name(), "callback_id", cb.CallbackID, "error", err)
	}

	// Forward payload as text content with metadata flagging it.
	metadata := map[string]string{
		"callback":         "true",
		"callback_payload": cb.Payload,
	}
	c.HandleMessage(senderID, chatID, cb.Payload, nil, metadata, "direct")
}

// detectMention returns true if the bot is @-mentioned in the message.
//
// Strategies:
//  1. Markup of type "user_mention" with user_id == bot.user_id
//  2. Plain "@<bot_username>" substring (case-insensitive)
//
// Reply-to-bot is NOT counted as mention here — Day 2 keeps it strict.
// (Telegram's mentionGate adds this; we'll port if/when tests show need.)
func (c *Channel) detectMention(msg Message) bool {
	if msg.Body == nil {
		return false
	}

	// Strategy 1: structured markup.
	if c.creds.BotID != 0 {
		for _, m := range msg.Body.Markup {
			if m.Type == "user_mention" && m.UserID == c.creds.BotID {
				return true
			}
		}
	}

	// Strategy 2: textual @username.
	if c.creds.Username != "" {
		needle := "@" + strings.ToLower(c.creds.Username)
		if strings.Contains(strings.ToLower(msg.Body.Text), needle) {
			return true
		}
	}

	return false
}

// stripBotMention removes the bot's @-mention from text for cleaner agent input.
// Best-effort — leaves text untouched if mention pattern is ambiguous.
func stripBotMention(text, botUsername string, botID int64) string {
	if botUsername == "" {
		return text
	}
	mention := "@" + botUsername
	// Case-insensitive replace, preserving rest of the string.
	lc := strings.ToLower(text)
	lcMention := strings.ToLower(mention)
	idx := strings.Index(lc, lcMention)
	if idx < 0 {
		return text
	}
	return strings.TrimSpace(text[:idx] + " " + text[idx+len(mention):])
}

// buildMetadata extracts non-content message attributes for downstream tools.
func buildMetadata(msg Message, edited bool) map[string]string {
	md := make(map[string]string)
	if msg.Body != nil {
		md["message_id"] = msg.Body.MID
	}
	md["timestamp"] = strconv.FormatInt(msg.Timestamp, 10)
	if edited {
		md["edited"] = "true"
	}
	if msg.Link != nil {
		md["link_type"] = msg.Link.Type
		if msg.Link.Sender != nil {
			md["link_sender_id"] = strconv.FormatInt(msg.Link.Sender.UserID, 10)
		}
		if msg.Link.Message != nil {
			md["link_message_id"] = msg.Link.Message.MID
			if msg.Link.Message.Text != "" {
				md["link_message_text"] = channels.Truncate(msg.Link.Message.Text, 200)
			}
		}
	}
	return md
}

// downloadInboundMedia fetches attachment files to local paths suitable for
// passing to BaseChannel.HandleMessage. Returns paths in attachment order;
// failed downloads are skipped (logged) so partial media still flows.
//
// Day 2: stub — returns empty paths for now (logs intent).
// Day 4 will implement actual HTTP GET → temp file persistence.
func (c *Channel) downloadInboundMedia(ctx context.Context, atts []Attachment) []string {
	if len(atts) == 0 {
		return nil
	}

	var paths []string
	for _, a := range atts {
		switch a.Type {
		case AttachmentTypeImage, AttachmentTypeVideo, AttachmentTypeAudio,
			AttachmentTypeFile, AttachmentTypeSticker:
			// Day 4: download a.Payload.URL to a temp file, sanitize filename, append path.
			slog.Debug("max: media download not yet implemented",
				"channel", c.Name(), "type", a.Type, "url", a.Payload.URL)
		case AttachmentTypeContact, AttachmentTypeShare,
			AttachmentTypeLocation, AttachmentTypeInlineKeyboard:
			// Non-file attachments — skip silently.
		}
	}
	return paths
}

// =====================================================================
// Helpers
// =====================================================================

// isCtxDone returns true if err is a context cancellation derivative.
func isCtxDone(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// getMarker returns the current polling cursor (nil-safe).
func (c *Channel) getMarker() *int64 {
	c.markerMu.Lock()
	defer c.markerMu.Unlock()
	if c.marker == nil {
		return nil
	}
	v := *c.marker
	return &v
}

// setMarker updates the polling cursor.
func (c *Channel) setMarker(m *int64) {
	c.markerMu.Lock()
	defer c.markerMu.Unlock()
	c.marker = m
}
