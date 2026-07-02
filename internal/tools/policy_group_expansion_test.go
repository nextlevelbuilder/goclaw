package tools

// Tests proving the group-expansion fix: PolicyEngine.registry was previously a
// field only set via SetRegistry() (never called in production, since PolicyEngine
// is a shared singleton and mutating a field per-call would be a data race across
// concurrent requests). That meant every "group:xxx" spec entry silently expanded
// to nothing in production. The fix threads the concrete *Registry through as an
// explicit parameter (derived from FilterTools' registry argument), with no
// SetRegistry() call required — these tests exercise exactly that path.

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// groupExpansionMockTool is a minimal Tool for populating a Registry in tests.
type groupExpansionMockTool struct{ name string }

func (m *groupExpansionMockTool) Name() string               { return m.name }
func (m *groupExpansionMockTool) Description() string        { return "mock " + m.name }
func (m *groupExpansionMockTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (m *groupExpansionMockTool) Execute(context.Context, map[string]any) *Result {
	return &Result{ForLLM: "ok"}
}

// TestFilterTools_GroupExpansion_PlainRegistry proves that a "group:mcp" allow
// spec expands to the actual tool names registered in a plain *Registry passed
// directly to FilterTools, with NO SetRegistry() call ever made — matching the
// production code path (toolPE.SetRegistry was never called anywhere).
func TestFilterTools_GroupExpansion_PlainRegistry(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&groupExpansionMockTool{name: "mcp_svc__get_data"})
	reg.Register(&groupExpansionMockTool{name: "mcp_svc__put_data"})
	reg.Register(&groupExpansionMockTool{name: "unrelated_tool"})
	reg.RegisterToolGroup("mcp", []string{"mcp_svc__get_data", "mcp_svc__put_data"})

	pe := NewPolicyEngine(&config.ToolsConfig{
		Allow: []string{"group:mcp"},
	})

	defs := pe.FilterTools(reg, "agent1", "openai", nil, nil, false, false)
	names := defNameSetGE(defs)

	if !names["mcp_svc__get_data"] || !names["mcp_svc__put_data"] {
		t.Errorf("group:mcp allow spec must expand to registered group members; got %v", names)
	}
	if names["unrelated_tool"] {
		t.Errorf("tool outside group:mcp must not be allowed via group expansion; got %v", names)
	}
}

// TestFilterTools_GroupExpansion_UserToolOverlay proves the same group-expansion
// behavior works when FilterTools is called with a userToolOverlay wrapping a
// *Registry (the loop_tool_filter.go production path when an actor has per-user
// MCP tools) — proving the Unwrap() fallback resolves the concrete registry.
func TestFilterTools_GroupExpansion_UserToolOverlay(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&groupExpansionMockTool{name: "mcp_svc__get_data"})
	reg.RegisterToolGroup("mcp", []string{"mcp_svc__get_data", "mcp_bx24__execute"})

	ov := NewUserToolOverlay(reg, []Tool{
		&groupExpansionMockTool{name: "mcp_bx24__execute"},
	})

	pe := NewPolicyEngine(&config.ToolsConfig{
		Allow: []string{"group:mcp"},
	})

	defs := pe.FilterTools(ov, "agent1", "openai", nil, nil, false, false)
	names := defNameSetGE(defs)

	if !names["mcp_svc__get_data"] {
		t.Errorf("group:mcp must still expand base registry tools through the overlay; got %v", names)
	}
	if !names["mcp_bx24__execute"] {
		t.Errorf("group:mcp must expand to include the per-user overlay tool that is a group member; got %v", names)
	}
}

// TestFilterTools_GroupExpansion_DenyGroup proves "group:xxx" also expands
// correctly on the deny side (subtractSpec) without any SetRegistry() call.
func TestFilterTools_GroupExpansion_DenyGroup(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&groupExpansionMockTool{name: "mcp_svc__get_data"})
	reg.Register(&groupExpansionMockTool{name: "safe_tool"})
	reg.RegisterToolGroup("mcp", []string{"mcp_svc__get_data"})

	pe := NewPolicyEngine(&config.ToolsConfig{
		Deny: []string{"group:mcp"},
	})

	defs := pe.FilterTools(reg, "agent1", "openai", nil, nil, false, false)
	names := defNameSetGE(defs)

	if names["mcp_svc__get_data"] {
		t.Errorf("group:mcp deny spec must expand and exclude group members; got %v", names)
	}
	if !names["safe_tool"] {
		t.Errorf("tool outside denied group must remain allowed; got %v", names)
	}
}

// TestPolicyEngine_WouldAllow_GroupExpansion proves WouldAllow (used by the MCP
// bridge server) also correctly expands group specs when passed a concrete
// *Registry, without any SetRegistry() call.
func TestPolicyEngine_WouldAllow_GroupExpansion(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterToolGroup("mcp", []string{"mcp_svc__get_data"})

	pe := NewPolicyEngine(&config.ToolsConfig{
		Deny: []string{"group:mcp"},
	})

	if pe.WouldAllow(reg, "mcp_svc__get_data", "claude-cli", nil, nil) {
		t.Error("WouldAllow must respect group:mcp deny expansion")
	}
	if !pe.WouldAllow(reg, "other_tool", "claude-cli", nil, nil) {
		t.Error("WouldAllow must allow a tool outside the denied group")
	}
}

func defNameSetGE(defs []providers.ToolDefinition) map[string]bool {
	m := make(map[string]bool, len(defs))
	for _, d := range defs {
		if d.Function != nil {
			m[d.Function.Name] = true
		}
	}
	return m
}
