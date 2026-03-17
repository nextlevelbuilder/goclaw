package channels

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// HandleAgentEvent routes agent lifecycle events to streaming/reaction channels.
// Called from the bus event subscriber — must be non-blocking.
// eventType: "run.started", "chunk", "tool.call", "tool.result", "run.completed", "run.failed"
func (m *Manager) HandleAgentEvent(eventType, runID string, payload any) {
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
			stream, err := sc.CreateStream(ctx, rc.ChatID, true)
			if err != nil {
				slog.Debug("stream start failed", "channel", rc.ChannelName, "error", err)
			} else {
				rc.mu.Lock()
				rc.stream = stream
				rc.mu.Unlock()
			}
		case protocol.ChatEventThinking:
			// Accumulate thinking/reasoning content and route to the current stream.
			// The stream created on run.started becomes the "reasoning lane":
			//  - DMs: edits the "Thinking..." placeholder with reasoning text
			//  - Groups: edits a fresh message with reasoning text
			// When the first chunk arrives, this stream is stopped (reasoning message stays
			// visible) and a new stream is created for the answer lane.
			// Gated by ReasoningStreamEnabled() — channels can opt out (e.g. Slack).
			if !sc.ReasoningStreamEnabled() {
				break
			}
			content := extractPayloadString(payload, "content")
			if content != "" {
				rc.mu.Lock()
				rc.thinkingBuffer += content
				rc.hasThinking = true
				thinkText := rc.thinkingBuffer
				currentStream := rc.stream
				rc.mu.Unlock()
				if currentStream != nil {
					currentStream.Update(ctx, formatReasoningPreview(thinkText))
				}
			}
		case protocol.AgentEventToolCall:
			// Agent is executing a tool — mark tool phase so the next chunk
			// (new LLM iteration) resets the stream buffer.
			// Stop the current stream (reasoning or answer) and finalize only
			// the answer stream (reasoning messages stay visible).
			rc.mu.Lock()
			// Capture current state before resetting for next iteration.
			wasReasoningStream := rc.hasThinking && !rc.thinkingDone
			currentStream := rc.stream
			rc.stream = nil
			rc.inToolPhase = true
			rc.thinkingDone = false    // allow new thinking in next iteration
			rc.thinkingBuffer = ""     // reset thinking buffer for new iteration
			rc.hasThinking = false     // new iteration starts fresh
			rc.tagParseSkipped = false // re-enable tag parsing for next iteration
			rc.mu.Unlock()
			if currentStream != nil {
				if err := currentStream.Stop(ctx); err != nil {
					slog.Debug("stream tool-phase stop failed", "channel", rc.ChannelName, "error", err)
				}
				// Only finalize answer streams (hand off messageID to Send()).
				// Reasoning streams stay as visible messages — don't put their
				// messageID into placeholders or it would confuse Send().
				if !wasReasoningStream {
					sc.FinalizeStream(ctx, rc.ChatID, currentStream)
				}
			}

			// Show tool status in streaming preview (edit placeholder with tool name).
			toolName := extractPayloadString(payload, "name")
			if toolName != "" && rc.ToolStatusEnabled {
				statusText := formatToolStatus(toolName)
				outMeta := copyRoutingMeta(rc.Metadata)
				outMeta["placeholder_update"] = "true"
				m.bus.PublishOutbound(bus.OutboundMessage{
					Channel:  rc.ChannelName,
					ChatID:   rc.ChatID,
					Content:  statusText,
					Metadata: outMeta,
				})
			}
		case protocol.ChatEventChunk:
			// Accumulate chunk deltas into full text.
			content := extractPayloadString(payload, "content")
			if content != "" {
				rc.mu.Lock()
				needNewStream := rc.inToolPhase
				if needNewStream {
					rc.streamBuffer = ""
					rc.inToolPhase = false
				}

				// Fallback <think> tag parsing: for providers that embed thinking
				// in the content stream (DeepSeek-via-OpenRouter, Qwen, some Ollama models).
				// Only activates when no native ChatEventThinking was received.
				if !rc.hasThinking && !rc.thinkingDone && !rc.tagParseSkipped {
					candidate := rc.streamBuffer + content
					split := SplitThinkTags(candidate)
					if split.Thinking != "" {
						// Found think tags — commit to buffer and route to reasoning lane
						rc.streamBuffer = candidate
						rc.hasThinking = true
						rc.thinkingBuffer = split.Thinking
						thinkText := rc.thinkingBuffer
						currentStream := rc.stream
						if split.Partial {
							// Still inside <think> — update reasoning stream, wait for close
							rc.mu.Unlock()
							if currentStream != nil {
								currentStream.Update(ctx, formatReasoningPreview(thinkText))
							}
							break
						}
						// Tag closed — transition to answer
						rc.thinkingDone = true
						rc.streamBuffer = split.Answer
						reasoningStream := currentStream
						rc.mu.Unlock()

						// Stop reasoning stream
						if reasoningStream != nil {
							_ = reasoningStream.Stop(ctx)
						}
						// Create answer stream
						stream, err := sc.CreateStream(ctx, rc.ChatID, false)
						if err != nil {
							slog.Debug("stream restart after think-tag failed", "channel", rc.ChannelName, "error", err)
						} else {
							rc.mu.Lock()
							rc.stream = stream
							rc.mu.Unlock()
						}
						// Update answer stream with extracted answer content
						if split.Answer != "" {
							rc.mu.Lock()
							currentStream = rc.stream
							rc.mu.Unlock()
							if currentStream != nil {
								currentStream.Update(ctx, split.Answer)
							}
						}
						break
					}
					// No think tags found — mark as skipped so we don't re-parse.
					// Don't commit to streamBuffer here — the normal flow below appends content.
					rc.tagParseSkipped = true
				}

				// Reasoning→answer transition: first chunk after native thinking events.
				// Stop the reasoning stream (keep message visible) and create a
				// new stream for the answer lane.
				needTransition := rc.hasThinking && !rc.thinkingDone
				if needTransition {
					rc.thinkingDone = true
					rc.streamBuffer = "" // fresh answer buffer
				}
				reasoningStream := rc.stream
				rc.mu.Unlock()

				// Finalize reasoning stream (stop editing, keep message)
				if needTransition && reasoningStream != nil {
					_ = reasoningStream.Stop(ctx)
					// Don't call FinalizeStream — reasoning messageID should NOT
					// go into placeholders. Send() must edit the answer message.
				}

				// Create fresh stream for answer (or new tool iteration)
				if needNewStream || needTransition {
					stream, err := sc.CreateStream(ctx, rc.ChatID, false)
					if err != nil {
						slog.Debug("stream restart failed", "channel", rc.ChannelName, "error", err)
					} else {
						rc.mu.Lock()
						rc.stream = stream
						rc.mu.Unlock()
					}
				}

				rc.mu.Lock()
				rc.streamBuffer += content
				fullText := rc.streamBuffer
				currentStream := rc.stream
				rc.mu.Unlock()
				if currentStream != nil {
					currentStream.Update(ctx, fullText)
				}
			}
		case protocol.AgentEventRunCompleted:
			rc.mu.Lock()
			currentStream := rc.stream
			rc.stream = nil
			rc.mu.Unlock()
			if currentStream != nil {
				if err := currentStream.Stop(ctx); err != nil {
					slog.Debug("stream end failed", "channel", rc.ChannelName, "error", err)
				}
				sc.FinalizeStream(ctx, rc.ChatID, currentStream)
			}
		case protocol.AgentEventRunFailed:
			// Clean up streaming state on failure
			rc.mu.Lock()
			currentStream := rc.stream
			rc.stream = nil
			rc.mu.Unlock()
			if currentStream != nil {
				_ = currentStream.Stop(ctx)
			}
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
			// Use tool-specific reaction statuses to activate existing variants
			// (web → ⚡, coding → 👨‍💻) that are already defined in channel reaction maps.
			toolName := extractPayloadString(payload, "name")
			status = resolveToolReactionStatus(toolName)
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
func extractPayloadString(payload any, key string) string {
	switch p := payload.(type) {
	case map[string]string:
		return p[key]
	case map[string]any:
		if v, ok := p[key].(string); ok {
			return v
		}
	}
	return ""
}

// copyRoutingMeta copies channel routing metadata (thread_id, local_key, group_id)
// from RunContext.Metadata into a new map suitable for outbound messages.
func copyRoutingMeta(src map[string]string) map[string]string {
	out := make(map[string]string)
	for _, k := range []string{"message_thread_id", "local_key", "group_id"} {
		if v := src[k]; v != "" {
			out[k] = v
		}
	}
	return out
}

// toolStatusMap maps builtin tool names to user-friendly status messages.
var toolStatusMap = map[string]string{
	// Filesystem
	"read_file":  "📝 Reading file...",
	"write_file": "📝 Writing file...",
	"list_files": "📝 Listing files...",
	"edit":       "📝 Editing file...",
	// Runtime
	"exec": "⚡ Running code...",
	// Web
	"web_search": "🔍 Searching the web...",
	"web_fetch":  "🔍 Fetching web content...",
	// Memory
	"memory_search":          "🧠 Searching memory...",
	"memory_get":             "🧠 Retrieving memory...",
	"knowledge_graph_search": "🧠 Querying knowledge graph...",
	// Media
	"read_image":    "👁 Analyzing image...",
	"read_document": "📄 Reading document...",
	"read_audio":    "🎧 Processing audio...",
	"read_video":    "🎬 Processing video...",
	"create_image":  "🎨 Creating image...",
	"create_video":  "🎬 Creating video...",
	"create_audio":  "🎵 Creating audio...",
	"tts":           "🔊 Generating speech...",
	// Browser
	"browser": "🌐 Browsing...",
	// Delegation & teams
	"spawn":        "👥 Delegating task...",
	"team_tasks":   "📋 Managing team tasks...",
	"team_message": "💬 Sending team message...",
	// Sessions
	"sessions_list":    "📋 Listing sessions...",
	"session_status":   "📋 Checking session...",
	"sessions_history": "📋 Reading history...",
	"sessions_send":    "📤 Sending message...",
	// Other
	"message":         "📤 Sending message...",
	"cron":            "⏰ Managing schedule...",
	"skill_search":    "🔍 Searching skills...",
	"use_skill":       "🧩 Using skill...",
	"mcp_tool_search": "🔌 Searching MCP tools...",
}

// toolPrefixStatus maps tool name prefixes to status messages (fallback for dynamic tools).
var toolPrefixStatus = []struct {
	prefix string
	status string
}{
	{"mcp_", "🔌 Using external tool..."},
}

// formatToolStatus returns a user-friendly status message for a tool name.
func formatToolStatus(toolName string) string {
	if s, ok := toolStatusMap[toolName]; ok {
		return s
	}
	for _, p := range toolPrefixStatus {
		if strings.HasPrefix(toolName, p.prefix) {
			return p.status
		}
	}
	return "🔧 Running " + toolName + "..."
}

// formatReasoningPreview formats accumulated thinking text for display as a
// streaming reasoning message. Uses markdown italic prefix so channels that
// convert markdown (Telegram, Slack) show "Reasoning:" in italics.
// Truncated to 4096 runes (Telegram limit, rune-safe for CJK/emoji).
func formatReasoningPreview(thinking string) string {
	if thinking == "" {
		return ""
	}
	const maxRunes = 4096
	text := "_Reasoning:_\n" + thinking
	runes := []rune(text)
	if len(runes) > maxRunes {
		text = string(runes[:maxRunes-3]) + "..."
	}
	return text
}

// resolveToolReactionStatus maps a tool name to a reaction status string.
// Returns tool-specific statuses ("web", "coding") that activate existing
// but previously unused reaction variants in channel implementations.
func resolveToolReactionStatus(toolName string) string {
	switch {
	case strings.HasPrefix(toolName, "web") || toolName == "browser":
		return "web"
	case toolName == "exec":
		return "coding"
	default:
		return "tool"
	}
}
