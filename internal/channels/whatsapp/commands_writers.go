package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// handleWriterCommand handles /addwriter and /removewriter commands.
// Target user is identified by replying to their message or @mentioning them.
func (c *Channel) handleWriterCommand(ctx context.Context, evt *events.Message, senderID, chatID string, chatJID types.JID, action string) {
	send := func(text string) {
		c.sendText(chatJID, text)
	}

	if c.configPermStore == nil {
		send("File writer management is not available.")
		return
	}

	if c.agentUUID == "" {
		send("File writer management is not available (no agent).")
		return
	}

	agentID, err := uuid.Parse(c.agentUUID)
	if err != nil {
		slog.Debug("writer command: invalid agent UUID", "error", err)
		send("File writer management is not available (no agent).")
		return
	}

	groupID := fmt.Sprintf("group:%s:%s", c.Name(), chatID)

	existingWriters, _ := c.configPermStore.ListFileWriters(ctx, agentID, groupID)

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
		send("No file writers configured yet. Use /addwriter to add the first one.")
		return
	}

	// Extract target user from reply-to message or @mention.
	targetID, targetName := resolveWriterTarget(evt)
	if targetID == "" {
		verb := "add"
		if action == "remove" {
			verb = "remove"
		}
		send(fmt.Sprintf("To %s a writer: reply to a message from that person with /%swriter, or @mention them.", verb, verb))
		return
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
			slog.Warn("add writer failed", "error", err, "target", targetID)
			send("Failed to add writer. Please try again.")
			return
		}
		label := targetName
		if label == "" {
			label = targetID
		}
		send(fmt.Sprintf("Added %s as a file writer.", label))

	case "remove":
		if len(existingWriters) <= 1 {
			send("Cannot remove the last file writer.")
			return
		}
		if err := c.configPermStore.Revoke(ctx, agentID, groupID, store.ConfigTypeFileWriter, targetID); err != nil {
			slog.Warn("remove writer failed", "error", err, "target", targetID)
			send("Failed to remove writer. Please try again.")
			return
		}
		label := targetName
		if label == "" {
			label = targetID
		}
		send(fmt.Sprintf("Removed %s from file writers.", label))
	}
}

// handleListWriters handles the /writers command.
func (c *Channel) handleListWriters(ctx context.Context, chatID string, chatJID types.JID) {
	send := func(text string) {
		c.sendText(chatJID, text)
	}

	if c.configPermStore == nil {
		send("File writer management is not available.")
		return
	}

	if c.agentUUID == "" {
		send("File writer management is not available (no agent).")
		return
	}

	agentID, err := uuid.Parse(c.agentUUID)
	if err != nil {
		slog.Debug("list writers: invalid agent UUID", "error", err)
		send("File writer management is not available (no agent).")
		return
	}

	groupID := fmt.Sprintf("group:%s:%s", c.Name(), chatID)

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
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("File writers for this group (%d):\n", len(writers)))
	for i, w := range writers {
		var meta fwMeta
		_ = json.Unmarshal(w.Metadata, &meta)
		label := w.UserID
		if meta.DisplayName != "" {
			label = meta.DisplayName
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, label))
	}
	send(sb.String())
}

// resolveWriterTarget extracts the target user JID and display name from a
// reply-to message or @mention in the given event.
func resolveWriterTarget(evt *events.Message) (jid string, displayName string) {
	if evt == nil || evt.Message == nil {
		return "", ""
	}

	// Try reply-to message first (ContextInfo.Participant = quoted message sender JID).
	if ext := evt.Message.GetExtendedTextMessage(); ext != nil {
		if ci := ext.GetContextInfo(); ci != nil {
			if participant := ci.GetParticipant(); participant != "" {
				return participant, phoneLabel(participant)
			}
		}
	}

	// Fallback: @mention.
	if ext := evt.Message.GetExtendedTextMessage(); ext != nil {
		if ci := ext.GetContextInfo(); ci != nil {
			for _, jidStr := range ci.GetMentionedJID() {
				if jidStr != "" {
					return jidStr, phoneLabel(jidStr)
				}
			}
		}
	}

	return "", ""
}

// phoneLabel extracts the phone number portion from a WhatsApp JID for display.
func phoneLabel(jid string) string {
	if at := strings.IndexByte(jid, '@'); at > 0 {
		return "+" + jid[:at]
	}
	return jid
}
