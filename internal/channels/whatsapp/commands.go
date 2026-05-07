package whatsapp

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const menuHelpText = "Available commands:\n" +
	"/reset — Reset conversation history\n" +
	"/stop — Stop current running task\n" +
	"/stopall — Stop all running tasks\n" +
	"/approve <code> — Approve a pending command (or reply 1)\n" +
	"/deny <code> — Deny a pending command (or reply 2)\n" +
	"/addwriter — Add a file writer (reply to their message or @mention)\n" +
	"/removewriter — Remove a file writer (reply to their message or @mention)\n" +
	"/writers — List file writers for this group\n" +
	"/menu — Show this help message\n" +
	"\nJust send a message to chat with the AI."

// handleCommand checks if the message is a known slash command and handles it.
// Returns true if the message was handled (caller should stop processing).
func (c *Channel) handleCommand(ctx context.Context, text, senderID, chatID, peerKind string, chatJID types.JID, evt *events.Message) bool {
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
			AgentID:  c.resolveAgentID(chatID, peerKind),
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
			AgentID:  c.resolveAgentID(chatID, peerKind),
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
			AgentID:  c.resolveAgentID(chatID, peerKind),
			UserID:   stripSenderUserID(senderID),
			TenantID: c.TenantID(),
			Metadata: map[string]string{
				tools.MetaCommand: "stopall",
			},
		})
		// Feedback is sent by the consumer after cancel result is known.
		return true

	case "/addwriter":
		if peerKind != "group" {
			c.sendText(chatJID, "This command only works in group chats.")
			return true
		}
		c.handleWriterCommand(ctx, evt, senderID, chatID, chatJID, "add")
		return true

	case "/removewriter":
		if peerKind != "group" {
			c.sendText(chatJID, "This command only works in group chats.")
			return true
		}
		c.handleWriterCommand(ctx, evt, senderID, chatID, chatJID, "remove")
		return true

	case "/writers":
		if peerKind != "group" {
			c.sendText(chatJID, "This command only works in group chats.")
			return true
		}
		c.handleListWriters(ctx, chatID, chatJID)
		return true

	case "/menu":
		c.sendText(chatJID, menuHelpText)
		return true

	case "/approve":
		c.handleExecApprovalCommand(ctx, text, chatJID, tools.ApprovalAllowOnce)
		return true

	case "/deny":
		c.handleExecApprovalCommand(ctx, text, chatJID, tools.ApprovalDeny)
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

// handleExecApprovalCommand handles /approve <code> and /deny <code> commands.
func (c *Channel) handleExecApprovalCommand(ctx context.Context, text string, chatJID types.JID, decision tools.ApprovalDecision) {
	if c.execApprovalMgr == nil {
		c.sendText(chatJID, "Approval system not available.")
		return
	}

	parts := strings.SplitN(strings.TrimSpace(text), " ", 2)
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		c.sendText(chatJID, "Usage: /approve <code> or /deny <code>")
		return
	}
	code := strings.ToUpper(strings.TrimSpace(parts[1]))

	err := c.execApprovalMgr.ResolveByShortCode(code, decision)
	if err != nil {
		c.sendText(chatJID, fmt.Sprintf("Error: %s", err.Error()))
		return
	}

	if decision == tools.ApprovalDeny {
		c.sendText(chatJID, "Command denied.")
	} else {
		c.sendText(chatJID, "Command approved.")
	}
}

// TryExecApprovalText detects approval responses in non-slash messages.
// Priority: "1" = approve, "2" = deny (by channel+chatID lookup), then
// "approve CODE" / "deny CODE" (by short code).
// Returns true if the message was handled as an approval response.
func (c *Channel) TryExecApprovalText(text string, chatJID types.JID, chatID string) bool {
	if c.execApprovalMgr == nil {
		return false
	}

	text = strings.TrimSpace(text)
	if len(text) == 0 {
		return false
	}

	// Strip WhatsApp reply-to context prefix so that "1"/"2" replies
	// to approval notifications are detected correctly.
	// extractTextContent prepends "[Replying to: ...]\n" for quoted replies.
	text = stripReplyContext(text)

	channelName := c.Name()
	lower := strings.ToLower(text)

	// Priority 1: Simple numbered reply — "1" (approve) or "2" (deny).
	if lower == "1" || lower == "2" {
		decision := tools.ApprovalAllowOnce
		if lower == "2" {
			decision = tools.ApprovalDeny
		}
		code, cmd, err := c.execApprovalMgr.ResolveByChannel(channelName, chatID, decision)
		if err != nil {
			return false // no pending approval for this chat — treat as normal message
		}

		verb := "Approved"
		if decision == tools.ApprovalDeny {
			verb = "Denied"
		}
		c.sendText(chatJID, fmt.Sprintf("%s [%s]: %s", verb, code, cmd))

		// List remaining pending approvals for this chat.
		remaining := c.execApprovalMgr.ListPendingByChannel(channelName, chatID)
		if len(remaining) > 0 {
			var b strings.Builder
			b.WriteString(fmt.Sprintf("\n%d pending approval(s) remaining:", len(remaining)))
			for _, pa := range remaining {
				b.WriteString(fmt.Sprintf("\n  [%s] %s", pa.ShortCode, truncateCmdLocal(pa.Command, 80)))
			}
			b.WriteString("\n\nReply \"approve CODE\" or \"deny CODE\" to target a specific one.")
			c.sendText(chatJID, b.String())
		}
		return true
	}

	// Priority 2: Natural language — "approve CODE" or "deny CODE".
	var decision tools.ApprovalDecision
	var code string

	if idx := strings.Index(lower, "approve"); idx >= 0 {
		rest := strings.TrimSpace(text[idx+len("approve"):])
		fields := strings.Fields(rest)
		if len(fields) > 0 {
			code = strings.ToUpper(fields[0])
			decision = tools.ApprovalAllowOnce
		}
	} else if idx := strings.Index(lower, "deny"); idx >= 0 {
		rest := strings.TrimSpace(text[idx+len("deny"):])
		fields := strings.Fields(rest)
		if len(fields) > 0 {
			code = strings.ToUpper(fields[0])
			decision = tools.ApprovalDeny
		}
	}

	if code == "" || len(code) != 6 {
		return false
	}

	// Validate code format (only chars from our charset)
	for _, ch := range code {
		if !((ch >= 'A' && ch <= 'Z') || (ch >= '2' && ch <= '9')) {
			return false
		}
	}

	err := c.execApprovalMgr.ResolveByShortCode(code, decision)
	if err != nil {
		c.sendText(chatJID, fmt.Sprintf("Approval: %s", err.Error()))
		return true
	}

	if decision == tools.ApprovalDeny {
		c.sendText(chatJID, "Command denied.")
	} else {
		c.sendText(chatJID, "Command approved.")
	}
	return true
}

// stripReplyContext removes the "[Replying to: ...]" prefix that extractTextContent
// prepends for quoted-reply messages. The approval detection logic needs the raw
// user response ("1", "2", "approve CODE") without the quoted context.
func stripReplyContext(text string) string {
	if !strings.HasPrefix(text, "[Replying to:") {
		return text
	}
	_, after, ok := strings.Cut(text, "]")
	if !ok {
		return text
	}
	rest := after
	return strings.TrimPrefix(rest, "\n")
}

func truncateCmdLocal(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
