package max

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// pollLoop runs GET /updates in a long-poll cycle, dispatching each Update
// to handleUpdate. Exits when ctx is cancelled.
//
// Errors are classified:
//   - polling context cancelled → exit cleanly with log
//   - HTTP-level timeout (http.Client.Timeout) → TRANSIENT, retry with backoff
//   - APIError (auth/4xx) → log + sleep + continue (could be transient config issue)
//   - Network errors → log + exponential backoff + continue
//
// Marker is advanced only on successful response, ensuring at-least-once delivery.
//
// Critical correctness note: pollLoop must NOT exit on HTTP-level deadline
// errors. http.Client.Timeout creates an internal child context that returns
// context.DeadlineExceeded on timeout — using errors.Is(err, context.DeadlineExceeded)
// here would conflate transient network/server slowness with operator-initiated
// shutdown, causing silent polling death (bot stops responding while pod
// stays up). See isCtxDone below for why we use ctx.Err() only.
func (c *Channel) pollLoop(ctx context.Context) {
	defer close(c.pollDone)
	defer func() {
		if r := recover(); r != nil {
			slog.Error("max: polling goroutine panicked (recovered)",
				"channel", c.Name(),
				"panic", r,
				"stack", string(debug.Stack()))
			// Mark channel degraded so the health endpoint reflects the
			// problem and operators see something is wrong.
			c.MarkDegraded(
				"Polling goroutine panicked",
				fmt.Sprintf("%v", r),
				channels.ChannelFailureKindUnknown,
				false, // not auto-recoverable from inside pollLoop
			)
		}
	}()

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

		// FIX: do NOT subscribe to UpdateTypeMessageEdited. Edit events should
		// not trigger new agent runs (the original message_created already did).
		// See follow-up analysis for details.
		resp, err := c.client.GetUpdates(ctx, GetUpdatesParams{
			Limit:   100,
			Timeout: c.cfg.PollingTimeout,
			Marker:  c.getMarker(),
			Types: []string{
				UpdateTypeMessageCreated,
				UpdateTypeMessageCallback,
				UpdateTypeBotAdded,
				UpdateTypeBotRemoved,
			},
		})

		if err != nil {
			// FIX: Check ONLY the polling context (ctx.Err()), NOT the error
			// chain. HTTP-level timeouts wrap context.DeadlineExceeded but
			// they're transient and must be retried, not treated as shutdown.
			if isCtxDone(ctx) {
				slog.Info("max: polling stopped (context cancelled during request)",
					"channel", c.Name())
				return
			}
			c.handlePollError(err, &backoff, maxBackoff)
			continue
		}

		// Reset backoff on success.
		backoff = baseBackoff

		// Heartbeat: update timestamp of last successful poll. Used by
		// health endpoint and external watchdogs to detect stuck polling.
		atomic.StoreInt64(&c.lastPollAt, time.Now().Unix())

		// Dispatch each update; advance marker only after dispatch.
		for _, u := range resp.Updates {
			c.handleUpdate(ctx, u)
		}

		// Advance marker (server tells us next page; nil means "you've seen them all").
		c.setMarker(resp.Marker)
	}
}

