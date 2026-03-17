package mcp

import (
	"sync"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// makeBridgeTool creates a minimal BridgeTool for testing without a real MCP connection.
func makeBridgeTool(serverName, toolName string) *BridgeTool {
	return NewBridgeTool(serverName, mcpgo.Tool{
		Name:        toolName,
		Description: "test tool " + toolName,
		InputSchema: mcpgo.ToolInputSchema{Type: "object"},
	}, nil, "", 30, nil)
}

// setupSearchModeManager returns a Manager already in search mode with the given
// original tool names deferred. Bypasses real MCP connections by populating
// deferredTools directly.
func setupSearchModeManager(t *testing.T, serverName string, originalNames []string) (*Manager, *tools.Registry) {
	t.Helper()
	reg := tools.NewRegistry()
	m := NewManager(reg)

	m.mu.Lock()
	m.deferredTools = make(map[string]*BridgeTool, len(originalNames))
	m.activatedTools = make(map[string]struct{})
	m.searchMode = true
	for _, orig := range originalNames {
		bt := makeBridgeTool(serverName, orig)
		m.deferredTools[bt.Name()] = bt
	}
	m.mu.Unlock()

	return m, reg
}

// deferredCount returns the current number of deferred tools (read-locked).
func deferredCount(m *Manager) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.deferredTools)
}

// --- ActivateToolIfDeferred ---

func TestManager_ActivateToolIfDeferred_NotInDeferred(t *testing.T) {
	m, _ := setupSearchModeManager(t, "svc", []string{"get_data", "list_items"})
	if m.ActivateToolIfDeferred("mcp_svc__nonexistent") {
		t.Error("expected false for tool not in deferredTools")
	}
}

func TestManager_ActivateToolIfDeferred_Success(t *testing.T) {
	m, reg := setupSearchModeManager(t, "svc", []string{"get_data"})
	name := makeBridgeTool("svc", "get_data").Name()

	if !m.ActivateToolIfDeferred(name) {
		t.Fatalf("expected true: tool %q should be activatable", name)
	}

	// Tool must now be in the registry.
	if _, ok := reg.Get(name); !ok {
		t.Errorf("tool %q should be in registry after activation", name)
	}

	// Tool must be removed from deferredTools.
	m.mu.RLock()
	_, stillDeferred := m.deferredTools[name]
	m.mu.RUnlock()
	if stillDeferred {
		t.Error("tool should be removed from deferredTools after activation")
	}

	// Tool must appear in activatedTools.
	m.mu.RLock()
	_, activated := m.activatedTools[name]
	m.mu.RUnlock()
	if !activated {
		t.Error("tool should be tracked in activatedTools")
	}
}

func TestManager_ActivateToolIfDeferred_Idempotent(t *testing.T) {
	m, reg := setupSearchModeManager(t, "svc", []string{"get_data"})
	name := makeBridgeTool("svc", "get_data").Name()

	// First activation succeeds.
	if !m.ActivateToolIfDeferred(name) {
		t.Fatal("first activation should return true")
	}

	// Second call: tool is no longer in deferredTools but is in activatedTools.
	// Must return true (idempotent) so concurrent goroutines don't get blocked.
	second := m.ActivateToolIfDeferred(name)
	if !second {
		t.Error("second call should return true — tool is in activatedTools")
	}
	if _, ok := reg.Get(name); !ok {
		t.Error("tool should still be in registry after second call")
	}
}

func TestManager_ActivateToolIfDeferred_NotInSearchMode(t *testing.T) {
	reg := tools.NewRegistry()
	m := NewManager(reg)
	// searchMode = false, deferredTools = nil.
	if m.ActivateToolIfDeferred("any_tool") {
		t.Error("expected false when not in search mode")
	}
}

func TestManager_ActivateToolIfDeferred_IndependentPerTool(t *testing.T) {
	// Activating one tool must not affect others still in deferredTools.
	origNames := []string{"get_activities_by_day", "get_daily_stats", "list_activities", "get_baby", "record_growth"}
	m, reg := setupSearchModeManager(t, "tinytimeline", origNames)

	btA := makeBridgeTool("tinytimeline", "get_activities_by_day")
	btB := makeBridgeTool("tinytimeline", "get_daily_stats")

	if !m.ActivateToolIfDeferred(btA.Name()) {
		t.Fatalf("activation of %q failed", btA.Name())
	}
	if !m.ActivateToolIfDeferred(btB.Name()) {
		t.Fatalf("activation of %q failed", btB.Name())
	}

	// Two activated, three still deferred.
	if got := deferredCount(m); got != 3 {
		t.Errorf("expected 3 tools still deferred, got %d", got)
	}

	// Both activated tools are in the registry.
	for _, bt := range []*BridgeTool{btA, btB} {
		if _, ok := reg.Get(bt.Name()); !ok {
			t.Errorf("tool %q should be in registry", bt.Name())
		}
	}
}

