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
	result := resolveSkillSlashCommand(ctx, l.skillsLoader, l.resolveSkillSlashCommandConfig(ctx), message)
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

func resolveSkillSlashCommand(ctx context.Context, loader *skills.Loader, cfg config.SkillSlashCommandConfig, message string) skillSlashCommandResult {
	if loader == nil || !cfg.EffectiveEnabled() {
		return skillSlashCommandResult{Kind: skillSlashCommandNone}
	}
	parsed, ok := parseSkillSlashCommand(message, cfg.EffectivePrefix())
	if !ok {
		return skillSlashCommandResult{Kind: skillSlashCommandNone}
	}
	all := loader.ListSkills(ctx)
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
