package gc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
)

// writeSkill writes a SKILL.md into root/slug so a skills.Loader rooted at
// root discovers it as a "global" skill.
func writeSkill(t *testing.T, root, slug, name, description, body string) {
	t.Helper()
	dir := filepath.Join(root, slug)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func newTestExecutor(t *testing.T) (*Executor, *Registry) {
	t.Helper()
	t.Setenv("GOCLAW_DISABLE_PERSONAL_SKILLS", "1")
	root := t.TempDir()
	writeSkill(t, root, "plan", "Plan", "Plan implementations.", "Understand, inspect, plan, verify.")
	writeSkill(t, root, "fix", "Fix", "Fix bugs.", "Reproduce, evidence, hypothesis, fix, regression.")
	writeSkill(t, root, "cook", "Cook", "Implement plans.", "Read plan, modify, test, repair, verify.")
	writeSkill(t, root, "review", "Review", "Review code.", "Review dimensions, severity, report.")
	writeSkill(t, root, "test", "Test", "Structured testing.", "Narrowest first, Docker gate, chaos.")
	writeSkill(t, root, "debug", "Debug", "Bug investigation.", "Frame, scout, diagnose, prove, fix, test.")
	writeSkill(t, root, "docs", "Docs", "Documentation management.", "Update on contract change, verify claims.")
	writeSkill(t, root, "architect", "Architect", "Architecture proposals.", "Goal, context, risks, rollback, acceptance.")
	writeSkill(t, root, "ui-ux-pro-max", "UIUX", "UI/UX mobile rules.", "h-dvh, touch targets, safe areas, dialogs.")

	reg := NewRegistry()
	reg.Register(KindPlan, "plan")
	reg.Register(KindFix, "fix")
	reg.Register(KindCook, "cook")
	reg.Register(KindReview, "review")
	reg.Register(KindTest, "test")
	reg.Register(KindDebug, "debug")
	reg.Register(KindDocs, "docs")
	reg.Register(KindArchitect, "architect")
	reg.Register(KindUIUX, "ui-ux-pro-max")

	exec := NewExecutor(skills.NewLoader("", root, ""), reg)
	return exec, reg
}

func TestExecutor_Resolve(t *testing.T) {
	exec, _ := newTestExecutor(t)
	d, ok := exec.Resolve(context.Background(), "/gc:plan build a feature")
	if !ok {
		t.Fatal("expected Resolve to return a Dispatch")
	}
	if d.Kind != KindPlan {
		t.Errorf("Kind = %q, want plan", d.Kind)
	}
	if d.Skill != "plan" {
		t.Errorf("Skill = %q, want plan", d.Skill)
	}
	if !strings.Contains(d.Content, "Understand, inspect, plan, verify.") {
		t.Errorf("Content does not include loaded SKILL.md body: %q", d.Content)
	}
	if d.Remaining != "build a feature" {
		t.Errorf("Remaining = %q, want %q", d.Remaining, "build a feature")
	}
}

func TestExecutor_ResolveNewKinds(t *testing.T) {
	exec, _ := newTestExecutor(t)
	cases := []struct {
		message string
		kind    CommandKind
		slug    string
		body    string
	}{
		{"/gc:test the parser", KindTest, "test", "Narrowest first, Docker gate, chaos."},
		{"/gc:debug the timezone bug", KindDebug, "debug", "Frame, scout, diagnose, prove, fix, test."},
		{"/gc:docs update the contract", KindDocs, "docs", "Update on contract change, verify claims."},
		{"/gc:architect the new store", KindArchitect, "architect", "Goal, context, risks, rollback, acceptance."},
		{"/gc:uiux review the chat screen", KindUIUX, "ui-ux-pro-max", "h-dvh, touch targets, safe areas, dialogs."},
	}
	for _, tc := range cases {
		d, ok := exec.Resolve(context.Background(), tc.message)
		if !ok {
			t.Fatalf("%q: expected Resolve to return a Dispatch", tc.message)
		}
		if d.Kind != tc.kind {
			t.Errorf("%q: Kind = %q, want %q", tc.message, d.Kind, tc.kind)
		}
		if d.Skill != tc.slug {
			t.Errorf("%q: Skill = %q, want %q", tc.message, d.Skill, tc.slug)
		}
		if !strings.Contains(d.Content, tc.body) {
			t.Errorf("%q: Content does not include loaded SKILL.md body: %q", tc.message, d.Content)
		}
	}
}

func TestExecutor_ResolveFlags(t *testing.T) {
	exec, _ := newTestExecutor(t)
	d, ok := exec.Resolve(context.Background(), "/gc:fix --deep --fast flaky test")
	if !ok {
		t.Fatal("expected Resolve to return a Dispatch")
	}
	if len(d.Flags) != 2 || d.Flags[0] != "--deep" || d.Flags[1] != "--fast" {
		t.Errorf("Flags = %v, want [--deep --fast]", d.Flags)
	}
	if d.Remaining != "flaky test" {
		t.Errorf("Remaining = %q, want %q", d.Remaining, "flaky test")
	}
}

func TestExecutor_ResolvePassthrough(t *testing.T) {
	exec, _ := newTestExecutor(t)
	for _, msg := range []string{
		"hello there",
		"/gc:unknown thing",
		"/other:plan thing",
	} {
		if d, ok := exec.Resolve(context.Background(), msg); ok {
			t.Errorf("%q: expected passthrough, got dispatch %+v", msg, d)
		}
	}
}

func TestExecutor_ResolveUnregisteredKind(t *testing.T) {
	exec, reg := newTestExecutor(t)
	reg.Register(KindPlan, "") // wipe the plan mapping
	reg.Register(KindPlan, "missing-skill")
	if _, ok := exec.Resolve(context.Background(), "/gc:plan build"); ok {
		t.Error("expected passthrough when skill slug has no SKILL.md")
	}
}

func TestExecutor_ResolveNilLoader(t *testing.T) {
	reg := NewRegistry()
	reg.Register(KindPlan, "plan")
	exec := NewExecutor(nil, reg)
	if _, ok := exec.Resolve(context.Background(), "/gc:plan build"); ok {
		t.Error("expected passthrough when loader is nil")
	}
}

func TestExecutor_BuildSystemPrompt(t *testing.T) {
	exec, _ := newTestExecutor(t)
	d, ok := exec.Resolve(context.Background(), "/gc:plan --deep build a feature")
	if !ok {
		t.Fatal("expected Resolve to return a Dispatch")
	}
	prompt := exec.BuildSystemPrompt(d)
	if !strings.Contains(prompt, "/gc:plan") {
		t.Errorf("prompt missing /gc:plan directive: %q", prompt)
	}
	if !strings.Contains(prompt, "Do not claim completion until verification passes.") {
		t.Errorf("prompt missing verification directive: %q", prompt)
	}
	if !strings.Contains(prompt, d.Content) {
		t.Error("prompt missing skill content")
	}
	if !strings.Contains(prompt, "--deep") {
		t.Errorf("prompt missing execution flags: %q", prompt)
	}
}

func TestExecutor_BuildSystemPromptNil(t *testing.T) {
	exec, _ := newTestExecutor(t)
	if got := exec.BuildSystemPrompt(nil); got != "" {
		t.Errorf("BuildSystemPrompt(nil) = %q, want empty", got)
	}
}

func TestExecutor_BuildSystemPromptNoFlags(t *testing.T) {
	exec, _ := newTestExecutor(t)
	d, ok := exec.Resolve(context.Background(), "/gc:review the PR")
	if !ok {
		t.Fatal("expected Resolve to return a Dispatch")
	}
	prompt := exec.BuildSystemPrompt(d)
	if strings.Contains(prompt, "Execution flags:") {
		t.Errorf("prompt should not include flags section when none: %q", prompt)
	}
}