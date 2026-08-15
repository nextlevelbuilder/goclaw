package channelmemory

import (
	"regexp"
	"slices"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type RedactionResult struct {
	Messages []store.PendingMessage
	Count    int
	Types    []string
}

type Redactor struct {
	patterns []namedPattern
}

type namedPattern struct {
	name string
	re   *regexp.Regexp
}

func NewRedactor() *Redactor {
	return &Redactor{patterns: []namedPattern{
		{"secret", regexp.MustCompile(`(?i)\b(api[_-]?key|token|secret|password|passwd|pwd)\b\s*[:=]\s*['"]?[^'" \n\r]+`)},
		{"token", regexp.MustCompile(`(?i)\b(bearer|sk-[a-z0-9_-]{12,}|ghp_[a-z0-9_]+|xox[baprs]-[a-z0-9-]+)\b[^\s]*`)},
		{"connection_string", regexp.MustCompile(`(?i)\b(postgres|postgresql|mysql|redis|mongodb)://[^\s]+`)},
		{"payment", regexp.MustCompile(`\b(?:\d[ -]*?){13,19}\b`)},
		{"phone", regexp.MustCompile(`(?i)(\+?\d[\d .()-]{7,}\d)`)},
		{"email", regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)},
	}}
}

func (r *Redactor) Redact(messages []store.PendingMessage, cfg Config) RedactionResult {
	excludePatterns := compileUserPatterns(cfg.ExcludePatterns)
	out := make([]store.PendingMessage, 0, len(messages))
	types := make([]string, 0)
	count := 0
	for _, msg := range messages {
		if slices.Contains(cfg.ExcludeUsers, msg.SenderID) {
			count++
			addType(&types, "excluded_user")
			continue
		}
		body := msg.Body
		excluded := false
		for _, re := range excludePatterns {
			if re.MatchString(body) {
				excluded = true
				count++
				addType(&types, "excluded_pattern")
				break
			}
		}
		if excluded {
			continue
		}
		for _, p := range r.patterns {
			next := p.re.ReplaceAllString(body, "[REDACTED:"+p.name+"]")
			if next != body {
				count++
				addType(&types, p.name)
				body = next
			}
		}
		msg.Body = strings.TrimSpace(body)
		if msg.Body != "" {
			out = append(out, msg)
		}
	}
	return RedactionResult{Messages: out, Count: count, Types: types}
}

func compileUserPatterns(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		if re, err := regexp.Compile(pattern); err == nil {
			out = append(out, re)
		}
	}
	return out
}

func addType(types *[]string, typ string) {
	if !slices.Contains(*types, typ) {
		*types = append(*types, typ)
	}
}
