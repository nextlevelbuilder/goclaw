package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// HeartbeatTool lets agents manage their own heartbeat configuration.
type HeartbeatTool struct {
	hbStore    store.HeartbeatStore
	permStore  store.ConfigPermissionStore
	agentStore store.AgentStore
	wakeFn     func(uuid.UUID) // triggers immediate heartbeat run
}

// NewHeartbeatTool creates a heartbeat management tool.
func NewHeartbeatTool(hb store.HeartbeatStore, perms store.ConfigPermissionStore) *HeartbeatTool {
	return &HeartbeatTool{hbStore: hb, permStore: perms}
}

// SetAgentStore sets the agent store for HEARTBEAT.md read/write.
func (t *HeartbeatTool) SetAgentStore(as store.AgentStore) {
	t.agentStore = as
}

// SetWakeFn sets the function called when the "test" action triggers an immediate run.
func (t *HeartbeatTool) SetWakeFn(fn func(uuid.UUID)) {
	t.wakeFn = fn
}

func (t *HeartbeatTool) Name() string { return "heartbeat" }

func (t *HeartbeatTool) Description() string {
	return `Manage agent heartbeat — periodic proactive check-in.

ACTIONS:
- status: Show heartbeat status (enabled, last/next run, counts)
- get: Get full heartbeat configuration
- set: Create or update heartbeat config (interval, active_hours, ack_max_chars, etc.)
- toggle: Enable or disable heartbeat (enabled: true/false)
- set_checklist: Set HEARTBEAT.md content (the checklist the agent follows each run)
- get_checklist: Read current HEARTBEAT.md content
- test: Trigger an immediate heartbeat run (background)
- logs: View heartbeat run history (limit, offset)

EXAMPLES:
  {"action":"status"}
  {"action":"set","interval":1800,"channel":"telegram","chat_id":"-100123456"}
  {"action":"toggle","enabled":true}
  {"action":"set_checklist","content":"# Heartbeat Checklist\n- Check server status\n- Report any alerts"}
  {"action":"test"}
  {"action":"logs","limit":10}`
}

func (t *HeartbeatTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"status", "get", "set", "toggle", "set_checklist", "get_checklist", "test", "logs"},
				"description": "Action to perform",
			},
			"enabled":          map[string]any{"type": "boolean", "description": "For toggle: enable/disable heartbeat"},
			"interval":         map[string]any{"type": "integer", "description": "Heartbeat interval in seconds (min 300)"},
			"prompt":           map[string]any{"type": "string", "description": "Custom heartbeat prompt (empty = default)"},
			"ack_max_chars":    map[string]any{"type": "integer", "description": "Max chars for suppression threshold (default 300)"},
			"max_retries":      map[string]any{"type": "integer", "description": "Max retry attempts on failure (default 2)"},
			"isolated_session": map[string]any{"type": "boolean", "description": "Use isolated session per run (default true)"},
			"light_context":    map[string]any{"type": "boolean", "description": "Skip loading context files, only inject heartbeat checklist (default false)"},
			"active_hours":     map[string]any{"type": "string", "description": "Active hours range, e.g. '08:00-22:00'"},
			"timezone":         map[string]any{"type": "string", "description": "IANA timezone for active hours, e.g. 'Asia/Ho_Chi_Minh'"},
			"channel":          map[string]any{"type": "string", "description": "Delivery channel name (auto-filled from current context if empty)"},
			"chat_id":          map[string]any{"type": "string", "description": "Delivery target chat ID (auto-filled from current context if empty)"},
			"content":          map[string]any{"type": "string", "description": "For set_checklist: HEARTBEAT.md content (the checklist the agent follows each run)"},
			"limit":            map[string]any{"type": "integer", "description": "For logs: max entries to return"},
			"offset":           map[string]any{"type": "integer", "description": "For logs: pagination offset"},
		},
		"required": []string{"action"},
	}
}

func (t *HeartbeatTool) Execute(ctx context.Context, args map[string]any) *Result {
	action, _ := args["action"].(string)
	if action == "" {
		return ErrorResult("action parameter is required")
	}

	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return ErrorResult("no agent context")
	}

	// Permission check for mutation actions.
	switch action {
	case "set", "toggle", "set_checklist":
		if err := t.checkPermission(ctx, agentID); err != nil {
			return ErrorResult(err.Error())
		}
	}

	switch action {
	case "status":
		return t.handleStatus(ctx, agentID)
	case "get":
		return t.handleGet(ctx, agentID)
	case "set":
		return t.handleSet(ctx, agentID, args)
	case "toggle":
		enabled, ok := args["enabled"].(bool)
		if !ok {
			return ErrorResult("toggle requires 'enabled' parameter (boolean)")
		}
		return t.handleToggle(ctx, agentID, enabled)
	case "set_checklist":
		content, _ := args["content"].(string)
		return t.handleSetChecklist(ctx, agentID, content)
	case "get_checklist":
		return t.handleGetChecklist(ctx, agentID)
	case "test":
		return t.handleTest(agentID)
	case "logs":
		limit := intArg(args, "limit", 10)
		offset := intArg(args, "offset", 0)
		return t.handleLogs(ctx, agentID, limit, offset)
	default:
		return ErrorResult(fmt.Sprintf("unknown action %q — use status/get/set/toggle/set_checklist/get_checklist/test/logs", action))
	}
}

