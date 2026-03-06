package channels

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// RunContext tracks an active agent run for streaming/reaction event forwarding.
type RunContext struct {
	ChannelName  string
	ChatID       string
	MessageID    string // platform message ID (string to support Feishu "om_xxx", Telegram "12345", etc.)
	Metadata     map[string]string // outbound routing metadata (thread_id, local_key, group_id)
	Streaming         bool              // whether run uses streaming (to avoid double-delivery of block replies)
	BlockReplyEnabled bool              // whether block.reply delivery is enabled for this run (resolved at RegisterRun time)
	mu           sync.Mutex
	streamBuffer string // accumulated streaming text (chunks are deltas)
	inToolPhase  bool   // true after tool.call, reset on next chunk (new LLM iteration)
}

// Manager manages all registered channels, handling their lifecycle
// and routing outbound messages to the correct channel.
type Manager struct {
	channels     map[string]Channel
	bus          *bus.MessageBus
	runs         sync.Map // runID string → *RunContext
	dispatchTask *asyncTask
	mu           sync.RWMutex
}

type asyncTask struct {
	cancel context.CancelFunc
}

// NewManager creates a new channel manager.
// Channels are registered externally via RegisterChannel.
func NewManager(msgBus *bus.MessageBus) *Manager {
	return &Manager{
		channels: make(map[string]Channel),
		bus:      msgBus,
	}
}

// StartAll starts all registered channels and the outbound dispatch loop.
// The dispatcher is always started even when no channels exist yet,
// because channels may be loaded dynamically later via Reload().
func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Always start the outbound dispatcher — channels may be added later via Reload().
	dispatchCtx, cancel := context.WithCancel(ctx)
	m.dispatchTask = &asyncTask{cancel: cancel}
	go m.dispatchOutbound(dispatchCtx)

	if len(m.channels) == 0 {
		slog.Warn("no channels enabled")
		return nil
	}

	slog.Info("starting all channels")

	for name, channel := range m.channels {
		slog.Info("starting channel", "channel", name)
		if err := channel.Start(ctx); err != nil {
			slog.Error("failed to start channel", "channel", name, "error", err)
		}
	}

	slog.Info("all channels started")
	return nil
}

// StopAll gracefully stops all channels and the outbound dispatch loop.
func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	slog.Info("stopping all channels")

	if m.dispatchTask != nil {
		m.dispatchTask.cancel()
		m.dispatchTask = nil
	}

	for name, channel := range m.channels {
		slog.Info("stopping channel", "channel", name)
		if err := channel.Stop(ctx); err != nil {
			slog.Error("error stopping channel", "channel", name, "error", err)
		}
	}

	slog.Info("all channels stopped")
	return nil
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

			if err := channel.Send(ctx, msg); err != nil {
				slog.Error("error sending message to channel",
					"channel", msg.Channel,
					"error", err,
				)
				// Try to send a text-only error notification back to the chat.
				// Only for media failures — text-only failures likely mean the chat
				// is inaccessible (kicked, blocked, etc.) so retrying won't help.
				if len(msg.Media) > 0 {
					notifyMsg := bus.OutboundMessage{
						Channel:  msg.Channel,
						ChatID:   msg.ChatID,
						Content:  formatChannelSendError(err),
						Metadata: sendErrorMeta(msg.Metadata),
					}
					if err2 := channel.Send(ctx, notifyMsg); err2 != nil {
						slog.Warn("failed to send error notification",
							"channel", msg.Channel, "error", err2)
					}
				}
			}

			// Clean up temporary media files after successful (or failed) send.
			// Files are created by tools (create_image, tts) and only needed for the send.
			for _, media := range msg.Media {
				if media.URL != "" {
					if err := os.Remove(media.URL); err != nil {
						slog.Debug("failed to clean up media file", "path", media.URL, "error", err)
					}
				}
			}
		}
	}
}

// GetChannel returns a channel by name.
func (m *Manager) GetChannel(name string) (Channel, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	channel, ok := m.channels[name]
	return channel, ok
}

// GetStatus returns the running status of all channels.
func (m *Manager) GetStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[string]interface{})
	for name, channel := range m.channels {
		status[name] = map[string]interface{}{
			"enabled": true,
			"running": channel.IsRunning(),
		}
	}
	return status
}

