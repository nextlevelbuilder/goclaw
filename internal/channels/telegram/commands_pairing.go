package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

// --- Pairing UX ---

// buildPairingReply builds the pairing reply message for unpaired users.
func buildPairingReply(code string) string {
	return fmt.Sprintf(
		"🔗 This account hasn't been paired yet.\n\nPairing code: %s\n\nShare this code with the bot owner to get access.",
		code,
	)
}

// sendPairingReply generates a pairing code and sends the reply to the user.
// Debounces: won't send another reply to the same user within 60 seconds.
func (c *Channel) sendPairingReply(ctx context.Context, chatID int64, userID, username string) {
	ps := c.PairingService()
	if ps == nil {
		return
	}

	if !c.CanSendPairingNotif(userID, pairingReplyDebounce) {
		slog.Debug("pairing reply debounced", "user_id", userID)
		return
	}

	meta := map[string]string{"username": username}
	code, err := ps.RequestPairing(ctx, userID, c.Name(), fmt.Sprintf("%d", chatID), "default", meta)
	if err != nil {
		slog.Debug("pairing request failed", "user_id", userID, "error", err)
		return
	}

	replyText := buildPairingReply(code)
	msg := tu.Message(tu.ID(chatID), replyText)
	if _, err := c.bot.SendMessage(ctx, msg); err != nil {
		slog.Warn("failed to send pairing reply", "chat_id", chatID, "error", err)
	} else {
		c.MarkPairingNotifSent(userID)
		slog.Info("telegram pairing reply sent",
			"user_id", userID, "username", username, "code", code,
		)
	}
}

// sendGroupPairingReply generates a pairing code for a group and sends the reply.
// Debounces: won't send another reply to the same group within 60 seconds.
// messageThreadID should be set for forum groups so the reply lands in the correct topic.
// localKey is the composite key (e.g. "-100123:topic:42") stored as chat_id in the pairing
// request so that the approval notification can be routed to the correct forum topic.
func (c *Channel) sendGroupPairingReply(ctx context.Context, chatID int64, chatIDStr, groupSenderID, localKey string, messageThreadID int, chatTitle string) {
	ps := c.PairingService()
	if ps == nil {
		return
	}

	if !c.CanSendPairingNotif(chatIDStr, pairingReplyDebounce) {
		return
	}

	var meta map[string]string
	if chatTitle != "" {
		meta = map[string]string{"chat_title": chatTitle}
	}
	code, err := ps.RequestPairing(ctx, groupSenderID, c.Name(), localKey, "default", meta)
	if err != nil {
		slog.Debug("group pairing request failed", "chat_id", chatIDStr, "error", err)
		return
	}

	replyText := fmt.Sprintf(
		"🔗 This group hasn't been paired yet.\n\nPairing code: %s\n\nShare this code with the bot owner to get access.",
		code,
	)
	msg := tu.Message(tu.ID(chatID), replyText)
	if messageThreadID > 0 {
		msg.MessageThreadID = messageThreadID
	}
	_, err = c.bot.SendMessage(ctx, msg)
	// Retry without thread ID if topic is hidden/deleted (forum group with General topic removed).
	if err != nil && messageThreadID > 0 && strings.Contains(err.Error(), "thread not found") {
		msg.MessageThreadID = 0
		_, err = c.bot.SendMessage(ctx, msg)
	}
	if err != nil {
		slog.Warn("failed to send group pairing reply", "chat_id", chatIDStr, "error", err)
	} else {
		c.MarkPairingNotifSent(chatIDStr)
		slog.Info("telegram group pairing reply sent", "chat_id", chatIDStr, "code", code)
	}
}

// SendPairingApproved sends the approval notification to a user.
// chatID may contain a topic suffix (e.g. "-100123:topic:42") for forum groups.
func (c *Channel) SendPairingApproved(ctx context.Context, chatID, botName string) error {
	id, err := parseRawChatID(chatID)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}
	if botName == "" {
		botName = "GoClaw"
	}

	msg := tu.Message(tu.ID(id), fmt.Sprintf("✅ %s access approved. Send a message to start chatting.", botName))

	// Extract thread ID from topic/thread suffix for forum groups.
	if idx := strings.Index(chatID, ":topic:"); idx > 0 {
		var threadID int
		fmt.Sscanf(chatID[idx+7:], "%d", &threadID)
		if threadID > 0 {
			msg.MessageThreadID = threadID
		}
	} else if idx := strings.Index(chatID, ":thread:"); idx > 0 {
		var threadID int
		fmt.Sscanf(chatID[idx+8:], "%d", &threadID)
		if threadID > 0 {
			msg.MessageThreadID = threadID
		}
	}

	_, err = c.bot.SendMessage(ctx, msg)
	return err
}

// SyncMenuCommands registers bot commands with Telegram via setMyCommands.
func (c *Channel) SyncMenuCommands(ctx context.Context, commands []telego.BotCommand) error {
	if err := c.bot.DeleteMyCommands(ctx, nil); err != nil {
		slog.Debug("deleteMyCommands failed (may not exist)", "error", err)
	}

	if len(commands) == 0 {
		return nil
	}

	if len(commands) > 100 {
		commands = commands[:100]
	}

	return c.bot.SetMyCommands(ctx, &telego.SetMyCommandsParams{
		Commands: commands,
	})
}

// DefaultMenuCommands returns the default bot menu commands.
func DefaultMenuCommands() []telego.BotCommand {
	return []telego.BotCommand{
		{Command: "start", Description: "Start chatting with the bot"},
		{Command: "help", Description: "Show available commands"},
		{Command: "stop", Description: "Stop current running task"},
		{Command: "stopall", Description: "Stop all running tasks"},
		{Command: "reset", Description: "Reset conversation history"},
		{Command: "status", Description: "Show bot status"},
		{Command: "reactions", Description: "Show reaction emoji legend"},
		{Command: "tasks", Description: "List team tasks"},
		{Command: "task_detail", Description: "View task detail by ID"},
		{Command: "subagents", Description: "List subagent tasks"},
		{Command: "subagent", Description: "View subagent task detail by ID"},
		{Command: "writers", Description: "List file writers for this group"},
		{Command: "addwriter", Description: "Add a file writer (reply to their message)"},
		{Command: "removewriter", Description: "Remove a file writer (reply to their message)"},
	}
}