func (t *HeartbeatTool) handleStatus(ctx context.Context, agentID uuid.UUID) *Result {
	hb, err := t.hbStore.Get(ctx, agentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NewResult("Heartbeat not configured for this agent.")
		}
		return ErrorResult(err.Error())
	}
	return NewResult(fmt.Sprintf(
		"Heartbeat: %s | interval: %ds | runs: %d | suppressed: %d | last: %s (%s) | next: %s",
		boolLabel(hb.Enabled, "enabled", "disabled"),
		hb.IntervalSec, hb.RunCount, hb.SuppressCount,
		fmtTimePtr(hb.LastRunAt), derefStr(hb.LastStatus, "never"),
		fmtTimePtr(hb.NextRunAt),
	))
}

func (t *HeartbeatTool) handleGet(ctx context.Context, agentID uuid.UUID) *Result {
	hb, err := t.hbStore.Get(ctx, agentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NewResult("Heartbeat not configured for this agent.")
		}
		return ErrorResult(err.Error())
	}
	data, _ := json.MarshalIndent(hb, "", "  ")
	return NewResult(string(data))
}

func (t *HeartbeatTool) handleSet(ctx context.Context, agentID uuid.UUID, args map[string]any) *Result {
	hb, err := t.hbStore.Get(ctx, agentID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ErrorResult(err.Error())
	}
	if hb == nil {
		hb = &store.AgentHeartbeat{
			AgentID:         agentID,
			IntervalSec:     1800,
			IsolatedSession: true,
			AckMaxChars:     300,
			MaxRetries:      2,
		}
	}

	if v, ok := args["enabled"].(bool); ok {
		hb.Enabled = v
	}
	if v := intArg(args, "interval", 0); v > 0 {
		if v < 300 {
			return ErrorResult("minimum interval is 300 seconds (5 minutes)")
		}
		hb.IntervalSec = v
	}
	if v, ok := args["prompt"].(string); ok {
		hb.Prompt = &v
	}
	if v := intArg(args, "ack_max_chars", 0); v > 0 {
		hb.AckMaxChars = v
	}
	if v := intArg(args, "max_retries", -1); v >= 0 {
		hb.MaxRetries = v
	}
	if v, ok := args["isolated_session"].(bool); ok {
		hb.IsolatedSession = v
	}
	if v, ok := args["light_context"].(bool); ok {
		hb.LightContext = v
	}
	if v, ok := args["timezone"].(string); ok {
		hb.Timezone = &v
	}
	if v, ok := args["active_hours"].(string); ok {
		start, end := parseActiveHoursRange(v)
		hb.ActiveHoursStart = &start
		hb.ActiveHoursEnd = &end
	}

	// Auto-fill delivery from context.
	if v, ok := args["channel"].(string); ok {
		hb.Channel = &v
	} else if hb.Channel == nil {
		if ch := ToolChannelFromCtx(ctx); ch != "" {
			hb.Channel = &ch
		}
	}
	if v, ok := args["chat_id"].(string); ok {
		hb.ChatID = &v
	} else if hb.ChatID == nil {
		if cid := ToolChatIDFromCtx(ctx); cid != "" {
			hb.ChatID = &cid
		}
	}

	if hb.Enabled && hb.NextRunAt == nil {
		nextRun := time.Now().Add(time.Duration(hb.IntervalSec)*time.Second + store.StaggerOffset(hb.AgentID, hb.IntervalSec))
		hb.NextRunAt = &nextRun
	}

	if err := t.hbStore.Upsert(ctx, hb); err != nil {
		return ErrorResult(fmt.Sprintf("failed to save heartbeat config: %v", err))
	}

	return NewResult(fmt.Sprintf("Heartbeat config saved. Enabled: %v, interval: %ds, delivery: %s/%s",
		hb.Enabled, hb.IntervalSec, derefStr(hb.Channel, "none"), derefStr(hb.ChatID, "none")))
}

func (t *HeartbeatTool) handleToggle(ctx context.Context, agentID uuid.UUID, enabled bool) *Result {
	hb, err := t.hbStore.Get(ctx, agentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrorResult("heartbeat not configured — use set action first")
		}
		return ErrorResult(err.Error())
	}
	hb.Enabled = enabled
	if enabled && hb.NextRunAt == nil {
		nextRun := time.Now().Add(time.Duration(hb.IntervalSec) * time.Second)
		hb.NextRunAt = &nextRun
	}
	if err := t.hbStore.Upsert(ctx, hb); err != nil {
		return ErrorResult(err.Error())
	}
	return NewResult(fmt.Sprintf("Heartbeat %s.", boolLabel(enabled, "enabled", "disabled")))
}

