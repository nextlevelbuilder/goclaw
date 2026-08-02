package agent

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// Blocking a canonical orchestration tool must also hide any advertised ALIAS of
// it: ProviderDefs advertises aliases as separate tool defs, so a run that blocks
// "spawn" must not leave "sessions_spawn" visible for the model to call. The
// advertisement filter canonicalizes the same way the execution-time deny does.
func TestBuildFilteredTools_BlockedToolsHideCanonicalAndAlias(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(metadataTestTool{name: tools.ToolNameSpawn})
	registry.Register(metadataTestTool{name: "write_file"})
	registry.RegisterAlias("sessions_spawn", tools.ToolNameSpawn)

	l := &Loop{
		provider:             &stubProvider{},
		allowImageGeneration: false,
		tools:                registry,
		registry:             registry,
	}

	defs, allowed, _ := l.buildFilteredTools(&RunRequest{BlockedTools: []string{tools.ToolNameSpawn}}, false, 1, 10, nil, nil)

	if hasFunctionTool(defs, tools.ToolNameSpawn) {
		t.Fatal("canonical spawn must be hidden when the run blocks it")
	}
	if hasFunctionTool(defs, "sessions_spawn") {
		t.Fatal("the sessions_spawn alias must be hidden when the run blocks canonical spawn")
	}
	if !hasFunctionTool(defs, "write_file") {
		t.Fatal("unblocked tools must remain visible")
	}
	if allowed != nil && (allowed[tools.ToolNameSpawn] || allowed["sessions_spawn"]) {
		t.Fatal("blocked spawn and its alias must be removed from the allowed execution map")
	}
}
