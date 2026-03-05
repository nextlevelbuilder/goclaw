package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TeamMessageTool exposes the team mailbox to agents.
// Actions: send, broadcast, read.
type TeamMessageTool struct {
	manager *TeamToolManager
}

func NewTeamMessageTool(manager *TeamToolManager) *TeamMessageTool {
	return &TeamMessageTool{manager: manager}
}

func (t *TeamMessageTool) Name() string { return "team_message" }

func (t *TeamMessageTool) Description() string {
	return "Send and receive messages within your team. Actions: send (direct message to a teammate), broadcast (message all teammates), read (check unread messages). See TEAM.md for your teammates."
}

func (t *TeamMessageTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "'send', 'broadcast', or 'read'",
			},
			"to": map[string]interface{}{
				"type":        "string",
				"description": "Target agent key (required for action=send)",
			},
			"text": map[string]interface{}{
				"type":        "string",
				"description": "Message content (required for action=send and action=broadcast)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *TeamMessageTool) Execute(ctx context.Context, args map[string]interface{}) *Result {
	action, _ := args["action"].(string)

	switch action {
	case "send":
		return t.executeSend(ctx, args)
	case "broadcast":
		return t.executeBroadcast(ctx, args)
	case "read":
		return t.executeRead(ctx)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %s (use send, broadcast, or read)", action))
	}
}

func (t *TeamMessageTool) executeSend(ctx context.Context, args map[string]interface{}) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	toKey, _ := args["to"].(string)
	if toKey == "" {
		return ErrorResult("to parameter is required for send action")
	}
	text, _ := args["text"].(string)
	if text == "" {
		return ErrorResult("text parameter is required for send action")
	}

	toAgentID, err := t.manager.resolveAgentByKey(toKey)
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Validate recipient is in the same team (prevent cross-team messaging).
	members, err := t.manager.teamStore.ListMembers(ctx, team.ID)
	if err != nil {
		return ErrorResult("failed to verify team membership: " + err.Error())
	}
	isMember := false
	for _, m := range members {
		if m.AgentID == toAgentID {
			isMember = true
			break
		}
	}
	if !isMember {
		return ErrorResult(fmt.Sprintf("agent %q is not a member of your team", toKey))
	}

	// Persist to DB
	msg := &store.TeamMessageData{
		TeamID:      team.ID,
		FromAgentID: agentID,
		ToAgentID:   &toAgentID,
		Content:     text,
		MessageType: store.TeamMessageTypeChat,
	}
	if err := t.manager.teamStore.SendMessage(ctx, msg); err != nil {
		return ErrorResult("failed to send message: " + err.Error())
	}

	// Real-time delivery via message bus
	fromKey := t.manager.agentKeyFromID(ctx, agentID)
	t.publishTeammateMessage(fromKey, toKey, text, ctx)

	preview := text
	if len(preview) > 100 {
		preview = preview[:100] + "..."
	}
	t.manager.broadcastTeamEvent(protocol.EventTeamMessageSent, protocol.TeamMessageEventPayload{
		TeamID:          team.ID.String(),
		FromAgentKey:    fromKey,
		FromDisplayName: t.manager.agentDisplayName(ctx, fromKey),
		ToAgentKey:      toKey,
		ToDisplayName:   t.manager.agentDisplayName(ctx, toKey),
		MessageType:     string(store.TeamMessageTypeChat),
		Preview:         preview,
		UserID:          store.UserIDFromContext(ctx),
		Channel:         ToolChannelFromCtx(ctx),
		ChatID:          ToolChatIDFromCtx(ctx),
	})

	return NewResult(fmt.Sprintf("Message sent to %s.", toKey))
}

func (t *TeamMessageTool) executeBroadcast(ctx context.Context, args map[string]interface{}) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	text, _ := args["text"].(string)
	if text == "" {
		return ErrorResult("text parameter is required for broadcast action")
	}

	// Persist to DB (to_agent_id = NULL means broadcast)
	msg := &store.TeamMessageData{
		TeamID:      team.ID,
		FromAgentID: agentID,
		ToAgentID:   nil,
		Content:     text,
		MessageType: store.TeamMessageTypeBroadcast,
	}
	if err := t.manager.teamStore.SendMessage(ctx, msg); err != nil {
		return ErrorResult("failed to broadcast message: " + err.Error())
	}

	// Real-time delivery to all teammates via message bus
	fromKey := t.manager.agentKeyFromID(ctx, agentID)
	members, err := t.manager.teamStore.ListMembers(ctx, team.ID)
	if err == nil {
		for _, m := range members {
			if m.AgentID == agentID {
				continue // don't send to self
			}
			t.publishTeammateMessage(fromKey, m.AgentKey, text, ctx)
		}
	}

	preview := text
	if len(preview) > 100 {
		preview = preview[:100] + "..."
	}
	t.manager.broadcastTeamEvent(protocol.EventTeamMessageSent, protocol.TeamMessageEventPayload{
		TeamID:          team.ID.String(),
		FromAgentKey:    fromKey,
		FromDisplayName: t.manager.agentDisplayName(ctx, fromKey),
		ToAgentKey:      "broadcast",
		MessageType:     string(store.TeamMessageTypeBroadcast),
		Preview:         preview,
		UserID:          store.UserIDFromContext(ctx),
		Channel:         ToolChannelFromCtx(ctx),
		ChatID:          ToolChatIDFromCtx(ctx),
	})

	return NewResult(fmt.Sprintf("Broadcast sent to all teammates."))
}

func (t *TeamMessageTool) executeRead(ctx context.Context) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	messages, err := t.manager.teamStore.GetUnread(ctx, team.ID, agentID)
	if err != nil {
		return ErrorResult("failed to get unread messages: " + err.Error())
	}

	// Mark all as read
	for _, msg := range messages {
		_ = t.manager.teamStore.MarkRead(ctx, msg.ID)
	}

	out, _ := json.Marshal(map[string]interface{}{
		"messages": messages,
		"count":    len(messages),
	})
	return SilentResult(string(out))
}

// publishTeammateMessage sends a real-time notification via the message bus.
// Uses "teammate:{fromKey}" sender prefix so the consumer can route it.
func (t *TeamMessageTool) publishTeammateMessage(fromKey, toKey, text string, ctx context.Context) {
	if t.manager.msgBus == nil {
		return
	}

	userID := store.UserIDFromContext(ctx)
	chatID := ToolChatIDFromCtx(ctx)
	originChannel := ToolChannelFromCtx(ctx)
	originPeerKind := ToolPeerKindFromCtx(ctx)

	teamMeta := map[string]string{
		"origin_channel":   originChannel,
		"origin_peer_kind": originPeerKind,
		"from_agent":       fromKey,
		"to_agent":         toKey,
	}
	if localKey := ToolLocalKeyFromCtx(ctx); localKey != "" {
		teamMeta["origin_local_key"] = localKey
	}
	t.manager.msgBus.PublishInbound(bus.InboundMessage{
		Channel:  "system",
		SenderID: fmt.Sprintf("teammate:%s", fromKey),
		ChatID:   chatID,
		Content:  fmt.Sprintf("[Team message from %s]: %s", fromKey, text),
		UserID:   userID,
		AgentID:  toKey,
		Metadata: teamMeta,
	})
}