// GetEnabledChannels returns the names of all enabled channels.
func (m *Manager) GetEnabledChannels() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.channels))
	for name := range m.channels {
		names = append(names, name)
	}
	return names
}

// RegisterChannel adds a channel to the manager.
func (m *Manager) RegisterChannel(name string, channel Channel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[name] = channel
}

// UnregisterChannel removes a channel from the manager.
func (m *Manager) UnregisterChannel(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.channels, name)
}

// WebhookRoute holds a path and handler pair for mounting on the main gateway mux.
type WebhookRoute struct {
	Path    string
	Handler http.Handler
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

// --- Run tracking for streaming/reaction event forwarding ---

// RegisterRun associates a run ID with a channel context so agent events
// (chunks, tool calls, completion) can be forwarded to the originating channel.
func (m *Manager) RegisterRun(runID, channelName, chatID, messageID string, metadata map[string]string, streaming, blockReply bool) {
	m.runs.Store(runID, &RunContext{
		ChannelName:       channelName,
		ChatID:            chatID,
		MessageID:         messageID,
		Metadata:          metadata,
		Streaming:         streaming,
		BlockReplyEnabled: blockReply,
	})
}

// UnregisterRun removes a run tracking entry.
func (m *Manager) UnregisterRun(runID string) {
	m.runs.Delete(runID)
}

// IsStreamingChannel checks if a named channel implements StreamingChannel
// AND has streaming currently enabled for the given chat type.
// isGroup: true for group chats, false for DMs.
func (m *Manager) IsStreamingChannel(channelName string, isGroup bool) bool {
	m.mu.RLock()
	ch, exists := m.channels[channelName]
	m.mu.RUnlock()
	if !exists {
		return false
	}
	sc, ok := ch.(StreamingChannel)
	if !ok {
		return false
	}
	return sc.StreamEnabled(isGroup)
}

// ResolveBlockReply checks per-channel override, falls back to gateway default.
// Returns true only if block.reply delivery should be enabled for this channel.
func (m *Manager) ResolveBlockReply(channelName string, globalDefault *bool) bool {
	m.mu.RLock()
	ch, exists := m.channels[channelName]
	m.mu.RUnlock()
	if exists {
		if bc, ok := ch.(BlockReplyChannel); ok {
			if v := bc.BlockReplyEnabled(); v != nil {
				return *v
			}
		}
	}
	return globalDefault != nil && *globalDefault
}

// HandleAgentEvent routes agent lifecycle events to streaming/reaction channels.
// Called from the bus event subscriber — must be non-blocking.
// eventType: "run.started", "chunk", "tool.call", "tool.result", "run.completed", "run.failed"
func (m *Manager) HandleAgentEvent(eventType, runID string, payload interface{}) {
	val, ok := m.runs.Load(runID)
	if !ok {
		return
	}
	rc := val.(*RunContext)

	m.mu.RLock()
	ch, exists := m.channels[rc.ChannelName]
	m.mu.RUnlock()
	if !exists {
		return
	}

	ctx := context.Background()

	// Forward to StreamingChannel
	if sc, ok := ch.(StreamingChannel); ok {
		switch eventType {
		case protocol.AgentEventRunStarted:
			if err := sc.OnStreamStart(ctx, rc.ChatID); err != nil {
				slog.Debug("stream start failed", "channel", rc.ChannelName, "error", err)
			}
		case protocol.AgentEventToolCall:
			// Agent is executing a tool — mark tool phase so the next chunk
			// (new LLM iteration) resets the stream buffer.
			// Also clear the current DraftStream so the next iteration starts
			// a fresh streaming message (matching TS onAssistantMessageStart pattern).
			rc.mu.Lock()
			rc.inToolPhase = true
			rc.mu.Unlock()
			if err := sc.OnStreamEnd(ctx, rc.ChatID, ""); err != nil {
				slog.Debug("stream tool-phase end failed", "channel", rc.ChannelName, "error", err)
			}
		case protocol.ChatEventChunk:
			// Accumulate chunk deltas into full text.
			// When entering a new LLM iteration (first chunk after tool.call),
			// reset the buffer so we don't concatenate text from previous iterations.
			content := extractPayloadString(payload, "content")
			if content != "" {
				rc.mu.Lock()
				if rc.inToolPhase {
					// New LLM iteration — reset buffer and start fresh stream
					rc.streamBuffer = ""
					rc.inToolPhase = false
					rc.mu.Unlock()
					// Create new DraftStream for this iteration
					if err := sc.OnStreamStart(ctx, rc.ChatID); err != nil {
						slog.Debug("stream restart failed", "channel", rc.ChannelName, "error", err)
					}
					rc.mu.Lock()
				}
				rc.streamBuffer += content
				fullText := rc.streamBuffer
				rc.mu.Unlock()
				if err := sc.OnChunkEvent(ctx, rc.ChatID, fullText); err != nil {
					slog.Debug("stream chunk failed", "channel", rc.ChannelName, "error", err)
				}
			}
		case protocol.AgentEventRunCompleted:
			rc.mu.Lock()
			finalText := rc.streamBuffer
			rc.mu.Unlock()
			if err := sc.OnStreamEnd(ctx, rc.ChatID, finalText); err != nil {
				slog.Debug("stream end failed", "channel", rc.ChannelName, "error", err)
			}
		case protocol.AgentEventRunFailed:
			// Clean up streaming state
			_ = sc.OnStreamEnd(ctx, rc.ChatID, "")
		}
	}

	// Handle block.reply: deliver intermediate assistant text to non-streaming channels.
	// Gated by BlockReplyEnabled (resolved from gateway + per-channel config at RegisterRun time).
	// Streaming channels already deliver via chunks, so skip to avoid double-delivery.
	if eventType == protocol.AgentEventBlockReply {
		if !rc.BlockReplyEnabled {
			return
		}
		content := extractPayloadString(payload, "content")
		if content == "" {
			return
		}
		rc.mu.Lock()
		streaming := rc.Streaming
		rc.mu.Unlock()

		if streaming {
			return // streaming already delivered via chunks
		}

		// Build outbound metadata: copy routing fields but strip reply_to_message_id
		// (block replies are standalone) and placeholder_key (reserve for final message).
		var outMeta map[string]string
		if rc.Metadata != nil {
			outMeta = make(map[string]string)
			for _, k := range []string{"message_thread_id", "local_key", "group_id"} {
				if v := rc.Metadata[k]; v != "" {
					outMeta[k] = v
				}
			}
			if len(outMeta) == 0 {
				outMeta = nil
			}
		}

		m.bus.PublishOutbound(bus.OutboundMessage{
			Channel:  rc.ChannelName,
			ChatID:   rc.ChatID,
			Content:  content,
			Metadata: outMeta,
		})
		return
	}

	// Handle LLM retry: update placeholder to notify user
	if eventType == protocol.AgentEventRunRetrying {
		attempt := extractPayloadString(payload, "attempt")
		maxAttempts := extractPayloadString(payload, "maxAttempts")
		retryMsg := fmt.Sprintf("Provider busy, retrying... (%s/%s)", attempt, maxAttempts)
		m.bus.PublishOutbound(bus.OutboundMessage{
			Channel: rc.ChannelName,
			ChatID:  rc.ChatID,
			Content: retryMsg,
			Metadata: map[string]string{
				"placeholder_update": "true",
			},
		})
	}

	// Forward to ReactionChannel
	if reactionCh, ok := ch.(ReactionChannel); ok {
		status := ""
		switch eventType {
		case protocol.AgentEventRunStarted:
			status = "thinking"
		case protocol.AgentEventToolCall:
			status = "tool"
		case protocol.AgentEventRunCompleted:
			status = "done"
		case protocol.AgentEventRunFailed:
			status = "error"
		}
		if status != "" {
			if err := reactionCh.OnReactionEvent(ctx, rc.ChatID, rc.MessageID, status); err != nil {
				slog.Debug("reaction event failed", "channel", rc.ChannelName, "status", status, "error", err)
			}
		}
	}

	// Clean up on terminal events
	if eventType == protocol.AgentEventRunCompleted || eventType == protocol.AgentEventRunFailed {
		m.runs.Delete(runID)
	}
}

// extractPayloadString extracts a string field from a payload (map[string]string or map[string]interface{}).
func extractPayloadString(payload interface{}, key string) string {
	switch p := payload.(type) {
	case map[string]string:
		return p[key]
	case map[string]interface{}:
		if v, ok := p[key].(string); ok {
			return v
		}
	}
	return ""
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
