package agent

import (
	"context"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type skillSlashCommandKind int

const (
	skillSlashCommandNone skillSlashCommandKind = iota
	skillSlashCommandActivate
	skillSlashCommandList
	skillSlashCommandHelp
	skillSlashCommandUnknown
)

type skillSlashCommandResult struct {
	Kind            skillSlashCommandKind
	Skill           skills.Info
	SkillContent    string
	RemainingPrompt string
	Guidance        string
	Suggestions     []skills.Info
}

func (l *Loop) applySkillSlashCommand(ctx context.Context, req *RunRequest, message, extraPrompt string, skillFilter []string) (string, string, []string) {
	// l.skillAllowList is the visibility + grant filter computed when the agent was
	// resolved (internal/agent/resolver.go). The inline <available_skills> block already
	// honours it; the slash path did not, so /<slug> activated any skill the loader could
	// see — including internal skills never granted to this agent, and skills belonging to
	// another tenant's store if the loader had them. Filtering here also stops the
	// not-found suggestions from disclosing that such a skill exists.
	result := resolveSkillSlashCommand(ctx, l.skillsLoader, l.skillAllowList, l.resolveSkillSlashCommandConfig(ctx), message)
	if result.Kind == skillSlashCommandNone {
		return message, extraPrompt, skillFilter
	}
	extraPrompt = appendExtraPrompt(extraPrompt, result.systemPromptSection())
	switch result.Kind {
	case skillSlashCommandActivate:
		if result.RemainingPrompt == "" {
			message = "Use the activated skill to help with the user's request."
		} else {
			message = result.RemainingPrompt
		}
		skillFilter = []string{result.Skill.Slug}
		l.recordSkillSlashUsageEvent(ctx, result.Skill.Slug)
		l.recordSkillUsage(ctx, req, result.Skill.Slug, "", "slash", store.SkillUsageStatusStarted, "", 0)
	case skillSlashCommandList:
		message = "List the available skills shown in the system instructions."
	case skillSlashCommandHelp:
		message = "Explain the requested skill and how it should be used."
	case skillSlashCommandUnknown:
		message = "Explain that the requested skill was not found and suggest available alternatives."
	}
	return message, extraPrompt, skillFilter
}

func (l *Loop) resolveSkillSlashCommandConfig(ctx context.Context) config.SkillSlashCommandConfig {
	cfg := l.skillSlashCommands
	if l.systemConfigs == nil {
		return cfg
	}
	if raw, err := l.systemConfigs.Get(ctx, config.SkillSlashCommandsEnabledSystemConfigKey); err == nil && strings.TrimSpace(raw) != "" {
		v := parseSkillSlashBool(raw)
		cfg.Enabled = &v
	}
	if raw, err := l.systemConfigs.Get(ctx, config.SkillSlashSuggestNotFoundSystemConfigKey); err == nil && strings.TrimSpace(raw) != "" {
		v := parseSkillSlashBool(raw)
		cfg.SuggestNotFound = &v
	}
	if raw, err := l.systemConfigs.Get(ctx, config.SkillSlashPartialMatchingSystemConfigKey); err == nil && strings.TrimSpace(raw) != "" {
		cfg.PartialMatching = parseSkillSlashBool(raw)
	}
	if raw, err := l.systemConfigs.Get(ctx, config.SkillSlashCommandPrefixSystemConfigKey); err == nil && strings.TrimSpace(raw) != "" {
		cfg.Prefix = raw
	}
	return cfg
}

// resolveSkillSlashCommand matches a slash command against the skills this agent may
// use. allowList follows the loader's convention: nil means every skill, an empty slice
// means none, and a populated slice is an explicit set of slugs.
func resolveSkillSlashCommand(ctx context.Context, loader *skills.Loader, allowList []string, cfg config.SkillSlashCommandConfig, message string) skillSlashCommandResult {
	if loader == nil || !cfg.EffectiveEnabled() {
		return skillSlashCommandResult{Kind: skillSlashCommandNone}
	}
	parsed, ok := parseSkillSlashCommand(message, cfg.EffectivePrefix())
	if !ok {
		return skillSlashCommandResult{Kind: skillSlashCommandNone}
	}
	all := skillsReachableBySlash(loader.ListSkills(ctx), allowList)
	switch parsed.verb {
	case "list-skills":
		return skillSlashCommandResult{Kind: skillSlashCommandList, Guidance: buildSkillSlashListGuidance(all)}
	case "help":
		skill, matched, _ := matchSkillCommandTarget(all, parsed.target, cfg.EffectivePartialMatching())
		if !matched {
			return unknownSkillSlashResult(all, parsed.target, cfg)
		}
		return skillSlashCommandResult{Kind: skillSlashCommandHelp, Skill: skill, Guidance: buildSkillSlashHelpGuidance(skill)}
	case "use", "activate":
		return resolveSkillActivation(ctx, loader, all, parsed.rest, cfg)
	default:
		return resolveSkillActivation(ctx, loader, all, parsed.target+" "+parsed.rest, cfg)
	}
}

// skillsReachableBySlash applies the agent's allow list to the skills the DB governs,
// and lets the rest through.
//
// The allow list comes from SkillAccessStore.ListAccessible, which queries the `skills`
// table only (internal/store/pg/skills_grants.go:410). Filesystem-tier skills — the
// workspace, .agents and ~/.agents/~/.goclaw directories of the five-tier loader — have
// no row there, so filtering every skill against the list would make those four tiers
// unreachable by slash while `skill_search` still finds them
// (internal/tools/skill_search.go:81 calls ListSkills unfiltered). Gating only the
// managed tier closes the grant bypass without stranding skills an operator placed on
// disk deliberately.
//
// Builtin skills are seeded into the table with is_system = true and ListAccessible
// returns those unconditionally, so they stay reachable either way.
//
// A nil allow list means no restriction; an empty one means no managed skill is allowed.
func skillsReachableBySlash(all []skills.Info, allowList []string) []skills.Info {
	if allowList == nil {
		return all
	}
	allowed := make(map[string]bool, len(allowList))
	for _, slug := range allowList {
		allowed[slug] = true
	}
	out := make([]skills.Info, 0, len(all))
	for _, s := range all {
		if s.Source == "managed" && !allowed[s.Slug] {
			continue
		}
		out = append(out, s)
	}
	return out
}

func resolveSkillActivation(ctx context.Context, loader *skills.Loader, all []skills.Info, raw string, cfg config.SkillSlashCommandConfig) skillSlashCommandResult {
	skill, matched, remainder := matchSkillCommandTarget(all, raw, cfg.EffectivePartialMatching())
	if !matched {
		fields := strings.Fields(raw)
		target := strings.TrimSpace(raw)
		if len(fields) > 0 {
			target = fields[0]
		}
		return unknownSkillSlashResult(all, target, cfg)
	}
	content, ok := loader.LoadSkill(ctx, skill.Slug)
	if !ok {
		return unknownSkillSlashResult(all, skill.Slug, cfg)
	}
	return skillSlashCommandResult{
		Kind:            skillSlashCommandActivate,
		Skill:           skill,
		SkillContent:    content,
		RemainingPrompt: strings.TrimSpace(remainder),
	}
}

func parseSkillSlashBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
