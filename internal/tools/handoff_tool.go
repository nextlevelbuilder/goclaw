package tools

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// HandoffTool enables agent-to-agent conversation transfer.
// When called, it sets a routing override so future messages from the user
// are handled by the target agent instead of the current one.
type HandoffTool struct {
	delegateMgr  *DelegateManager
	teamStore    store.TeamStore
	sessionStore store.SessionStore
	msgBus       *bus.MessageBus
}

func NewHandoffTool(
	delegateMgr *DelegateManager,
	teamStore store.TeamStore,
	sessionStore store.SessionStore,
	msgBus *bus.MessageBus,
) *HandoffTool {
	return &HandoffTool{
		delegateMgr:  delegateMgr,
		teamStore:    teamStore,
		sessionStore: sessionStore,
		msgBus:       msgBus,
	}
}

func (t *HandoffTool) Name() string { return "handoff" }

func (t *HandoffTool) Description() string {
	return "Transfer conversation control to another agent. " +
		"The target agent becomes the active handler for this user/chat. " +
		"Use action=transfer to hand off, action=clear to remove a previous handoff."
}

func (t *HandoffTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "'transfer' (default) or 'clear'",
			},
			"agent": map[string]any{
				"type":        "string",
				"description": "Target agent key (required for action=transfer)",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Why the handoff is happening (required for action=transfer)",
			},
			"transfer_context": map[string]any{
				"type":        "boolean",
				"description": "Pass conversation summary to target agent (default true)",
			},
		},
		"required": []string{"agent"},
	}
}

func (t *HandoffTool) Execute(ctx context.Context, args map[string]any) *Result {
	action, _ := args["action"].(string)
	if action == "" {
		action = "transfer"
	}

	switch action {
	case "transfer":
		return t.executeTransfer(ctx, args)
	case "clear":
		return t.executeClear(ctx)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %s (use 'transfer' or 'clear')", action))
	}
}

func (t *HandoffTool) executeTransfer(ctx context.Context, args map[string]any) *Result {
	targetAgentKey, _ := args["agent"].(string)
	if targetAgentKey == "" {
		return ErrorResult("agent is required for handoff")
	}

	reason, _ := args["reason"].(string)
	if reason == "" {
		return ErrorResult("reason is required for handoff")
	}

	transferContext := true
	if v, ok := args["transfer_context"].(bool); ok {
		transferContext = v
	}

	// Get current agent and channel context
	sourceAgentID := store.AgentIDFromContext(ctx)
	if sourceAgentID == uuid.Nil {
		return ErrorResult("handoff requires database stores")
	}

	sourceAgent, err := t.delegateMgr.agentStore.GetByID(ctx, sourceAgentID)
	if err != nil {
		return ErrorResult("failed to resolve source agent")
	}

	// Verify target agent exists
	targetAgent, err := t.delegateMgr.agentStore.GetByKey(ctx, targetAgentKey)
	if err != nil {
		return ErrorResult(fmt.Sprintf("target agent %q not found", targetAgentKey))
	}

	// Permission check: reuse agent link verification
	link, err := t.delegateMgr.linkStore.GetLinkBetween(ctx, sourceAgentID, targetAgent.ID)
	if err != nil || link == nil {
		return ErrorResult(fmt.Sprintf("no link from this agent to %q — create an agent link first", targetAgentKey))
	}

	channel := ToolChannelFromCtx(ctx)
	chatID := ToolChatIDFromCtx(ctx)
	peerKind := ToolPeerKindFromCtx(ctx)
	userID := store.UserIDFromContext(ctx)

	if channel == "" || chatID == "" {
		return ErrorResult("handoff requires channel context (not available in this session type)")
	}

	// Get conversation context to transfer
	var sessionContext string
	if transferContext && t.sessionStore != nil {
		sessionKey := ToolSandboxKeyFromCtx(ctx) // sandbox key IS the session key
		if sessionKey != "" {
			sessionContext = t.sessionStore.GetSummary(sessionKey)
		}
	}

	// Set routing override
	if t.teamStore != nil {
		route := &store.HandoffRouteData{
			Channel:      channel,
			ChatID:       chatID,
			FromAgentKey: sourceAgent.AgentKey,
			ToAgentKey:   targetAgentKey,
			Reason:       reason,
			CreatedBy:    userID,
		}
		if err := t.teamStore.SetHandoffRoute(ctx, route); err != nil {
			slog.Warn("handoff: failed to set route", "error", err)
			return ErrorResult("failed to set handoff route: " + err.Error())
		}
	}

	// Broadcast handoff event (WS clients can react to switch UI)
	if t.msgBus != nil {
		t.msgBus.Broadcast(bus.Event{
			Name: protocol.EventHandoff,
			Payload: map[string]string{
				"from_agent": sourceAgent.AgentKey,
				"to_agent":   targetAgentKey,
				"reason":     reason,
				"channel":    channel,
				"chat_id":    chatID,
			},
		})
	}

	// Publish initial message to target agent via message bus
	handoffID := uuid.NewString()[:12]
	if t.msgBus != nil {
		content := fmt.Sprintf(
			"[Handoff from %s]\nReason: %s",
			sourceAgent.AgentKey, reason)
		if sessionContext != "" {
			content += fmt.Sprintf("\n\nConversation context:\n%s", sessionContext)
		}
		content += "\n\nPlease greet the user and continue the conversation."

		handoffMeta := map[string]string{
			"origin_channel":   channel,
			"origin_peer_kind": peerKind,
			"handoff_id":       handoffID,
			"from_agent":       sourceAgent.AgentKey,
		}
		if localKey := ToolLocalKeyFromCtx(ctx); localKey != "" {
			handoffMeta["origin_local_key"] = localKey
		}
		t.msgBus.PublishInbound(bus.InboundMessage{
			Channel:  "system",
			SenderID: fmt.Sprintf("handoff:%s", handoffID),
			ChatID:   chatID,
			Content:  content,
			AgentID:  targetAgentKey,
			UserID:   userID,
			Metadata: handoffMeta,
		})
	}

	slog.Info("handoff: conversation transferred",
		"from", sourceAgent.AgentKey, "to", targetAgentKey,
		"channel", channel, "chat_id", chatID, "reason", reason)

	return NewResult(fmt.Sprintf(
		"Conversation has been transferred to agent %q. "+
			"The user's next messages will be handled by %s. "+
			"You should let the user know they're being transferred.",
		targetAgentKey, targetAgentKey))
}

func (t *HandoffTool) executeClear(ctx context.Context) *Result {
	channel := ToolChannelFromCtx(ctx)
	chatID := ToolChatIDFromCtx(ctx)

	if channel == "" || chatID == "" {
		return ErrorResult("clear requires channel context")
	}

	if t.teamStore != nil {
		if err := t.teamStore.ClearHandoffRoute(ctx, channel, chatID); err != nil {
			return ErrorResult("failed to clear handoff route: " + err.Error())
		}
	}

	return NewResult("Handoff route cleared. Messages will route to the default agent for this chat.")
}
