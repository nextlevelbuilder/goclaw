package tools

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestExecToolEffectiveDenyGroups_GlobalOnly: with an empty agent context,
// effectiveDenyGroups must return the global toggles set via SetGlobalShellDenyGroups.
func TestExecToolEffectiveDenyGroups_GlobalOnly(t *testing.T) {
	tool := NewExecTool("/tmp", false)
	tool.SetGlobalShellDenyGroups(map[string]bool{"package_install": false})

	got := tool.effectiveDenyGroups(context.Background())
	if v, ok := got["package_install"]; !ok || v != false {
		t.Fatalf("expected global package_install=false in effective map, got %v", got)
	}
}

// TestExecToolEffectiveDenyGroups_AgentOverridesGlobal: per-agent context
// override wins per-key over global; non-overridden global keys must remain.
func TestExecToolEffectiveDenyGroups_AgentOverridesGlobal(t *testing.T) {
	tool := NewExecTool("/tmp", false)
	tool.SetGlobalShellDenyGroups(map[string]bool{
		"package_install": false,
		"env_dump":        false,
	})

	ctx := store.WithShellDenyGroups(context.Background(), map[string]bool{"package_install": true})

	got := tool.effectiveDenyGroups(ctx)
	if v, ok := got["package_install"]; !ok || v != true {
		t.Errorf("expected agent override package_install=true, got %v (ok=%v)", v, ok)
	}
	if v, ok := got["env_dump"]; !ok || v != false {
		t.Errorf("expected global env_dump=false preserved, got %v (ok=%v)", v, ok)
	}
}

// TestExecToolEffectiveDenyGroups_NilGlobalReturnsAgent: when no global
// is configured, return the agent map (preserving existing per-agent semantics).
func TestExecToolEffectiveDenyGroups_NilGlobalReturnsAgent(t *testing.T) {
	tool := NewExecTool("/tmp", false)

	agent := map[string]bool{"foo": true}
	ctx := store.WithShellDenyGroups(context.Background(), agent)

	got := tool.effectiveDenyGroups(ctx)
	if v, ok := got["foo"]; !ok || v != true {
		t.Fatalf("expected agent map returned when global empty, got %v", got)
	}
}

// TestExecToolEffectiveDenyGroups_EmptyAgentReturnsGlobal: when agent
// context has no overrides, return the global map.
func TestExecToolEffectiveDenyGroups_EmptyAgentReturnsGlobal(t *testing.T) {
	tool := NewExecTool("/tmp", false)
	tool.SetGlobalShellDenyGroups(map[string]bool{"foo": true})

	got := tool.effectiveDenyGroups(context.Background())
	if v, ok := got["foo"]; !ok || v != true {
		t.Fatalf("expected global map returned when agent empty, got %v", got)
	}
}

// TestExecToolSetGlobalShellDenyGroups_DefensiveCopy: mutating the caller's
// map after SetGlobalShellDenyGroups must not affect the tool's internal state.
func TestExecToolSetGlobalShellDenyGroups_DefensiveCopy(t *testing.T) {
	tool := NewExecTool("/tmp", false)

	src := map[string]bool{"package_install": true}
	tool.SetGlobalShellDenyGroups(src)

	// Mutate the caller's map AFTER passing it in.
	src["package_install"] = false
	src["env_dump"] = true

	got := tool.effectiveDenyGroups(context.Background())
	if v := got["package_install"]; v != true {
		t.Errorf("expected internal copy to be insulated from caller mutation; got package_install=%v", v)
	}
	if _, ok := got["env_dump"]; ok {
		t.Errorf("expected internal copy to be insulated from new caller-side keys; got %v", got)
	}
}

// TestExecToolSetGlobalShellDenyGroups_EmptyClears: passing an empty map
// must clear the internal state, not retain a stale copy.
func TestExecToolSetGlobalShellDenyGroups_EmptyClears(t *testing.T) {
	tool := NewExecTool("/tmp", false)
	tool.SetGlobalShellDenyGroups(map[string]bool{"foo": true})
	tool.SetGlobalShellDenyGroups(map[string]bool{})

	got := tool.effectiveDenyGroups(context.Background())
	if len(got) != 0 {
		t.Fatalf("expected cleared global to yield empty effective map, got %v", got)
	}
}
