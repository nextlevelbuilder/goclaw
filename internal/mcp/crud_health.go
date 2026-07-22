package mcp

import (
	"context"
	"database/sql"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// registerHealthCRUDTool registers goclaw_health, closing the `goclaw
// health`/`status` CLI-vs-MCP coverage gap. A successful MCP tool call
// already proves the gateway is reachable and this bearer token is valid —
// what it can't prove on its own is that the DB behind it is up, so this
// tool's only real value-add over "the call succeeded" is the DB ping.
func registerHealthCRUDTool(srv *mcpserver.MCPServer, db *sql.DB, version string) {
	srv.AddTool(mcpgo.NewTool("goclaw_health",
		mcpgo.WithDescription("Check gateway health: protocol version, server version, and database connectivity."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleHealth(db, version))
}

func handleHealth(db *sql.DB, version string) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		result := map[string]any{
			"status":   "ok",
			"protocol": protocol.ProtocolVersion,
			"version":  version,
		}
		if db != nil {
			if err := db.PingContext(ctx); err != nil {
				result["status"] = "degraded"
				result["db_error"] = err.Error()
			} else {
				result["db"] = "ok"
			}
		}
		return jsonToolResult(result)
	}
}
