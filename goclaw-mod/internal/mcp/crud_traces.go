package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerTracesCRUDTools registers the goclaw_traces_* MCP tools backed by
// store.TracingStore — closes a CLI-vs-MCP coverage gap (the `goclaw traces
// get/list` commands had no MCP equivalent; goclaw_run_timeline_get covers a
// different, run-oriented view). Read-only: trace/span data is written by
// the tracing pipeline itself (internal/tracing), not by operators.
func registerTracesCRUDTools(srv *mcpserver.MCPServer, tracing store.TracingStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_traces_list",
		mcpgo.WithDescription("List LLM call traces, optionally filtered by agent/user/session/status."),
		mcpgo.WithString("agent_id", mcpgo.Description("Filter by agent UUID.")),
		mcpgo.WithString("user_id", mcpgo.Description("Filter by user ID.")),
		mcpgo.WithString("session_key", mcpgo.Description("Filter by session key.")),
		mcpgo.WithString("status", mcpgo.Description("Filter by status (e.g. \"success\", \"error\").")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum traces to return; defaults to 50.")),
		mcpgo.WithNumber("offset", mcpgo.Description("Pagination offset.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleTracesList(tracing))

	srv.AddTool(mcpgo.NewTool("goclaw_traces_get",
		mcpgo.WithDescription("Get a single trace and its spans by UUID."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Trace UUID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleTracesGet(tracing))
}

func handleTracesList(tracing store.TracingStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		opts := store.TraceListOpts{
			UserID:     req.GetString("user_id", ""),
			SessionKey: req.GetString("session_key", ""),
			Status:     req.GetString("status", ""),
			Limit:      intArg(req, "limit", 50),
			Offset:     intArg(req, "offset", 0),
		}
		if agentIDStr := req.GetString("agent_id", ""); agentIDStr != "" {
			agentID, err := uuid.Parse(agentIDStr)
			if err != nil {
				return toolError("traces.list", fmt.Errorf("invalid agent_id: %w", err))
			}
			opts.AgentID = &agentID
		}
		traces, err := tracing.ListTraces(ctx, opts)
		if err != nil {
			return toolError("traces.list", err)
		}
		return jsonToolResult(traces)
	}
}

func handleTracesGet(tracing store.TracingStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("traces.get", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("traces.get", fmt.Errorf("invalid id: %w", err))
		}
		trace, err := tracing.GetTrace(ctx, id)
		if err != nil {
			return toolError("traces.get", err)
		}
		spans, err := tracing.GetTraceSpans(ctx, id)
		if err != nil {
			return toolError("traces.get", err)
		}
		return jsonToolResult(map[string]any{"trace": trace, "spans": spans})
	}
}
