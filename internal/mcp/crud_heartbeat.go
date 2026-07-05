package mcp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// minHeartbeatIntervalSec mirrors internal/gateway/methods/heartbeat.go's
// inline "minimum interval is 300 seconds" check.
const minHeartbeatIntervalSec = 300

// maxHeartbeatRetries mirrors internal/gateway/methods/heartbeat.go's inline
// "maxRetries must be 0-10" check.
const maxHeartbeatRetries = 10

// registerHeartbeatCRUDTools registers the goclaw_heartbeat_* MCP tools
// backed by store.HeartbeatStore. Mirrors internal/gateway/methods/heartbeat.go
// minus heartbeat.test (no wake function is available on this standalone MCP
// surface — see final report) and minus the cache-invalidation/audit-event
// side effects (WS-only concerns).
func registerHeartbeatCRUDTools(srv *mcpserver.MCPServer, hb store.HeartbeatStore, agents store.AgentStore, providers store.ProviderStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_heartbeat_get",
		mcpgo.WithDescription("Get an agent's heartbeat configuration."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent key or UUID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleHeartbeatGet(hb, agents))

	srv.AddTool(mcpgo.NewTool("goclaw_heartbeat_set",
		mcpgo.WithDescription("Create or update an agent's heartbeat configuration."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent key or UUID.")),
		mcpgo.WithBoolean("enabled", mcpgo.Description("Enabled state.")),
		mcpgo.WithNumber("interval_sec", mcpgo.Description("Interval in seconds (minimum 300).")),
		mcpgo.WithString("prompt", mcpgo.Description("Heartbeat prompt override.")),
		mcpgo.WithString("provider_name", mcpgo.Description("Provider name override; empty string clears the override.")),
		mcpgo.WithString("model", mcpgo.Description("Model override; empty string clears the override.")),
		mcpgo.WithBoolean("isolated_session", mcpgo.Description("Run heartbeat in an isolated session.")),
		mcpgo.WithBoolean("light_context", mcpgo.Description("Use light context for the heartbeat run.")),
		mcpgo.WithNumber("ack_max_chars", mcpgo.Description("Max chars for acknowledgement (>= 0).")),
		mcpgo.WithNumber("max_retries", mcpgo.Description("Max retries (0-10).")),
		mcpgo.WithString("active_hours_start", mcpgo.Description("Active hours window start (HH:MM).")),
		mcpgo.WithString("active_hours_end", mcpgo.Description("Active hours window end (HH:MM).")),
		mcpgo.WithString("timezone", mcpgo.Description("IANA timezone.")),
		mcpgo.WithString("channel", mcpgo.Description("Delivery channel override.")),
		mcpgo.WithString("chat_id", mcpgo.Description("Delivery chat ID override.")),
	), handleHeartbeatSet(hb, agents, providers))

	srv.AddTool(mcpgo.NewTool("goclaw_heartbeat_toggle",
		mcpgo.WithDescription("Enable or disable an agent's heartbeat."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent key or UUID.")),
		mcpgo.WithBoolean("enabled", mcpgo.Required(), mcpgo.Description("Desired enabled state.")),
	), handleHeartbeatToggle(hb, agents))

	srv.AddTool(mcpgo.NewTool("goclaw_heartbeat_test",
		mcpgo.WithDescription("Trigger an immediate heartbeat run. NOTE: not available on this MCP surface — no heartbeat ticker wake function is wired here (see internal/gateway/methods/heartbeat.go's SetWakeFn, only attached to the WS RPC surface); always returns an error."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent key or UUID.")),
	), handleHeartbeatTest())

	srv.AddTool(mcpgo.NewTool("goclaw_heartbeat_logs",
		mcpgo.WithDescription("List heartbeat run log entries for an agent."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent key or UUID.")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum entries to return.")),
		mcpgo.WithNumber("offset", mcpgo.Description("Pagination offset.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleHeartbeatLogs(hb, agents))

	srv.AddTool(mcpgo.NewTool("goclaw_heartbeat_checklist_get",
		mcpgo.WithDescription("Read an agent's HEARTBEAT.md checklist content."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent key or UUID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleHeartbeatChecklistGet(agents))

	srv.AddTool(mcpgo.NewTool("goclaw_heartbeat_checklist_set",
		mcpgo.WithDescription("Write an agent's HEARTBEAT.md checklist content."),
		mcpgo.WithString("agent_id", mcpgo.Required(), mcpgo.Description("Agent key or UUID.")),
		mcpgo.WithString("content", mcpgo.Required(), mcpgo.Description("New HEARTBEAT.md content.")),
	), handleHeartbeatChecklistSet(agents))

	srv.AddTool(mcpgo.NewTool("goclaw_heartbeat_targets",
		mcpgo.WithDescription("List known (channel, chatID) delivery targets for the current tenant."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleHeartbeatTargets(hb))
}

func handleHeartbeatGet(hb store.HeartbeatStore, agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentRef, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("heartbeat.get", err)
		}
		agentID, err := resolveAgentUUID(ctx, agents, agentRef)
		if err != nil {
			return toolError("heartbeat.get", fmt.Errorf("invalid agent_id: %w", err))
		}
		h, err := hb.Get(ctx, agentID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return jsonToolResult(map[string]any{"heartbeat": nil})
			}
			return toolError("heartbeat.get", err)
		}
		return jsonToolResult(map[string]any{"heartbeat": h})
	}
}

func handleHeartbeatSet(hb store.HeartbeatStore, agents store.AgentStore, providers store.ProviderStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentRef, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("heartbeat.set", err)
		}
		agentID, err := resolveAgentUUID(ctx, agents, agentRef)
		if err != nil {
			return toolError("heartbeat.set", fmt.Errorf("invalid agent_id: %w", err))
		}

		h, err := hb.Get(ctx, agentID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return toolError("heartbeat.set", err)
		}
		const (
			defaultIntervalSec = 1800
			defaultAckMaxChars = 300
			defaultMaxRetries  = 2
		)
		if h == nil {
			h = &store.AgentHeartbeat{
				AgentID: agentID, IntervalSec: defaultIntervalSec, IsolatedSession: true,
				AckMaxChars: defaultAckMaxChars, MaxRetries: defaultMaxRetries,
			}
		}

		args := req.GetArguments()
		if v, ok := args["enabled"].(bool); ok {
			h.Enabled = v
		}
		if v, ok := args["interval_sec"].(float64); ok {
			if int(v) < minHeartbeatIntervalSec {
				return mcpgo.NewToolResultError("heartbeat.set: minimum interval is 300 seconds"), nil
			}
			h.IntervalSec = int(v)
		}
		if v, ok := args["prompt"].(string); ok {
			h.Prompt = &v
		}
		if v, ok := args["provider_name"].(string); ok {
			if v == "" {
				h.ProviderID = nil
			} else if providers != nil {
				prov, err := providers.GetProviderByName(ctx, v)
				if err != nil {
					return mcpgo.NewToolResultError("heartbeat.set: provider not found: " + v), nil
				}
				h.ProviderID = &prov.ID
			}
		}
		if v, ok := args["model"].(string); ok {
			if v == "" {
				h.Model = nil
			} else {
				h.Model = &v
			}
		}
		if v, ok := args["isolated_session"].(bool); ok {
			h.IsolatedSession = v
		}
		if v, ok := args["light_context"].(bool); ok {
			h.LightContext = v
		}
		if v, ok := args["ack_max_chars"].(float64); ok {
			if int(v) < 0 {
				return mcpgo.NewToolResultError("heartbeat.set: ack_max_chars must be >= 0"), nil
			}
			h.AckMaxChars = int(v)
		}
		if v, ok := args["max_retries"].(float64); ok {
			if int(v) < 0 || int(v) > maxHeartbeatRetries {
				return mcpgo.NewToolResultError("heartbeat.set: max_retries must be 0-10"), nil
			}
			h.MaxRetries = int(v)
		}
		if v, ok := args["active_hours_start"].(string); ok {
			h.ActiveHoursStart = &v
		}
		if v, ok := args["active_hours_end"].(string); ok {
			h.ActiveHoursEnd = &v
		}
		if v, ok := args["timezone"].(string); ok {
			if v != "" {
				if _, err := time.LoadLocation(v); err != nil {
					return mcpgo.NewToolResultError("heartbeat.set: invalid timezone: " + v), nil
				}
			}
			h.Timezone = &v
		}
		if v, ok := args["channel"].(string); ok {
			h.Channel = &v
		}
		if v, ok := args["chat_id"].(string); ok {
			h.ChatID = &v
		}

		if h.Enabled && h.NextRunAt == nil {
			nextRun := time.Now().Add(time.Duration(h.IntervalSec)*time.Second + store.StaggerOffset(h.AgentID, h.IntervalSec))
			h.NextRunAt = &nextRun
		}

		if err := hb.Upsert(ctx, h); err != nil {
			return toolError("heartbeat.set", err)
		}
		return jsonToolResult(map[string]any{"heartbeat": h})
	}
}

func handleHeartbeatToggle(hb store.HeartbeatStore, agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentRef, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("heartbeat.toggle", err)
		}
		agentID, err := resolveAgentUUID(ctx, agents, agentRef)
		if err != nil {
			return toolError("heartbeat.toggle", fmt.Errorf("invalid agent_id: %w", err))
		}
		enabled, err := req.RequireBool("enabled")
		if err != nil {
			return toolError("heartbeat.toggle", err)
		}
		h, err := hb.Get(ctx, agentID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return mcpgo.NewToolResultError("heartbeat.toggle: heartbeat not configured"), nil
			}
			return toolError("heartbeat.toggle", err)
		}
		h.Enabled = enabled
		if enabled && h.NextRunAt == nil {
			nextRun := time.Now().Add(time.Duration(h.IntervalSec) * time.Second)
			h.NextRunAt = &nextRun
		}
		if err := hb.Upsert(ctx, h); err != nil {
			return toolError("heartbeat.toggle", err)
		}
		return jsonToolResult(map[string]any{"agentId": agentRef, "enabled": enabled})
	}
}

func handleHeartbeatTest() mcpserver.ToolHandlerFunc {
	return func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return mcpgo.NewToolResultError("heartbeat.test: not available on this MCP surface (no heartbeat ticker wake function wired)"), nil
	}
}

func handleHeartbeatLogs(hb store.HeartbeatStore, agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentRef, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("heartbeat.logs", err)
		}
		agentID, err := resolveAgentUUID(ctx, agents, agentRef)
		if err != nil {
			return toolError("heartbeat.logs", fmt.Errorf("invalid agent_id: %w", err))
		}
		limit := int(req.GetFloat("limit", 0))
		offset := int(req.GetFloat("offset", 0))
		logs, total, err := hb.ListLogs(ctx, agentID, limit, offset)
		if err != nil {
			return toolError("heartbeat.logs", err)
		}
		return jsonToolResult(map[string]any{"logs": logs, "total": total})
	}
}

