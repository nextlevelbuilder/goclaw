// Package agent — input guard for prompt injection detection.
//
// InputGuard scans user messages for known injection patterns.
// Action is configurable via gateway.injection_action:
//   - "log":   info-level logging (quiet)
//   - "warn":  warning-level logging (default)
//   - "block": reject the message with an error
//   - "off":   disable scanning entirely
package agent

import (
	"regexp"
	"strings"
)

// guardPattern pairs a human-readable name with a compiled regex.
type guardPattern struct {
	name    string
	pattern *regexp.Regexp
}

// InputGuard scans user input for known prompt injection patterns.
type InputGuard struct {
	patterns []guardPattern
}

// NewInputGuard creates an InputGuard with the default set of injection detection patterns.
func NewInputGuard() *InputGuard {
	return &InputGuard{
		patterns: defaultGuardPatterns(),
	}
}

// Scan checks a message against all known injection patterns.
// Returns the names of matched patterns (empty slice = no matches).
func (g *InputGuard) Scan(message string) []string {
	if message == "" {
		return nil
	}
	var matches []string
	for _, gp := range g.patterns {
		if gp.pattern.MatchString(message) {
			matches = append(matches, gp.name)
		}
	}
	return matches
}

// defaultGuardPatterns returns the built-in set of injection detection patterns.
// These are designed to detect common prompt injection techniques while
// minimizing false positives on legitimate user messages.
func defaultGuardPatterns() []guardPattern {
	return []guardPattern{
		{
			name:    "ignore_instructions",
			pattern: regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|prior|above|earlier|preceding)\s+(instructions?|rules?|prompts?|directives?|guidelines?)`),
		},
		{
			name:    "role_override",
			pattern: regexp.MustCompile(`(?i)(you are now|from now on you are|pretend you are|act as if you are|imagine you are)\s+`),
		},
		{
			name:    "system_tags",
			pattern: regexp.MustCompile(`(?i)</?system>|\[SYSTEM\]|\[INST\]|<<SYS>>|<\|im_start\|>system`),
		},
		{
			name:    "instruction_injection",
			pattern: regexp.MustCompile(`(?i)(new instructions?:|override:|system prompt:|<\|system\|>)`),
		},
		{
			name:    "null_bytes",
			pattern: regexp.MustCompile(`\x00`),
		},
		{
			name:    "delimiter_escape",
			pattern: regexp.MustCompile(`(?i)(end of system|begin user input|</?(instructions?|rules|prompt|context)>)`),
		},
	}
}

// HasPatterns returns true if the guard has any patterns configured.
func (g *InputGuard) HasPatterns() bool {
	return len(g.patterns) > 0
}

// PatternNames returns the names of all configured patterns.
func (g *InputGuard) PatternNames() []string {
	names := make([]string, len(g.patterns))
	for i, gp := range g.patterns {
		names[i] = gp.name
	}
	return names
}

// ContainsNullBytes is a fast check for null bytes without regex overhead.
func ContainsNullBytes(s string) bool {
	return strings.ContainsRune(s, 0)
}
