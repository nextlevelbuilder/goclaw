package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// resolveAgentUUID looks up the agent UUID from the channel's agent key.
func (c *Channel) resolveAgentUUID(ctx context.Context) (uuid.UUID, error) {
	key := c.AgentID()
	if key == "" {
		return uuid.Nil, fmt.Errorf("no agent key configured")
	}
	if id, err := uuid.Parse(key); err == nil {
		return id, nil
	}
	ctx = store.WithTenantID(ctx, c.TenantID())
	agent, err := c.agentStore.GetByKey(ctx, key)
	if err != nil {
		return uuid.Nil, fmt.Errorf("agent %q not found: %w", key, err)
	}
	return agent.ID, nil
}

// tryHandleCommand checks if the message is a known bot command and handles it.
// Returns true if the message was consumed as a command.
func (c *Channel) tryHandleCommand(m *discordgo.MessageCreate) bool {
	content := strings.TrimSpace(m.Content)
	if content == "" {
		return false
	}

	// Accept both !command and /command prefixes.
	if content[0] != '!' && content[0] != '/' {
		return false
	}

	cmd := strings.SplitN(content, " ", 2)[0]
	cmd = strings.ToLower(cmd)
	// Normalize: strip prefix and compare.
	cmdName := cmd[1:] // remove ! or /

	switch cmdName {
	case "addwriter":
		c.handleWriterCommand(m, "add")
		return true
	case "removewriter":
		c.handleWriterCommand(m, "remove")
		return true
	case "writers":
		c.handleListWriters(m)
		return true
	}

	return false
}

// handleWriterCommand handles !addwriter and !removewriter commands.
// Target user is identified by @mention or by replying to their message.
func (c *Channel) handleWriterCommand(m *discordgo.MessageCreate, action string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	channelID := m.ChannelID

	send := func(text string) {
		c.session.ChannelMessageSend(channelID, text)
	}

	if m.GuildID == "" {
		send("This command only works in server channels.")
		return
	}

	if c.configPermStore == nil || c.agentStore == nil {
		send("File writer management is not available.")
		return
	}

	agentID, err := c.resolveAgentUUID(ctx)
	if err != nil {
		slog.Debug("discord writer command: agent resolve failed", "error", err)
		send("File writer management is not available (no agent).")
		return
	}

	// Guild-wide wildcard scope for grant/revoke/list operations.
	scope := fmt.Sprintf("guild:%s:*", m.GuildID)
	senderID := m.Author.ID

	// Fetch existing writers (cached 60s) for both permission check and remove guard.
	// Bootstrap exception: if no writers exist yet, the first !addwriter caller
	// is allowed to bootstrap the allowlist.
	existingWriters, _ := c.configPermStore.ListFileWriters(ctx, agentID, scope)

	if len(existingWriters) > 0 {
		isWriter := false
		for _, w := range existingWriters {
			if w.UserID == senderID {
				isWriter = true
				break
			}
		}
		if !isWriter {
			send("Only existing file writers can manage the writer list.")
			return
		}
	} else if action == "remove" {
		send("No file writers configured yet. Use !addwriter to add the first one.")
		return
	}

	// Resolve target user: prefer reply-to, fall back to @mention.
	var targetUser *discordgo.User
	if m.ReferencedMessage != nil && m.ReferencedMessage.Author != nil {
		targetUser = m.ReferencedMessage.Author
	} else if len(m.Mentions) > 0 {
		// Pick first non-bot mention that isn't the bot itself.
		for _, u := range m.Mentions {
			if u.ID != c.botUserID && !u.Bot {
				targetUser = u
				break
			}
		}
	}

	if targetUser == nil {
		verb := "add"
		if action == "remove" {
			verb = "remove"
		}
		send(fmt.Sprintf("To %s a writer: reply to their message with `!%swriter`, or mention them: `!%swriter @user`.", verb, verb, verb))
		return
	}

	targetID := targetUser.ID
	targetName := targetUser.Username
	if targetUser.GlobalName != "" {
		targetName = targetUser.GlobalName
	}

	switch action {
	case "add":
		meta, _ := json.Marshal(map[string]string{"displayName": targetName, "username": targetUser.Username})
		if err := c.configPermStore.Grant(ctx, &store.ConfigPermission{
			AgentID:    agentID,
			Scope:      scope,
			ConfigType: store.ConfigTypeFileWriter,
			UserID:     targetID,
			Permission: "allow",
			Metadata:   meta,
		}); err != nil {
			slog.Warn("discord add writer failed", "error", err, "target", targetID)
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
		// Revoke guild-wide permission.
		if err := c.configPermStore.Revoke(ctx, agentID, scope, store.ConfigTypeFileWriter, targetID); err != nil {
			slog.Warn("discord remove writer failed", "error", err, "target", targetID)
			send("Failed to remove writer. Please try again.")
			return
		}
		// Also revoke per-user scoped permission if it exists (guild:{guildID}:user:{userID}).
		perUserScope := fmt.Sprintf("guild:%s:user:%s", m.GuildID, targetID)
		_ = c.configPermStore.Revoke(ctx, agentID, perUserScope, store.ConfigTypeFileWriter, targetID)
		send(fmt.Sprintf("Removed %s from file writers.", targetName))
	}
}

// handleListWriters handles the !writers command.
func (c *Channel) handleListWriters(m *discordgo.MessageCreate) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	channelID := m.ChannelID

	send := func(text string) {
		c.session.ChannelMessageSend(channelID, text)
	}

	if m.GuildID == "" {
		send("This command only works in server channels.")
		return
	}

	if c.configPermStore == nil || c.agentStore == nil {
		send("File writer management is not available.")
		return
	}

	agentID, err := c.resolveAgentUUID(ctx)
	if err != nil {
		slog.Debug("discord list writers: agent resolve failed", "error", err)
		send("File writer management is not available (no agent).")
		return
	}

	// Guild-wide wildcard scope: matches any user context (guild:{guildID}:user:{userID})
	// via matchWildcard in CheckPermission.
	scope := fmt.Sprintf("guild:%s:*", m.GuildID)

	writers, err := c.configPermStore.List(ctx, agentID, store.ConfigTypeFileWriter, scope)
	if err != nil {
		slog.Warn("discord list writers failed", "error", err)
		send("Failed to list writers. Please try again.")
		return
	}

	if len(writers) == 0 {
		send("No file writers configured for this server. Use !addwriter to add one.")
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("File writers for this server (%d):\n", len(writers)))
	for i, w := range writers {
		sb.WriteString(fmt.Sprintf("%d. %s (<@%s>)\n", i+1, channels.WriterLabel(w.Metadata, w.UserID), w.UserID))
	}
	send(sb.String())
}
