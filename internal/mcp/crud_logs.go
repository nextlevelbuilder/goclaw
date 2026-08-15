package mcp

import (
	"context"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	runtimelogs "github.com/nextlevelbuilder/goclaw/internal/logs"
)

// RuntimeLogSnapshotter returns a bounded, in-memory aggregate of recent
// runtime log entries. Implemented by *gateway.LogTee (see
// internal/gateway/log_tee.go's AggregateRuntimeLogs), which already backs
// the HTTP GET /v1/logs/runtime/aggregate endpoint
// (internal/http/logs.go) — reused here via a narrow interface so this
// package does not need to import internal/gateway (which itself imports
// this package, see crud_server.go's package doc comment for the cycle
// rationale shared across this file's siblings).
type RuntimeLogSnapshotter interface {
	AggregateRuntimeLogs(opts runtimelogs.RuntimeAggregateOpts) runtimelogs.RuntimeAggregateResult
}

// registerLogsCRUDTool registers goclaw_logs_tail. Unlike the WS logs.tail
// RPC method — which starts/stops a live push subscription over the
// connection's own WebSocket — this MCP surface is a stateless HTTP server
// (mcpserver.WithStateLess(true), see crud_server.go) with no persistent
// per-caller connection to push log lines to. There is therefore no way to
// honor the "start tailing, then receive server-pushed log events" contract
// here. Instead this tool returns a one-shot aggregate snapshot of the
// gateway's bounded runtime log ring buffer (the same data backing
// GET /v1/logs/runtime/aggregate), which is the closest real, queryable
// equivalent available to a stateless caller. The "action" param is accepted
// for naming parity with the WS method but only "start" (or empty) produces
// a snapshot; "stop" is a no-op success (nothing was subscribed).
func registerLogsCRUDTool(srv *mcpserver.MCPServer, snapshotter RuntimeLogSnapshotter) {
	srv.AddTool(mcpgo.NewTool("goclaw_logs_tail",
		mcpgo.WithDescription("Return a snapshot aggregate of recent runtime log entries (grouped by level or source). This is a point-in-time read, not a live push subscription: MCP tool calls are stateless request/response, so there is no channel to stream log lines to the caller as they occur."),
		mcpgo.WithString("action", mcpgo.Description("\"start\" (default) returns a snapshot; \"stop\" is a no-op success.")),
		mcpgo.WithString("group_by", mcpgo.Description("\"level\" (default) or \"source\".")),
		mcpgo.WithString("level", mcpgo.Description("Filter to a single level (\"debug\", \"info\", \"warn\", \"error\").")),
		mcpgo.WithString("source", mcpgo.Description("Filter to a single log source/component.")),
		mcpgo.WithString("since", mcpgo.Description("RFC3339 timestamp; only entries at or after this time are included.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleLogsTail(snapshotter))
}

func handleLogsTail(snapshotter RuntimeLogSnapshotter) mcpserver.ToolHandlerFunc {
	return func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		action := req.GetString("action", "start")
		if action == "stop" {
			return jsonToolResult(map[string]any{"status": "stopped"})
		}
		if snapshotter == nil {
			return mcpgo.NewToolResultError("logs.tail: runtime log snapshotter not available"), nil
		}

		groupBy := req.GetString("group_by", "level")
		var fromMS int64
		if since := req.GetString("since", ""); since != "" {
			t, err := time.Parse(time.RFC3339, since)
			if err != nil {
				return toolError("logs.tail", err)
			}
			fromMS = t.UnixMilli()
		}

		result := snapshotter.AggregateRuntimeLogs(runtimelogs.RuntimeAggregateOpts{
			GroupBy: groupBy,
			Level:   req.GetString("level", ""),
			Source:  req.GetString("source", ""),
			FromMS:  fromMS,
		})
		return jsonToolResult(map[string]any{"status": "ok", "aggregate": result})
	}
}
