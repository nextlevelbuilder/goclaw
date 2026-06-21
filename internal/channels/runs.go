package channels

import (
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// --- Run tracking for streaming/reaction event forwarding ---

// RegisterRun associates a run ID with a channel context so agent events
// (chunks, tool calls, completion) can be forwarded to the originating channel.
func (m *Manager) RegisterRun(runID, channelName, chatID, messageID string, metadata map[string]string, tenantID uuid.UUID, streaming, blockReply, toolStatus bool) {
	m.RegisterRunWithBehavior(runID, channelName, chatID, messageID, metadata, tenantID, streaming, blockReply, toolStatus, ResolvedChatBehavior{})
}

// RegisterRunWithBehavior associates a run ID with channel context and
// resolved delivery behavior so event handlers do not read mutable config mid-run.
func (m *Manager) RegisterRunWithBehavior(runID, channelName, chatID, messageID string, metadata map[string]string, tenantID uuid.UUID, streaming, blockReply, toolStatus bool, chatBehavior ResolvedChatBehavior, reasoningDelivery ...ResolvedReasoningDelivery) {
	m.RegisterRunWithDelivery(runID, channelName, chatID, messageID, metadata, tenantID, streaming, blockReply, toolStatus, chatBehavior, DeliveryRuntime{}, reasoningDelivery...)
}

func (m *Manager) RegisterRunWithDelivery(runID, channelName, chatID, messageID string, metadata map[string]string, tenantID uuid.UUID, streaming, blockReply, toolStatus bool, chatBehavior ResolvedChatBehavior, deliveryRuntime DeliveryRuntime, reasoningDelivery ...ResolvedReasoningDelivery) {
	delivery := ResolveReasoningDelivery("", nil)
	if len(reasoningDelivery) > 0 {
		delivery = reasoningDelivery[0]
	}
	m.runs.Store(runID, &RunContext{
		ChannelName:       channelName,
		ChatID:            chatID,
		MessageID:         messageID,
		Metadata:          metadata,
		TenantID:          tenantID,
		Streaming:         streaming,
		BlockReplyEnabled: blockReply,
		ToolStatusEnabled: toolStatus,
		ChatBehavior:      chatBehavior,
		Delivery:          deliveryRuntime,
		ReasoningDelivery: delivery,
	})
}

// UnregisterRun removes a run tracking entry.
func (m *Manager) UnregisterRun(runID string) {
	if val, ok := m.runs.LoadAndDelete(runID); ok {
		if rc, ok := val.(*RunContext); ok {
			m.cancelQuickAck(rc)
			m.flushReasoningBubblesForContext(rc)
		}
	}
}

func (m *Manager) InterimDeliverySnapshot(runID string) (int, string) {
	val, ok := m.runs.Load(runID)
	if ok {
		rc, ok := val.(*RunContext)
		if !ok {
			return 0, ""
		}
		rc.mu.Lock()
		defer rc.mu.Unlock()
		return rc.interimDelivered, rc.lastInterimReply
	}
	return 0, ""
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

// ResolveChatBehavior checks per-channel override, then falls back to gateway config.
func (m *Manager) ResolveChatBehavior(channelName string, globalDefault *config.ChatBehaviorConfig) ResolvedChatBehavior {
	return m.ResolveChatBehaviorWithAgent(channelName, globalDefault, nil)
}

func (m *Manager) ResolveChatBehaviorWithAgent(channelName string, globalDefault, agentOverride *config.ChatBehaviorConfig) ResolvedChatBehavior {
	var override *config.ChatBehaviorConfig
	var channelBlockReply *bool
	m.mu.RLock()
	ch, exists := m.channels[channelName]
	m.mu.RUnlock()
	if exists {
		if bc, ok := ch.(BlockReplyChannel); ok {
			channelBlockReply = bc.BlockReplyEnabled()
		}
		if bc, ok := ch.(ChatBehaviorChannel); ok {
			override = bc.ChatBehaviorConfig()
		}
	}
	override = ChatBehaviorConfigWithIntermediateDefault(override, channelBlockReply)
	return ResolveChatBehaviorWithAgent(globalDefault, agentOverride, override)
}

func (m *Manager) ResolveReasoningDelivery(channelName string) ResolvedReasoningDelivery {
	m.mu.RLock()
	ch, exists := m.channels[channelName]
	m.mu.RUnlock()
	if !exists {
		return ResolveReasoningDelivery("", nil)
	}
	if rc, ok := ch.(ReasoningDeliveryChannel); ok {
		mode, legacy := rc.ReasoningDeliveryConfig()
		return ResolveReasoningDelivery(mode, legacy)
	}
	if sc, ok := ch.(StreamingChannel); ok {
		legacy := sc.ReasoningStreamEnabled()
		return ResolveReasoningDelivery("", &legacy)
	}
	return ResolveReasoningDelivery("", nil)
}
