package agent

import "context"

// Hybrid skill thresholds: when skill count and total token estimate are below
// these limits, inline all skills as XML in the system prompt (like TS).
// Above these limits, only include skill_search instructions.
const (
	skillInlineMaxCount  = 60   // max skills to inline
	skillInlineMaxTokens = 3000 // max estimated tokens for skill descriptions
)

// resolveSkillsSummary dynamically builds the skills summary for the system prompt.
// Called per-message so it picks up hot-reloaded skills automatically.
// Returns (summary XML, useInline) — useInline=true means skills are inlined and
// the system prompt should use TS-style "scan <available_skills>" instructions
// instead of "use skill_search".
func (l *Loop) resolveSkillsSummary(ctx context.Context, skillFilter []string) string {
	if l.skillsLoader == nil {
		return ""
	}

	// Per-request skill filter overrides agent-level allowList.
	allowList := l.skillAllowList
	if skillFilter != nil {
		allowList = skillFilter
	}

	filtered := l.skillsLoader.FilterSkills(ctx, allowList)
	if len(filtered) == 0 {
		return ""
	}

	// Estimate tokens: ~1 token per 4 chars for name+description.
	// Cap description length to match BuildSummary() truncation (skillDescMaxLen=200 runes).
	totalChars := 0
	for _, s := range filtered {
		descLen := min(len(s.Description), 200)
		totalChars += len(s.Name) + descLen + 10 // +10 for XML tags overhead
	}
	estimatedTokens := totalChars / 4

	if len(filtered) <= skillInlineMaxCount && estimatedTokens <= skillInlineMaxTokens {
		// Inline mode: build full XML summary
		return l.skillsLoader.BuildSummary(ctx, allowList)
	}

	// Search mode: no XML in prompt, agent uses skill_search tool
	return ""
}

// resolvePinnedSkillsSummary builds XML for pinned skills only (always inline).
func (l *Loop) resolvePinnedSkillsSummary(ctx context.Context) string {
	if l.skillsLoader == nil || len(l.pinnedSkills) == 0 {
		return ""
	}
	return l.skillsLoader.BuildPinnedSummary(ctx, l.pinnedSkills)
}
