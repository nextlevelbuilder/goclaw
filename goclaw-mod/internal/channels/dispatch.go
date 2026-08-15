package channels

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// WebhookRoute holds a path and handler pair for mounting on the main gateway mux.
type WebhookRoute struct {
	Path    string
	Handler http.Handler
}

// Outbound dispatch is sharded by conversation.
//
// A single dispatch goroutine made every reply in the process — every channel,
// every user — wait behind the slowest Send. One media upload of a few seconds
// froze all outbound traffic for that long. The ordering the old loop actually
// protected is per-conversation (a run emits block replies, retry notices and
// the final answer to one ChatID, and those must arrive in that order); it
// never needed a global order.
//
// Sharding on channel+ChatID keeps that guarantee — a conversation always maps
// to the same shard and is processed serially there — while letting unrelated
// conversations proceed in parallel.
const (
	defaultOutboundShards = 8

	// outboundShardQueue buffers each shard so a brief stall on one Send does
	// not immediately push back on the reader (which would re-serialize the
	// shards). The bus itself remains the real backpressure boundary.
	outboundShardQueue = 256
)

// outboundShardCount resolves the dispatch worker count.
//
//	GOCLAW_OUTBOUND_SHARDS=8
//
// Raising it past the point where the *remote* API meters us (DingTalk's
// per-app card QPS, a provider's rate limit) only moves queueing from our
// process into theirs.
func outboundShardCount() int {
	if v := os.Getenv("GOCLAW_OUTBOUND_SHARDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultOutboundShards
}

// outboundShardIndex maps a conversation to a stable shard.
//
// The channel name is part of the key because ChatIDs are only unique within a
// channel — two channels can legitimately use the same conversation id.
func outboundShardIndex(channel, chatID string, shards int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(channel))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(chatID))
	return int(h.Sum32() % uint32(shards))
}

// dispatchOutbound consumes outbound messages from the bus and fans them out to
// per-conversation shard workers. Internal channels are silently skipped.
func (m *Manager) dispatchOutbound(ctx context.Context) {
	shards := outboundShardCount()

	queues := make([]chan bus.OutboundMessage, shards)
	var wg sync.WaitGroup
	for i := range queues {
		queues[i] = make(chan bus.OutboundMessage, outboundShardQueue)
		wg.Add(1)
		go func(id int, q <-chan bus.OutboundMessage) {
			defer wg.Done()
			m.dispatchShard(ctx, id, q)
		}(i, queues[i])
	}

	slog.Info("outbound dispatcher started", "shards", shards)

	defer func() {
		// Closing the queues drains whatever each shard already accepted, so a
		// reply that made it out of the bus is not dropped on shutdown.
		for _, q := range queues {
			close(q)
		}
		wg.Wait()
		slog.Info("outbound dispatcher stopped")
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, ok := m.bus.SubscribeOutbound(ctx)
		if !ok {
			return
		}
		if IsInternalChannel(msg.Channel) {
			continue
		}

		idx := outboundShardIndex(msg.Channel, msg.ChatID, shards)
		select {
		case queues[idx] <- msg:
		case <-ctx.Done():
			return
		}
	}
}

// dispatchShard delivers one shard's messages, strictly in order.
func (m *Manager) dispatchShard(ctx context.Context, id int, q <-chan bus.OutboundMessage) {
	for msg := range q {
		m.deliverOutbound(ctx, msg)
	}
	slog.Debug("outbound shard stopped", "shard", id)
}

