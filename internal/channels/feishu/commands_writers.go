package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// writerCommandTimeout bounds the worst-case latency of a writer management
// command (DB lookups + optional parent-message fetch + send reply). Mirrors
// Discord's 10s handler timeout so a flaky Lark backend cannot block the
// inbound message goroutine indefinitely.
const writerCommandTimeout = 10 * time.Second

// isWriterSlashCommand reports whether the message content begins with one
// of the writer management slash commands. Used to short-circuit DM slash
// commands early, before any agent routing or group-policy gating runs.
func (c *Channel) isWriterSlashCommand(mc *messageContext) bool {
	text := strings.TrimSpace(mc.Content)
	if !strings.HasPrefix(text, "/") {
		return false
	}
	cmd := strings.ToLower(strings.SplitN(text, " ", 2)[0])
	switch cmd {
	case "/addwriter", "/removewriter", "/writers":
		return true
	}
	return false
}

// resolveAgentUUID converts the channel's configured agent key (which may be
// either a raw UUID or an agent_key string) into a canonical UUID the
// ConfigPermissionStore expects. Required by all writer commands — if the
// channel has no agent context, the commands disable themselves with a clear
// message rather than crashing.
func (c *Channel) resolveAgentUUID(ctx context.Context) (uuid.UUID, error) {
	key := c.AgentID()
	if key == "" {
		return uuid.Nil, fmt.Errorf("no agent key configured")
	}
	if id, err := uuid.Parse(key); err == nil {
		return id, nil
	}
	if c.agentStore == nil {
		return uuid.Nil, fmt.Errorf("agent store unavailable")
	}
	ctx = store.WithTenantID(ctx, c.TenantID())
	agent, err := c.agentStore.GetByKey(ctx, key)
	if err != nil {
		return uuid.Nil, fmt.Errorf("agent %q not found: %w", key, err)
	}
	return agent.ID, nil
}

// maybeHandleWriterCommand inspects an inbound Feishu message for writer
// management slash commands and handles them in-channel (without routing to
// the agent pipeline). Returns true when the message was consumed as a
// command so the caller can short-circuit.
//
// Supported commands (group chats only):
//   - /addwriter       — grant file_writer permission to a target user
//   - /removewriter    — revoke file_writer permission
//   - /writers         — list current writers in the group
//
// Target user is identified by reply-to first, then by the first @mention
// that is not the bot itself. Mirrors Telegram / Discord patterns for
// consistency across channels.
func (c *Channel) maybeHandleWriterCommand(ctx context.Context, mc *messageContext) bool {
	text := strings.TrimSpace(mc.Content)
	if text == "" || !strings.HasPrefix(text, "/") {
		return false
	}
	cmd := strings.SplitN(text, " ", 2)[0]
	switch strings.ToLower(cmd) {
	case "/addwriter":
		c.handleFeishuWriterCommand(ctx, mc, "add")
		return true
	case "/removewriter":
		c.handleFeishuWriterCommand(ctx, mc, "remove")
		return true
	case "/writers":
		c.handleFeishuListWriters(ctx, mc)
		return true
	}
	return false
}

// sendCommandReply posts a short text reply to the chat where the command
// was received. Uses the normal sendText path so thread routing remains
// consistent with the rest of the channel.
func (c *Channel) sendCommandReply(ctx context.Context, mc *messageContext, text string) {
	receiveIDType := resolveReceiveIDType(mc.ChatID)
	replyTarget := ""
	if mc.ThreadID != "" {
		replyTarget = mc.MessageID
	}
	if err := c.sendText(ctx, mc.ChatID, receiveIDType, text, replyTarget); err != nil {
		slog.Warn("feishu.writer_cmd.reply_failed", "error", err, "chat_id", mc.ChatID)
	}
}

// resolveWriterTarget selects the target user for grant/revoke.
// Preference: reply-to (via ParentID → fetch sender) first, then first
// non-bot @mention. Returns empty strings if no target can be determined.
func (c *Channel) resolveWriterTarget(ctx context.Context, mc *messageContext) (userID, displayName string) {
	// Reply-to path: fetch parent message to recover its sender open_id.
	if mc.ParentID != "" && c.client != nil {
		resp, err := c.client.GetMessage(ctx, mc.ParentID)
		if err == nil && len(resp.Items) > 0 && resp.Items[0].Sender.ID != "" {
			id := resp.Items[0].Sender.ID
			name := c.resolveSenderName(ctx, id)
			return id, name
		}
	}
	// Mention path: first non-bot mention in the command text.
	for _, m := range mc.Mentions {
		if m.OpenID == "" || m.OpenID == c.botOpenID {
			continue
		}
		return m.OpenID, m.Name
	}
	return "", ""
}

