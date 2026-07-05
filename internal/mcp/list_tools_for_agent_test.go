package mcp

import (
	"encoding/json"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// TestListToolsForAgent_EmptyToolAllowUsesCache verifies that when an agent's
// grant on an MCP server has an unrestricted (empty) ToolAllow list, but the
// server's settings contain a populated tool_cache, ListToolsForAgent
// enumerates one MCPToolPreviewInfo per cached tool instead of collapsing the
// entire server into a single "__*" placeholder entry.
func TestListToolsForAgent_EmptyToolAllowUsesCache(t *testing.T) {
	serverID := uuid.New()

	toolCache := map[string]string{
		"list_zones":  "List DNS zones",
		"purge_cache": "Purge the CDN cache",
	}
	settings, err := json.Marshal(map[string]any{
		"tool_cache": toolCache,
	})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}

	mockStore := &mockMCPStore{
		accessible: []store.MCPAccessInfo{
			{
				Server: store.MCPServerData{
					BaseModel: store.BaseModel{ID: serverID},
					Name:      "cloudflare",
					Enabled:   true,
					Settings:  settings,
				},
				ToolAllow: nil, // unrestricted grant
				ToolDeny:  nil,
			},
		},
	}

	mgr := NewManager(tools.NewRegistry(), WithStore(mockStore))

	got, err := mgr.ListToolsForAgent(t.Context(), uuid.New(), "user-1")
	if err != nil {
		t.Fatalf("ListToolsForAgent: %v", err)
	}

	if len(got) != len(toolCache) {
		t.Fatalf("expected %d tool entries (one per cached tool), got %d: %+v", len(toolCache), len(got), got)
	}

	sort.Slice(got, func(i, j int) bool { return got[i].RegisteredName < got[j].RegisteredName })

	wantNames := map[string]string{
		"mcp_cloudflare__list_zones":  "List DNS zones",
		"mcp_cloudflare__purge_cache": "Purge the CDN cache",
	}
	for _, entry := range got {
		wantDesc, ok := wantNames[entry.RegisteredName]
		if !ok {
			t.Fatalf("unexpected registered name %q", entry.RegisteredName)
		}
		if entry.Description != wantDesc {
			t.Fatalf("registered name %q: got description %q, want %q", entry.RegisteredName, entry.Description, wantDesc)
		}
		if entry.RegisteredName == "mcp_cloudflare__*" {
			t.Fatalf("got placeholder entry despite non-empty tool_cache")
		}
	}
}

// TestListToolsForAgent_CacheWithRealSchemaReturnsParameters verifies that
// when the server's tool_cache uses the new shape (map[string]CachedToolInfo,
// written by buildCachedToolInfo in manager_connect.go), ListToolsForAgent
// returns the real cached parameter schema on MCPToolPreviewInfo.Parameters,
// closing the gap between preview and the live conversation path.
func TestListToolsForAgent_CacheWithRealSchemaReturnsParameters(t *testing.T) {
	serverID := uuid.New()

	toolCache := map[string]store.CachedToolInfo{
		"pg_query": {
			Description: "Run PostgreSQL queries",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
		},
	}
	settings, err := json.Marshal(map[string]any{
		"tool_cache": toolCache,
	})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}

	mockStore := &mockMCPStore{
		accessible: []store.MCPAccessInfo{
			{
				Server: store.MCPServerData{
					BaseModel: store.BaseModel{ID: serverID},
					Name:      "postgres",
					Enabled:   true,
					Settings:  settings,
				},
				ToolAllow: nil, // unrestricted grant
				ToolDeny:  nil,
			},
		},
	}

	mgr := NewManager(tools.NewRegistry(), WithStore(mockStore))

	got, err := mgr.ListToolsForAgent(t.Context(), uuid.New(), "user-1")
	if err != nil {
		t.Fatalf("ListToolsForAgent: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 tool entry, got %d: %+v", len(got), got)
	}
	entry := got[0]
	if entry.RegisteredName != "mcp_postgres__pg_query" {
		t.Fatalf("unexpected registered name: %q", entry.RegisteredName)
	}
	if entry.Description != "Run PostgreSQL queries" {
		t.Fatalf("unexpected description: %q", entry.Description)
	}
	if len(entry.Parameters) == 0 {
		t.Fatal("expected real cached parameter schema, got empty")
	}
	var schema map[string]any
	if err := json.Unmarshal(entry.Parameters, &schema); err != nil {
		t.Fatalf("unmarshal cached parameters: %v", err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties in cached schema, got %+v", schema)
	}
	if _, ok := props["query"]; !ok {
		t.Errorf("expected 'query' property in cached schema, got %+v", props)
	}
}

