package agent

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
)

type parsedSkillSlashCommand struct {
	verb   string
	target string
	rest   string
}

func parseSkillSlashCommand(message, prefix string) (parsedSkillSlashCommand, bool) {
	message = strings.TrimSpace(message)
	if message == "" || !strings.HasPrefix(message, prefix) {
		return parsedSkillSlashCommand{}, false
	}
	after := strings.TrimSpace(strings.TrimPrefix(message, prefix))
	if after == "" || looksLikePath(after) {
		return parsedSkillSlashCommand{}, false
	}
	first, rest := cutFirstField(after)
	switch strings.ToLower(first) {
	case "list-skills":
		return parsedSkillSlashCommand{verb: "list-skills"}, true
	case "help":
		if rest == "" {
			return parsedSkillSlashCommand{}, false
		}
		return parsedSkillSlashCommand{verb: "help", target: rest}, true
	case "use", "activate":
		if rest == "" {
			return parsedSkillSlashCommand{}, false
		}
		return parsedSkillSlashCommand{verb: strings.ToLower(first), rest: rest}, true
	default:
		return parsedSkillSlashCommand{verb: "direct", target: first, rest: rest}, true
	}
}

func looksLikePath(value string) bool {
	first, _ := cutFirstField(value)
	return strings.Contains(first, "/") || strings.Contains(first, "\\") || strings.Contains(first, ".")
}

// cutFirstField splits value at the first run of whitespace and returns the leading
// field plus the trimmed remainder.
//
// This replaces strings.Cut(value, " "), which split on a literal space only. A user who
// types the slash command and presses Enter before the rest of the message sends
// "/ck:git\nreview the diff"; the old split yielded the target "ck:git\nreview", which
// matches no skill. The reply was "skill not found" followed by near-matches that
// included the skill they had just named — the similarity fallback searched the mangled
// string and still landed beside it. Multi-line messages are normal in Slack and
// Telegram, so this was reachable in ordinary use.
func cutFirstField(value string) (string, string) {
	value = strings.TrimSpace(value)
	i := strings.IndexFunc(value, unicode.IsSpace)
	if i < 0 {
		return value, ""
	}
	return value[:i], strings.TrimSpace(value[i:])
}

// hasFieldPrefix reports whether raw begins with value followed by whitespace, i.e.
// value occupies a whole leading field rather than merely being a string prefix.
// Both arguments are expected to be lowercased by the caller.
func hasFieldPrefix(raw, value string) bool {
	if !strings.HasPrefix(raw, value) || len(raw) == len(value) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(raw[len(value):])
	return unicode.IsSpace(r)
}

func matchSkillCommandTarget(all []skills.Info, raw string, partial bool) (skills.Info, bool, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return skills.Info{}, false, ""
	}
	type candidate struct {
		info      skills.Info
		matchText string
		remainder string
		score     int
	}
	var matches []candidate
	lowerRaw := strings.ToLower(raw)
	partialTarget, partialRemainder := cutFirstField(raw)
	lowerPartialTarget := strings.ToLower(partialTarget)
	for _, skill := range all {
		for _, value := range []string{skill.Slug, skill.Name} {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			lowerValue := strings.ToLower(value)
			if lowerRaw == lowerValue {
				matches = append(matches, candidate{info: skill, matchText: value, score: len([]rune(value))})
				continue
			}
			if hasFieldPrefix(lowerRaw, lowerValue) {
				remainder := trimMatchedSkillCommandPrefix(raw, value)
				matches = append(matches, candidate{info: skill, matchText: value, remainder: remainder, score: len([]rune(value))})
				continue
			}
			if partial && lowerPartialTarget != "" && strings.HasPrefix(lowerValue, lowerPartialTarget) {
				matches = append(matches, candidate{info: skill, matchText: value, remainder: partialRemainder, score: len([]rune(partialTarget))})
			}
		}
	}
	if len(matches) == 0 {
		return skills.Info{}, false, ""
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})
	bestBySlug := make([]candidate, 0, len(matches))
	seenSlug := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if _, ok := seenSlug[match.info.Slug]; ok {
			continue
		}
		seenSlug[match.info.Slug] = struct{}{}
		bestBySlug = append(bestBySlug, match)
	}
	best := bestBySlug[0]
	if len(bestBySlug) > 1 && bestBySlug[0].score == bestBySlug[1].score {
		return skills.Info{}, false, ""
	}
	return best.info, true, best.remainder
}

func trimMatchedSkillCommandPrefix(raw, matched string) string {
	rawRunes := []rune(raw)
	matchedRunes := []rune(matched)
	if len(rawRunes) < len(matchedRunes) {
		return ""
	}
	return strings.TrimSpace(string(rawRunes[len(matchedRunes):]))
}
