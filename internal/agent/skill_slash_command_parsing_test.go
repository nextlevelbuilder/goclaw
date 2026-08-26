package agent

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// The tokenizer used to split the slash command off with strings.Cut(after, " ") — a
// literal space. Typing the command and pressing Enter before the rest of the message
// produced the target "ck:git\nreview", which matches no skill, and the user was told the
// skill did not exist while it sat in the suggestion list right below. Multi-line
// messages are ordinary in Slack and Telegram, so this was reachable in normal use.
func TestParseSkillSlashCommand_SeparatorIsAnyWhitespace(t *testing.T) {
	cfg := config.SkillSlashCommandConfig{Enabled: new(true), Prefix: "/"}

	for _, tc := range []struct {
		name       string
		message    string
		wantTarget string
		wantRest   string
	}{
		{"space", "/ck:git review the diff", "ck:git", "review the diff"},
		{"newline", "/ck:git\nreview the diff", "ck:git", "review the diff"},
		{"crlf", "/ck:git\r\nreview the diff", "ck:git", "review the diff"},
		{"blank line between", "/ck:git\n\nreview the diff", "ck:git", "review the diff"},
		{"tab", "/ck:git\treview the diff", "ck:git", "review the diff"},
		{"no arguments", "/ck:git", "ck:git", ""},
		{"trailing newline only", "/ck:git\n", "ck:git", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, ok := parseSkillSlashCommand(tc.message, cfg.EffectivePrefix())
			if !ok {
				t.Fatalf("parse failed for %q", tc.message)
			}
			if parsed.target != tc.wantTarget {
				t.Errorf("target = %q, want %q", parsed.target, tc.wantTarget)
			}
			if parsed.rest != tc.wantRest {
				t.Errorf("rest = %q, want %q", parsed.rest, tc.wantRest)
			}
		})
	}
}

// The verb forms split on the same separator.
func TestParseSkillSlashCommand_VerbsAcceptNewline(t *testing.T) {
	cfg := config.SkillSlashCommandConfig{Enabled: new(true), Prefix: "/"}

	parsed, ok := parseSkillSlashCommand("/help\nck:git", cfg.EffectivePrefix())
	if !ok || parsed.verb != "help" || parsed.target != "ck:git" {
		t.Fatalf("help: got verb=%q target=%q ok=%v", parsed.verb, parsed.target, ok)
	}

	parsed, ok = parseSkillSlashCommand("/use\nck:git and then stop", cfg.EffectivePrefix())
	if !ok || parsed.verb != "use" || parsed.rest != "ck:git and then stop" {
		t.Fatalf("use: got verb=%q rest=%q ok=%v", parsed.verb, parsed.rest, ok)
	}
}

// A newline-separated command must reach the same skill as the space-separated one.
func TestResolveSkillSlashCommand_NewlineActivatesSameSkill(t *testing.T) {
	loader := newSlashTestLoader(t)
	cfg := config.SkillSlashCommandConfig{Enabled: new(true), Prefix: "/"}

	spaced := resolveSkillSlashCommand(context.Background(), loader, nil, cfg, "/frontend-design build a landing page")
	newlined := resolveSkillSlashCommand(context.Background(), loader, nil, cfg, "/frontend-design\nbuild a landing page")

	if spaced.Kind != skillSlashCommandActivate {
		t.Fatalf("space form did not activate: kind=%v", spaced.Kind)
	}
	if newlined.Kind != spaced.Kind {
		t.Fatalf("newline form kind = %v, want %v", newlined.Kind, spaced.Kind)
	}
	if newlined.Skill.Slug != spaced.Skill.Slug {
		t.Fatalf("newline form matched %q, want %q", newlined.Skill.Slug, spaced.Skill.Slug)
	}
	if newlined.RemainingPrompt != spaced.RemainingPrompt {
		t.Fatalf("newline remainder = %q, want %q", newlined.RemainingPrompt, spaced.RemainingPrompt)
	}
}

// hasFieldPrefix must not treat a longer skill name as a match for a shorter one.
func TestMatchSkillCommandTarget_RequiresWholeField(t *testing.T) {
	loader := newSlashTestLoader(t)
	cfg := config.SkillSlashCommandConfig{Enabled: new(true), Prefix: "/"}

	// "frontend-design-extra" is not a skill; without partial matching this must not
	// resolve to "frontend-design" merely because that is a string prefix of it.
	res := resolveSkillSlashCommand(context.Background(), loader, nil, cfg, "/frontend-design-extra do a thing")
	if res.Kind == skillSlashCommandActivate {
		t.Fatalf("string-prefix match should not activate, got %q", res.Skill.Slug)
	}
}
