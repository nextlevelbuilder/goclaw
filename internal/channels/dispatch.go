package channels

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// WebhookRoute holds a path and handler pair for mounting on the main gateway mux.
type WebhookRoute struct {
	Path    string
	Handler http.Handler
}

// dispatchOutbound consumes outbound messages from the bus and routes them
// to the appropriate channel. Internal channels are silently skipped.
func (m *Manager) dispatchOutbound(ctx context.Context) {
	slog.Info("outbound dispatcher started")

	for {
		select {
		case <-ctx.Done():
			slog.Info("outbound dispatcher stopped")
			return
		default:
			msg, ok := m.bus.SubscribeOutbound(ctx)
			if !ok {
				continue
			}

			// Skip internal channels
			if IsInternalChannel(msg.Channel) {
				continue
			}

			m.mu.RLock()
			channel, exists := m.channels[msg.Channel]
			m.mu.RUnlock()

			if !exists {
				slog.Warn("unknown channel for outbound message", "channel", msg.Channel)
				continue
			}

			// Filter out temp media files that no longer exist (already sent by another dispatch).
			if len(msg.Media) > 0 {
				tmpDir := os.TempDir()
				filtered := msg.Media[:0]
				for _, media := range msg.Media {
					if media.URL != "" && strings.HasPrefix(media.URL, tmpDir) {
						if _, err := os.Stat(media.URL); err != nil {
							slog.Debug("skipping already-delivered temp media", "path", media.URL)
							continue
						}
					}
					filtered = append(filtered, media)
				}
				msg.Media = filtered
				// If only media was in this message and all files are gone, skip entirely.
				if len(msg.Media) == 0 && msg.Content == "" {
					continue
				}
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

			// Clean up temp media files only. Workspace-generated files are preserved
			// so they remain accessible via workspace/web UI after delivery.
			tmpDir := os.TempDir()
			for _, media := range msg.Media {
				if media.URL != "" && strings.HasPrefix(media.URL, tmpDir) {
					if err := os.Remove(media.URL); err != nil {
						slog.Debug("failed to clean up media file", "path", media.URL, "error", err)
					}
				}
			}
		}
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
