package mcp

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/google/uuid"

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
