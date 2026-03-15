package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
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
	return "Send and receive messages within your team. Actions: send, broadcast, read. See TEAM.md for your teammates."
}

func (t *TeamMessageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "'send', 'broadcast', or 'read'",
			},
			"to": map[string]any{
				"type":        "string",
				"description": "Target agent key (required for action=send)",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Message content (required for action=send and action=broadcast)",
			},
			"media": map[string]any{
				"type":        "array",
				"description": "Optional file paths to attach as media (for action=send)",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
		"required": []string{"action"},
	}
}

func (t *TeamMessageTool) Execute(ctx context.Context, args map[string]any) *Result {
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

func (t *TeamMessageTool) executeSend(ctx context.Context, args map[string]any) *Result {
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
	members, err := t.manager.cachedListMembers(ctx, team.ID, agentID)
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

	// Parse optional media paths
	var mediaFiles []bus.MediaFile
	if rawMedia, ok := args["media"]; ok {
		if mediaArr, ok := rawMedia.([]any); ok {
			for _, item := range mediaArr {
				if path, ok := item.(string); ok && path != "" {
					mediaFiles = append(mediaFiles, bus.MediaFile{Path: path})
				}
			}
		}
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

	// Auto-create a "message" task so team messages appear in the Tasks tab.
	subject := text
	if len(subject) > 100 {
		subject = subject[:100] + "..."
	}
	now := time.Now()
	lockExpires := now.Add(30 * time.Minute)
	taskData := &store.TeamTaskData{
		TeamID:           team.ID,
		Subject:          subject,
		Description:      text,
		Status:           store.TeamTaskStatusInProgress,
		TaskType:         "message",
		CreatedByAgentID: &agentID,
		OwnerAgentID:     &toAgentID,
		UserID:           store.UserIDFromContext(ctx),
		Channel:          ToolChannelFromCtx(ctx),
		ChatID:           ToolChatIDFromCtx(ctx),
		LockedAt:         &now,
		LockExpiresAt:    &lockExpires,
	}
	var teamTaskID uuid.UUID
	slog.Info("team_message: creating auto-task",
		"team_id", team.ID,
		"from_agent", agentID,
		"to_agent", toAgentID,
		"status", taskData.Status,
		"task_type", taskData.TaskType,
		"user_id", taskData.UserID,
		"channel", taskData.Channel,
		"chat_id", taskData.ChatID,
	)
	if err := t.manager.teamStore.CreateTask(ctx, taskData); err != nil {
		slog.Warn("team_message: failed to auto-create task",
			"error", err,
			"team_id", team.ID,
			"from_agent", agentID,
			"to_agent", toAgentID,
		)
	} else {
		teamTaskID = taskData.ID
		t.manager.broadcastTeamEvent(protocol.EventTeamTaskCreated, protocol.TeamTaskEventPayload{
			TeamID:           team.ID.String(),
			TaskID:           teamTaskID.String(),
			Subject:          subject,
			Status:           store.TeamTaskStatusInProgress,
			OwnerAgentKey:    toKey,
			OwnerDisplayName: t.manager.agentDisplayName(ctx, toKey),
			UserID:           store.UserIDFromContext(ctx),
			Channel:          ToolChannelFromCtx(ctx),
			ChatID:           ToolChatIDFromCtx(ctx),
			Timestamp:        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
			ActorType:        "agent",
			ActorID:          t.manager.agentKeyFromID(ctx, agentID),
		})
	}

	// Real-time delivery via message bus
	fromKey := t.manager.agentKeyFromID(ctx, agentID)
	t.publishTeammateMessage(fromKey, toKey, text, mediaFiles, teamTaskID, team.ID, ctx)

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

func (t *TeamMessageTool) executeBroadcast(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if err := t.manager.requireLead(ctx, team, agentID); err != nil {
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
	members, err := t.manager.cachedListMembers(ctx, team.ID, agentID)
	if err == nil {
		for _, m := range members {
			if m.AgentID == agentID {
				continue // don't send to self
			}
			t.publishTeammateMessage(fromKey, m.AgentKey, text, nil, uuid.Nil, team.ID, ctx)
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

	resp := map[string]any{
		"messages": messages,
		"count":    len(messages),
	}
	if len(messages) >= 50 {
		resp["note"] = "Showing latest 50 unread messages. Read again after processing these to get more."
		resp["has_more"] = true
	}
	out, _ := json.Marshal(resp)
	return SilentResult(string(out))
}

// publishTeammateMessage sends a real-time notification via the message bus.
// Uses "teammate:{fromKey}" sender prefix so the consumer can route it.
func (t *TeamMessageTool) publishTeammateMessage(fromKey, toKey, text string, media []bus.MediaFile, teamTaskID uuid.UUID, teamID uuid.UUID, ctx context.Context) {
	if t.manager.msgBus == nil {
		return
	}

	userID := store.UserIDFromContext(ctx)
	chatID := ToolChatIDFromCtx(ctx)
	originChannel := ToolChannelFromCtx(ctx)
	originPeerKind := ToolPeerKindFromCtx(ctx)

	slog.Info("team_message: publishTeammateMessage",
		"from", fromKey, "to", toKey,
		"origin_channel", originChannel,
		"chat_id", chatID,
		"origin_peer_kind", originPeerKind,
		"user_id", userID,
		"team_task_id", teamTaskID,
	)

	teamMeta := map[string]string{
		"origin_channel":   originChannel,
		"origin_peer_kind": originPeerKind,
		"origin_chat_id":   chatID,
		"origin_user_id":   userID,
		"from_agent":       fromKey,
		"to_agent":         toKey,
		"team_id":          teamID.String(),
	}
	if localKey := ToolLocalKeyFromCtx(ctx); localKey != "" {
		teamMeta["origin_local_key"] = localKey
	}
	if teamTaskID != uuid.Nil {
		teamMeta["team_task_id"] = teamTaskID.String()
	}
	// Pass team workspace so the receiving agent can access shared files.
	if ws, err := workspaceDir(t.manager.dataDir, teamID, "", chatID); err == nil {
		teamMeta["team_workspace"] = ws
	}
	// Propagate trace context so the receiving agent's trace links back.
	if traceID := tracing.TraceIDFromContext(ctx); traceID != uuid.Nil {
		teamMeta["origin_trace_id"] = traceID.String()
	}
	if rootSpanID := tracing.ParentSpanIDFromContext(ctx); rootSpanID != uuid.Nil {
		teamMeta["origin_root_span_id"] = rootSpanID.String()
	}
	t.manager.msgBus.PublishInbound(bus.InboundMessage{
		Channel:  "system",
		SenderID: fmt.Sprintf("teammate:%s", fromKey),
		ChatID:   chatID,
		Content:  fmt.Sprintf("[Team message from %s]: %s", fromKey, text),
		Media:    media,
		UserID:   userID,
		AgentID:  toKey,
		Metadata: teamMeta,
	})
}