// deliverOutbound sends one message and cleans up its temp media.
func (m *Manager) deliverOutbound(ctx context.Context, msg bus.OutboundMessage) {
	m.mu.RLock()
	channel, exists := m.channels[msg.Channel]
	m.mu.RUnlock()

	if !exists {
		slog.Warn("unknown channel for outbound message", "channel", msg.Channel)
		return
	}

	kept, claimed := m.claimTempMedia(msg.Media)
	defer m.releaseTempMedia(claimed)
	msg.Media = kept

	// If only media was in this message and every file is gone, skip entirely.
	if len(msg.Media) == 0 && msg.Content == "" {
		return
	}

	// Add tenant context for per-tenant TTS auto-apply
	sendCtx := ctx
	if msg.TenantID != uuid.Nil {
		sendCtx = store.WithTenantID(ctx, msg.TenantID)
	}

	// Add agent audio context for per-agent TTS voice override
	if msg.AgentID != uuid.Nil && len(msg.AgentOtherConfig) > 0 {
		sendCtx = store.WithAgentAudio(sendCtx, store.AgentAudioSnapshot{
			AgentID:     msg.AgentID,
			OtherConfig: msg.AgentOtherConfig,
		})
	}

	if err := channel.Send(sendCtx, msg); err != nil {
		m.handleSendFailure(sendCtx, channel, msg, err)
	}
}

// claimTempMedia filters msg.Media down to the files this dispatch owns, and
// returns the temp paths it claimed so they can be released after the send.
//
// Serial dispatch deduped temp media implicitly: the first delivery removed the
// file, and a later message carrying the same path found os.Stat failing and
// skipped it. Across concurrent shards that check alone is a TOCTOU race — two
// shards can both stat a live file and upload it twice.
//
// The claim set closes the window. Entries live only for the duration of a
// send, so the set stays bounded, and a message that arrives after the owner
// finished still finds the file gone and skips it exactly as before.
//
// Non-temp media (workspace files) is passed through untouched: it is never
// removed after delivery, so it needs no claim.
func (m *Manager) claimTempMedia(media []bus.MediaAttachment) (kept []bus.MediaAttachment, claimed []string) {
	if len(media) == 0 {
		return nil, nil
	}

	tmpDir := os.TempDir()
	kept = make([]bus.MediaAttachment, 0, len(media))

	for _, mf := range media {
		if mf.URL == "" || !strings.HasPrefix(mf.URL, tmpDir) {
			kept = append(kept, mf)
			continue
		}
		if _, err := os.Stat(mf.URL); err != nil {
			slog.Debug("skipping already-delivered temp media", "path", mf.URL)
			continue
		}
		if _, loaded := m.mediaClaims.LoadOrStore(mf.URL, struct{}{}); loaded {
			slog.Debug("skipping temp media already claimed by another shard", "path", mf.URL)
			continue
		}
		kept = append(kept, mf)
		claimed = append(claimed, mf.URL)
	}
	return kept, claimed
}

// releaseTempMedia removes delivered temp files and drops their claims.
//
// Workspace-generated files are preserved so they remain accessible via the
// workspace/web UI after delivery; only os.TempDir() entries reach here.
func (m *Manager) releaseTempMedia(claimed []string) {
	for _, path := range claimed {
		if err := os.Remove(path); err != nil {
			slog.Debug("failed to clean up media file", "path", path, "error", err)
		}
		m.mediaClaims.Delete(path)
	}
}