// handleFeishuWriterCommand implements /addwriter and /removewriter.
//
// The target user MUST be specified explicitly — either by reply-to or by
// @mention. Target-less self-grant is intentionally NOT supported: a curious
// user exploring the command should not accidentally capture first-writer
// privilege in an empty group. To self-grant, users can @mention themselves.
// This aligns Feishu's semantics with Telegram and Discord.
//
// The only "bootstrap" carveout is that when the writer list is empty, any
// sender is allowed to pass the authorization gate — so whoever types the
// first /addwriter @target gets to seed the allowlist.
func (c *Channel) handleFeishuWriterCommand(parentCtx context.Context, mc *messageContext, action string) {
	// Group policy + DM rejection are already enforced by bot.go before
	// this handler is invoked — the DM check here is belt-and-suspenders
	// for callers that bypass the normal pipeline (e.g. direct test calls).
	if mc.ChatType != "group" {
		c.sendCommandReply(parentCtx, mc, "This command only works in group chats.")
		return
	}
	if c.configPermStore == nil {
		c.sendCommandReply(parentCtx, mc, "File writer management is not available.")
		return
	}
	// When the bot probe has not yet resolved the bot's open_id, mention
	// filtering cannot distinguish bot self-mentions. Refuse to run writer
	// commands until the probe completes so a user cannot accidentally
	// grant the bot itself as a file writer via /addwriter @bot.
	if c.botOpenID == "" {
		c.sendCommandReply(parentCtx, mc, "Bot identity not yet resolved — please retry in a moment.")
		return
	}

	ctx, cancel := context.WithTimeout(parentCtx, writerCommandTimeout)
	defer cancel()

	agentID, err := c.resolveAgentUUID(ctx)
	if err != nil {
		slog.Debug("feishu.writer_cmd.agent_resolve_failed", "error", err)
		c.sendCommandReply(ctx, mc, "File writer management is not available (no agent).")
		return
	}

	groupID := fmt.Sprintf("group:%s:%s", c.Name(), mc.ChatID)
	senderID := mc.SenderID // Feishu open_id has no suffix (Telegram's "|username" strip not needed)

	existingWriters, _ := c.configPermStore.ListFileWriters(ctx, agentID, groupID)

	// Authorization gate: only existing writers can manage the allowlist.
	// Empty-writer groups allow the very first caller to seed the list —
	// but an explicit target is still required, so we don't silently grant
	// the caller when they type /addwriter with no reply/@mention.
	if len(existingWriters) > 0 {
		isWriter := false
		for _, w := range existingWriters {
			if w.UserID == senderID {
				isWriter = true
				break
			}
		}
		if !isWriter {
			c.sendCommandReply(ctx, mc, "Only existing file writers can manage the writer list.")
			return
		}
	} else if action == "remove" {
		c.sendCommandReply(ctx, mc, "No file writers configured yet. Use /addwriter to add the first one.")
		return
	}

	targetID, targetName := c.resolveWriterTarget(ctx, mc)
	if targetID == "" {
		verb := "add"
		if action == "remove" {
			verb = "remove"
		}
		c.sendCommandReply(ctx, mc, fmt.Sprintf("To %s a writer: reply to their message with /%swriter, or @mention them (including yourself to self-grant).", verb, verb))
		return
	}
	if targetName == "" {
		targetName = targetID
	}

	switch action {
	case "add":
		meta, _ := json.Marshal(map[string]string{"displayName": targetName})
		if err := c.configPermStore.Grant(ctx, &store.ConfigPermission{
			AgentID:    agentID,
			Scope:      groupID,
			ConfigType: store.ConfigTypeFileWriter,
			UserID:     targetID,
			Permission: "allow",
			Metadata:   meta,
		}); err != nil {
			slog.Warn("feishu.writer_cmd.add_failed", "error", err, "target", targetID)
			c.sendCommandReply(ctx, mc, "Failed to add writer. Please try again.")
			return
		}
		c.sendCommandReply(ctx, mc, fmt.Sprintf("Added %s as a file writer.", targetName))

	case "remove":
		if len(existingWriters) <= 1 {
			c.sendCommandReply(ctx, mc, "Cannot remove the last file writer.")
			return
		}
		if err := c.configPermStore.Revoke(ctx, agentID, groupID, store.ConfigTypeFileWriter, targetID); err != nil {
			slog.Warn("feishu.writer_cmd.remove_failed", "error", err, "target", targetID)
			c.sendCommandReply(ctx, mc, "Failed to remove writer. Please try again.")
			return
		}
		c.sendCommandReply(ctx, mc, fmt.Sprintf("Removed %s from file writers.", targetName))
	}
}

// handleFeishuListWriters implements /writers.
func (c *Channel) handleFeishuListWriters(parentCtx context.Context, mc *messageContext) {
	if mc.ChatType != "group" {
		c.sendCommandReply(parentCtx, mc, "This command only works in group chats.")
		return
	}
	if c.configPermStore == nil {
		c.sendCommandReply(parentCtx, mc, "File writer management is not available.")
		return
	}
	ctx, cancel := context.WithTimeout(parentCtx, writerCommandTimeout)
	defer cancel()
	agentID, err := c.resolveAgentUUID(ctx)
	if err != nil {
		slog.Debug("feishu.writer_cmd.agent_resolve_failed", "error", err)
		c.sendCommandReply(ctx, mc, "File writer management is not available (no agent).")
		return
	}

	groupID := fmt.Sprintf("group:%s:%s", c.Name(), mc.ChatID)
	writers, err := c.configPermStore.List(ctx, agentID, store.ConfigTypeFileWriter, groupID)
	if err != nil {
		slog.Warn("feishu.writer_cmd.list_failed", "error", err)
		c.sendCommandReply(ctx, mc, "Failed to list writers. Please try again.")
		return
	}
	if len(writers) == 0 {
		c.sendCommandReply(ctx, mc, "No file writers configured for this group. Use /addwriter to add one.")
		return
	}

	type fwMeta struct {
		DisplayName string `json:"displayName"`
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "File writers for this group (%d):\n", len(writers))
	for i, w := range writers {
		var meta fwMeta
		_ = json.Unmarshal(w.Metadata, &meta)
		label := w.UserID
		if meta.DisplayName != "" {
			label = meta.DisplayName
		}
		fmt.Fprintf(&sb, "%d. %s (ID: %s)\n", i+1, label, w.UserID)
	}
	c.sendCommandReply(ctx, mc, sb.String())
}
