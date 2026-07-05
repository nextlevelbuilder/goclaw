package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// TestSelfConnectedServer_CacheRetainsDescriptionAndSchema is a regression
// test for a reported bug where connecting to an already-connected MCP
// server as a client showed tools with an empty description and empty
// parameters in the system-prompt preview (`- function: {description: '',
// parameters: {type: object}}`), while newly-discovered tools came through
// with full schema.
//
// It builds a minimal generic MCP server directly with the underlying
// mark3labs/mcp-go library (no dependency on any goclaw-specific server such
// as the CRUD server), serves it over streamable-http via httptest
// (mirroring a goclaw gateway connecting to an MCP endpoint), connects to it
// with the exact client path used by the Manager (connectAndDiscover), and
// proves the resulting tool cache (buildCachedToolInfo) — and the full
// downstream pipeline through ListToolsForAgent with a bare-name tool_allow
// grant — retains the real description and JSON Schema.
func TestSelfConnectedServer_CacheRetainsDescriptionAndSchema(t *testing.T) {
	srv := mcpserver.NewMCPServer("test-server", "1.0.0", mcpserver.WithToolCapabilities(false))

	tool := mcpgo.NewTool("test_tool",
		mcpgo.WithDescription("A simple test tool used to verify description/schema propagation."),
		mcpgo.WithString("param",
			mcpgo.Description("An example string parameter."),
			mcpgo.Required(),
		),
	)
	srv.AddTool(tool, func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return mcpgo.NewToolResultText("ok"), nil
	})

	httpSrv := mcpserver.NewStreamableHTTPServer(srv)
	ts := httptest.NewServer(httpSrv)
	defer ts.Close()

	// "goclaw" is the same ClientInfo.Name used by connectAndDiscover in
	// production (manager_connect.go).
	ss, mcpTools, err := connectAndDiscover(context.Background(), "goclaw", "streamable-http", "", nil, nil, ts.URL, nil, 10)
	if err != nil {
		t.Fatalf("connectAndDiscover: %v", err)
	}
	defer func() { _ = ss.client.Close() }()

	cache := buildCachedToolInfo(mcpTools)
	entry, ok := cache["test_tool"]
	if !ok {
		t.Fatalf("expected test_tool in cache, got %+v", cache)
	}
	if entry.Description == "" {
		t.Fatal("expected non-empty description for test_tool, got empty (regression)")
	}
	if len(entry.Parameters) == 0 {
		t.Fatal("expected non-empty parameters schema for test_tool, got empty (regression)")
	}

	var schema map[string]any
	if err := json.Unmarshal(entry.Parameters, &schema); err != nil {
		t.Fatalf("unmarshal cached parameters: %v", err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		t.Fatalf("expected non-empty properties in cached schema, got %+v", schema)
	}
	if _, ok := props["param"]; !ok {
		t.Fatalf("expected 'param' property in cached schema, got %+v", props)
	}

	// End-to-end: feed the cache into ListToolsForAgent exactly as it is
	// persisted in production (settings.tool_cache), with an explicit
	// tool_allow grant using the BARE tool name — the shape ListToolsForAgent
	// expects (internal/mcp/manager.go) and the shape ServerToolInfos returns
	// (internal/mcp/manager_tools.go).
	settings, err := json.Marshal(map[string]any{"tool_cache": cache})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	serverID := uuid.New()
	mockStore := &mockMCPStore{
		accessible: []store.MCPAccessInfo{
			{
				Server: store.MCPServerData{
					BaseModel: store.BaseModel{ID: serverID},
					Name:      "goclaw",
					Enabled:   true,
					Settings:  settings,
				},
				ToolAllow: []string{"test_tool"},
			},
		},
	}
	mgr := NewManager(tools.NewRegistry(), WithStore(mockStore))
	previews, err := mgr.ListToolsForAgent(t.Context(), uuid.New(), "user-1")
	if err != nil {
		t.Fatalf("ListToolsForAgent: %v", err)
	}
	var found *MCPToolPreviewInfo
	for i := range previews {
		if previews[i].RegisteredName == "mcp_goclaw__test_tool" {
			found = &previews[i]
		}
	}
	if found == nil {
		t.Fatalf("expected mcp_goclaw__test_tool in preview, got %+v", previews)
	}
	if found.Description == "" {
		t.Fatal("preview: expected non-empty description (regression)")
	}
	if len(found.Parameters) == 0 {
		t.Fatal("preview: expected non-empty parameters (regression)")
	}
}
