package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ============================================================
// sessions_send
// ============================================================

type SessionsSendTool struct {
	sessions store.SessionStore
	msgBus   *bus.MessageBus
}

func NewSessionsSendTool() *SessionsSendTool { return &SessionsSendTool{} }

func (t *SessionsSendTool) SetSessionStore(s store.SessionStore) { t.sessions = s }
func (t *SessionsSendTool) SetMessageBus(b *bus.MessageBus)      { t.msgBus = b }

func (t *SessionsSendTool) Name() string { return "sessions_send" }
func (t *SessionsSendTool) Description() string {
	return "Send a message into another session. Use session_key or label to identify the target."
}

func (t *SessionsSendTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_key": map[string]any{
				"type":        "string",
				"description": "Target session key",
			},
			"label": map[string]any{
				"type":        "string",
				"description": "Target session label (alternative to session_key)",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Message to send",
			},
		},
		"required": []string{"message"},
	}
}

func (t *SessionsSendTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.sessions == nil {
		return ErrorResult("session store not available")
	}
	if t.msgBus == nil {
		return ErrorResult("message bus not available")
	}

	sessionKey, _ := args["session_key"].(string)
	label, _ := args["label"].(string)
	message, _ := args["message"].(string)

	if message == "" {
		return ErrorResult("message is required")
	}
	if sessionKey == "" && label == "" {
		return ErrorResult("either session_key or label is required")
	}

	// Security: fail-closed when agent key missing.
	// Session keys use agent_key (e.g. "agent:victoria:..."), not UUID.
	agentKey := ToolAgentKeyFromCtx(ctx)
	if agentKey == "" {
		return ErrorResult("agent context required")
	}

	// Resolve by label if needed
	if sessionKey == "" && label != "" {
		sessions := t.sessions.List(ctx, agentKey)
		for _, s := range sessions {
			if s.Label == label {
				sessionKey = s.Key
				break
			}
		}
		if sessionKey == "" {
			return ErrorResult(fmt.Sprintf("no session found with label: %s", label))
		}
	}

	// Security: validate target session belongs to same agent
	if !strings.HasPrefix(sessionKey, "agent:"+agentKey+":") {
		return ErrorResult("access denied: target session belongs to a different agent")
	}

	// Scope check: group-scoped users cannot send to other groups' sessions.
	currentSession := ToolSandboxKeyFromCtx(ctx)
	if !isSessionInScope(ctx, sessionKey, currentSession) {
		return ErrorResult("access denied: cannot send to session outside current scope")
	}

	// Block self-send: agent should not send to its own current session
	// to prevent re-processing loops (same pattern as message tool).
	if currentSession != "" && sessionKey == currentSession {
		return ErrorResult("cannot send to your own current session — your response is already delivered to it")
	}

	// Publish as an inbound message (same mechanism as channels)
	t.msgBus.PublishInbound(bus.InboundMessage{
		Channel:  "system",
		SenderID: "session_send_tool",
		ChatID:   sessionKey,
		Content:  message,
		PeerKind: "direct",
		TenantID: store.TenantIDFromContext(ctx),
	})

	out, _ := json.Marshal(map[string]any{
		"status":      "accepted",
		"session_key": sessionKey,
	})
	return SilentResult(string(out))
}

// ============================================================
// helpers
// ============================================================

func resolveAgentIDString(ctx context.Context) string {
	id := store.AgentIDFromContext(ctx)
	if id.String() == "00000000-0000-0000-0000-000000000000" {
		return "" // no agent ID in context
	}
	return id.String()
}
