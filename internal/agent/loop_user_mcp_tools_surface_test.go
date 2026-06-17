package agent

// Regression tests: per-user MCP tools (require_user_credentials servers) must be
// surfaced to the LLM tool list. Their objects live ONLY in l.mcpUserTools (cross-user
// isolation) and are NEVER in the shared registry, so the ONLY path that exposes their
// defs to the model is the userTools argument of buildFilteredTools. A prior regression
// (anti-leak hardening) dropped that merge → the model could never call mcp_<prefix>__*.
// See plans/reports/bugreport-260617-1708-per-user-mcp-tools-not-surfaced-to-llm.md.

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// fakeUserMCPTool is a minimal tools.Tool standing in for a per-user MCP BridgeTool.
type fakeUserMCPTool struct{ name string }

func (f *fakeUserMCPTool) Name() string              { return f.name }
func (f *fakeUserMCPTool) Description() string        { return "per-user mcp tool stub" }
func (f *fakeUserMCPTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (f *fakeUserMCPTool) Execute(context.Context, map[string]any) *tools.Result {
	return &tools.Result{ForLLM: "ok"}
}

// hasToolNamed reports whether defs contains a function tool with the given name.
func hasToolNamed(defs []providers.ToolDefinition, name string) bool {
	for _, d := range defs {
		if d.Function != nil && d.Function.Name == name {
			return true
		}
	}
	return false
}

// ─── Per-user MCP tools appear in the LLM tool list ───────────────────────

func TestUserMCPTools_SurfacedToLLM(t *testing.T) {
	l := buildImageGenLoop(false, &stubProvider{}) // toolPolicy nil → defs come only from userTools
	userTools := []tools.Tool{
		&fakeUserMCPTool{name: "mcp_bx24__search"},
		&fakeUserMCPTool{name: "mcp_bx24__execute"},
	}

	defs, _, _ := l.buildFilteredTools(&RunRequest{}, false, 1, 10, nil, userTools)

	if !hasToolNamed(defs, "mcp_bx24__search") || !hasToolNamed(defs, "mcp_bx24__execute") {
		t.Errorf("per-user MCP tools must surface to the LLM tool list; got %d defs: %v", len(defs), defs)
	}
}

// ─── Final iteration strips per-user MCP tools too (force text response) ───

func TestUserMCPTools_FinalIterationStripped(t *testing.T) {
	l := buildImageGenLoop(false, &stubProvider{})
	userTools := []tools.Tool{&fakeUserMCPTool{name: "mcp_bx24__execute"}}

	// iteration == maxIter → final stripping path: all tools removed, including per-user.
	defs, _, _ := l.buildFilteredTools(&RunRequest{}, false, 5, 5, nil, userTools)

	if len(defs) != 0 {
		t.Errorf("final iteration must strip per-user MCP tools; got %d: %v", len(defs), defs)
	}
}

// ─── Duplicate per-user tool names emit only one def ──────────────────────

func TestUserMCPTools_DedupByName(t *testing.T) {
	l := buildImageGenLoop(false, &stubProvider{})
	userTools := []tools.Tool{
		&fakeUserMCPTool{name: "mcp_bx24__execute"},
		&fakeUserMCPTool{name: "mcp_bx24__execute"}, // duplicate
	}

	defs, _, _ := l.buildFilteredTools(&RunRequest{}, false, 1, 10, nil, userTools)

	count := 0
	for _, d := range defs {
		if d.Function != nil && d.Function.Name == "mcp_bx24__execute" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("duplicate per-user tool name must emit exactly one def; got %d", count)
	}
}
