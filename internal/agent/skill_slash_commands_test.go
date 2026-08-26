package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestResolveSkillSlashCommandExactSlug(t *testing.T) {
	loader := newSlashTestLoader(t)
	result := resolveSkillSlashCommand(context.Background(), loader, nil, config.SkillSlashCommandConfig{Enabled: new(true), Prefix: "/"}, "/frontend-design build a landing page")

	if result.Kind != skillSlashCommandActivate {
		t.Fatalf("kind = %v, want activate", result.Kind)
	}
	if result.Skill.Slug != "frontend-design" {
		t.Fatalf("slug = %q, want frontend-design", result.Skill.Slug)
	}
	if result.RemainingPrompt != "build a landing page" {
		t.Fatalf("remaining = %q", result.RemainingPrompt)
	}
	if !strings.Contains(result.SkillContent, "Use responsive components.") {
		t.Fatal("expected loaded SKILL.md content")
	}
}

func TestResolveSkillSlashCommandExactNameUseSyntax(t *testing.T) {
	loader := newSlashTestLoader(t)
	result := resolveSkillSlashCommand(context.Background(), loader, nil, config.SkillSlashCommandConfig{Enabled: new(true), Prefix: "/"}, "/use Frontend Design build a landing page")

	if result.Kind != skillSlashCommandActivate {
		t.Fatalf("kind = %v, want activate", result.Kind)
	}
	if result.Skill.Slug != "frontend-design" {
		t.Fatalf("slug = %q, want frontend-design", result.Skill.Slug)
	}
	if result.RemainingPrompt != "build a landing page" {
		t.Fatalf("remaining = %q", result.RemainingPrompt)
	}
}

func TestResolveSkillSlashCommandPartialMatchRequiresUniqueEnabled(t *testing.T) {
	loader := newSlashTestLoader(t)

	disabled := resolveSkillSlashCommand(context.Background(), loader, nil, config.SkillSlashCommandConfig{Enabled: new(true), Prefix: "/"}, "/front build")
	if disabled.Kind != skillSlashCommandUnknown {
		t.Fatalf("disabled partial kind = %v, want unknown", disabled.Kind)
	}

	enabled := resolveSkillSlashCommand(context.Background(), loader, nil, config.SkillSlashCommandConfig{Enabled: new(true), Prefix: "/", PartialMatching: true}, "/front build")
	if enabled.Kind != skillSlashCommandActivate {
		t.Fatalf("enabled partial kind = %v, want activate", enabled.Kind)
	}
	if enabled.Skill.Slug != "frontend-design" {
		t.Fatalf("slug = %q, want frontend-design", enabled.Skill.Slug)
	}
}

func TestResolveSkillSlashCommandFalsePositives(t *testing.T) {
	loader := newSlashTestLoader(t)
	for _, msg := range []string{"/home/user/project", "/etc/config.yaml", "https://example.com/path", "regular prompt"} {
		result := resolveSkillSlashCommand(context.Background(), loader, nil, config.SkillSlashCommandConfig{Enabled: new(true), Prefix: "/"}, msg)
		if result.Kind != skillSlashCommandNone {
			t.Fatalf("%q kind = %v, want none", msg, result.Kind)
		}
	}
}

func TestResolveSkillSlashCommandListAndHelp(t *testing.T) {
	loader := newSlashTestLoader(t)
	cfg := config.SkillSlashCommandConfig{Enabled: new(true), Prefix: "/"}

	list := resolveSkillSlashCommand(context.Background(), loader, nil, cfg, "/list-skills")
	if list.Kind != skillSlashCommandList {
		t.Fatalf("list kind = %v, want list", list.Kind)
	}
	if !strings.Contains(list.Guidance, "frontend-design") || !strings.Contains(list.Guidance, "git-helper") {
		t.Fatalf("list guidance missing skills: %s", list.Guidance)
	}

	help := resolveSkillSlashCommand(context.Background(), loader, nil, cfg, "/help frontend-design")
	if help.Kind != skillSlashCommandHelp {
		t.Fatalf("help kind = %v, want help", help.Kind)
	}
	if help.Skill.Slug != "frontend-design" || !strings.Contains(help.Guidance, "Frontend Design") {
		t.Fatalf("unexpected help result: %#v", help)
	}

	helpByName := resolveSkillSlashCommand(context.Background(), loader, nil, cfg, "/help Frontend Design")
	if helpByName.Kind != skillSlashCommandHelp {
		t.Fatalf("help by name kind = %v, want help", helpByName.Kind)
	}
	if helpByName.Skill.Slug != "frontend-design" {
		t.Fatalf("help by name slug = %q, want frontend-design", helpByName.Skill.Slug)
	}
}

func TestResolveSkillSlashCommandSuggestsUnknown(t *testing.T) {
	loader := newSlashTestLoader(t)
	result := resolveSkillSlashCommand(context.Background(), loader, nil, config.SkillSlashCommandConfig{Enabled: new(true), Prefix: "/", SuggestNotFound: new(true)}, "/fronted build")

	if result.Kind != skillSlashCommandUnknown {
		t.Fatalf("kind = %v, want unknown", result.Kind)
	}
	if len(result.Suggestions) == 0 || result.Suggestions[0].Slug != "frontend-design" {
		t.Fatalf("suggestions = %#v", result.Suggestions)
	}
}

//go:fix inline
func boolPtr(v bool) *bool {
	return new(v)
}

func newSlashTestLoader(t *testing.T) *skills.Loader {
	t.Helper()
	t.Setenv("GOCLAW_DISABLE_PERSONAL_SKILLS", "1")
	root := t.TempDir()
	writeSkill(t, root, "frontend-design", "Frontend Design", "Create polished UI layouts.", "Use responsive components.")
	writeSkill(t, root, "git-helper", "Git Helper", "Handle git workflows.", "Use clean commits.")
	return skills.NewLoader("", root, "")
}

// newManagedSlashTestLoader builds a loader whose skills come from the managed
// (DB-backed) tier, so the allow-list gate applies to them. The filesystem tiers are
// deliberately left empty: skillsReachableBySlash only gates Source == "managed".
func newManagedSlashTestLoader(t *testing.T) *skills.Loader {
	t.Helper()
	t.Setenv("GOCLAW_DISABLE_PERSONAL_SKILLS", "1")
	dataDir := t.TempDir()
	storeDir := config.TenantSkillsStoreDir(dataDir, store.MasterTenantID, "")
	// Managed layout is <store>/<slug>/<version>/SKILL.md.
	writeSkill(t, filepath.Join(storeDir, "managed-only"), "1", "managed-only", "A managed skill.", "Body.")
	loader := skills.NewLoader("", "", "")
	loader.SetManagedDir(dataDir)
	return loader
}

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