// TestListToolsForAgent_LegacyCacheShapeDegradesGracefully verifies that old
// cache rows stored as a bare map[string]string (name -> description, from
// before schema caching existed) are handled gracefully: descriptions are
// preserved via the backward-compat fallback path, and Parameters is left
// nil (no schema ever cached) rather than crashing on the unmarshal error.
func TestListToolsForAgent_LegacyCacheShapeDegradesGracefully(t *testing.T) {
	serverID := uuid.New()

	legacyCache := map[string]string{
		"list_zones": "List DNS zones",
	}
	settings, err := json.Marshal(map[string]any{
		"tool_cache": legacyCache,
	})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}

	mockStore := &mockMCPStore{
		accessible: []store.MCPAccessInfo{
			{
				Server: store.MCPServerData{
					BaseModel: store.BaseModel{ID: serverID},
					Name:      "cloudflare",
					Enabled:   true,
					Settings:  settings,
				},
				ToolAllow: nil,
				ToolDeny:  nil,
			},
		},
	}

	mgr := NewManager(tools.NewRegistry(), WithStore(mockStore))

	got, err := mgr.ListToolsForAgent(t.Context(), uuid.New(), "user-1")
	if err != nil {
		t.Fatalf("ListToolsForAgent: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 tool entry from legacy cache, got %d: %+v", len(got), got)
	}
	if got[0].Description != "List DNS zones" {
		t.Fatalf("expected legacy description preserved, got %q", got[0].Description)
	}
	if got[0].Parameters != nil {
		t.Errorf("expected nil Parameters for legacy cache entry, got %s", got[0].Parameters)
	}
}

