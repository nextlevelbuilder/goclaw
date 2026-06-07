package agent

import (
	"sort"
	"strings"
	"unicode"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
)

func unknownSkillSlashResult(all []skills.Info, target string, cfg config.SkillSlashCommandConfig) skillSlashCommandResult {
	if !cfg.EffectiveSuggestNotFound() {
		return skillSlashCommandResult{Kind: skillSlashCommandNone}
	}
	suggestions := similarSkills(all, target, 3)
	if len(suggestions) == 0 {
		return skillSlashCommandResult{Kind: skillSlashCommandNone}
	}
	return skillSlashCommandResult{
		Kind:        skillSlashCommandUnknown,
		Guidance:    buildSkillSlashUnknownGuidance(target, suggestions),
		Suggestions: suggestions,
	}
}

func similarSkills(all []skills.Info, target string, limit int) []skills.Info {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		return nil
	}
	type scored struct {
		info  skills.Info
		score int
	}
	var scoredSkills []scored
	for _, skill := range all {
		best := scoreSimilarSkill(target, skill)
		if best <= 3 {
			scoredSkills = append(scoredSkills, scored{info: skill, score: best})
		}
	}
	sort.Slice(scoredSkills, func(i, j int) bool {
		if scoredSkills[i].score == scoredSkills[j].score {
			return scoredSkills[i].info.Slug < scoredSkills[j].info.Slug
		}
		return scoredSkills[i].score < scoredSkills[j].score
	})
	if len(scoredSkills) > limit {
		scoredSkills = scoredSkills[:limit]
	}
	out := make([]skills.Info, len(scoredSkills))
	for i, scored := range scoredSkills {
		out[i] = scored.info
	}
	return out
}

func scoreSimilarSkill(target string, skill skills.Info) int {
	targetRunes := []rune(target)
	slug := strings.ToLower(skill.Slug)
	best := slashSkillDistance(target, slug)
	if slugRunes := []rune(slug); len(slugRunes) >= len(targetRunes) {
		best = min(best, slashSkillDistance(target, string(slugRunes[:len(targetRunes)])))
	}
	name := strings.ToLower(skill.Name)
	if name != "" {
		best = min(best, slashSkillDistance(target, name))
		if nameRunes := []rune(name); len(nameRunes) >= len(targetRunes) {
			best = min(best, slashSkillDistance(target, string(nameRunes[:len(targetRunes)])))
		}
	}
	if strings.HasPrefix(slug, target) || strings.Contains(name, target) {
		best = 0
	}
	return best
}

func slashSkillDistance(a, b string) int {
	ar := []rune(a)
	br := []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	prev := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, ca := range ar {
		cur := make([]int, len(br)+1)
		cur[0] = i + 1
		for j, cb := range br {
			cost := 0
			if unicode.ToLower(ca) != unicode.ToLower(cb) {
				cost = 1
			}
			cur[j+1] = min(min(cur[j]+1, prev[j+1]+1), prev[j]+cost)
		}
		prev = cur
	}
	return prev[len(br)]
}
