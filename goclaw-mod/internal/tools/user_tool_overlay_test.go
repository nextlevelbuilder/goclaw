package tools

// Tests for userToolOverlay — the request-scoped registry overlay that surfaces
// per-user MCP tools through the PolicyEngine without registering them in the shared
// registry. The decisive case is TestUserToolOverlay_PolicyDenyByNameSuppresses:
// it proves per-user MCP tools now honor an agent/global deny (Finding 1 fix), which
// the previous "append after FilterTools" approach bypassed.

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// overlayFakeTool is a minimal Tool standing in for a per-user MCP BridgeTool.
type overlayFakeTool struct{ name string }

func (f *overlayFakeTool) Name() string              { return f.name }
func (f *overlayFakeTool) Description() string        { return "fake " + f.name }
func (f *overlayFakeTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (f *overlayFakeTool) Execute(context.Context, map[string]any) *Result {
	return &Result{ForLLM: "ok"}
}

// emptyBaseExecutor is a ToolExecutor with no tools — stands in for a shared registry
// that does NOT contain the per-user MCP tools (the real-world condition).
type emptyBaseExecutor struct{}

func (emptyBaseExecutor) ExecuteWithContext(context.Context, string, map[string]any, string, string, string, string, AsyncCallback) *Result {
	return &Result{}
}
func (emptyBaseExecutor) TryActivateDeferred(string) bool          { return false }
func (emptyBaseExecutor) ProviderDefs() []providers.ToolDefinition { return nil }
func (emptyBaseExecutor) Get(string) (Tool, bool)                  { return nil, false }
func (emptyBaseExecutor) List() []string                           { return nil }
func (emptyBaseExecutor) Aliases() map[string]string               { return nil }

func defNameSet(defs []providers.ToolDefinition) map[string]bool {
	m := make(map[string]bool, len(defs))
	for _, d := range defs {
		if d.Function != nil {
			m[d.Function.Name] = true
		}
	}
	return m
}

// ─── Overlay exposes per-user tools via List/Get ──────────────────────────

func TestUserToolOverlay_AddsPerUserTools(t *testing.T) {
	ov := NewUserToolOverlay(emptyBaseExecutor{}, []Tool{
		&overlayFakeTool{name: "mcp_bx24__execute"},
		&overlayFakeTool{name: "mcp_bx24__search"},
	})

	list := ov.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 names in overlay List, got %d: %v", len(list), list)
	}
	if _, ok := ov.Get("mcp_bx24__execute"); !ok {
		t.Error("overlay must resolve per-user tool via Get")
	}
	if _, ok := ov.Get("does_not_exist"); ok {
		t.Error("overlay must not resolve unknown tools")
	}
}

// ─── Empty / all-nil userTools returns base unchanged ─────────────────────

func TestUserToolOverlay_EmptyReturnsBase(t *testing.T) {
	base := emptyBaseExecutor{}
	if ov := NewUserToolOverlay(base, nil); ov.List() != nil {
		t.Error("nil userTools should return base unchanged (nil List)")
	}
	if ov := NewUserToolOverlay(base, []Tool{nil, nil}); ov.List() != nil {
		t.Error("all-nil userTools should return base unchanged (nil List)")
	}
}

// ─── Policy with default (full) profile emits per-user tools ──────────────

func TestUserToolOverlay_PolicyEmitsByDefault(t *testing.T) {
	pe := NewPolicyEngine(&config.ToolsConfig{}) // empty profile = full = all allowed
	ov := NewUserToolOverlay(emptyBaseExecutor{}, []Tool{
		&overlayFakeTool{name: "mcp_bx24__execute"},
		&overlayFakeTool{name: "mcp_bx24__search"},
	})

	defs := pe.FilterTools(ov, "agent", "openai", nil, nil, false, false)
	names := defNameSet(defs)
	if !names["mcp_bx24__execute"] || !names["mcp_bx24__search"] {
		t.Errorf("full profile must emit per-user tools through the overlay; got %v", names)
	}
}

// ─── Finding 1 fix: an explicit deny on a per-user tool name suppresses it ─

func TestUserToolOverlay_PolicyDenyByNameSuppresses(t *testing.T) {
	pe := NewPolicyEngine(&config.ToolsConfig{Deny: []string{"mcp_bx24__execute"}})
	ov := NewUserToolOverlay(emptyBaseExecutor{}, []Tool{
		&overlayFakeTool{name: "mcp_bx24__execute"},
		&overlayFakeTool{name: "mcp_bx24__search"},
	})

	defs := pe.FilterTools(ov, "agent", "openai", nil, nil, false, false)
	names := defNameSet(defs)
	if names["mcp_bx24__execute"] {
		t.Error("denied per-user tool must NOT be emitted (Finding 1: policy must apply)")
	}
	if !names["mcp_bx24__search"] {
		t.Error("non-denied per-user tool must still be emitted")
	}
}