// handlePollError logs and waits with bounded backoff before next poll attempt.
//
// This is called for transient errors (including HTTP-level timeouts) where
// the polling context is still alive. We log, mark the channel degraded
// briefly, then sleep with exponential backoff.
func (c *Channel) handlePollError(err error, backoff *time.Duration, maxBackoff time.Duration) {
	var apiErr *APIError
	// Network-class errors (timeouts, broken pipe, conn reset) can be the
	// product of a half-broken TCP/HTTP2 connection lingering in the
	// transport pool. Track whether to evict that pool before the next
	// retry so we don't loop on the same dead connection.
	evictIdleConns := false
	switch {
	case errors.As(err, &apiErr):
		slog.Warn("max: API error during poll",
			"channel", c.Name(), "code", apiErr.Code, "message", apiErr.Message,
			"backoff_s", backoff.Seconds())
	case errors.Is(err, context.DeadlineExceeded):
		// HTTP-level timeout. NOT a polling-context cancellation (we already
		// checked that above). Log clearly so operators can distinguish from
		// a normal shutdown.
		slog.Warn("max: poll request timed out (transient, retrying)",
			"channel", c.Name(), "error", err,
			"backoff_s", backoff.Seconds())
		evictIdleConns = true
	default:
		slog.Warn("max: poll request failed",
			"channel", c.Name(), "error", err,
			"backoff_s", backoff.Seconds())
		// Pessimistically evict the pool for unknown errors too. The only
		// errors we definitely don't want to evict on are APIError (4xx/5xx
		// from a healthy connection), and those are handled above.
		evictIdleConns = true
	}

	// Drop any idle pooled connections so the next retry establishes a
	// fresh TCP/TLS session. Safe to call concurrently with in-flight
	// requests: only IDLE connections are closed; the current (failing)
	// request has already returned at this point.
	if evictIdleConns {
		c.client.CloseIdleConnections()
		slog.Debug("max: evicted idle pool connections after transient error",
			"channel", c.Name())
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
	defer func() {
		if r := recover(); r != nil {
			slog.Error("max: handleUpdate panicked (recovered)",
				"channel", c.Name(),
				"update_type", u.UpdateType,
				"panic", r,
				"stack", string(debug.Stack()))
		}
	}()

	switch u.UpdateType {
	case UpdateTypeMessageCreated:
		if u.Message == nil {
			return
		}
		c.spawnMessageHandler(ctx, *u.Message, false)

	case UpdateTypeMessageEdited:
		// FIX: do not re-dispatch edits to the agent loop. The original
		// message_created already triggered a run; an edit doesn't carry
		// new intent the agent should respond to a second time.
		if u.Message != nil && u.Message.Body != nil {
			slog.Debug("max: ignoring message_edited",
				"channel", c.Name(),
				"chat_id", chatIDFromUpdate(u),
				"message_id", u.Message.Body.MID)
		}
		return

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
			defer func() {
				if r := recover(); r != nil {
					slog.Error("max: message handler panicked (recovered)",
						"channel", c.Name(),
						"panic", r,
						"stack", string(debug.Stack()))
				}
			}()
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
	// Defense-in-depth: also check IsBot for cases where BotID was reset.
	if c.creds.BotID != 0 && msg.Sender.UserID == c.creds.BotID {
		return
	}
	if msg.Sender.IsBot {
		slog.Debug("max: ignoring message from bot sender",
			"channel", c.Name(), "sender_id", msg.Sender.UserID)
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
	mediaInfos := c.downloadInboundMediaInfo(ctx, msg.Body.Attachments)
	mediaPaths := mediaPathsFromInfos(mediaInfos)

	// Strip bot mention from content for cleaner agent input.
	if peerKind == "group" && c.creds.BotID != 0 {
		content = stripBotMention(content, c.creds.Username, c.creds.BotID)
	}

	// Make the agent aware of attachments even when the message has no
	// caption. Without this, a file sent with no accompanying text reaches
	// the agent as an empty message and is ignored. Mirrors the Telegram
	// channel via the shared media package: builds <media:*> tags and inlines
	// text-document content (binary files get a read_document hint).
	content = enrichContentWithMedia(content, mediaInfos)

	// Enforce DM / group policy before dispatch. CheckDMPolicy /
	// CheckGroupPolicy in BaseChannel evaluate allowlist + pairing state;
	// PolicyNeedsPairing causes a pairing-code reply to be sent (handled
	// inside checkDMPolicy/checkGroupPolicy) and the message is dropped.
	chatIDInt, _ := strconv.ParseInt(chatID, 10, 64)
	if peerKind == "direct" {
		if !c.checkDMPolicy(ctx, senderID, chatIDInt) {
			return
		}
	} else {
		if !c.checkGroupPolicy(ctx, senderID, chatIDInt) {
			return
		}
	}

	// Hand off to BaseChannel — publishes to bus (allowlist already
	// applied above by CheckDMPolicy / CheckGroupPolicy).
	c.HandleMessage(senderID, chatID, content, mediaPaths, metadata, peerKind)
}

// handleCallback responds to inline keyboard button clicks.
func (c *Channel) handleCallback(ctx context.Context, cb Callback) {
	if cb.User == nil {
		return
	}

	senderID := strconv.FormatInt(cb.User.UserID, 10)
	// Callbacks don't carry chat_id directly — treat as DMs.
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

// downloadInboundMedia is implemented in media_download.go.

// =====================================================================
// Helpers
// =====================================================================

// isCtxDone returns true if and only if the polling context itself has
// been cancelled by the caller (operator Stop, gateway shutdown, etc.).
//
// CRITICAL: do NOT inspect the error chain via errors.Is here. HTTP-level
// timeouts from http.Client.Timeout wrap context.DeadlineExceeded internally,
// but those are TRANSIENT errors that should be retried, not treated as
// polling cancellation. The previous version used:
//
//     errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
//
// which caused silent polling death whenever an HTTP request hit its
// 120-second client timeout (e.g. during Tailscale instability or
// upstream proxy slowness). pollLoop returned without restarting, leaving
// the bot permanently unresponsive until pod restart.
func isCtxDone(ctx context.Context) bool {
	return ctx.Err() != nil
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
