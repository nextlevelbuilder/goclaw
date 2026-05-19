package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// HandleAgentEvent routes agent lifecycle events to streaming/reaction channels.
// Called from the bus event subscriber — must be non-blocking.
// eventType: "run.started", "chunk", "tool.call", "tool.result", "run.completed", "run.failed", "run.cancelled"
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
	// Use RunContext's TenantID directly (set at RegisterRun time from channel instance)
	// rather than querying the channel interface - more direct and future-proof for
	// channels that might serve multiple tenants.
	if rc.TenantID != uuid.Nil {
		ctx = store.WithTenantID(ctx, rc.TenantID)
	}

	if eventType == protocol.AgentEventActivity {
		m.forwardGatewayProgressEvent(ctx, rc, ch, runID, payload)
	}

	// Forward to StreamingChannel (only when streaming is enabled for this run).
	// Without this gate, channels that implement StreamingChannel but have streaming
	// disabled (e.g. group_stream=false) would create stream messages AND emit
	// block.reply outbound messages, causing duplicate delivery.
	if sc, ok := ch.(StreamingChannel); ok && rc.Streaming {
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
				// Don't finalize mid-run streams — their messageID must NOT go
				// into placeholders. Otherwise tool_status placeholder_update
				// overwrites streamed content, and subsequent FinalizeStream
				// calls overwrite the placeholder key, leaving earlier messages
				// stuck at tool status text. Only run.completed finalizes.
			}

			// Show tool status by editing placeholder message (non-streaming only).
			// Streaming channels show tool status via reaction emoji instead —
			// editing the placeholder would overwrite streamed content.
			toolName := extractPayloadString(payload, "name")
			if toolName != "" && rc.ToolStatusEnabled && !rc.Streaming {
				statusText := formatToolStatus(toolName)
				outMeta := copyRoutingMeta(rc.Metadata)
				outMeta["placeholder_update"] = "true"
				m.bus.PublishOutbound(bus.OutboundMessage{
					Channel:  rc.ChannelName,
					ChatID:   rc.ChatID,
					Content:  statusText,
					Metadata: outMeta,
					TenantID: rc.TenantID,
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
			// Issue 958: Send user-friendly error message instead of silent "..."
			errStr := extractPayloadString(payload, "error")
			if friendlyMsg := FormatAgentError(errStr); friendlyMsg != "" {
				m.bus.PublishOutbound(bus.OutboundMessage{
					Channel:  rc.ChannelName,
					ChatID:   rc.ChatID,
					Content:  friendlyMsg,
					TenantID: rc.TenantID,
				})
			}
		case protocol.AgentEventRunCancelled:
			// Clean up streaming state on cancellation
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
		// feishu_reply_target_id MUST be preserved so intermediate block replies for
		// threaded Lark messages also land inside the same thread.
		var outMeta map[string]string
		if rc.Metadata != nil {
			outMeta = make(map[string]string)
			for _, k := range routingMetaKeys {
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
			TenantID: rc.TenantID,
		})
		return
	}

	// Handle LLM retry: update placeholder to notify user
	if eventType == protocol.AgentEventRunRetrying {
		attempt := extractPayloadString(payload, "attempt")
		maxAttempts := extractPayloadString(payload, "maxAttempts")
		retryMsg := fmt.Sprintf("Provider busy, retrying... (%s/%s)", attempt, maxAttempts)
		m.bus.PublishOutbound(bus.OutboundMessage{
			Channel:  rc.ChannelName,
			ChatID:   rc.ChatID,
			Content:  retryMsg,
			TenantID: rc.TenantID,
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
		case protocol.AgentEventRunCancelled:
			status = "done"
		}
		if status != "" {
			if err := reactionCh.OnReactionEvent(ctx, rc.ChatID, rc.MessageID, status); err != nil {
				slog.Debug("reaction event failed", "channel", rc.ChannelName, "status", status, "error", err)
			}
		}
	}

	// Clean up on terminal events
	if eventType == protocol.AgentEventRunCompleted || eventType == protocol.AgentEventRunFailed || eventType == protocol.AgentEventRunCancelled {
		m.runs.Delete(runID)
	}
}

const (
	gatewayReplyVersion           = "goclaw.gateway.reply.v1"
	gatewayProgressEventType      = "goclaw.gateway.progress"
	gatewayProgressMetaKey        = "goclaw_gateway_event"
	gatewayProgressModeMetaKey    = "gateway_progress"
	gatewayProgressModeJSON       = "json_outbound"
	gatewayProgressModeJSONAlias  = "outbound"
	gatewayProgressModeTrue       = "true"
	gatewayProgressModeEnabled    = "enabled"
	gatewayProgressPayloadMetaKey = "goclaw_gateway_payload"
)

func (m *Manager) forwardGatewayProgressEvent(ctx context.Context, rc *RunContext, ch Channel, runID string, payload any) {
	event, ok := BuildGatewayProgressEvent(runID, payload, GatewayProgressRoute{
		Channel:   rc.ChannelName,
		ChatID:    rc.ChatID,
		MessageID: rc.MessageID,
		TenantID:  rc.TenantID.String(),
		Metadata:  rc.Metadata,
	})
	if !ok {
		return
	}

	if gatewayCh, ok := ch.(GatewayProgressChannel); ok {
		if err := gatewayCh.OnGatewayProgress(ctx, event); err != nil {
			slog.Debug("gateway progress event failed",
				"channel", rc.ChannelName,
				"run_id", runID,
				"event", event.Event,
				"kind", event.Kind,
				"error", err,
			)
		}
		return
	}

	// Temporary compatibility path for external gateway adapters that only
	// receive OutboundMessage. This is opt-in so normal chat channels do not
	// receive raw JSON progress messages.
	if !gatewayProgressJSONOutboundEnabled(rc.Metadata) {
		return
	}

	raw, err := json.Marshal(event)
	if err != nil {
		slog.Debug("gateway progress marshal failed", "channel", rc.ChannelName, "run_id", runID, "error", err)
		return
	}

	outMeta := copyRoutingMeta(rc.Metadata)
	if outMeta == nil {
		outMeta = map[string]string{}
	}
	outMeta[gatewayProgressMetaKey] = gatewayProgressEventType
	outMeta["goclaw_gateway_version"] = event.Version
	outMeta["goclaw_gateway_kind"] = event.Kind
	outMeta["goclaw_gateway_run_id"] = event.RunID
	outMeta["goclaw_gateway_child_run_id"] = event.ChildRun
	outMeta["goclaw_gateway_progress_event"] = event.Event
	outMeta[gatewayProgressPayloadMetaKey] = string(raw)

	m.bus.PublishOutbound(bus.OutboundMessage{
		Channel:  rc.ChannelName,
		ChatID:   rc.ChatID,
		Content:  string(raw),
		Metadata: outMeta,
		TenantID: rc.TenantID,
	})
}

func BuildGatewayProgressEvent(runID string, payload any, route GatewayProgressRoute) (GatewayProgressEvent, bool) {
	payloadMap, ok := payload.(map[string]any)
	if !ok {
		return GatewayProgressEvent{}, false
	}
	phase, _ := payloadMap["phase"].(string)
	if phase != "mcp_progress" {
		return GatewayProgressEvent{}, false
	}

	eventData, ok := asStringAnyMap(payloadMap["event_data"])
	if !ok {
		return GatewayProgressEvent{}, false
	}
	replyPayload, ok := extractGatewayReplyPayload(eventData)
	if !ok {
		return GatewayProgressEvent{}, false
	}

	kind, _ := replyPayload["kind"].(string)
	if !IsGatewayProgressKindForwardable(kind) {
		return GatewayProgressEvent{}, false
	}
	tool, _ := payloadMap["tool"].(string)
	mcpTool, _ := payloadMap["mcp_tool"].(string)
	message, _ := payloadMap["message"].(string)
	eventName, _ := payloadMap["event"].(string)
	childRunID, _ := payloadMap["run_id"].(string)
	timestamp, _ := payloadMap["timestamp"].(string)

	gatewayContext := extractGatewayContext(route.Metadata)
	channel := route.Channel
	messageID := route.MessageID
	if gatewayContext != nil {
		if v := gatewayContext["channel"]; v != "" {
			channel = v
		}
		if v := gatewayContext["message_id"]; v != "" {
			messageID = v
		}
	}
	return GatewayProgressEvent{
		Kind:              kind,
		GatewayContextID:  gatewayContext["gateway_context_id"],
		Channel:           channel,
		ConversationID:    gatewayContext["conversation_id"],
		MessageID:         messageID,
		InternalSessionID: gatewayContext["internal_session_id"],
		OutTrackID:        gatewayContext["out_track_id"],
		ReplyMode:         gatewayContext["reply_mode"],
		Payload:           replyPayload,

		EventType:      gatewayProgressEventType,
		Version:        gatewayReplyVersion,
		RunID:          runID,
		AgentID:        route.AgentID,
		SessionKey:     route.SessionKey,
		ChatID:         route.ChatID,
		UserID:         route.UserID,
		SenderID:       route.SenderID,
		TenantID:       route.TenantID,
		GatewayContext: gatewayContext,
		Metadata:       gatewayContext,
		Tool:           tool,
		MCPTool:        mcpTool,
		Progress:       payloadMap["progress"],
		Total:          payloadMap["total"],
		Message:        message,
		Event:          eventName,
		ChildRun:       childRunID,
		Timestamp:      timestamp,
		EventData:      eventData,
	}, true
}

func extractGatewayReplyPayload(eventData map[string]any) (map[string]any, bool) {
	candidates := []any{
		eventData["payload"],
		eventData["data"],
	}
	if data, ok := asStringAnyMap(eventData["data"]); ok {
		candidates = append(candidates, data["payload"])
	}

	for _, candidate := range candidates {
		m, ok := asStringAnyMap(candidate)
		if !ok {
			continue
		}
		if version, _ := m["version"].(string); version == gatewayReplyVersion {
			return m, true
		}
	}
	return nil, false
}

func IsGatewayProgressKindForwardable(kind string) bool {
	switch kind {
	case "progress", "text", "ask_user", "result", "error":
		return true
	default:
		return false
	}
}

func IsGatewayProgressKindCritical(kind string) bool {
	switch kind {
	case "ask_user", "result", "error":
		return true
	default:
		return false
	}
}

func extractGatewayContext(metadata map[string]string) map[string]string {
	keys := []string{
		"gateway_context_id",
		"channel",
		"internal_session_id",
		"conversation_id",
		"message_id",
		"out_track_id",
		"reply_mode",
	}
	out := make(map[string]string)
	for _, key := range keys {
		if v := metadata[key]; v != "" {
			out[key] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func gatewayProgressJSONOutboundEnabled(metadata map[string]string) bool {
	mode := strings.ToLower(strings.TrimSpace(metadata[gatewayProgressModeMetaKey]))
	switch mode {
	case gatewayProgressModeJSON, gatewayProgressModeJSONAlias, gatewayProgressModeTrue, gatewayProgressModeEnabled:
		return true
	default:
		return false
	}
}

func asStringAnyMap(v any) (map[string]any, bool) {
	if m, ok := v.(map[string]any); ok {
		return m, true
	}
	if m, ok := v.(map[string]interface{}); ok {
		return map[string]any(m), true
	}
	return nil, false
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
	"spawn":      "👥 Delegating task...",
	"team_tasks": "📋 Managing team tasks...",
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
