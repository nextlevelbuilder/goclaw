package whatsapp

import (
	"context"
	"strings"

	"go.mau.fi/whatsmeow/types"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const menuHelpText = "Available commands:\n" +
	"/reset — Reset conversation history\n" +
	"/stop — Stop current running task\n" +
	"/stopall — Stop all running tasks\n" +
	"/menu — Show this help message\n" +
	"\nJust send a message to chat with the AI."

// handleCommand checks if the message is a known slash command and handles it.
// Returns true if the message was handled (caller should stop processing).
func (c *Channel) handleCommand(ctx context.Context, text, senderID, chatID, peerKind string, chatJID types.JID) bool {
	if len(text) == 0 || text[0] != '/' {
		return false
	}

	cmd := strings.SplitN(text, " ", 2)[0]
	cmd = strings.ToLower(cmd)

	switch cmd {
	case "/reset":
		c.Bus().PublishInbound(bus.InboundMessage{
			Channel:  c.Name(),
			SenderID: senderID,
			ChatID:   chatID,
			Content:  "/reset",
			PeerKind: peerKind,
			AgentID:  c.AgentID(),
			UserID:   stripSenderUserID(senderID),
			TenantID: c.TenantID(),
			Metadata: map[string]string{
				tools.MetaCommand: "reset",
			},
		})
		c.sendText(chatJID, "Conversation history has been reset.")
		return true

	case "/stop":
		c.Bus().PublishInbound(bus.InboundMessage{
			Channel:  c.Name(),
			SenderID: senderID,
			ChatID:   chatID,
			Content:  "/stop",
			PeerKind: peerKind,
			AgentID:  c.AgentID(),
			UserID:   stripSenderUserID(senderID),
			TenantID: c.TenantID(),
			Metadata: map[string]string{
				tools.MetaCommand: "stop",
			},
		})
		// Feedback is sent by the consumer after cancel result is known.
		return true

	case "/stopall":
		c.Bus().PublishInbound(bus.InboundMessage{
			Channel:  c.Name(),
			SenderID: senderID,
			ChatID:   chatID,
			Content:  "/stopall",
			PeerKind: peerKind,
			AgentID:  c.AgentID(),
			UserID:   stripSenderUserID(senderID),
			TenantID: c.TenantID(),
			Metadata: map[string]string{
				tools.MetaCommand: "stopall",
			},
		})
		// Feedback is sent by the consumer after cancel result is known.
		return true

	case "/menu":
		c.sendText(chatJID, menuHelpText)
		return true
	}

	return false
}

// stripSenderUserID extracts the user ID portion before the '|' separator.
func stripSenderUserID(senderID string) string {
	if idx := strings.IndexByte(senderID, '|'); idx > 0 {
		return senderID[:idx]
	}
	return senderID
}
