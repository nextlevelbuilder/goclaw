package agent

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
)

func (r skillSlashCommandResult) systemPromptSection() string {
	switch r.Kind {
	case skillSlashCommandActivate:
		return fmt.Sprintf("## Explicit Skill Activation\n\nThe user explicitly requested skill `%s` (%s). Use this skill for the current request and treat the remaining user message as the skill input. Call `use_skill` with name `%s` for observability when available, then follow these instructions:\n\n%s", r.Skill.Slug, r.Skill.Name, r.Skill.Slug, r.SkillContent)
	case skillSlashCommandList, skillSlashCommandHelp, skillSlashCommandUnknown:
		return r.Guidance
	default:
		return ""
	}
}

func buildSkillSlashListGuidance(all []skills.Info) string {
	if len(all) == 0 {
		return "## Skill Slash Command\n\nNo skills are currently available."
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Slug < all[j].Slug })
	var lines []string
	lines = append(lines, "## Skill Slash Command", "", "Available skills:")
	for _, skill := range all {
		lines = append(lines, fmt.Sprintf("- `%s` - %s", skill.Slug, skillDisplayDescription(skill)))
	}
	return strings.Join(lines, "\n")
}

func buildSkillSlashHelpGuidance(skill skills.Info) string {
	return fmt.Sprintf("## Skill Slash Command\n\nSkill `%s` (%s)\nDescription: %s\nLocation: %s\nExplain this skill and how to invoke it with the configured slash prefix.", skill.Slug, skill.Name, skillDisplayDescription(skill), filepath.ToSlash(skill.Path))
}

func buildSkillSlashUnknownGuidance(target string, suggestions []skills.Info) string {
	var lines []string
	lines = append(lines, "## Skill Slash Command", "", fmt.Sprintf("Requested skill `%s` was not found. Suggest these available alternatives:", target))
	for _, skill := range suggestions {
		lines = append(lines, fmt.Sprintf("- `%s` - %s", skill.Slug, skillDisplayDescription(skill)))
	}
	return strings.Join(lines, "\n")
}

func skillDisplayDescription(skill skills.Info) string {
	if strings.TrimSpace(skill.Description) != "" {
		return strings.TrimSpace(skill.Description)
	}
	return skill.Name
}

func appendExtraPrompt(existing, addition string) string {
	addition = strings.TrimSpace(addition)
	if addition == "" {
		return existing
	}
	if strings.TrimSpace(existing) == "" {
		return addition
	}
	return existing + "\n\n" + addition
}
