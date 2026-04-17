package agent

import "context"

// SkillMode determines how skills are presented in the system prompt.
type SkillMode int

const (
	// SkillModeSearch means too many skills to inline; agent uses skill_search tool.
	SkillModeSearch SkillMode = iota
	// SkillModeSummary means skill descriptions (200 chars) are inlined as XML.
	SkillModeSummary
	// SkillModeFull means complete SKILL.md content is loaded into the prompt.
	SkillModeFull
)

// Skill budget constants.
const (
	skillBudgetFraction  = 0.12 // 12% of available context allocated to skills
	skillBudgetMinTokens = 1000 // minimum skill budget in tokens
	skillBudgetMaxTokens = 20000
	skillBudgetMinChars  = 3000  // minimum char budget (~1000 tokens)
	skillBudgetMaxChars  = 60000 // maximum char budget (~20000 tokens)
)

// Legacy static thresholds for inline mode (used by resolveSkillsSummary fallback).
const (
	skillInlineMaxCount  = 60   // max skills to inline
	skillInlineMaxTokens = 3000 // max estimated tokens for skill descriptions
)

// resolveSkillsSummary builds the skills summary for the system prompt using
// legacy static thresholds. Kept for backward compatibility.
func (l *Loop) resolveSkillsSummary(ctx context.Context, skillFilter []string) string {
	if l.skillsLoader == nil {
		return ""
	}

	allowList := l.skillAllowList
	if skillFilter != nil {
		allowList = skillFilter
	}

	filtered := l.skillsLoader.FilterSkills(ctx, allowList)
	if len(filtered) == 0 {
		return ""
	}

	totalChars := 0
	for _, s := range filtered {
		descLen := min(len(s.Description), 200)
		totalChars += len(s.Name) + descLen + 10
	}
	estimatedTokens := totalChars / 4

	if len(filtered) <= skillInlineMaxCount && estimatedTokens <= skillInlineMaxTokens {
		return l.skillsLoader.BuildSummary(ctx, allowList)
	}

	return ""
}

// resolvePinnedSkillsSummary builds XML for pinned skills only (always inline).
func (l *Loop) resolvePinnedSkillsSummary(ctx context.Context) string {
	if l.skillsLoader == nil || len(l.pinnedSkills) == 0 {
		return ""
	}
	return l.skillsLoader.BuildPinnedSummary(ctx, l.pinnedSkills)
}

// resolveSkillsContent dynamically decides how to include skills in the system prompt.
// Returns the skill content string and the mode used.
func (l *Loop) resolveSkillsContent(ctx context.Context, skillFilter []string, contextWindow, overheadTokens int) (string, SkillMode) {
	if l.skillsLoader == nil {
		return "", SkillModeSearch
	}

	allowList := l.skillAllowList
	if skillFilter != nil {
		allowList = skillFilter
	}

	filtered := l.skillsLoader.FilterSkills(ctx, allowList)
	if len(filtered) == 0 {
		return "", SkillModeSearch
	}

	if contextWindow > 0 && overheadTokens > 0 {
		availableTokens := contextWindow - overheadTokens
		if availableTokens < skillBudgetMinTokens {
			return "", SkillModeSearch
		}

		skillTokenBudget := int(clamp(float64(availableTokens)*skillBudgetFraction, float64(skillBudgetMinTokens), float64(skillBudgetMaxTokens)))
		charBudget := clampInt(skillTokenBudget*4, skillBudgetMinChars, skillBudgetMaxChars)

		fullSize := l.skillsLoader.EstimateFullContentSize(ctx, allowList)
		if fullSize <= charBudget {
			content := l.skillsLoader.LoadSkillsForPrompt(ctx, allowList, charBudget)
			if content != "" {
				return content, SkillModeFull
			}
		}

		summary := l.skillsLoader.BuildSummary(ctx, allowList)
		if summary != "" && len(summary) <= charBudget {
			return summary, SkillModeSummary
		}

		return "", SkillModeSearch
	}

	return l.resolveSkillsSummary(ctx, skillFilter), SkillModeSummary
}

func clamp(val, minVal, maxVal float64) float64 {
	if val < minVal {
		return minVal
	}
	if val > maxVal {
		return maxVal
	}
	return val
}

func clampInt(val, minVal, maxVal int) int {
	if val < minVal {
		return minVal
	}
	if val > maxVal {
		return maxVal
	}
	return val
}