// TestListToolsForAgent_LegacyPrefixedToolAllowResolvesLiveDescriptions
// reproduces the production regression: an agent's MCP grant has a persisted
// tool_allow list containing OLD registered-name-shaped entries (e.g.
// "mcp_goclaw__goclaw_agent_get" instead of the bare "goclaw_agent_get"),
// captured before the grant-capture fix in mcp_tools.go. Before this fix,
// ListToolsForAgent looked up tool_cache/registry entries keyed by the raw
// stored name, which never matched the bare-name keys used elsewhere,
// silently producing empty descriptions/schemas for every tool. This test
// asserts descriptions and parameter schemas are still resolved correctly —
// both from a live-registered tool and from the settings tool_cache — by
// normalizing the legacy prefixed entries to bare names first.
func TestListToolsForAgent_LegacyPrefixedToolAllowResolvesLiveDescriptions(t *testing.T) {
	serverID := uuid.New()

	// One tool ("goclaw_agent_get") is live-registered in the tool registry
	// (simulating an already-connected self-hosted MCP server), and its
	// description/schema must come from there, not from settings.
	registry := tools.NewRegistry()
	clientPtr := &atomic.Pointer[mcpclient.Client]{}
	connected := &atomic.Bool{}
	connected.Store(true)
	liveTool := NewBridgeTool(
		"goclaw",
		mcpgo.Tool{
			Name:        "goclaw_agent_get",
			Description: "Fetch an agent by ID",
			InputSchema: mcpgo.ToolInputSchema{
				Type:       "object",
				Properties: map[string]any{"agent_id": map[string]any{"type": "string"}},
				Required:   []string{"agent_id"},
			},
		},
		clientPtr, "", 0, connected, uuid.Nil, nil,
	)
	registry.Register(liveTool)

	// The other tool ("goclaw_agent_list") has no live registration but does
	// have a settings tool_cache entry — description must come from there.
	toolCache := map[string]store.CachedToolInfo{
		"goclaw_agent_list": {
			Description: "List all agents",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		},
	}
	settings, err := json.Marshal(map[string]any{"tool_cache": toolCache})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}

	mockStore := &mockMCPStore{
		accessible: []store.MCPAccessInfo{
			{
				Server: store.MCPServerData{
					BaseModel: store.BaseModel{ID: serverID},
					Name:      "goclaw",
					Enabled:   true,
					Settings:  settings,
				},
				// Legacy-persisted entries: already-prefixed registered names,
				// exactly the shape a pre-fix grant capture would have stored.
				ToolAllow: []string{
					"mcp_goclaw__goclaw_agent_get",
					"mcp_goclaw__goclaw_agent_list",
				},
				ToolDeny: nil,
			},
		},
	}

	mgr := NewManager(registry, WithStore(mockStore))

	got, err := mgr.ListToolsForAgent(t.Context(), uuid.New(), "user-1")
	if err != nil {
		t.Fatalf("ListToolsForAgent: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 tool entries, got %d: %+v", len(got), got)
	}

	sort.Slice(got, func(i, j int) bool { return got[i].RegisteredName < got[j].RegisteredName })

	// Live-registered tool: description/schema resolved from the registry.
	live := got[0]
	if live.RegisteredName != "mcp_goclaw__goclaw_agent_get" {
		t.Fatalf("unexpected registered name: %q", live.RegisteredName)
	}
	if live.Description != "Fetch an agent by ID" {
		t.Fatalf("expected live description resolved from registry, got %q (has_desc=%v)", live.Description, live.Description != "")
	}
	if len(live.Parameters) == 0 {
		t.Fatal("expected live parameter schema resolved from registry, got empty")
	}
	var liveSchema map[string]any
	if err := json.Unmarshal(live.Parameters, &liveSchema); err != nil {
		t.Fatalf("unmarshal live parameters: %v", err)
	}
	if _, ok := liveSchema["properties"].(map[string]any)["agent_id"]; !ok {
		t.Errorf("expected 'agent_id' property in live schema, got %+v", liveSchema)
	}

	// Cache-only tool: description/schema resolved from settings tool_cache.
	cached := got[1]
	if cached.RegisteredName != "mcp_goclaw__goclaw_agent_list" {
		t.Fatalf("unexpected registered name: %q", cached.RegisteredName)
	}
	if cached.Description != "List all agents" {
		t.Fatalf("expected cached description resolved despite legacy prefixed tool_allow entry, got %q", cached.Description)
	}
	if len(cached.Parameters) == 0 {
		t.Fatal("expected cached parameter schema resolved despite legacy prefixed tool_allow entry, got empty")
	}
}

// TestListToolsForAgent_EmptyToolAllowNoCacheFallsBackToPlaceholder verifies
// that when a server has never been connected (no tool_cache present), the
// existing single-placeholder behavior is preserved.
func TestListToolsForAgent_EmptyToolAllowNoCacheFallsBackToPlaceholder(t *testing.T) {
	serverID := uuid.New()

	mockStore := &mockMCPStore{
		accessible: []store.MCPAccessInfo{
			{
				Server: store.MCPServerData{
					BaseModel: store.BaseModel{ID: serverID},
					Name:      "never-connected",
					Enabled:   true,
				},
				ToolAllow: nil,
				ToolDeny:  nil,
			},
		},
	}

	mgr := NewManager(tools.NewRegistry(), WithStore(mockStore))

	got, err := mgr.ListToolsForAgent(t.Context(), uuid.New(), "user-1")
	if err != nil {
		t.Fatalf("ListToolsForAgent: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected single placeholder entry, got %d: %+v", len(got), got)
	}
	if got[0].RegisteredName != "mcp_never_connected__*" {
		t.Fatalf("unexpected placeholder registered name: %q", got[0].RegisteredName)
	}
}