// checkPermission verifies the calling user has permission to modify heartbeat config.
// Flow: deny list → allow list → agent owner/shares fallback.
// Read-only actions (status, get, get_checklist, logs) skip this check.
func (t *HeartbeatTool) checkPermission(ctx context.Context, agentID uuid.UUID) error {
	if t.permStore == nil {
		return nil // no permission store = allow all (backward compat)
	}
	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		return nil // system context (cron, subagent) = allow
	}

	// Determine scope from context: "agent" for DM, "group:{channel}:{chatId}" for groups.
	scope := "agent"
	if ch := ToolChannelFromCtx(ctx); ch != "" {
		if cid := ToolChatIDFromCtx(ctx); cid != "" {
			scope = "group:" + ch + ":" + cid
		}
	}

	allowed, err := t.permStore.CheckPermission(ctx, agentID, scope, "heartbeat", userID)
	if err != nil {
		return fmt.Errorf("permission check failed: %w", err)
	}
	if allowed {
		return nil
	}

	// Fallback: check if user is agent owner (via agent store).
	if t.agentStore != nil {
		ag, agErr := t.agentStore.GetByID(ctx, agentID)
		if agErr == nil {
			senderID := store.SenderIDFromContext(ctx)
			if senderID == "" {
				senderID = userID
			}
			if ag.OwnerID != "" && ag.OwnerID == senderID {
				return nil // agent owner = allow
			}
		}
	}

	return fmt.Errorf("permission denied: you are not authorized to modify heartbeat config for this agent")
}

func (t *HeartbeatTool) handleSetChecklist(ctx context.Context, agentID uuid.UUID, content string) *Result {
	if t.agentStore == nil {
		return ErrorResult("agent store not configured")
	}
	if content == "" {
		return ErrorResult("content parameter is required — provide the HEARTBEAT.md checklist")
	}
	if err := t.agentStore.SetAgentContextFile(ctx, agentID, "HEARTBEAT.md", content); err != nil {
		return ErrorResult(fmt.Sprintf("failed to save HEARTBEAT.md: %v", err))
	}
	return NewResult(fmt.Sprintf("HEARTBEAT.md saved (%d chars). The heartbeat ticker will use this checklist on each run.", len([]rune(content))))
}

func (t *HeartbeatTool) handleGetChecklist(ctx context.Context, agentID uuid.UUID) *Result {
	if t.agentStore == nil {
		return ErrorResult("agent store not configured")
	}
	files, err := t.agentStore.GetAgentContextFiles(ctx, agentID)
	if err != nil {
		return ErrorResult(err.Error())
	}
	for _, f := range files {
		if f.FileName == "HEARTBEAT.md" && f.Content != "" {
			return NewResult(f.Content)
		}
	}
	return NewResult("HEARTBEAT.md not found. Use set_checklist to create one.")
}

func (t *HeartbeatTool) handleTest(agentID uuid.UUID) *Result {
	if t.wakeFn == nil {
		return ErrorResult("heartbeat ticker not available")
	}
	t.wakeFn(agentID)
	return NewResult("Heartbeat test triggered (running in background).")
}

func (t *HeartbeatTool) handleLogs(ctx context.Context, agentID uuid.UUID, limit, offset int) *Result {
	logs, total, err := t.hbStore.ListLogs(ctx, agentID, limit, offset)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if len(logs) == 0 {
		return NewResult("No heartbeat run logs found.")
	}
	data, _ := json.MarshalIndent(map[string]any{
		"total": total,
		"logs":  logs,
	}, "", "  ")
	return NewResult(string(data))
}

// parseActiveHoursRange parses "HH:MM-HH:MM" into start and end strings.
func parseActiveHoursRange(s string) (string, string) {
	for _, sep := range []string{"-", " - ", "~"} {
		parts := splitOnce(s, sep)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}
	return s, ""
}

func splitOnce(s, sep string) []string {
	idx := -1
	for i := range len(s) - len(sep) + 1 {
		if s[i:i+len(sep)] == sep {
			idx = i
			break
		}
	}
	if idx < 0 {
		return []string{s}
	}
	return []string{s[:idx], s[idx+len(sep):]}
}

func intArg(args map[string]any, key string, fallback int) int {
	if v, ok := args[key].(float64); ok {
		return int(v)
	}
	return fallback
}

func boolLabel(v bool, trueStr, falseStr string) string {
	if v {
		return trueStr
	}
	return falseStr
}

func derefStr(s *string, fallback string) string {
	if s == nil || *s == "" {
		return fallback
	}
	return *s
}

func fmtTimePtr(t *time.Time) string {
	if t == nil {
		return "never"
	}
	return t.Format(time.RFC3339)
}
