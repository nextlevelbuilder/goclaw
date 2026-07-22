package mcp

// crud_test_helpers_test.go provides small shared helpers for invoking
// registered MCP tool handlers directly in tests, without going through the
// HTTP/streamable transport. mcp-go's *mcpserver.MCPServer exposes GetTool()
// which returns the registered ServerTool{Tool, Handler} — calling Handler
// directly is the most faithful way to exercise the exact registration code
// path (tool names, required-arg validation via req.RequireString, etc.)
// without needing a real network listener.

import (
	"context"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// newTestMCPServer builds a bare MCPServer suitable for registering CRUD tool
// families in tests.
func newTestMCPServer() *mcpserver.MCPServer {
	return mcpserver.NewMCPServer("goclaw-crud-test", "test", mcpserver.WithToolCapabilities(false))
}

// callTool looks up a registered tool by name and invokes its handler with
// the given arguments, failing the test immediately if the tool isn't
// registered.
func callTool(t *testing.T, srv *mcpserver.MCPServer, name string, args map[string]any) *mcpgo.CallToolResult {
	t.Helper()
	tool := srv.GetTool(name)
	if tool == nil {
		t.Fatalf("tool %q not registered", name)
	}
	req := mcpgo.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	result, err := tool.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("tool %q handler returned transport error: %v", name, err)
	}
	return result
}

// toolResultText extracts the concatenated text content of a tool result.
func toolResultText(result *mcpgo.CallToolResult) string {
	return extractTextContent(result)
}

// toolIsError reports whether result represents an MCP tool-level error.
func toolIsError(result *mcpgo.CallToolResult) bool {
	return result != nil && result.IsError
}
