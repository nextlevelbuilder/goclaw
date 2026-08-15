package tools

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

type commandKeywordAllowAudit struct {
	RuleID     string
	Command    string
	Subcommand string
	Arg        string
	Keyword    string
	Reason     string
}

func applyCommandKeywordAllowlist(command string, args []string, rules []config.CommandKeywordAllowlistRule) ([]string, []commandKeywordAllowAudit) {
	if len(args) == 0 || len(rules) == 0 {
		return args, nil
	}

	var out []string
	var audits []commandKeywordAllowAudit
	for _, rule := range rules {
		if !commandKeywordAllowlistRuleEnabled(rule) {
			continue
		}
		if normalizeBinaryName(rule.Command) != normalizeBinaryName(command) {
			continue
		}
		subcommand, argStart, ok := matchCommandKeywordSubcommand(args, rule.Subcommands)
		if !ok {
			continue
		}
		argNames := commandKeywordSet(rule.Args)
		argPositions := commandKeywordPositionSet(rule.ArgPositions)
		keywords := commandKeywordSet(rule.Keywords)
		if (len(argNames) == 0 && len(argPositions) == 0) || len(keywords) == 0 {
			continue
		}
		if len(argPositions) > 0 && subcommand == "" {
			continue
		}
		if out == nil {
			out = slicesClone(args)
		}
		for i := argStart; i < len(out); i++ {
			argName, valueIndex, inlineValue, ok := commandKeywordArgValue(out, i, argNames)
			if !ok {
				if _, positional := argPositions[i-argStart]; positional {
					argName = "argPositions[" + strconv.Itoa(i-argStart) + "]"
					valueIndex = i
					inlineValue = out[i]
					ok = true
				}
			}
			if !ok {
				continue
			}
			value := inlineValue
			if valueIndex != i {
				value = out[valueIndex]
			}
			masked, hits := maskCommandKeywords(value, keywords)
			if len(hits) == 0 {
				continue
			}
			if valueIndex == i {
				prefix := out[i][:strings.Index(out[i], "=")+1]
				out[i] = prefix + masked
			} else {
				out[valueIndex] = masked
				i = valueIndex
			}
			for _, keyword := range hits {
				audits = append(audits, commandKeywordAllowAudit{
					RuleID:     rule.ID,
					Command:    normalizeBinaryName(command),
					Subcommand: subcommand,
					Arg:        argName,
					Keyword:    keyword,
					Reason:     rule.Reason,
				})
			}
		}
	}
	if out == nil {
		return args, audits
	}
	return out, audits
}

func commandKeywordAllowlistRuleEnabled(rule config.CommandKeywordAllowlistRule) bool {
	return rule.Enabled == nil || *rule.Enabled
}

func matchCommandKeywordSubcommand(args []string, subcommands []string) (string, int, bool) {
	if len(subcommands) == 0 {
		return "", 0, true
	}
	for _, subcommand := range subcommands {
		parts := strings.Fields(strings.ToLower(strings.TrimSpace(subcommand)))
		if len(parts) == 0 || len(parts) > len(args) {
			continue
		}
		matched := true
		for i, part := range parts {
			if strings.ToLower(args[i]) != part {
				matched = false
				break
			}
		}
		if matched {
			return strings.Join(parts, " "), len(parts), true
		}
	}
	return "", 0, false
}

func commandKeywordSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			set[value] = struct{}{}
		}
	}
	return set
}

func commandKeywordPositionSet(values []int) map[int]struct{} {
	if len(values) == 0 {
		return nil
	}
	set := make(map[int]struct{}, len(values))
	for _, value := range values {
		if value >= 0 {
			set[value] = struct{}{}
		}
	}
	return set
}

func commandKeywordArgValue(args []string, i int, argNames map[string]struct{}) (string, int, string, bool) {
	arg := args[i]
	if eq := strings.Index(arg, "="); eq > 0 {
		name := strings.ToLower(arg[:eq])
		if _, ok := argNames[name]; ok {
			return name, i, arg[eq+1:], true
		}
		return "", 0, "", false
	}
	name := strings.ToLower(arg)
	if _, ok := argNames[name]; !ok || i+1 >= len(args) {
		return "", 0, "", false
	}
	return name, i + 1, "", true
}

func maskCommandKeywords(value string, keywords map[string]struct{}) (string, []string) {
	if value == "" || len(keywords) == 0 {
		return value, nil
	}
	var hits []string
	masked := value
	for keyword := range keywords {
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(keyword) + `\b`)
		if !re.MatchString(masked) {
			continue
		}
		hits = append(hits, keyword)
		masked = re.ReplaceAllString(masked, "__allowlisted_keyword__")
	}
	return masked, hits
}

func slicesClone[T any](src []T) []T {
	dst := make([]T, len(src))
	copy(dst, src)
	return dst
}
