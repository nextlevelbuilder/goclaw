package mcp

import (
	"context"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// fakeBridgeTool is a minimal tools.Tool for registry-driven bridge tests.
type fakeBridgeTool struct{ name string }

func (f *fakeBridgeTool) Name() string               { return f.name }
func (f *fakeBridgeTool) Description() string        { return "fake " + f.name }
func (f *fakeBridgeTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (f *fakeBridgeTool) Execute(context.Context, map[string]any) *tools.Result {
	return &tools.Result{ForLLM: "ok"}
}

func bridgeTestRegistry(names ...string) *tools.Registry {
	reg := tools.NewRegistry()
	for _, n := range names {
		reg.Register(&fakeBridgeTool{name: n})
	}
	return reg
}

// Without an agent policy in ctx (anonymous caller, or agent without a
// tools_config), the bridge must fall back to the conservative legacy set —
// deriving from a nil policy would WIDEN exposure for unauthenticated callers.
func TestBridgeToolAllowed_NoAgentPolicy_FallsBackToLegacySet(t *testing.T) {
	reg := bridgeTestRegistry("read_file", "use_skill")
	pe := tools.NewPolicyEngine(&config.ToolsConfig{})
	ctx := context.Background()

	if !bridgeToolAllowed(reg, pe, ctx, "read_file") {
		t.Error("read_file is in the legacy set — must stay allowed for policy-less callers")
	}
	if bridgeToolAllowed(reg, pe, ctx, "use_skill") {
		t.Error("use_skill is NOT in the legacy set — must stay blocked for policy-less callers")
	}
}

// With a verified agent policy in ctx, the bridge derives its tool surface
// from that policy — the fix for the BridgeToolNames drift (#1373): tools the
// agent's policy allows (like use_skill) become callable; tools outside the
// agent's allow list are rejected even if the legacy set contained them.
func TestBridgeToolAllowed_AgentPolicyDerivesSurface(t *testing.T) {
	reg := bridgeTestRegistry("read_file", "use_skill", "datetime")
	pe := tools.NewPolicyEngine(&config.ToolsConfig{})
	ctx := tools.WithToolAgentPolicy(context.Background(),
		&config.ToolPolicySpec{Allow: []string{"use_skill", "datetime"}})

	if !bridgeToolAllowed(reg, pe, ctx, "use_skill") {
		t.Error("use_skill allowed by agent policy — bridge must expose it")
	}
	if !bridgeToolAllowed(reg, pe, ctx, "datetime") {
		t.Error("datetime allowed by agent policy — bridge must expose it")
	}
	if bridgeToolAllowed(reg, pe, ctx, "read_file") {
		t.Error("read_file NOT in the agent allow list — bridge must reject it")
	}
}

// Hard-excluded tools can never cross the bridge, regardless of policy.
func TestBridgeToolAllowed_HardExclusionsWinOverPolicy(t *testing.T) {
	reg := bridgeTestRegistry("spawn", "create_forum_topic")
	pe := tools.NewPolicyEngine(&config.ToolsConfig{})
	ctx := tools.WithToolAgentPolicy(context.Background(),
		&config.ToolPolicySpec{Allow: []string{"spawn", "create_forum_topic"}})

	for _, name := range []string{"spawn", "create_forum_topic"} {
		if bridgeToolAllowed(reg, pe, ctx, name) {
			t.Errorf("%s is bridge-excluded — policy must not re-enable it", name)
		}
	}
}

// Nil policy engine (legacy constructor path) keeps the legacy static behavior.
func TestBridgeToolAllowed_NilEngine_LegacyBehavior(t *testing.T) {
	reg := bridgeTestRegistry("exec", "use_skill")
	ctx := tools.WithToolAgentPolicy(context.Background(),
		&config.ToolPolicySpec{Allow: []string{"use_skill"}})

	if !bridgeToolAllowed(reg, nil, ctx, "exec") {
		t.Error("nil engine: legacy set must apply (exec allowed)")
	}
	if bridgeToolAllowed(reg, nil, ctx, "use_skill") {
		t.Error("nil engine: legacy set must apply (use_skill blocked)")
	}
}

// The tools/list filter must show each caller exactly the surface it can call.
func TestBridgeToolFilter_ListsMatchCallableSurface(t *testing.T) {
	reg := bridgeTestRegistry("read_file", "use_skill")
	pe := tools.NewPolicyEngine(&config.ToolsConfig{})
	filter := newBridgeToolFilter(reg, pe)

	listed := []mcpgo.Tool{{Name: "read_file"}, {Name: "use_skill"}}

	// Anonymous caller → legacy set only.
	got := filter(context.Background(), listed)
	if len(got) != 1 || got[0].Name != "read_file" {
		t.Errorf("anonymous list = %v, want [read_file]", toolNames(got))
	}

	// Policy'd agent → derived surface.
	ctx := tools.WithToolAgentPolicy(context.Background(),
		&config.ToolPolicySpec{Allow: []string{"use_skill"}})
	got = filter(ctx, listed)
	if len(got) != 1 || got[0].Name != "use_skill" {
		t.Errorf("policy'd list = %v, want [use_skill]", toolNames(got))
	}
}

// Registration must cover the whole registry minus hard exclusions, so a
// policy'd agent can actually call tools beyond the legacy set (the drift bug).
func TestBridgeRegisteredToolNames_RegistryMinusExclusions(t *testing.T) {
	reg := bridgeTestRegistry("read_file", "use_skill", "spawn")
	names := bridgeRegisteredToolNames(reg)

	got := make(map[string]bool, len(names))
	for _, n := range names {
		got[n] = true
	}
	if !got["read_file"] || !got["use_skill"] {
		t.Errorf("registered = %v, want read_file + use_skill included", names)
	}
	if got["spawn"] {
		t.Errorf("registered = %v, spawn must be excluded", names)
	}
}

func toolNames(ts []mcpgo.Tool) []string {
	out := make([]string, len(ts))
	for i, tt := range ts {
		out[i] = tt.Name
	}
	return out
}
