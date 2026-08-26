package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
)

func managedSkill(slug string) skills.Info {
	return skills.Info{Slug: slug, Name: slug, Source: "managed", Description: slug + " description"}
}

func fsSkill(slug, source string) skills.Info {
	return skills.Info{Slug: slug, Name: slug, Source: source, Description: slug + " description"}
}

// The inline <available_skills> block has always been filtered by visibility and agent
// grants, but slash activation read the loader's full list, so /<slug> could activate a
// managed skill the agent was never granted.
func TestSkillsReachableBySlash_GatesManagedSkills(t *testing.T) {
	all := []skills.Info{managedSkill("granted"), managedSkill("ungranted")}

	if got := len(skillsReachableBySlash(all, nil)); got != 2 {
		t.Errorf("nil allow list should not restrict, got %d skills", got)
	}
	if got := len(skillsReachableBySlash(all, []string{})); got != 0 {
		t.Errorf("empty allow list should block every managed skill, got %d", got)
	}

	only := skillsReachableBySlash(all, []string{"granted"})
	if len(only) != 1 || only[0].Slug != "granted" {
		t.Errorf("expected only the granted skill, got %+v", only)
	}
}

// The allow list comes from a query over the `skills` table, so filesystem-tier skills
// have no row and would be filtered out by slug. Gating them would strand four of the
// loader's five tiers for slash while skill_search still reaches them unfiltered.
func TestSkillsReachableBySlash_LeavesFilesystemTiersAlone(t *testing.T) {
	all := []skills.Info{
		managedSkill("managed-ungranted"),
		fsSkill("workspace-skill", "workspace"),
		fsSkill("project-skill", "agents-project"),
		fsSkill("personal-skill", "agents-personal"),
		fsSkill("global-skill", "global"),
		fsSkill("builtin-skill", "builtin"),
	}

	got := skillsReachableBySlash(all, []string{})
	var slugs []string
	for _, s := range got {
		slugs = append(slugs, s.Slug)
	}
	if len(got) != 5 {
		t.Fatalf("expected the five non-managed skills to survive, got %v", slugs)
	}
	for _, s := range got {
		if s.Source == "managed" {
			t.Errorf("ungranted managed skill leaked through: %s", s.Slug)
		}
	}
}

// End-to-end through the resolver: a managed skill outside the allow list must not
// activate, and must not be disclosed by the not-found suggestions, /list-skills or
// /help either — blocking activation alone would still leak the name and description.
func TestResolveSkillSlashCommand_ManagedSkillOutsideAllowList(t *testing.T) {
	loader := newManagedSlashTestLoader(t)
	cfg := config.SkillSlashCommandConfig{Enabled: new(true), Prefix: "/", SuggestNotFound: new(true)}

	granted := resolveSkillSlashCommand(context.Background(), loader, nil, cfg, "/managed-only build a page")
	if granted.Kind != skillSlashCommandActivate {
		t.Fatalf("nil allow list should activate, got kind=%v", granted.Kind)
	}

	denied := resolveSkillSlashCommand(context.Background(), loader, []string{"something-else"}, cfg, "/managed-only build a page")
	if denied.Kind == skillSlashCommandActivate {
		t.Fatalf("managed skill outside the allow list must not activate")
	}

	suggest := resolveSkillSlashCommand(context.Background(), loader, []string{}, cfg, "/managed-onl build")
	if strings.Contains(suggest.Guidance, "managed-only") {
		t.Errorf("suggestions disclosed an ungranted managed skill: %q", suggest.Guidance)
	}

	list := resolveSkillSlashCommand(context.Background(), loader, []string{}, cfg, "/list-skills")
	if strings.Contains(list.Guidance, "managed-only") {
		t.Errorf("/list-skills disclosed an ungranted managed skill: %q", list.Guidance)
	}

	help := resolveSkillSlashCommand(context.Background(), loader, []string{}, cfg, "/help managed-only")
	if help.Kind == skillSlashCommandHelp {
		t.Error("/help described an ungranted managed skill")
	}
}
