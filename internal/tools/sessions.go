package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ============================================================
// sessions_list
// ============================================================

type SessionsListTool struct {
	sessions store.SessionStore
}

func NewSessionsListTool() *SessionsListTool { return &SessionsListTool{} }

func (t *SessionsListTool) SetSessionStore(s store.SessionStore) { t.sessions = s }

func (t *SessionsListTool) Name() string { return "sessions_list" }
func (t *SessionsListTool) Description() string {
	return "List sessions for this agent with optional filters."
}

func (t *SessionsListTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit": map[string]any{
				"type":        "number",
				"description": "Max sessions to return (default 20)",
			},
			"active_minutes": map[string]any{
				"type":        "number",
				"description": "Only show sessions active in the last N minutes",
			},
		},
	}
}

func (t *SessionsListTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.sessions == nil {
		return ErrorResult("session store not available")
	}

	limit := 20
	if v, ok := args["limit"].(float64); ok && int(v) > 0 {
		limit = int(v)
	}

	var activeMinutes int
	if v, ok := args["active_minutes"].(float64); ok && int(v) > 0 {
		activeMinutes = int(v)
	}

	// Session keys use agent_key (e.g. "agent:victoria:..."), not UUID.
	agentKey := ToolAgentKeyFromCtx(ctx)
	if agentKey == "" {
		return ErrorResult("agent context required")
	}
	sessions := t.sessions.List(ctx, agentKey)

	// Filter by active_minutes
	if activeMinutes > 0 {
		cutoff := time.Now().Add(-time.Duration(activeMinutes) * time.Minute)
		var filtered []store.SessionInfo
		for _, s := range sessions {
			if s.Updated.After(cutoff) {
				filtered = append(filtered, s)
			}
		}
		sessions = filtered
	}

	// Apply limit
	if len(sessions) > limit {
		sessions = sessions[:limit]
	}

	type sessionEntry struct {
		Key          string `json:"key"`
		MessageCount int    `json:"message_count"`
		Updated      string `json:"updated"`
	}

	entries := make([]sessionEntry, 0, len(sessions))
	for _, s := range sessions {
		entries = append(entries, sessionEntry{
			Key:          s.Key,
			MessageCount: s.MessageCount,
			Updated:      s.Updated.Format(time.RFC3339),
		})
	}

	out, _ := json.Marshal(map[string]any{
		"count":    len(entries),
		"sessions": entries,
	})
	return SilentResult(string(out))
}

// ============================================================
// session_status
// ============================================================

type SessionStatusTool struct {
	sessions store.SessionStore
}

func NewSessionStatusTool() *SessionStatusTool { return &SessionStatusTool{} }

func (t *SessionStatusTool) SetSessionStore(s store.SessionStore) { t.sessions = s }

func (t *SessionStatusTool) Name() string { return "session_status" }
func (t *SessionStatusTool) Description() string {
	return "Show session status: model, tokens, compaction count, channel, last update."
}

func (t *SessionStatusTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_key": map[string]any{
				"type":        "string",
				"description": "Session key to inspect (default: current session)",
			},
		},
	}
}

func (t *SessionStatusTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.sessions == nil {
		return ErrorResult("session store not available")
	}

	sessionKey, _ := args["session_key"].(string)
	if sessionKey == "" {
		sessionKey = ToolSandboxKeyFromCtx(ctx) // sandboxKey == sessionKey in registry
	}
	if sessionKey == "" {
		return ErrorResult("session_key is required (could not detect current session)")
	}

	// Security: validate session belongs to current agent (fail-closed).
	// Session keys use agent_key (e.g. "agent:victoria:..."), not UUID.
	agentKey := ToolAgentKeyFromCtx(ctx)
	if agentKey == "" {
		return ErrorResult("agent context required")
	}
	if !strings.HasPrefix(sessionKey, "agent:"+agentKey+":") {
		return ErrorResult("access denied: session belongs to a different agent")
	}

	data := t.sessions.Get(ctx, sessionKey)
	if data == nil {
		return ErrorResult("session not found")
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Session: %s", data.Key))
	if data.Model != "" {
		lines = append(lines, fmt.Sprintf("Model: %s", data.Model))
	}
	if data.Provider != "" {
		lines = append(lines, fmt.Sprintf("Provider: %s", data.Provider))
	}
	if data.Channel != "" {
		lines = append(lines, fmt.Sprintf("Channel: %s", data.Channel))
	}
	lines = append(lines, fmt.Sprintf("Messages: %d", len(data.Messages)))
	lines = append(lines, fmt.Sprintf("Tokens: %d input / %d output", data.InputTokens, data.OutputTokens))
	lines = append(lines, fmt.Sprintf("Compactions: %d", data.CompactionCount))
	if data.Summary != "" {
		lines = append(lines, fmt.Sprintf("Has summary: yes (%d chars)", len(data.Summary)))
	}
	if data.Label != "" {
		lines = append(lines, fmt.Sprintf("Label: %s", data.Label))
	}
	lines = append(lines, fmt.Sprintf("Updated: %s", data.Updated.Format(time.RFC3339)))

	return SilentResult(strings.Join(lines, "\n"))
}
