package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ============================================================
// sessions_history
// ============================================================

const (
	historyMaxCharsPerMessage = 4000
	historyMaxTotalBytes      = 80 * 1024
)

type SessionsHistoryTool struct {
	sessions store.SessionStore
}

func NewSessionsHistoryTool() *SessionsHistoryTool { return &SessionsHistoryTool{} }

func (t *SessionsHistoryTool) SetSessionStore(s store.SessionStore) { t.sessions = s }

func (t *SessionsHistoryTool) Name() string { return "sessions_history" }
func (t *SessionsHistoryTool) Description() string {
	return "Fetch message history for a session."
}

func (t *SessionsHistoryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"session_key": map[string]any{
				"type":        "string",
				"description": "Session key to fetch history from",
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "Max messages to return (default 20)",
			},
			"include_tools": map[string]any{
				"type":        "boolean",
				"description": "Include tool call/result messages (default false)",
			},
		},
		"required": []string{"session_key"},
	}
}

func (t *SessionsHistoryTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.sessions == nil {
		return ErrorResult("session store not available")
	}

	sessionKey, _ := args["session_key"].(string)
	if sessionKey == "" {
		return ErrorResult("session_key is required")
	}

	limit := 20
	if v, ok := args["limit"].(float64); ok && int(v) > 0 {
		limit = int(v)
	}

	includeTools, _ := args["include_tools"].(bool)

	// Security: validate session belongs to current agent (fail-closed).
	// Session keys use agent_key (e.g. "agent:victoria:..."), not UUID.
	agentKey := ToolAgentKeyFromCtx(ctx)
	if agentKey == "" {
		return ErrorResult("agent context required")
	}
	if !strings.HasPrefix(sessionKey, "agent:"+agentKey+":") {
		return ErrorResult("access denied: session belongs to a different agent")
	}

	history := t.sessions.GetHistory(ctx, sessionKey)
	if history == nil {
		out, _ := json.Marshal(map[string]any{
			"session_key": sessionKey,
			"messages":    []any{},
			"count":       0,
		})
		return SilentResult(string(out))
	}

	// Filter tool messages
	type msgEntry struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	var entries []msgEntry
	for _, m := range history {
		if !includeTools && m.Role == "tool" {
			continue
		}
		// Skip assistant messages that are only tool calls with no text
		if !includeTools && m.Role == "assistant" && len(m.ToolCalls) > 0 && strings.TrimSpace(m.Content) == "" {
			continue
		}

		content := m.Content
		// Truncate per-message
		if utf8.RuneCountInString(content) > historyMaxCharsPerMessage {
			runes := []rune(content)
			content = string(runes[:historyMaxCharsPerMessage]) + "... [truncated]"
		}

		entries = append(entries, msgEntry{Role: m.Role, Content: content})
	}

	// Keep last N
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	out, _ := json.Marshal(map[string]any{
		"session_key": sessionKey,
		"messages":    entries,
		"count":       len(entries),
	})

	// Cap total bytes
	if len(out) > historyMaxTotalBytes {
		errOut, _ := json.Marshal(map[string]any{
			"session_key": sessionKey,
			"error":       fmt.Sprintf("history too large (%d bytes), use smaller limit", len(out)),
			"count":       len(entries),
		})
		return SilentResult(string(errOut))
	}

	return SilentResult(string(out))
}
