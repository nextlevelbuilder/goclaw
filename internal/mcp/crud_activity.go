package mcp

import (
	"context"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerActivityCRUDTools registers goclaw_activity_list, backed by
// store.ActivityStore — closes a CLI-vs-MCP coverage gap (`goclaw activity
// list`, the audit log of admin/agent actions emitted via emitAudit
// throughout internal/http).
func registerActivityCRUDTools(srv *mcpserver.MCPServer, activity store.ActivityStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_activity_list",
		mcpgo.WithDescription("List audit log entries (admin/agent actions), optionally filtered."),
		mcpgo.WithString("actor_type", mcpgo.Description("Filter by actor type (e.g. \"user\", \"agent\", \"system\").")),
		mcpgo.WithString("actor_id", mcpgo.Description("Filter by actor ID.")),
		mcpgo.WithString("action", mcpgo.Description("Filter by action name (e.g. \"skill.file_updated\").")),
		mcpgo.WithString("entity_type", mcpgo.Description("Filter by entity type (e.g. \"skill\", \"agent\").")),
		mcpgo.WithString("entity_id", mcpgo.Description("Filter by entity ID.")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum entries to return; defaults to 50.")),
		mcpgo.WithNumber("offset", mcpgo.Description("Pagination offset.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleActivityList(activity))
}

func handleActivityList(activity store.ActivityStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		opts := store.ActivityListOpts{
			ActorType:  req.GetString("actor_type", ""),
			ActorID:    req.GetString("actor_id", ""),
			Action:     req.GetString("action", ""),
			EntityType: req.GetString("entity_type", ""),
			EntityID:   req.GetString("entity_id", ""),
			Limit:      intArg(req, "limit", 50),
			Offset:     intArg(req, "offset", 0),
		}
		list, err := activity.List(ctx, opts)
		if err != nil {
			return toolError("activity.list", err)
		}
		total, err := activity.Count(ctx, opts)
		if err != nil {
			return toolError("activity.list", err)
		}
		return jsonToolResult(map[string]any{"entries": list, "total": total})
	}
}
