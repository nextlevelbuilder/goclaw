package tools

import "testing"

func TestRegisterToolGroup(t *testing.T) {
	// Register a new MCP group
	RegisterToolGroup("mcp:postgres", []string{"mcp_pg__query", "mcp_pg__list_tables"})

	members, ok := toolGroups["mcp:postgres"]
	if !ok {
		t.Fatal("expected mcp:postgres group to exist")
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members, got %d", len(members))
	}

	// Unregister
	UnregisterToolGroup("mcp:postgres")
	if _, ok := toolGroups["mcp:postgres"]; ok {
		t.Error("expected mcp:postgres group to be removed")
	}
}

func TestRegisterToolGroup_UsedInExpand(t *testing.T) {
	RegisterToolGroup("mcp:test", []string{"mcp_test__tool_a", "mcp_test__tool_b"})
	defer UnregisterToolGroup("mcp:test")

	available := []string{"mcp_test__tool_a", "mcp_test__tool_b", "read_file", "exec"}
	expanded := expandSpec(available, []string{"group:mcp:test"})

	if len(expanded) != 2 {
		t.Errorf("expected 2 tools from group:mcp:test, got %d: %v", len(expanded), expanded)
	}

	// Verify it works with subtractSpec too
	remaining := subtractSpec(available, []string{"group:mcp:test"})
	if len(remaining) != 2 {
		t.Errorf("expected 2 remaining after subtract, got %d: %v", len(remaining), remaining)
	}
}
