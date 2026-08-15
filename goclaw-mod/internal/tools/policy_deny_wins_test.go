package tools

// Tests proving deny always wins over AlsoAllow: previously AlsoAllow's
// unionWithSpec call ran AFTER Deny with no re-check, so a tool explicitly
// denied but also reachable via a "group:x" spec in AlsoAllow was silently
// reintroduced into the allowed set. See internal/tools/policy.go evaluate().

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// denyWinsMockTool is a minimal Tool for populating a Registry in tests.
type denyWinsMockTool struct{ name string }

func (m *denyWinsMockTool) Name() string               { return m.name }
func (m *denyWinsMockTool) Description() string        { return "mock " + m.name }
func (m *denyWinsMockTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (m *denyWinsMockTool) Execute(context.Context, map[string]any) *Result {
	return &Result{ForLLM: "ok"}
}

// TestFilterTools_DenyWinsOverAlsoAllowGroup proves that a tool explicitly
// listed in Deny is NOT reintroduced by an AlsoAllow spec that references a
// group containing that same tool.
func TestFilterTools_DenyWinsOverAlsoAllowGroup(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&denyWinsMockTool{name: "mcp_svc__get_data"})
	reg.Register(&denyWinsMockTool{name: "mcp_svc__put_data"})
	reg.RegisterToolGroup("mcp", []string{"mcp_svc__get_data", "mcp_svc__put_data"})

	pe := NewPolicyEngine(&config.ToolsConfig{
		Deny:      []string{"mcp_svc__get_data"},
		AlsoAllow: []string{"group:mcp"},
	})

	defs := pe.FilterTools(reg, "agent1", "openai", nil, nil, false, false)
	names := defNameSetGE(defs)

	if names["mcp_svc__get_data"] {
		t.Errorf("explicitly denied tool must NOT be reintroduced via AlsoAllow group expansion; got %v", names)
	}
	if !names["mcp_svc__put_data"] {
		t.Errorf("non-denied tool in the same AlsoAllow group must still be allowed; got %v", names)
	}
}

// TestFilterTools_DenyWinsOverAlsoAllowGroup_AgentPolicy proves the same
// deny-wins guarantee for the per-agent Deny/AlsoAllow lists.
func TestFilterTools_DenyWinsOverAlsoAllowGroup_AgentPolicy(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&denyWinsMockTool{name: "mcp_svc__get_data"})
	reg.Register(&denyWinsMockTool{name: "mcp_svc__put_data"})
	reg.RegisterToolGroup("mcp", []string{"mcp_svc__get_data", "mcp_svc__put_data"})

	pe := NewPolicyEngine(&config.ToolsConfig{})
	agentPolicy := &config.ToolPolicySpec{
		Deny:      []string{"mcp_svc__get_data"},
		AlsoAllow: []string{"group:mcp"},
	}

	defs := pe.FilterTools(reg, "agent1", "openai", agentPolicy, nil, false, false)
	names := defNameSetGE(defs)

	if names["mcp_svc__get_data"] {
		t.Errorf("agent-level denied tool must NOT be reintroduced via agent-level AlsoAllow group expansion; got %v", names)
	}
	if !names["mcp_svc__put_data"] {
		t.Errorf("non-denied tool in the same AlsoAllow group must still be allowed; got %v", names)
	}
}
