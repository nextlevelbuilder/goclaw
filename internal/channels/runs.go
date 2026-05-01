package channels

import "github.com/google/uuid"

// --- Run tracking for streaming/reaction event forwarding ---

// RegisterRun associates a run ID with a channel context so agent events
// (chunks, tool calls, completion) can be forwarded to the originating channel.
func (m *Manager) RegisterRun(runID, channelName, chatID, messageID string, metadata map[string]string, tenantID uuid.UUID, streaming, blockReply, toolStatus bool) {
	m.runs.Store(runID, &RunContext{
		ChannelName:       channelName,
		ChatID:            chatID,
		MessageID:         messageID,
		Metadata:          metadata,
		TenantID:          tenantID,
		Streaming:         streaming,
		BlockReplyEnabled: blockReply,
		ToolStatusEnabled: toolStatus,
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
