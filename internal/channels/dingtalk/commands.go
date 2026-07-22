package dingtalk

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// Command intents, resolved from the raw text below.
type commandIntent int

const (
	cmdNone commandIntent = iota
	cmdNewSession
	cmdStop
	cmdStopAll
)

// newSessionCommands start a fresh conversation, discarding history and the
// accumulated compaction summary.
//
// The aliases are the upstream connector's list (src/utils/session.ts), kept so
// a user migrating from OpenClaw types what they already know. Auto-compaction
// bounds a long session's token count but never forgets it; this is the only way
// to drop the context deliberately.
var newSessionCommands = map[string]bool{
	"/new":   true,
	"/reset": true,
	"/clear": true,
	"新会话":    true,
	"重新开始":   true,
	"清空对话":   true,
}

// parseCommand classifies a message. The slash commands match feishu/telegram/
// whatsapp; the Chinese aliases are DingTalk/OpenClaw-specific.
func parseCommand(text string) commandIntent {
	t := strings.TrimSpace(text)
	if newSessionCommands[t] {
		return cmdNewSession
	}
	switch strings.ToLower(strings.SplitN(t, " ", 2)[0]) {
	case "/stop":
		return cmdStop
	case "/stopall":
		return cmdStopAll
	}
	return cmdNone
}

// handleCommand intercepts slash commands. Returns true when the message was a
// command and must not proceed to the agent.
//
// None of these reset or stop anything directly. They publish an inbound message
// carrying Metadata[tools.MetaCommand], which the shared consumer
// (cmd/gateway_consumer_handlers.go) turns into a reset or a run cancellation —
// rebuilding the session key exactly as a normal turn would — then `continue`s,
// so no agent run happens. Telegram and WhatsApp do the same; only the parsing
// is per-channel.
func (c *Channel) handleCommand(ctx context.Context, in *inbound, chatID string) bool {
	intent := parseCommand(in.Text)
	if intent == cmdNone {
		return false
	}

	// /new on a group wipes a session shared by everyone in it, so it is gated on
	// the file-writer permission. /stop and /stopall only cancel the sender's own
	// in-flight run and need no gate.
	if intent == cmdNewSession && in.IsGroup && !c.mayResetGroup(ctx, in, chatID) {
		c.replyCommand(ctx, in, "只有群文件写入者可以清空群会话历史。")
		return true
	}

	peerKind := "direct"
	if in.IsGroup {
		peerKind = "group"
	}

	var command, content, reply string
	switch intent {
	case cmdNewSession:
		command, content, reply = "reset", "/reset", "会话已重置，我们从头开始。"
	case cmdStop:
		command, content, reply = "stop", "/stop", "已停止当前任务。"
	case cmdStopAll:
		command, content, reply = "stopall", "/stopall", "已停止所有任务。"
	}

	metadata := c.buildMetadata(in)
	metadata[tools.MetaCommand] = command

	c.Bus().PublishInbound(bus.InboundMessage{
		Channel:  c.Name(),
		SenderID: in.SenderID,
		ChatID:   chatID,
		Content:  content,
		PeerKind: peerKind,
		AgentID:  c.AgentID(),
		UserID:   in.SenderID,
		TenantID: c.TenantID(),
		Metadata: metadata,
	})

	slog.Info("dingtalk command", "channel", c.Name(), "command", command,
		"chat_id", chatID, "sender_id", in.SenderID)
	c.replyCommand(ctx, in, reply)
	return true
}

// mayResetGroup reports whether the sender may wipe a group's shared history.
//
// Gated on the file-writer permission, like Telegram — but fail-closed where
// Telegram is fail-open. A destructive operation on state shared by the whole
// group must not default to permitted because the thing that decides permission
// is unavailable or erroring. A nil permission store (the operator cannot express
// who may reset) likewise denies. DMs never reach this path.
func (c *Channel) mayResetGroup(ctx context.Context, in *inbound, chatID string) bool {
	if c.configPermStore == nil {
		slog.Debug("dingtalk group reset denied; no config permission store",
			"channel", c.Name(), "chat_id", chatID)
		return false
	}

	agentID, err := c.resolveAgentUUID(ctx)
	if err != nil {
		slog.Warn("security.reset_agent_resolve_failed, denying (fail-closed)",
			"channel", c.Name(), "chat_id", chatID, "error", err)
		return false
	}

	groupID := fmt.Sprintf("group:%s:%s", c.Name(), chatID)
	isWriter, err := c.configPermStore.CheckPermission(ctx, agentID, groupID, store.ConfigTypeFileWriter, in.SenderID)
	if err != nil {
		slog.Warn("security.reset_writer_check_failed, denying (fail-closed)",
			"channel", c.Name(), "chat_id", chatID, "sender_id", in.SenderID, "error", err)
		return false
	}
	return isWriter
}

// resolveAgentUUID turns the channel's agent key into the UUID the permission
// store indexes on. The loader stores an agent_key here, not a UUID.
func (c *Channel) resolveAgentUUID(ctx context.Context) (uuid.UUID, error) {
	key := c.AgentID()
	if key == "" {
		return uuid.Nil, fmt.Errorf("no agent key configured")
	}
	if id, err := uuid.Parse(key); err == nil {
		return id, nil
	}
	if c.agentStore == nil {
		return uuid.Nil, fmt.Errorf("no agent store configured")
	}

	ctx = store.WithTenantID(ctx, c.TenantID())
	ag, err := c.agentStore.GetByKey(ctx, key)
	if err != nil {
		return uuid.Nil, fmt.Errorf("agent %q not found: %w", key, err)
	}
	return ag.ID, nil
}

// replyCommand acknowledges a command inline via the session webhook. Best-
// effort: a command that ran but could not be acknowledged beats a failed run.
func (c *Channel) replyCommand(ctx context.Context, in *inbound, text string) {
	if err := c.replyWebhookText(ctx, in.SessionWebhook, text); err != nil {
		slog.Warn("dingtalk command reply failed",
			"channel", c.Name(), "chat_id", in.ConversationID, "error", err)
	}
}
