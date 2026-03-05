package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// BridgeToolNames is the subset of GoClaw tools exposed via the MCP bridge.
// Excluded: spawn (agent loop), message/create_forum_topic (channels),
// handoff/delegate_search/evaluate_loop/team_* (managed mode stores).
var BridgeToolNames = map[string]bool{
	// Filesystem
	"read_file":  true,
	"write_file": true,
	"list_files": true,
	"edit":       true,
	"exec":       true,
	// Web
	"web_search": true,
	"web_fetch":  true,
	// Memory & knowledge
	"memory_search": true,
	"memory_get":    true,
	"skill_search":  true,
	// Media
	"read_image":   true,
	"create_image": true,
	"tts":          true,
	// Browser automation
	"browser": true,
	// Scheduler
	"cron": true,
	// Sessions (read + send)
	"sessions_list":    true,
	"session_status":   true,
	"sessions_history": true,
	"sessions_send":    true,
}

// NewBridgeServer creates a StreamableHTTPServer that exposes GoClaw tools as MCP tools.
// It reads tools from the registry, filters to BridgeToolNames, and serves them
// over streamable-http transport (stateless mode).
func NewBridgeServer(reg *tools.Registry, version string) *mcpserver.StreamableHTTPServer {
	srv := mcpserver.NewMCPServer("goclaw-bridge", version,
		mcpserver.WithToolCapabilities(false),
	)

	// Register each safe tool from the GoClaw registry
	var registered int
	for name := range BridgeToolNames {
		t, ok := reg.Get(name)
		if !ok {
			continue
		}

		mcpTool := convertToMCPTool(t)
		handler := makeToolHandler(reg, name)
		srv.AddTool(mcpTool, handler)
		registered++
	}

	slog.Info("mcp.bridge: tools registered", "count", registered)

	return mcpserver.NewStreamableHTTPServer(srv,
		mcpserver.WithStateLess(true),
	)
}

// convertToMCPTool converts a GoClaw tools.Tool into an mcp-go Tool.
func convertToMCPTool(t tools.Tool) mcpgo.Tool {
	schema, err := json.Marshal(t.Parameters())
	if err != nil {
		// Fallback: empty object schema
		schema = []byte(`{"type":"object"}`)
	}
	return mcpgo.NewToolWithRawSchema(t.Name(), t.Description(), schema)
}

// makeToolHandler creates a ToolHandlerFunc that delegates to the GoClaw tool registry.
func makeToolHandler(reg *tools.Registry, toolName string) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()

		result := reg.Execute(ctx, toolName, args)

		if result.IsError {
			return mcpgo.NewToolResultError(result.ForLLM), nil
		}

		return mcpgo.NewToolResultText(result.ForLLM), nil
	}
}
