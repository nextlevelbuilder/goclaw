package tools

import (
	"slices"
	"sync"
	"testing"
)

// TestToolGroups_PerRegistry_Isolation verifies that each Registry has isolated
// tool groups. When two registries (representing two agent Loops) register their
// MCP tools to their own "mcp" group, they don't pollute each other.
//
// This test PASSES after Phase 03 fix because toolGroups is now per-Registry.
func TestToolGroups_PerRegistry_Isolation(t *testing.T) {
	// Create two separate registries (simulating two agent Loops)
	regA := NewRegistry()
	regB := NewRegistry()

	// Agent A registers its MCP tools to its registry
	toolsA := []string{"mcp_serverA__toolA1", "mcp_serverA__toolA2"}
	regA.RegisterToolGroup("mcp", toolsA)

	// Agent B registers its MCP tools to its registry
	toolsB := []string{"mcp_serverB__toolB1", "mcp_serverB__toolB2"}
	regB.RegisterToolGroup("mcp", toolsB)

	// Agent A expands group:mcp from their registry — should see ONLY toolsA
	allToolsA := append(append([]string{}, toolsA...), toolsB...)
	expandedA := regA.ExpandToolGroups(allToolsA, []string{"group:mcp"})

	// Agent B expands group:mcp from their registry — should see ONLY toolsB
	allToolsB := append(append([]string{}, toolsA...), toolsB...)
	expandedB := regB.ExpandToolGroups(allToolsB, []string{"group:mcp"})

	// Verify isolation: A sees only A's tools
	if !containsTool(expandedA, "mcp_serverA__toolA1") || !containsTool(expandedA, "mcp_serverA__toolA2") {
		t.Errorf("Agent A should see their own tools, got: %v", expandedA)
	}
	if containsTool(expandedA, "mcp_serverB__toolB1") || containsTool(expandedA, "mcp_serverB__toolB2") {
		t.Errorf("Agent A should NOT see Agent B's tools, got: %v", expandedA)
	}

	// Verify isolation: B sees only B's tools
	if !containsTool(expandedB, "mcp_serverB__toolB1") || !containsTool(expandedB, "mcp_serverB__toolB2") {
		t.Errorf("Agent B should see their own tools, got: %v", expandedB)
	}
	if containsTool(expandedB, "mcp_serverA__toolA1") || containsTool(expandedB, "mcp_serverA__toolA2") {
		t.Errorf("Agent B should NOT see Agent A's tools, got: %v", expandedB)
	}
}

// TestToolGroups_PerRegistry_ConcurrentWrite verifies concurrent writes to
// the same Registry are safe (no data race) but last-writer-wins within
// that registry is expected behavior.
func TestToolGroups_PerRegistry_ConcurrentWrite(t *testing.T) {
	reg := NewRegistry()

	var wg sync.WaitGroup
	wg.Add(2)

	toolsA := []string{"toolA1", "toolA2"}
	toolsB := []string{"toolB1", "toolB2"}

	barrier := make(chan struct{})

	go func() {
		defer wg.Done()
		<-barrier
		reg.RegisterToolGroup("mcp", toolsA)
	}()

	go func() {
		defer wg.Done()
		<-barrier
		reg.RegisterToolGroup("mcp", toolsB)
	}()

	close(barrier)
	wg.Wait()

	// One of them wins — that's expected within a single registry
	// The important thing is: no data race (run with -race flag)
	members, ok := reg.GetToolGroup("mcp")
	if !ok {
		t.Fatal("expected mcp group to exist")
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members (from winner), got %d: %v", len(members), members)
	}
}

// TestToolGroups_Clone_DeepCopy verifies that Clone() deep-copies toolGroups
// so modifications to the clone don't affect the original.
func TestToolGroups_Clone_DeepCopy(t *testing.T) {
	orig := NewRegistry()
	orig.RegisterToolGroup("mcp", []string{"tool1", "tool2"})

	clone := orig.Clone()

	// Modify clone's mcp group
	clone.RegisterToolGroup("mcp", []string{"tool3", "tool4"})

	// Original should still have tool1, tool2
	members, ok := orig.GetToolGroup("mcp")
	if !ok {
		t.Fatal("original mcp group should exist")
	}
	if !containsTool(members, "tool1") || !containsTool(members, "tool2") {
		t.Errorf("original mcp group modified, got: %v", members)
	}
	if containsTool(members, "tool3") || containsTool(members, "tool4") {
		t.Errorf("original mcp group polluted by clone, got: %v", members)
	}
}

// TestToolGroups_MergeToolGroup_Additive verifies MergeToolGroup behavior.
func TestToolGroups_MergeToolGroup_Additive(t *testing.T) {
	reg := NewRegistry()

	// First merge
	reg.MergeToolGroup("test_merge", []string{"tool1", "tool2"})

	// Second merge (should add, not replace)
	reg.MergeToolGroup("test_merge", []string{"tool2", "tool3"})

	// Expand
	allTools := []string{"tool1", "tool2", "tool3", "tool4"}
	expanded := reg.ExpandToolGroups(allTools, []string{"group:test_merge"})

	// Should have tool1, tool2, tool3 (merged)
	if !containsTool(expanded, "tool1") {
		t.Error("tool1 missing from merged group")
	}
	if !containsTool(expanded, "tool2") {
		t.Error("tool2 missing from merged group")
	}
	if !containsTool(expanded, "tool3") {
		t.Error("tool3 missing from merged group")
	}
}

// TestToolGroups_BuiltinGroups_Seeded verifies that NewRegistry() seeds
// builtin tool groups from builtinToolGroups.
func TestToolGroups_BuiltinGroups_Seeded(t *testing.T) {
	reg := NewRegistry()

	// Check that builtin groups are present
	memory, ok := reg.GetToolGroup("memory")
	if !ok {
		t.Fatal("expected 'memory' builtin group to exist")
	}
	if !containsTool(memory, "memory_search") {
		t.Errorf("memory group should contain memory_search, got: %v", memory)
	}

	web, ok := reg.GetToolGroup("web")
	if !ok {
		t.Fatal("expected 'web' builtin group to exist")
	}
	if !containsTool(web, "web_search") || !containsTool(web, "web_fetch") {
		t.Errorf("web group should contain web_search and web_fetch, got: %v", web)
	}
}

func containsTool(tools []string, name string) bool {
	return slices.Contains(tools, name)
}