func TestManager_ActivateToolIfDeferred_Concurrent(t *testing.T) {
	// Concurrently activate different tools — no data races, all end up in registry.
	origNames := []string{"tool_a", "tool_b", "tool_c", "tool_d", "tool_e"}
	m, reg := setupSearchModeManager(t, "svc", origNames)

	var wg sync.WaitGroup
	for _, orig := range origNames {
		name := makeBridgeTool("svc", orig).Name()
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			m.ActivateToolIfDeferred(n)
		}(name)
	}
	wg.Wait()

	for _, orig := range origNames {
		name := makeBridgeTool("svc", orig).Name()
		if _, ok := reg.Get(name); !ok {
			t.Errorf("tool %q should be in registry after concurrent activation", name)
		}
	}
}

func TestManager_ActivateToolIfDeferred_SameToolRace(t *testing.T) {
	// Many goroutines racing to activate the same tool — all must return true.
	m, reg := setupSearchModeManager(t, "svc", []string{"shared_tool"})
	name := makeBridgeTool("svc", "shared_tool").Name()

	const goroutines = 20
	results := make([]bool, goroutines)
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = m.ActivateToolIfDeferred(name)
		}(i)
	}
	wg.Wait()

	if _, ok := reg.Get(name); !ok {
		t.Error("tool should be in registry after concurrent activation")
	}
	for i, ok := range results {
		if !ok {
			t.Errorf("goroutine %d got false — all should return true (idempotent)", i)
		}
	}
}

// --- Registry.SetDeferredActivator wired to Manager.ActivateToolIfDeferred ---

func TestRegistry_WiredToManager_LazyActivation(t *testing.T) {
	// End-to-end: mirrors what resolver.go does when search mode is detected.
	// Verifies that TryActivateDeferred activates a deferred MCP tool correctly.
	m, reg := setupSearchModeManager(t, "tinytimeline", []string{"get_activities_by_day", "get_daily_stats"})
	reg.SetDeferredActivator(m.ActivateToolIfDeferred)

	name := makeBridgeTool("tinytimeline", "get_activities_by_day").Name()

	// Tool is NOT in registry before lazy activation (it is deferred).
	if _, ok := reg.Get(name); ok {
		t.Fatal("tool should not be in registry before lazy activation")
	}

	// Simulate what loop.go does: TryActivateDeferred when allowedTools blocks the call.
	if !reg.TryActivateDeferred(name) {
		t.Fatalf("TryActivateDeferred should succeed for deferred tool %q", name)
	}

	// Tool must now be in the registry.
	if _, ok := reg.Get(name); !ok {
		t.Errorf("tool %q should be in registry after TryActivateDeferred", name)
	}
}

func TestRegistry_WiredToManager_UnknownToolReturnsFalse(t *testing.T) {
	m, reg := setupSearchModeManager(t, "svc", []string{"tool_a"})
	reg.SetDeferredActivator(m.ActivateToolIfDeferred)

	if reg.TryActivateDeferred("mcp_svc__nonexistent") {
		t.Error("expected false for a tool that was never deferred")
	}
}

func TestRegistry_WiredToManager_ActivatedToolAppearsInList(t *testing.T) {
	// After TryActivateDeferred, the tool must appear in registry.List() so
	// FilterTools includes it in the allowed set on the next loop iteration.
	m, reg := setupSearchModeManager(t, "svc", []string{"get_data"})
	reg.SetDeferredActivator(m.ActivateToolIfDeferred)

	name := makeBridgeTool("svc", "get_data").Name()

	// Not in List() before activation.
	for _, n := range reg.List() {
		if n == name {
			t.Fatal("tool should not be in List() before activation")
		}
	}

	reg.TryActivateDeferred(name)

	// Appears in List() after activation.
	found := false
	for _, n := range reg.List() {
		if n == name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("tool %q should appear in registry.List() after activation", name)
	}
}

func TestRegistry_WiredToManager_MultipleActivations_AllInList(t *testing.T) {
	origNames := []string{"get_activities_by_day", "get_daily_stats", "list_babies"}
	m, reg := setupSearchModeManager(t, "tinytimeline", origNames)
	reg.SetDeferredActivator(m.ActivateToolIfDeferred)

	for _, orig := range origNames {
		name := makeBridgeTool("tinytimeline", orig).Name()
		if !reg.TryActivateDeferred(name) {
			t.Errorf("TryActivateDeferred failed for %q", name)
		}
	}

	listed := make(map[string]bool, len(reg.List()))
	for _, n := range reg.List() {
		listed[n] = true
	}
	for _, orig := range origNames {
		name := makeBridgeTool("tinytimeline", orig).Name()
		if !listed[name] {
			t.Errorf("tool %q missing from registry.List() after activation", name)
		}
	}
	// deferredTools should now be empty.
	if got := deferredCount(m); got != 0 {
		t.Errorf("expected 0 deferred tools after all activations, got %d", got)
	}
}
