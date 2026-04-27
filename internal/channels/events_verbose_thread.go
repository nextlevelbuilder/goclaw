package channels

import (
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// handleVerboseThreadEvent publishes discrete OutboundMessages in response
// to per-iteration LLM events when the run targets a "verbose" chat
// (today: Discord threads). The parent channel of the conversation
// still gets the terse final answer via the normal block-reply /
// run-completed path; this surfaces the agent's reasoning trace
// alongside it.
//
// Lifecycle of a single agent turn looks like:
//
//	run.started
//	  thinking (delta)  thinking (delta)  thinking (delta)
//	  tool.call(name=read_file, args=…)
//	  tool.result(...)
//	  thinking (delta)  thinking (delta)
//	  tool.call(name=exec, args=…)
//	  tool.result(...)
//	  chunk(...) chunk(...)             (final assistant text)
//	run.completed
//
// We accumulate thinking deltas into rc.thinkingBuffer and flush at every
// iteration boundary (tool.call) and at run completion. Each flush + each
// tool.call becomes one Discord message in the thread.
func (m *Manager) handleVerboseThreadEvent(rc *RunContext, eventType string, payload any) {
	switch eventType {
	case protocol.ChatEventThinking:
		// Accumulate. We don't flush per-delta — that would be 50+
		// Discord messages per turn and definitely trip rate limits
		// on the sending side. Flush is on iteration boundaries.
		content := extractPayloadString(payload, "content")
		if content == "" {
			return
		}
		rc.mu.Lock()
		rc.thinkingBuffer += content
		rc.hasThinking = true
		rc.mu.Unlock()

	case protocol.AgentEventToolCall:
		// Iteration boundary: flush whatever reasoning came before
		// this tool call, then announce the tool itself.
		m.flushVerboseThinking(rc)

		toolName := extractPayloadString(payload, "name")
		if toolName == "" {
			return
		}
		// Tool args are not surfaced in the message — they can contain
		// secrets, file paths the user doesn't want public, large
		// blobs. Just the name keeps the trace readable and safe by
		// default. If we want richer detail later, do it via a tool-
		// specific summarizer (e.g. exec → first 80 chars of cmd).
		m.bus.PublishOutbound(bus.OutboundMessage{
			Channel:  rc.ChannelName,
			ChatID:   rc.ChatID,
			Content:  "🔧 " + toolName,
			TenantID: rc.TenantID,
		})

	case protocol.AgentEventRunCompleted,
		protocol.AgentEventRunFailed,
		protocol.AgentEventRunCancelled:
		// Final flush: if the model produced reasoning AFTER the last
		// tool call (no further iteration), surface it. The final
		// assistant text itself rides on the normal completion path,
		// not this one.
		m.flushVerboseThinking(rc)
	}
}

// flushVerboseThinking publishes the accumulated thinking buffer (if any)
// as a discrete reasoning message and resets the buffer. Mirrors the
// reset that the streaming path does on tool-call boundaries.
func (m *Manager) flushVerboseThinking(rc *RunContext) {
	rc.mu.Lock()
	text := rc.thinkingBuffer
	rc.thinkingBuffer = ""
	rc.hasThinking = false
	rc.mu.Unlock()

	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	// Cap the message length. Discord rejects single messages over
	// 2000 chars; even a long chain-of-thought paragraph can blow
	// past that. Truncate with a clear marker so the user can ask
	// for more if they care.
	const maxLen = 1900
	if len(text) > maxLen {
		text = text[:maxLen] + "\n…[truncated]"
	}

	m.bus.PublishOutbound(bus.OutboundMessage{
		Channel:  rc.ChannelName,
		ChatID:   rc.ChatID,
		Content:  "💭 " + text,
		TenantID: rc.TenantID,
	})
}