func handleHeartbeatChecklistGet(agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentRef, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("heartbeat.checklist.get", err)
		}
		agentID, err := resolveAgentUUID(ctx, agents, agentRef)
		if err != nil {
			return toolError("heartbeat.checklist.get", fmt.Errorf("invalid agent_id: %w", err))
		}
		files, err := agents.GetAgentContextFiles(ctx, agentID)
		if err != nil {
			return toolError("heartbeat.checklist.get", err)
		}
		var content string
		for _, f := range files {
			if f.FileName == "HEARTBEAT.md" {
				content = f.Content
				break
			}
		}
		return jsonToolResult(map[string]any{"content": content})
	}
}

func handleHeartbeatChecklistSet(agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		agentRef, err := req.RequireString("agent_id")
		if err != nil {
			return toolError("heartbeat.checklist.set", err)
		}
		content, err := req.RequireString("content")
		if err != nil {
			return toolError("heartbeat.checklist.set", err)
		}
		agentID, err := resolveAgentUUID(ctx, agents, agentRef)
		if err != nil {
			return toolError("heartbeat.checklist.set", fmt.Errorf("invalid agent_id: %w", err))
		}
		if err := agents.SetAgentContextFile(ctx, agentID, "HEARTBEAT.md", content); err != nil {
			return toolError("heartbeat.checklist.set", err)
		}
		return jsonToolResult(map[string]any{"ok": true, "length": len([]rune(content))})
	}
}

func handleHeartbeatTargets(hb store.HeartbeatStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			tenantID = store.MasterTenantID
		}
		targets, err := hb.ListDeliveryTargets(ctx, tenantID)
		if err != nil {
			return toolError("heartbeat.targets", err)
		}
		return jsonToolResult(map[string]any{"targets": targets})
	}
}
