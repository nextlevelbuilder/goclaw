package whatsapp

import (
	"fmt"
	"log/slog"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

const (
	// Text command triggers (WhatsApp has no native slash commands).
	cmdMenu    = "#menu"
	cmdHelp    = "#help"
	cmdStop    = "#stop"
	cmdStopAll = "#stopall"
	cmdReset   = "#reset"
)

// isCommand checks if the given text is a recognized command trigger.
// Commands use "#" prefix. Case-insensitive, matches first whitespace-delimited token.
func isCommand(text string) bool {
	if len(text) == 0 || text[0] != '#' {
		return false
	}
	token := strings.ToLower(strings.SplitN(text, " ", 2)[0])
	switch token {
	case cmdMenu, cmdHelp, cmdStop, cmdStopAll, cmdReset:
		return true
	default:
		return false
	}
}

// handleCommand processes a recognized text command (e.g. "#menu", "#stop").
// Returns true if the message was fully handled and should not reach the agent pipeline.
func (c *Channel) handleCommand(text, senderID, chatID, peerKind string) bool {
	token := strings.ToLower(strings.SplitN(text, " ", 2)[0])

	switch token {
	case cmdMenu, cmdHelp:
		chatJID, err := types.ParseJID(chatID)
		if err != nil {
			slog.Warn("whatsapp: menu: invalid chat JID", "chat_id", chatID, "error", err)
			return true
		}
		c.sendMenu(chatJID)
		return true

	case cmdStop:
		c.publishCommand("stop", senderID, chatID, peerKind)
		return true

	case cmdStopAll:
		c.publishCommand("stopall", senderID, chatID, peerKind)
		return true

	case cmdReset:
		c.publishCommand("reset", senderID, chatID, peerKind)
		chatJID, err := types.ParseJID(chatID)
		if err == nil {
			c.sendTextMessage(chatJID, "Conversation history has been reset.")
		}
		return true

	default:
		return false
	}
}

// sendMenu sends a text-based command menu.
// WhatsApp linked devices (whatsmeow) cannot send interactive ListMessage —
// WhatsApp's server silently drops them. Text-based commands are the reliable approach.
func (c *Channel) sendMenu(chatJID types.JID) {
	menuText := "*GoClaw Menu*\n\n" +
		"#menu / #help — Show this menu\n" +
		"#stop — Stop current running task\n" +
		"#stopall — Stop all running tasks\n" +
		"#reset — Reset conversation history\n\n" +
		"Just send a message to chat with the AI."
	c.sendTextMessage(chatJID, menuText)
}

// sendTextMessage sends a plain text WhatsApp message.
func (c *Channel) sendTextMessage(chatJID types.JID, text string) {
	if c.client == nil || !c.client.IsConnected() {
		slog.Warn("whatsapp: cannot send text, not connected")
		return
	}
	msg := &waE2E.Message{
		Conversation: proto.String(text),
	}
	if _, err := c.client.SendMessage(c.ctx, chatJID, msg); err != nil {
		slog.Warn("whatsapp: failed to send text message", "error", err)
	}
}

// publishCommand publishes a command as an InboundMessage with command metadata,
// following the same pattern as Telegram's /stop, /stopall, /reset handlers.
// The gateway consumer (handleResetCommand, handleStopCommand) processes these.
func (c *Channel) publishCommand(command, senderID, chatID, peerKind string) {
	userID := senderID
	if idx := strings.IndexByte(senderID, '|'); idx > 0 {
		userID = senderID[:idx]
	}

	c.Bus().PublishInbound(bus.InboundMessage{
		Channel:  c.Name(),
		SenderID: senderID,
		ChatID:   chatID,
		Content:  fmt.Sprintf("/%s", command),
		PeerKind: peerKind,
		AgentID:  c.AgentID(),
		UserID:   userID,
		TenantID: c.TenantID(),
		Metadata: map[string]string{
			"command": command,
		},
	})
}
