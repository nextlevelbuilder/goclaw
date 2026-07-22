package mcp

import (
	"context"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerRunTimelineCRUDTools registers the goclaw_run_timeline_get MCP tool
// backed by store.RunTimelineStore.
func registerRunTimelineCRUDTools(srv *mcpserver.MCPServer, timeline store.RunTimelineStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_run_timeline_get",
		mcpgo.WithDescription("Fetch the archived timeline for a run or session."),
		mcpgo.WithString("run_id", mcpgo.Description("Run ID; preferred when known.")),
		mcpgo.WithString("session_key", mcpgo.Description("Session key; used to find the latest run when run_id is not known.")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum items to return.")),
		mcpgo.WithNumber("offset", mcpgo.Description("Pagination offset.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleRunTimelineGet(timeline))
}

func handleRunTimelineGet(timeline store.RunTimelineStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		opts := store.RunTimelineListOpts{
			RunID:      req.GetString("run_id", ""),
			SessionKey: req.GetString("session_key", ""),
			Limit:      int(req.GetFloat("limit", 0)),
			Offset:     int(req.GetFloat("offset", 0)),
		}
		items, err := timeline.ListRunTimelineItems(ctx, opts)
		if err != nil {
			return toolError("run_timeline.get", err)
		}
		return jsonToolResult(map[string]any{
			"runId":      opts.RunID,
			"sessionKey": opts.SessionKey,
			"items":      items,
			"limit":      opts.Limit,
			"offset":     opts.Offset,
		})
	}
}
