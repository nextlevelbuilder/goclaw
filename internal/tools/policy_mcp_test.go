package tools

import "testing"

func TestRegistry_RegisterToolGroup(t *testing.T) {
	reg := NewRegistry()

	// Register a new MCP group
	reg.RegisterToolGroup("mcp:postgres", []string{"mcp_pg__query", "mcp_pg__list_tables"})

	members, ok := reg.GetToolGroup("mcp:postgres")
	if !ok {
		t.Fatal("expected mcp:postgres group to exist")
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members, got %d", len(members))
	}

	// Unregister
	reg.UnregisterToolGroup("mcp:postgres")
	if _, ok := reg.GetToolGroup("mcp:postgres"); ok {
		t.Error("expected mcp:postgres group to be removed")
	}
}

func TestRegistry_RegisterToolGroup_UsedInExpand(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterToolGroup("mcp:test", []string{"mcp_test__tool_a", "mcp_test__tool_b"})
	defer reg.UnregisterToolGroup("mcp:test")

	available := []string{"mcp_test__tool_a", "mcp_test__tool_b", "read_file", "exec"}
	expanded := expandSpec(reg, available, []string{"group:mcp:test"})

	if len(expanded) != 2 {
		t.Errorf("expected 2 tools from group:mcp:test, got %d: %v", len(expanded), expanded)
	}

	// Verify it works with subtractSpec too
	remaining := subtractSpec(reg, available, []string{"group:mcp:test"})
	if len(remaining) != 2 {
		t.Errorf("expected 2 remaining after subtract, got %d: %v", len(remaining), remaining)
	}
}
