package channels

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

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

	if eventType == protocol.AgentEventRunStarted {
		m.scheduleQuickAck(rc)
	}

	// Forward to StreamingChannel (only when streaming is enabled for this run).
	// Without this gate, channels that implement StreamingChannel but have streaming
	// disabled (e.g. group_stream=false) would create stream messages AND emit
	// block.reply outbound messages, causing duplicate delivery.
	if sc, ok := ch.(StreamingChannel); ok && rc.Streaming {
		switch eventType {
		case protocol.AgentEventRunStarted:
			firstStreamIsReasoning := rc.ReasoningDelivery.ShowInChannel && !rc.ReasoningDelivery.BubbleDelivery && sc.ReasoningStreamEnabled()
			stream, err := sc.CreateStream(ctx, rc.ChatID, firstStreamIsReasoning)
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
			content := extractPayloadString(payload, "content")
			if content != "" {
				if rc.ReasoningDelivery.BubbleDelivery {
					m.appendReasoningBubble(runID, rc, content)
					break
				}
				if !rc.ReasoningDelivery.ShowInChannel || !sc.ReasoningStreamEnabled() {
					break
				}
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
			if rc.ReasoningDelivery.BubbleDelivery {
				m.flushReasoningBubbles(runID)
			}
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

			toolName := extractPayloadString(payload, "name")
			if toolName != "" && ShouldDeliverGeneratedProgress(rc.ChatBehavior, rc.Streaming) {
				go m.sendIntermediateProgress(rc, toolName)
			}
		case protocol.ChatEventChunk:
			// Accumulate chunk deltas into full text.
			content := extractPayloadString(payload, "content")
			if content != "" {
				if rc.ReasoningDelivery.BubbleDelivery {
					m.flushReasoningBubbles(runID)
				}
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
						// Found think tags — commit to buffer and route or suppress reasoning
						// before any tagged content can leak into the answer lane.
						displayReasoningInStream := rc.ReasoningDelivery.ShowInChannel && !rc.ReasoningDelivery.BubbleDelivery && sc.ReasoningStreamEnabled()
						previousThinking := rc.thinkingBuffer
						rc.streamBuffer = candidate
						rc.thinkingBuffer = split.Thinking
						thinkText := rc.thinkingBuffer
						currentStream := rc.stream
						if split.Partial {
							// Still inside <think> — wait for the close tag before streaming
							// answer content. Native thinking uses hasThinking; tag parsing
							// keeps it false until close so later chunks continue parsing.
							rc.mu.Unlock()
							if rc.ReasoningDelivery.BubbleDelivery {
								if delta := reasoningDelta(previousThinking, thinkText); delta != "" {
									m.appendReasoningBubble(runID, rc, delta)
								}
							} else if displayReasoningInStream && currentStream != nil {
								currentStream.Update(ctx, formatReasoningPreview(thinkText))
							}
							break
						}
						// Tag closed — transition to answer, or strip reasoning entirely
						// when Show Reasoning is off.
						answerText := split.Answer
						rc.thinkingDone = true
						rc.hasThinking = displayReasoningInStream
						rc.streamBuffer = answerText
						reasoningStream := currentStream
						rc.mu.Unlock()

						if rc.ReasoningDelivery.BubbleDelivery {
							if delta := reasoningDelta(previousThinking, thinkText); delta != "" {
								m.appendReasoningBubble(runID, rc, delta)
							}
							m.flushReasoningBubbles(runID)
						}

						if !displayReasoningInStream {
							if reasoningStream != nil && answerText != "" {
								reasoningStream.Update(ctx, answerText)
							}
							break
						}

						// Stop reasoning stream after showing the final extracted thinking.
						if reasoningStream != nil {
							reasoningStream.Update(ctx, formatReasoningPreview(thinkText))
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
						if answerText != "" {
							rc.mu.Lock()
							currentStream = rc.stream
							rc.mu.Unlock()
							if currentStream != nil {
								currentStream.Update(ctx, answerText)
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

	if eventType == protocol.AgentEventToolCall {
		toolName := extractPayloadString(payload, "name")
		if toolName != "" && ShouldDeliverGeneratedProgress(rc.ChatBehavior, rc.Streaming) {
			go m.sendIntermediateProgress(rc, toolName)
		}
	}

	if !rc.Streaming && rc.ReasoningDelivery.BubbleDelivery {
		switch eventType {
		case protocol.ChatEventThinking:
			content := extractPayloadString(payload, "content")
			if content != "" {
				m.appendReasoningBubble(runID, rc, content)
			}
			return
		case protocol.ChatEventChunk:
			content := extractPayloadString(payload, "content")
			if content != "" {
				m.appendReasoningBubbleFromThinkTagChunk(runID, rc, content)
			}
			m.flushReasoningBubbles(runID)
			return
		case protocol.AgentEventToolCall, protocol.AgentEventRunCompleted, protocol.AgentEventRunFailed, protocol.AgentEventRunCancelled:
			m.flushReasoningBubbles(runID)
		}
	}

	// Handle block.reply: deliver intermediate assistant text to non-streaming channels.
	// Gated by explicit block_reply or generated-progress chat behavior.
	// Streaming channels already deliver via chunks, so skip to avoid double-delivery.
	if eventType == protocol.AgentEventBlockReply {
		content := extractPayloadString(payload, "content")
		if content == "" {
			return
		}
		source := extractPayloadString(payload, "source")
		rc.mu.Lock()
		streaming := rc.Streaming
		rc.blockReplySeen++
		isInitialBlockReply := rc.blockReplySeen == 1
		blockReplyEnabled := rc.BlockReplyEnabled
		chatBehavior := rc.ChatBehavior
		rc.mu.Unlock()

		if streaming {
			return // streaming already delivered via chunks
		}
		if !blockReplyEnabled {
			return
		}
		isToolAnnouncement := source == protocol.BlockReplySourceToolAnnouncement
		if isInitialBlockReply && !isToolAnnouncement && blockReplyEnabled && ShouldSuppressInitialBlockReply(chatBehavior, streaming) {
			return
		}

		m.cancelQuickAck(rc)
		rc.mu.Lock()
		rc.blockReplySent = true
		rc.interimDelivered++
		rc.lastInterimReply = content
		rc.mu.Unlock()

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
		m.cancelQuickAck(rc)
		rc.mu.Lock()
		stopReasoningBubbleTimerLocked(rc)
		rc.mu.Unlock()
	}
}

func (m *Manager) scheduleQuickAck(rc *RunContext) {
	if !ShouldSendQuickAck(rc.ChatBehavior, rc.Streaming) {
		return
	}
	delay := time.Duration(rc.ChatBehavior.QuickAck.MinDelayMs) * time.Millisecond
	if delay <= 0 {
		m.sendQuickAck(rc)
		return
	}
	rc.mu.Lock()
	if rc.ackTimer == nil && !rc.ackSent && !rc.blockReplySent {
		rc.ackTimer = time.AfterFunc(delay, func() {
			m.sendQuickAck(rc)
		})
	}
	rc.mu.Unlock()
}

func (m *Manager) cancelQuickAck(rc *RunContext) {
	rc.mu.Lock()
	rc.ackCancelled = true
	if rc.ackTimer != nil {
		rc.ackTimer.Stop()
		rc.ackTimer = nil
	}
	rc.mu.Unlock()
}

func (m *Manager) sendQuickAck(rc *RunContext) {
	rc.mu.Lock()
	if rc.ackCancelled || rc.ackSent || rc.blockReplySent || !ShouldSendQuickAck(rc.ChatBehavior, rc.Streaming) {
		rc.mu.Unlock()
		return
	}
	mode := effectiveQuickAckMode(rc.ChatBehavior.QuickAck.Mode)
	generator := rc.Delivery.QuickAckGenerator
	request := rc.deliveryRequestLocked(DeliveryPurposeQuickAck, "")
	templates := append([]string(nil), rc.ChatBehavior.QuickAck.Templates...)
	rc.ackSent = true
	rc.ackTimer = nil
	rc.mu.Unlock()

	content := ""
	if mode == QuickAckModeLLMGenerated || mode == QuickAckModeSidecar {
		if generator == nil {
			slog.Warn("channel delivery quick ack generator unavailable",
				"channel", rc.ChannelName, "purpose", request.Purpose)
			return
		}
		generated, err := generator.GenerateDeliveryMessage(context.Background(), request)
		if err != nil {
			slog.Warn("channel delivery quick ack generation failed",
				"channel", rc.ChannelName, "purpose", request.Purpose, "error", err)
			return
		}
		if generated == "" {
			slog.Warn("channel delivery quick ack generation empty",
				"channel", rc.ChannelName, "purpose", request.Purpose)
			return
		}
		content = generated
	} else {
		if len(templates) == 0 {
			return
		}
		content = sanitizeDeliveryMessage(templates[0], request.MaxChars)
		if content == "" {
			return
		}
	}

	rc.mu.Lock()
	if rc.ackCancelled || rc.blockReplySent {
		rc.mu.Unlock()
		return
	}
	rc.mu.Unlock()

	m.bus.PublishOutbound(bus.OutboundMessage{
		Channel:  rc.ChannelName,
		ChatID:   rc.ChatID,
		Content:  content,
		Metadata: copyRoutingMeta(rc.Metadata),
		TenantID: rc.TenantID,
	})
}

func (m *Manager) sendIntermediateProgress(rc *RunContext, toolName string) {
	rc.mu.Lock()
	if rc.ackCancelled || rc.blockReplySent || !ShouldDeliverGeneratedProgress(rc.ChatBehavior, rc.Streaming) {
		rc.mu.Unlock()
		return
	}
	generator := rc.Delivery.ProgressGenerator
	request := rc.deliveryRequestLocked(DeliveryPurposeProgress, toolName)
	rc.mu.Unlock()

	content := ""
	if generator == nil {
		slog.Warn("channel delivery progress generator unavailable",
			"channel", rc.ChannelName, "purpose", request.Purpose)
		return
	} else if generated, err := generator.GenerateDeliveryMessage(context.Background(), request); err != nil {
		slog.Warn("channel delivery progress generation failed",
			"channel", rc.ChannelName, "purpose", request.Purpose, "error", err)
		return
	} else if generated == "" {
		slog.Warn("channel delivery progress generation empty",
			"channel", rc.ChannelName, "purpose", request.Purpose)
		return
	} else {
		content = generated
	}

	rc.mu.Lock()
	if rc.ackCancelled || rc.blockReplySent {
		rc.mu.Unlock()
		return
	}
	rc.ackCancelled = true
	if rc.ackTimer != nil {
		rc.ackTimer.Stop()
		rc.ackTimer = nil
	}
	rc.blockReplySent = true
	rc.interimDelivered++
	rc.lastInterimReply = content
	rc.mu.Unlock()

	m.bus.PublishOutbound(bus.OutboundMessage{
		Channel:  rc.ChannelName,
		ChatID:   rc.ChatID,
		Content:  content,
		Metadata: copyRoutingMeta(rc.Metadata),
		TenantID: rc.TenantID,
	})
}

func (rc *RunContext) deliveryRequestLocked(purpose, toolName string) DeliveryMessageRequest {
	maxTokens := rc.ChatBehavior.QuickAck.MaxTokens
	maxChars := rc.ChatBehavior.QuickAck.MaxChars
	timeout := rc.ChatBehavior.QuickAck.Timeout
	if purpose == DeliveryPurposeProgress {
		maxTokens = rc.ChatBehavior.IntermediateReplies.MaxTokens
		maxChars = rc.ChatBehavior.IntermediateReplies.MaxChars
		timeout = rc.ChatBehavior.IntermediateReplies.Timeout
	}
	return DeliveryMessageRequest{
		Purpose:      purpose,
		UserMessage:  rc.Delivery.Inbound,
		Locale:       rc.Delivery.Locale,
		PeerKind:     rc.Delivery.PeerKind,
		ChannelType:  rc.Delivery.Channel,
		AgentName:    rc.Delivery.AgentName,
		ToolName:     toolName,
		PersonaBrief: rc.Delivery.PersonaBrief,
		MaxTokens:    maxTokens,
		MaxChars:     maxChars,
		Timeout:      timeout,
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
