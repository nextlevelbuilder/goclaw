package agent

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// mockExecTool is a simple tool that records whether it was executed.
type mockExecTool struct {
	name    string
	executed bool
}

func (m *mockExecTool) Name() string        { return m.name }
func (m *mockExecTool) Description() string { return "mock tool" }
func (m *mockExecTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (m *mockExecTool) Execute(_ context.Context, _ map[string]any) *tools.Result {
	m.executed = true
	return tools.NewResult("ok from " + m.name)
}

// simulateLazyActivationCheck mimics the allowedTools check from loop.go for one tool call:
//
//	if allowedTools != nil && !allowedTools[tc.Name] {
//	    if l.tools.TryActivateDeferred(tc.Name) { allowedTools[tc.Name] = true }
//	    else { result = ErrorResult(...) }
//	}
//	if result == nil { result = l.tools.ExecuteWithContext(...) }
//
// Returns (result, blocked).
func simulateLazyActivationCheck(reg *tools.Registry, allowedTools map[string]bool, toolName string) (*tools.Result, bool) {
	var result *tools.Result
	blocked := false
	if allowedTools != nil && !allowedTools[toolName] {
		if reg.TryActivateDeferred(toolName) {
			allowedTools[toolName] = true
		} else {
			result = tools.ErrorResult("tool not allowed by policy: " + toolName)
			blocked = true
		}
	}
	if result == nil {
		result = reg.ExecuteWithContext(context.Background(), toolName, nil, "", "", "", "", nil)
	}
	return result, blocked
}

// --- Lazy activation in loop policy check ---

func TestLoop_LazyMCP_Blocked_NoActivator(t *testing.T) {
	// When no deferredActivator is set, a tool not in allowedTools is blocked.
	reg := tools.NewRegistry()
	tool := &mockExecTool{name: "mcp_svc__get_data"}
	reg.Register(tool)

	allowedTools := map[string]bool{"mcp_svc__other_tool": true}

	result, blocked := simulateLazyActivationCheck(reg, allowedTools, "mcp_svc__get_data")
	if !blocked {
		t.Error("expected tool to be blocked when no deferredActivator is set")
	}
	if !result.IsError {
		t.Error("expected error result for blocked tool")
	}
	if tool.executed {
		t.Error("tool must not execute when blocked")
	}
}

func TestLoop_LazyMCP_Allowed_DirectlyInAllowedTools(t *testing.T) {
	// Tool already in allowedTools — no lazy activation needed, executed directly.
	reg := tools.NewRegistry()
	tool := &mockExecTool{name: "mcp_svc__get_data"}
	reg.Register(tool)

	allowedTools := map[string]bool{"mcp_svc__get_data": true}

	result, blocked := simulateLazyActivationCheck(reg, allowedTools, "mcp_svc__get_data")
	if blocked {
		t.Error("expected tool to be allowed (it is in allowedTools)")
	}
	if result.IsError {
		t.Errorf("expected success, got error: %s", result.ForLLM)
	}
	if !tool.executed {
		t.Error("tool should have been executed")
	}
}

func TestLoop_LazyMCP_Activated_WhenActivatorSucceeds(t *testing.T) {
	// Tool not in allowedTools, but deferredActivator registers it → allowed.
	reg := tools.NewRegistry()
	tool := &mockExecTool{name: "mcp_svc__get_data"}

	// Activator: registers the tool (simulates MCP Manager.ActivateToolIfDeferred).
	reg.SetDeferredActivator(func(name string) bool {
		if name == "mcp_svc__get_data" {
			reg.Register(tool)
			return true
		}
		return false
	})

	allowedTools := map[string]bool{"mcp_svc__other": true}

	result, blocked := simulateLazyActivationCheck(reg, allowedTools, "mcp_svc__get_data")
	if blocked {
		t.Error("expected tool to be lazily activated, not blocked")
	}
	if result.IsError {
		t.Errorf("expected success after lazy activation, got: %s", result.ForLLM)
	}
	if !tool.executed {
		t.Error("tool should have been executed after lazy activation")
	}
	// allowedTools must be updated for subsequent calls in the same iteration.
	if !allowedTools["mcp_svc__get_data"] {
		t.Error("allowedTools should be updated after lazy activation")
	}
}

func TestLoop_LazyMCP_Blocked_WhenActivatorFails(t *testing.T) {
	// Tool not in allowedTools, and activator cannot activate it → blocked.
	reg := tools.NewRegistry()
	tool := &mockExecTool{name: "mcp_svc__unknown"}
	reg.Register(tool)

	reg.SetDeferredActivator(func(name string) bool { return false })
	allowedTools := map[string]bool{}

	result, blocked := simulateLazyActivationCheck(reg, allowedTools, "mcp_svc__unknown")
	if !blocked {
		t.Error("expected tool to be blocked when activator returns false")
	}
	if !result.IsError {
		t.Error("expected error result")
	}
	if tool.executed {
		t.Error("tool must not execute when blocked")
	}
}

func TestLoop_LazyMCP_NilAllowedTools_AllowsAll(t *testing.T) {
	// nil allowedTools means no policy filtering — all tools allowed.
	reg := tools.NewRegistry()
	tool := &mockExecTool{name: "any_tool"}
	reg.Register(tool)

	result, blocked := simulateLazyActivationCheck(reg, nil, "any_tool")
	if blocked {
		t.Error("nil allowedTools should allow all tools")
	}
	if result.IsError {
		t.Errorf("expected success with nil allowedTools: %s", result.ForLLM)
	}
	if !tool.executed {
		t.Error("tool should execute when allowedTools is nil")
	}
}

func TestLoop_LazyMCP_SecondCall_UsesUpdatedAllowedTools(t *testing.T) {
	// After first lazy activation, subsequent calls in the same iteration use the
	// updated allowedTools map and don't invoke the activator again.
	reg := tools.NewRegistry()
	tool := &mockExecTool{name: "mcp_svc__get_data"}

	activatorCalls := 0
	reg.SetDeferredActivator(func(name string) bool {
		activatorCalls++
		if name == "mcp_svc__get_data" {
			reg.Register(tool)
			return true
		}
		return false
	})

	allowedTools := map[string]bool{}

	// First call: lazy activator fires.
	simulateLazyActivationCheck(reg, allowedTools, "mcp_svc__get_data")
	if activatorCalls != 1 {
		t.Errorf("expected 1 activator call, got %d", activatorCalls)
	}

	// Second call in same iteration: tool is now in allowedTools, activator not called again.
	tool.executed = false
	simulateLazyActivationCheck(reg, allowedTools, "mcp_svc__get_data")
	if activatorCalls != 1 {
		t.Errorf("expected still 1 activator call after second use, got %d", activatorCalls)
	}
	if !tool.executed {
		t.Error("tool should execute on second call too")
	}
}

// --- Policy engine sees lazily activated tools on next FilterTools call ---

func TestLoop_LazyMCP_PolicySeesToolAfterActivation(t *testing.T) {
	// Verify: after lazy activation adds a tool to the registry,
	// FilterTools picks it up on the next iteration (when allowedTools is rebuilt).
	reg := tools.NewRegistry()
	tool := &mockExecTool{name: "mcp_svc__get_data"}

	reg.SetDeferredActivator(func(name string) bool {
		if name == "mcp_svc__get_data" {
			reg.Register(tool)
			return true
		}
		return false
	})

	pe := tools.NewPolicyEngine(&config.ToolsConfig{}) // full profile, no restrictions

	// Before activation: tool not in FilterTools result.
	defs := pe.FilterTools(reg, "agent1", "gemini-native", nil, nil, false, false)
	for _, d := range defs {
		if d.Function.Name == "mcp_svc__get_data" {
			t.Fatal("tool should not appear in FilterTools before activation")
		}
	}

	// Lazy-activate the tool (simulates what loop.go does when policy blocks the call).
	reg.TryActivateDeferred("mcp_svc__get_data")

	// On next iteration FilterTools is called again — tool must now be included.
	defs = pe.FilterTools(reg, "agent1", "gemini-native", nil, nil, false, false)
	found := false
	for _, d := range defs {
		if d.Function.Name == "mcp_svc__get_data" {
			found = true
			break
		}
	}
	if !found {
		t.Error("tool should appear in FilterTools after lazy activation")
	}
}

func TestLoop_LazyMCP_PolicyDenyList_StillBlocked(t *testing.T) {
	// A tool in the deny list must NOT be lazy-activated.
	// The loop's policy check fires BEFORE lazy activation: if allowedTools (built from
	// FilterTools) already excludes the tool due to deny, the tool is deferred but also denied.
	// TryActivateDeferred may succeed, but FilterTools will still exclude it on rebuild.
	reg := tools.NewRegistry()

	activated := false
	reg.SetDeferredActivator(func(name string) bool {
		if name == "mcp_svc__exec_cmd" {
			reg.Register(&mockExecTool{name: name})
			activated = true
			return true
		}
		return false
	})

	// Policy with explicit deny.
	pe := tools.NewPolicyEngine(&config.ToolsConfig{
		Deny: []string{"mcp_svc__exec_cmd"},
	})

	// Pre-activate via TryActivateDeferred (simulates lazy activation in the loop).
	reg.TryActivateDeferred("mcp_svc__exec_cmd")
	if !activated {
		t.Skip("activator did not fire — tool was never deferred in this setup")
	}

	// FilterTools must still exclude the denied tool on the next iteration.
	defs := pe.FilterTools(reg, "agent1", "gemini-native", nil, nil, false, false)
	for _, d := range defs {
		if d.Function.Name == "mcp_svc__exec_cmd" {
			t.Error("denied tool should not appear in FilterTools even after activation")
		}
	}
}

func TestLoop_LazyMCP_DenyList_BlockedOnFirstCall(t *testing.T) {
	// Simulates the FULL loop behavior: TryActivateDeferred succeeds but IsDenied
	// blocks the tool from executing on the CURRENT iteration (not just the next).
	reg := tools.NewRegistry()
	tool := &mockExecTool{name: "mcp_svc__exec_cmd"}

	reg.SetDeferredActivator(func(name string) bool {
		if name == "mcp_svc__exec_cmd" {
			reg.Register(tool)
			return true
		}
		return false
	})

	pe := tools.NewPolicyEngine(&config.ToolsConfig{
		Deny: []string{"mcp_svc__exec_cmd"},
	})
	allowedTools := map[string]bool{}

	// Simulate loop.go's lazy activation + deny check.
	var result *tools.Result
	toolName := "mcp_svc__exec_cmd"
	if allowedTools != nil && !allowedTools[toolName] {
		if reg.TryActivateDeferred(toolName) {
			// This is the NEW deny check added by the fix.
			if pe.IsDenied(toolName, nil) {
				result = tools.ErrorResult("tool not allowed by policy: " + toolName)
			} else {
				allowedTools[toolName] = true
			}
		} else {
			result = tools.ErrorResult("tool not allowed by policy: " + toolName)
		}
	}
	if result == nil {
		result = reg.ExecuteWithContext(context.Background(), toolName, nil, "", "", "", "", nil)
	}

	if !result.IsError {
		t.Error("denied tool must be blocked even after lazy activation")
	}
	if tool.executed {
		t.Error("denied tool must not execute")
	}
	if allowedTools[toolName] {
		t.Error("denied tool must not be added to allowedTools")
	}
}

func TestLoop_LazyMCP_DenyList_GroupDeny(t *testing.T) {
	// Verify deny via group: pattern also blocks lazy activation.
	reg := tools.NewRegistry()
	tool := &mockExecTool{name: "mcp_svc__get_data"}

	reg.SetDeferredActivator(func(name string) bool {
		reg.Register(tool)
		return true
	})

	// Register a custom group containing the MCP tool (using per-Registry method).
	reg.RegisterToolGroup("mcp_test", []string{"mcp_svc__get_data"})
	defer reg.UnregisterToolGroup("mcp_test")

	pe := tools.NewPolicyEngine(&config.ToolsConfig{
		Deny: []string{"group:mcp_test"},
	})
	// PolicyEngine needs registry to expand group:mcp_test
	pe.SetRegistry(reg)

	if !pe.IsDenied("mcp_svc__get_data", nil) {
		t.Error("tool should be denied via group: pattern")
	}
}
