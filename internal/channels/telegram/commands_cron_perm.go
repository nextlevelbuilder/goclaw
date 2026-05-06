package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// handleCronPermCommand handles /addcron and /removecron commands.
// Mirrors handleWriterCommand but operates on ConfigTypeCron — explicit cron-only
// grant so admins can give users cron access without granting full file_writer.
//
// Bootstrap policy:
//   - First /addcron caller in the group: bootstrap allowed (matches /addwriter).
//   - Subsequent: existing croner OR file_writer (full-access fallback) can grant.
//
// Target user: identified by replying to one of their messages.
func (c *Channel) handleCronPermCommand(ctx context.Context, message *telego.Message, chatID int64, chatIDStr, senderID string, isGroup bool, setThread func(*telego.SendMessageParams), action string) {
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
		send("Cron permission management is not available.")
		return
	}

	agentID, err := c.resolveAgentUUID(ctx)
	if err != nil {
		slog.Debug("cron-perm command: agent resolve failed", "error", err)
		send("Cron permission management is not available (no agent).")
		return
	}

	groupID := fmt.Sprintf("group:%s:%s", c.Name(), chatIDStr)
	senderNumericID := strings.SplitN(senderID, "|", 2)[0]

	// Bootstrap exception: if no croners AND no file_writers exist, the first
	// caller can bootstrap. Otherwise either an existing croner or file_writer
	// (full-access role) can manage the croner list.
	existingCroners, _ := c.configPermStore.List(ctx, agentID, store.ConfigTypeCron, groupID)
	existingWriters, _ := c.configPermStore.ListFileWriters(ctx, agentID, groupID)

	if len(existingCroners) > 0 || len(existingWriters) > 0 {
		isAuthorized := false
		for _, w := range existingCroners {
			if w.UserID == senderNumericID && w.Permission == "allow" {
				isAuthorized = true
				break
			}
		}
		if !isAuthorized {
			for _, w := range existingWriters {
				if w.UserID == senderNumericID && w.Permission == "allow" {
					isAuthorized = true
					break
				}
			}
		}
		if !isAuthorized {
			send("Only existing cron managers (or file writers) can manage the cron list.")
			return
		}
	} else if action == "remove" {
		send("No cron managers configured yet. Use /addcron to add the first one.")
		return
	}

	// Extract target user from reply-to message
	if message.ReplyToMessage == nil || message.ReplyToMessage.From == nil {
		verb := "add"
		if action == "remove" {
			verb = "remove"
		}
		send(fmt.Sprintf("To %s a cron manager: find a message from that person, swipe to reply it, then type /%scron.", verb, verb))
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
			ConfigType: store.ConfigTypeCron,
			UserID:     targetID,
			Permission: "allow",
			Metadata:   meta,
		}); err != nil {
			slog.Warn("add cron permission failed", "error", err, "target", targetID)
			send("Failed to add cron manager. Please try again.")
			return
		}
		send(fmt.Sprintf("Added %s as a cron manager.", targetName))

	case "remove":
		// Prevent removing the last croner ONLY if no file_writers can fall back.
		// File writers retain cron access via the fallback in CheckCronPermission,
		// so removing the last croner is safe when at least one writer exists.
		if len(existingCroners) <= 1 && len(existingWriters) == 0 {
			send("Cannot remove the last cron manager (no file_writers to fall back on).")
			return
		}
		if err := c.configPermStore.Revoke(ctx, agentID, groupID, store.ConfigTypeCron, targetID); err != nil {
			slog.Warn("remove cron permission failed", "error", err, "target", targetID)
			send("Failed to remove cron manager. Please try again.")
			return
		}
		send(fmt.Sprintf("Removed %s from cron managers.", targetName))
	}
}

// handleListCronPerm handles the /croners command — lists users with explicit
// cron grants in this group. Note: file_writers also have implicit cron access
// via CheckCronPermission's fallback; this list shows ONLY explicit cron grants.
func (c *Channel) handleListCronPerm(ctx context.Context, chatID int64, chatIDStr string, isGroup bool, setThread func(*telego.SendMessageParams)) {
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
		send("Cron permission management is not available.")
		return
	}

	agentID, err := c.resolveAgentUUID(ctx)
	if err != nil {
		slog.Debug("list cron managers: agent resolve failed", "error", err)
		send("Cron permission management is not available (no agent).")
		return
	}

	groupID := fmt.Sprintf("group:%s:%s", c.Name(), chatIDStr)

	croners, err := c.configPermStore.List(ctx, agentID, store.ConfigTypeCron, groupID)
	if err != nil {
		slog.Warn("list cron managers failed", "error", err)
		send("Failed to list cron managers. Please try again.")
		return
	}

	// Also fetch writers for the trailing note (they have implicit cron access).
	writers, _ := c.configPermStore.ListFileWriters(ctx, agentID, groupID)

	if len(croners) == 0 && len(writers) == 0 {
		send("No cron managers configured for this group. Use /addcron to add one (or /addwriter for full file access).")
		return
	}

	var sb strings.Builder
	if len(croners) == 0 {
		sb.WriteString("No explicit cron managers configured.\n")
	} else {
		sb.WriteString(fmt.Sprintf("Cron managers for this group (%d):\n", len(croners)))
		for i, w := range croners {
			label := channels.WriterLabel(w.Metadata, w.UserID)
			sb.WriteString(fmt.Sprintf("%d. %s (ID: %s) — %s\n", i+1, label, w.UserID, w.Permission))
		}
	}
	if len(writers) > 0 {
		sb.WriteString(fmt.Sprintf("\nPlus %d file_writer(s) with implicit cron access (use /writers to list).", len(writers)))
	}
	send(sb.String())
}
