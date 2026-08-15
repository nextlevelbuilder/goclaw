package providers

import (
	"regexp"
	"strings"
	"testing"
)

func TestGenerateHookScriptUsesConfiguredDenyPatterns(t *testing.T) {
	pattern := regexp.MustCompile(`\bpip3?\s+install\b`)

	script := generateHookScript("", true, []*regexp.Regexp{pattern})

	if !strings.Contains(script, pattern.String()) {
		t.Fatalf("hook script missing configured pattern %q", pattern.String())
	}
	if strings.Contains(script, `^\s*env\s*$`) {
		t.Fatalf("hook script included default env_dump pattern when configured patterns were supplied")
	}
}

func TestGenerateHookScriptAllowsConfiguredEmptyDenyPatterns(t *testing.T) {
	script := generateHookScript("", true, []*regexp.Regexp{})

	if strings.Contains(script, `^\s*env\s*$`) {
		t.Fatalf("hook script included default env_dump pattern for explicitly empty configured patterns")
	}
	if strings.Contains(script, `\bpip3?\s+install\b`) {
		t.Fatalf("hook script included package-install pattern for explicitly empty configured patterns")
	}
}

func TestGenerateHookScriptDefaultsWhenNoConfiguredDenyPatterns(t *testing.T) {
	script := generateHookScript("", true)

	if !strings.Contains(script, `^\s*env\s*$`) {
		t.Fatalf("hook script missing default env_dump pattern")
	}
}
