package agent

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
)

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
	if !shouldInlineSkills(filtered) {
		// Search mode: no XML in prompt, agent uses skill_search tool
		return ""
	}
	return l.skillsLoader.BuildSummary(ctx, allowList)
}

// shouldInlineSkills decides between inline mode and search mode for a set of skills.
//
// Both the live prompt builder and the system-prompt preview endpoint must call this.
// They used to decide separately and disagree: the preview counted tokens with
// tokencount.NewFallbackCounter() over the fully rendered XML — tags, <location> paths
// and all, at roughly runes/2 — while the runtime estimated name+description at chars/4.
// On the same 18 skills that was 3087 against 944, so the preview reported search mode
// for an agent that was actually running inline. A preview whose whole purpose is to show
// the real prompt cannot use a different rule than the real prompt.
//
// The estimate deliberately mirrors BuildSummary()'s truncation (skillDescMaxLen=200
// runes) rather than measuring the rendered string, so the decision costs no rendering.
func shouldInlineSkills(filtered []skills.Info) bool {
	if len(filtered) == 0 {
		return false
	}
	if len(filtered) > skillInlineMaxCount {
		return false
	}
	// ~1 token per 4 chars for name+description, +10 for the XML tag overhead per entry.
	totalChars := 0
	for _, s := range filtered {
		descLen := min(len(s.Description), 200)
		totalChars += len(s.Name) + descLen + 10
	}
	return totalChars/4 <= skillInlineMaxTokens
}

// resolvePinnedSkillsSummary builds XML for pinned skills only (always inline).
func (l *Loop) resolvePinnedSkillsSummary(ctx context.Context) string {
	if l.skillsLoader == nil || len(l.pinnedSkills) == 0 {
		return ""
	}
	return l.skillsLoader.BuildPinnedSummary(ctx, l.pinnedSkills)
}
