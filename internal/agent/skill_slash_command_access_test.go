package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// The inline <available_skills> block has always been filtered by the agent's
// visibility + grant list, but the slash path read the loader's full list. /<slug> could
// therefore activate a skill the agent was never granted, and the not-found suggestions
// disclosed that such skills existed.
func TestResolveSkillSlashCommand_HonoursAllowList(t *testing.T) {
	loader := newSlashTestLoader(t)
	cfg := config.SkillSlashCommandConfig{Enabled: new(true), Prefix: "/"}

	// nil = no restriction; the skill activates.
	granted := resolveSkillSlashCommand(context.Background(), loader, nil, cfg, "/frontend-design build a page")
	if granted.Kind != skillSlashCommandActivate {
		t.Fatalf("nil allow list should activate, got kind=%v", granted.Kind)
	}

	// An allow list that omits the slug must not activate it.
	denied := resolveSkillSlashCommand(context.Background(), loader, []string{"some-other-skill"}, cfg, "/frontend-design build a page")
	if denied.Kind == skillSlashCommandActivate {
		t.Fatalf("skill outside the allow list must not activate, got %q", denied.Skill.Slug)
	}

	// An empty allow list means no skills at all.
	none := resolveSkillSlashCommand(context.Background(), loader, []string{}, cfg, "/frontend-design build a page")
	if none.Kind == skillSlashCommandActivate {
		t.Fatalf("empty allow list must not activate, got %q", none.Skill.Slug)
	}

	// An allow list containing the slug still activates it.
	allowed := resolveSkillSlashCommand(context.Background(), loader, []string{"frontend-design"}, cfg, "/frontend-design build a page")
	if allowed.Kind != skillSlashCommandActivate {
		t.Fatalf("allow-listed skill should activate, got kind=%v", allowed.Kind)
	}
}

// A denied skill must not surface through the not-found suggestions either — otherwise
// the block on activation still leaks the name and description.
func TestResolveSkillSlashCommand_DeniedSkillNotSuggested(t *testing.T) {
	loader := newSlashTestLoader(t)
	cfg := config.SkillSlashCommandConfig{Enabled: new(true), Prefix: "/", SuggestNotFound: new(true)}

	res := resolveSkillSlashCommand(context.Background(), loader, []string{}, cfg, "/fronted build")
	if strings.Contains(res.Guidance, "frontend-design") {
		t.Errorf("guidance disclosed a skill outside the allow list: %q", res.Guidance)
	}
}

// /list-skills lists only what the agent may use.
func TestResolveSkillSlashCommand_ListHonoursAllowList(t *testing.T) {
	loader := newSlashTestLoader(t)
	cfg := config.SkillSlashCommandConfig{Enabled: new(true), Prefix: "/"}

	all := resolveSkillSlashCommand(context.Background(), loader, nil, cfg, "/list-skills")
	if !strings.Contains(all.Guidance, "frontend-design") {
		t.Fatalf("unfiltered list should name the skill, got %q", all.Guidance)
	}

	restricted := resolveSkillSlashCommand(context.Background(), loader, []string{}, cfg, "/list-skills")
	if strings.Contains(restricted.Guidance, "frontend-design") {
		t.Errorf("restricted list disclosed a skill outside the allow list: %q", restricted.Guidance)
	}
}

// /help must not describe a skill the agent cannot use.
func TestResolveSkillSlashCommand_HelpHonoursAllowList(t *testing.T) {
	loader := newSlashTestLoader(t)
	cfg := config.SkillSlashCommandConfig{Enabled: new(true), Prefix: "/"}

	res := resolveSkillSlashCommand(context.Background(), loader, []string{}, cfg, "/help frontend-design")
	if res.Kind == skillSlashCommandHelp {
		t.Errorf("help should not describe a skill outside the allow list")
	}
}
