package tools

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestExecToolEffectiveDenyGroups_GlobalOnly(t *testing.T) {
	tool := NewExecTool(".", true)
	tool.SetGlobalShellDenyGroups(map[string]bool{
		"package_install": false,
	})

	got := tool.effectiveDenyGroups(context.Background())
	if got["package_install"] != false {
		t.Fatalf("package_install = %v, want false", got["package_install"])
	}
}

func TestExecToolEffectiveDenyGroups_AgentOverridesGlobal(t *testing.T) {
	tool := NewExecTool(".", true)
	tool.SetGlobalShellDenyGroups(map[string]bool{
		"package_install": false,
		"env_dump":        false,
	})

	ctx := store.WithShellDenyGroups(context.Background(), map[string]bool{
		"package_install": true, // override global=false
	})
	got := tool.effectiveDenyGroups(ctx)
	if got["package_install"] != true {
		t.Fatalf("package_install = %v, want true (agent override)", got["package_install"])
	}
	if got["env_dump"] != false {
		t.Fatalf("env_dump = %v, want false (global fallback)", got["env_dump"])
	}
}
