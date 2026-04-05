package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// handleWriterCommand handles /addwriter and /removewriter commands.
// The target user is identified by replying to one of their messages.
func (c *Channel) handleWriterCommand(ctx context.Context, message *telego.Message, chatID int64, chatIDStr, senderID string, isGroup bool, setThread func(*telego.SendMessageParams), action string) {
	chatIDObj := tu.ID(chatID)

	send := func(text string) {
		msg := tu.Message(chatIDObj, text)
		setThread(msg)
		c.bot.SendMessage(ctx, msg)
	}

	if !isGroup {
		send("This command only works in group chats.")
		return
	}

	if c.configPermStore == nil {
		send("File writer management is not available.")
		return
	}

	agentID, err := c.resolveAgentUUID(ctx)
	if err != nil {
		slog.Debug("writer command: agent resolve failed", "error", err)
		send("File writer management is not available (no agent).")
		return
	}

	groupID := fmt.Sprintf("group:%s:%s", c.Name(), chatIDStr)
	senderNumericID := strings.SplitN(senderID, "|", 2)[0]

	// Fetch existing writers (cached 60s) for both permission check and remove guard.
	// Bootstrap exception: if no writers exist yet, the first /addwriter caller
	// is allowed to bootstrap the allowlist.
	existingWriters, _ := c.configPermStore.ListFileWriters(ctx, agentID, groupID)

	if len(existingWriters) > 0 {
		isWriter := false
		for _, w := range existingWriters {
			if w.UserID == senderNumericID {
				isWriter = true
				break
			}
		}
		if !isWriter {
			send("Only existing file writers can manage the writer list.")
			return
		}
	} else if action == "remove" {
		send("No file writers configured yet. Use /addwriter to add the first one.")
		return
	}

	// Extract target user from reply-to message
	if message.ReplyToMessage == nil || message.ReplyToMessage.From == nil {
		verb := "add"
		if action == "remove" {
			verb = "remove"
		}
		send(fmt.Sprintf("To %s a writer: find a message from that person, swipe to reply it, then type /%swriter.", verb, verb))
		return
	}

	targetUser := message.ReplyToMessage.From
	targetID := fmt.Sprintf("%d", targetUser.ID)
	targetName := targetUser.FirstName
	if targetUser.Username != "" {
		targetName = "@" + targetUser.Username
	}

	switch action {
	case "add":
		meta, _ := json.Marshal(map[string]string{"displayName": targetUser.FirstName, "username": targetUser.Username})
		if err := c.configPermStore.Grant(ctx, &store.ConfigPermission{
			AgentID:    agentID,
			Scope:      groupID,
			ConfigType: store.ConfigTypeFileWriter,
			UserID:     targetID,
			Permission: "allow",
			Metadata:   meta,
		}); err != nil {
			slog.Warn("add writer failed", "error", err, "target", targetID)
			send("Failed to add writer. Please try again.")
			return
		}
		send(fmt.Sprintf("Added %s as a file writer.", targetName))

	case "remove":
		// Prevent removing the last writer (reuse cached existingWriters)
		if len(existingWriters) <= 1 {
			send("Cannot remove the last file writer.")
			return
		}
		if err := c.configPermStore.Revoke(ctx, agentID, groupID, store.ConfigTypeFileWriter, targetID); err != nil {
			slog.Warn("remove writer failed", "error", err, "target", targetID)
			send("Failed to remove writer. Please try again.")
			return
		}
		send(fmt.Sprintf("Removed %s from file writers.", targetName))
	}
}

// handleListWriters handles the /writers command.
func (c *Channel) handleListWriters(ctx context.Context, chatID int64, chatIDStr string, isGroup bool, setThread func(*telego.SendMessageParams)) {
	chatIDObj := tu.ID(chatID)

	send := func(text string) {
		msg := tu.Message(chatIDObj, text)
		setThread(msg)
		c.bot.SendMessage(ctx, msg)
	}

	if !isGroup {
		send("This command only works in group chats.")
		return
	}

	if c.configPermStore == nil {
		send("File writer management is not available.")
		return
	}

	agentID, err := c.resolveAgentUUID(ctx)
	if err != nil {
		slog.Debug("list writers: agent resolve failed", "error", err)
		send("File writer management is not available (no agent).")
		return
	}

	groupID := fmt.Sprintf("group:%s:%s", c.Name(), chatIDStr)

	writers, err := c.configPermStore.List(ctx, agentID, store.ConfigTypeFileWriter, groupID)
	if err != nil {
		slog.Warn("list writers failed", "error", err)
		send("Failed to list writers. Please try again.")
		return
	}

	if len(writers) == 0 {
		send("No file writers configured for this group. Use /addwriter to add one.")
		return
	}

	type fwMeta struct {
		DisplayName string `json:"displayName"`
		Username    string `json:"username"`
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("File writers for this group (%d):\n", len(writers)))
	for i, w := range writers {
		var meta fwMeta
		_ = json.Unmarshal(w.Metadata, &meta)
		label := w.UserID
		if meta.Username != "" {
			label = "@" + meta.Username
		} else if meta.DisplayName != "" {
			label = meta.DisplayName
		}
		sb.WriteString(fmt.Sprintf("%d. %s (ID: %s)\n", i+1, label, w.UserID))
	}
	send(sb.String())
}