// handleSendFailure reports a failed channel.Send from dispatchOutbound.
//
// Cross-target forwards (message tool, forward=true) are fire-and-forget onto
// the bus — the tool already returned "sent" to the model and announced
// success to the origin chat before this consumer ever ran. If the
// destination itself was bad (e.g. the model passed a display name instead
// of a real chat ID), this tells the ORIGIN chat the truth instead of
// retrying against the same broken destination or, for text-only forwards,
// silently dropping the failure.
//
// Non-forward sends keep the older behavior: retry-notify the same chat, and
// only for media failures (text-only failures on a chat the agent is already
// bound to usually mean the chat itself is inaccessible — kicked, blocked —
// so retrying won't help).
func (m *Manager) handleSendFailure(sendCtx context.Context, channel Channel, msg bus.OutboundMessage, sendErr error) {
	slog.Error("error sending message to channel",
		"channel", msg.Channel,
		"chat_id", msg.ChatID,
		"content_len", len(msg.Content),
		"content_preview", Truncate(msg.Content, 160),
		"error", sendErr,
	)

	if originCh := msg.Metadata[bus.MetaForwardOriginChannel]; originCh != "" {
		originChat := msg.Metadata[bus.MetaForwardOriginChatID]
		m.mu.RLock()
		origin, originExists := m.channels[originCh]
		m.mu.RUnlock()
		if originExists && originChat != "" {
			notifyMsg := bus.OutboundMessage{
				Channel: originCh,
				ChatID:  originChat,
				Content: fmt.Sprintf("⚠️ Không gửi được tin nhắn forward tới %q — kiểm tra lại đích gửi (có thể chưa đúng ID nhóm/chat).", msg.ChatID),
			}
			if err2 := origin.Send(sendCtx, notifyMsg); err2 != nil {
				slog.Warn("failed to send forward-failure notice to origin",
					"origin_channel", originCh, "origin_chat", originChat, "error", err2)
			}
		}
		return
	}

	if len(msg.Media) > 0 {
		notifyMsg := bus.OutboundMessage{
			Channel:  msg.Channel,
			ChatID:   msg.ChatID,
			Content:  formatChannelSendError(sendErr),
			Metadata: sendErrorMeta(msg.Metadata),
			TenantID: msg.TenantID,
		}
		if err2 := channel.Send(sendCtx, notifyMsg); err2 != nil {
			slog.Warn("failed to send error notification",
				"channel", msg.Channel, "error", err2)
		}
	}
}

// WebhookHandlers returns all webhook handlers from channels that implement WebhookChannel.
// Used to mount webhook routes on the main gateway mux.
func (m *Manager) WebhookHandlers() []WebhookRoute {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var routes []WebhookRoute
	for _, ch := range m.channels {
		if wh, ok := ch.(WebhookChannel); ok {
			if path, handler := wh.WebhookHandler(); path != "" && handler != nil {
				routes = append(routes, WebhookRoute{Path: path, Handler: handler})
			}
		}
	}
	return routes
}

// SendToChannel delivers a message to a specific channel by name.
func (m *Manager) SendToChannel(ctx context.Context, channelName, chatID, content string) error {
	m.mu.RLock()
	channel, exists := m.channels[channelName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("channel %s not found", channelName)
	}

	msg := bus.OutboundMessage{
		Channel: channelName,
		ChatID:  chatID,
		Content: content,
	}

	return channel.Send(ctx, msg)
}

// MessageEditor is optionally implemented by channels that support editing an
// existing message in place (e.g. Telegram admin editing a channel post).
type MessageEditor interface {
	EditMessage(ctx context.Context, chatID string, messageID int, content string) error
}

// EditChannelMessage edits an existing message in a channel by name. Returns an
// error if the channel is unknown or its type does not support editing.
func (m *Manager) EditChannelMessage(ctx context.Context, channelName, chatID string, messageID int, content string) error {
	m.mu.RLock()
	channel, exists := m.channels[channelName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("channel %s not found", channelName)
	}
	editor, ok := channel.(MessageEditor)
	if !ok {
		return fmt.Errorf("channel %s (%s) does not support editing messages", channelName, channel.Type())
	}
	return editor.EditMessage(ctx, chatID, messageID, content)
}

// MessageReactor is optionally implemented by channels that can set an emoji
// reaction on an existing message.
type MessageReactor interface {
	ReactToMessage(ctx context.Context, chatID string, messageID int, emoji string) error
}

// ReactToMessage sets an emoji reaction on a message in a channel by name.
// Returns an error if the channel is unknown or does not support reactions.
func (m *Manager) ReactToMessage(ctx context.Context, channelName, chatID string, messageID int, emoji string) error {
	m.mu.RLock()
	channel, exists := m.channels[channelName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("channel %s not found", channelName)
	}
	reactor, ok := channel.(MessageReactor)
	if !ok {
		return fmt.Errorf("channel %s (%s) does not support reactions", channelName, channel.Type())
	}
	return reactor.ReactToMessage(ctx, chatID, messageID, emoji)
}

// TopicMessagePoster is optionally implemented by channels that can post a
// message into a forum topic and return the sent message's id.
type TopicMessagePoster interface {
	PostToTopic(ctx context.Context, chatID string, threadID int, content string) (int, error)
}

