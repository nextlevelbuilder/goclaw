package tools

import (
	"context"
	"strings"
	"testing"
)

type fakeSkillLoader map[string]string

func (f fakeSkillLoader) LoadSkill(_ context.Context, name string) (string, bool) {
	content, ok := f[name]
	return content, ok
}

func TestUseSkillReturnsSkillInstructions(t *testing.T) {
	tool := NewUseSkillTool(fakeSkillLoader{
		"pr-review": "# PR Review\n\nRun prepare-worktree.",
	})

	res := tool.Execute(context.Background(), map[string]any{"name": "pr-review"})
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, `Skill "pr-review" activated`) {
		t.Fatalf("missing activation text: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Run prepare-worktree.") {
		t.Fatalf("missing skill body: %s", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, "Proceed to read") {
		t.Fatalf("should not instruct read_file when loader is configured: %s", res.ForLLM)
	}
}

func TestUseSkillMissingSkill(t *testing.T) {
	tool := NewUseSkillTool(fakeSkillLoader{})

	res := tool.Execute(context.Background(), map[string]any{"name": "missing"})
	if !res.IsError {
		t.Fatalf("expected missing skill error, got: %+v", res)
	}
	if !strings.Contains(res.ForLLM, `skill "missing" not found`) {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestUseSkillLegacyFallbackWithoutLoader(t *testing.T) {
	tool := NewUseSkillTool()

	res := tool.Execute(context.Background(), map[string]any{"name": "pr-review"})
	if res.IsError {
		t.Fatalf("expected fallback success, got error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Proceed to read") {
		t.Fatalf("expected legacy read_file fallback: %s", res.ForLLM)
	}
}
