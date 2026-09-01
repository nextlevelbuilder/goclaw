package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// newTestSkillLoader writes a single skill (name/SKILL.md) under a temp
// workspace and returns a Loader that resolves it via the flat
// <workspace>/skills/<name>/SKILL.md path (see skills.Loader.LoadSkill).
func newTestSkillLoader(t *testing.T, name, body string) *skills.Loader {
	t.Helper()
	workspace := t.TempDir()
	dir := filepath.Join(workspace, "skills", name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: test skill\n---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	return skills.NewLoader(workspace, "", "")
}

// TestUseSkillTool_NoReadFile_InlinesContent covers issue #1477: an agent
// whose resolved tool set does not include read_file has no way to follow up
// on the "activated" message, so Execute must inline the skill body directly.
func TestUseSkillTool_NoReadFile_InlinesContent(t *testing.T) {
	loader := newTestSkillLoader(t, "ck-plan", "SKILL BODY CONTENT")
	tool := NewUseSkillTool(loader)

	restricted := map[string]bool{"web_search": true} // no read_file
	ctx := store.WithAvailableToolNames(context.Background(), restricted)

	res := tool.Execute(ctx, map[string]any{"name": "ck-plan"})
	if res == nil {
		t.Fatal("Execute returned nil result")
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", res.ForLLM)
	}
	if res.ForLLM != "SKILL BODY CONTENT" {
		t.Fatalf("ForLLM = %q, want inlined skill body", res.ForLLM)
	}
}

// TestUseSkillTool_HasReadFile_BehaviorUnchanged is a regression check: an
// agent whose resolved tool set DOES include read_file must see the exact
// same "activated" message as before this change — byte for byte.
func TestUseSkillTool_HasReadFile_BehaviorUnchanged(t *testing.T) {
	loader := newTestSkillLoader(t, "ck-plan", "SKILL BODY CONTENT")
	tool := NewUseSkillTool(loader)

	allowed := map[string]bool{"read_file": true, "write_file": true}
	ctx := store.WithAvailableToolNames(context.Background(), allowed)

	res := tool.Execute(ctx, map[string]any{"name": "ck-plan"})
	if res == nil {
		t.Fatal("Execute returned nil result")
	}
	want := `Skill "ck-plan" activated. Proceed to read the skill's SKILL.md with read_file.`
	if res.ForLLM != want {
		t.Fatalf("ForLLM = %q, want %q", res.ForLLM, want)
	}
}

// TestUseSkillTool_NoAllowlistInContext_BehaviorUnchanged is the direction-2
// nil case: when the context carries no tool allowlist at all (e.g. a call
// path outside the pipeline's per-iteration tool dispatch, or an agent with
// no tool policy configured — see store.AvailableToolNamesFromContext), the
// tool must NOT assume read_file is missing. It must keep the original
// two-step behavior, since nil here means "no restriction known", not
// "nothing is available".
func TestUseSkillTool_NoAllowlistInContext_BehaviorUnchanged(t *testing.T) {
	loader := newTestSkillLoader(t, "ck-plan", "SKILL BODY CONTENT")
	tool := NewUseSkillTool(loader)

	res := tool.Execute(context.Background(), map[string]any{"name": "ck-plan"})
	if res == nil {
		t.Fatal("Execute returned nil result")
	}
	want := `Skill "ck-plan" activated. Proceed to read the skill's SKILL.md with read_file.`
	if res.ForLLM != want {
		t.Fatalf("ForLLM = %q, want %q (nil allowlist must not be treated as \"no tools available\")", res.ForLLM, want)
	}
}

// TestUseSkillTool_NoReadFile_SkillNotFound_ReturnsError covers the inlining
// path's failure mode: a restricted agent asking for a nonexistent skill must
// get a normal error result, not a panic or an empty inline.
func TestUseSkillTool_NoReadFile_SkillNotFound_ReturnsError(t *testing.T) {
	loader := newTestSkillLoader(t, "ck-plan", "SKILL BODY CONTENT")
	tool := NewUseSkillTool(loader)

	restricted := map[string]bool{"web_search": true}
	ctx := store.WithAvailableToolNames(context.Background(), restricted)

	res := tool.Execute(ctx, map[string]any{"name": "does-not-exist"})
	if res == nil {
		t.Fatal("Execute returned nil result")
	}
	if !res.IsError {
		t.Fatalf("expected IsError, got ForLLM = %q", res.ForLLM)
	}
}

// TestUseSkillTool_EmptyName_ReturnsError is unchanged pre-existing behavior.
func TestUseSkillTool_EmptyName_ReturnsError(t *testing.T) {
	tool := NewUseSkillTool(nil)
	res := tool.Execute(context.Background(), map[string]any{})
	if res == nil {
		t.Fatal("Execute returned nil result")
	}
	if !res.IsError {
		t.Fatalf("expected IsError for missing name, got ForLLM = %q", res.ForLLM)
	}
}