// PostToTopic posts a message into a forum topic of a channel and returns the
// sent message id. Errors if the channel is unknown or does not support it.
func (m *Manager) PostToTopic(ctx context.Context, channelName, chatID string, threadID int, content string) (int, error) {
	m.mu.RLock()
	channel, exists := m.channels[channelName]
	m.mu.RUnlock()

	if !exists {
		return 0, fmt.Errorf("channel %s not found", channelName)
	}
	poster, ok := channel.(TopicMessagePoster)
	if !ok {
		return 0, fmt.Errorf("channel %s (%s) does not support topic posting", channelName, channel.Type())
	}
	return poster.PostToTopic(ctx, chatID, threadID, content)
}

// SendMediaToChannel delivers a message with media attachments to a specific channel by name.
// media must be non-empty; use SendToChannel for text-only messages.
// Returns ErrMediaUnsupported if the channel type does not support media.
func (m *Manager) SendMediaToChannel(ctx context.Context, channelName, chatID, content string, media []bus.MediaAttachment) error {
	if len(media) == 0 {
		return fmt.Errorf("SendMediaToChannel: media slice must not be empty; use SendToChannel for text-only messages")
	}

	m.mu.RLock()
	channel, exists := m.channels[channelName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("channel %s not found", channelName)
	}

	if !IsMediaCapable(channel.Type()) {
		return fmt.Errorf("%w: %s (%s)", ErrMediaUnsupported, channelName, channel.Type())
	}

	msg := bus.OutboundMessage{
		Channel: channelName,
		ChatID:  chatID,
		Content: content,
		Media:   media,
	}

	return channel.Send(ctx, msg)
}

// --- Send error notification helpers ---

// telegramAPIDescRe extracts the human-readable description from Telegram Bot API errors.
// Example: `telego: sendPhoto: api: 400 "Bad Request: not enough rights to send photos to the chat"`
//
//	→ "not enough rights to send photos to the chat"
var telegramAPIDescRe = regexp.MustCompile(`"Bad Request:\s*(.+?)"`)

// formatChannelSendError converts a channel.Send error into a user-friendly message.
// Never exposes raw library/HTTP details.
func formatChannelSendError(err error) string {
	raw := err.Error()
	lower := strings.ToLower(raw)

	// Telegram "Bad Request: <description>" — extract description
	if m := telegramAPIDescRe.FindStringSubmatch(raw); len(m) == 2 {
		return fmt.Sprintf("⚠️ Send failed: %s", m[1])
	}

	// Common Telegram API errors (non-Bad Request)
	switch {
	case strings.Contains(lower, "not enough rights"):
		return "⚠️ Send failed: bot doesn't have permission to send this type of message."
	case strings.Contains(lower, "chat not found"):
		return "⚠️ Send failed: chat not found."
	case strings.Contains(lower, "bot was blocked"):
		return "⚠️ Send failed: bot was blocked by the user."
	case strings.Contains(lower, "user is deactivated"):
		return "⚠️ Send failed: user account is deactivated."
	case strings.Contains(lower, "too many requests") || strings.Contains(lower, "flood"):
		return "⚠️ Send failed: rate limited by Telegram. Please try again later."
	case strings.Contains(lower, "file is too big") || strings.Contains(lower, "wrong file"):
		return "⚠️ Send failed: file is too large or invalid for Telegram."
	}

	// Generic fallback — don't expose internals
	return "⚠️ Failed to deliver message. Check bot logs for details."
}

// sendErrorMeta copies only the routing fields from outbound metadata.
// Strips reply_to_message_id, placeholder_key, audio_as_voice, etc.
// that could cause unintended side effects on the error notification.
func sendErrorMeta(orig map[string]string) map[string]string {
	if orig == nil {
		return nil
	}
	meta := make(map[string]string)
	for _, k := range []string{"local_key", "message_thread_id"} {
		if v := orig[k]; v != "" {
			meta[k] = v
		}
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}
