package providers

import (
	"regexp"
	"testing"
)

func TestShellDenyPatterns_AllowDockerFormatFlag(t *testing.T) {
	var patterns []*regexp.Regexp
	for _, raw := range ShellDenyPatterns {
		patterns = append(patterns, regexp.MustCompile(raw))
	}

	allowed := []string{
		"docker info --format '{{.OSType}}'",
		"docker inspect --format '{{json .State}}' container",
	}
	for _, cmd := range allowed {
		for _, pattern := range patterns {
			if pattern.MatchString(cmd) {
				t.Fatalf("unexpected deny for %q (matched %s)", cmd, pattern.String())
			}
		}
	}

	denied := []string{
		"format c:",
		"cmd /c format d:",
	}
	for _, cmd := range denied {
		matched := false
		for _, pattern := range patterns {
			if pattern.MatchString(cmd) {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("expected deny for %q", cmd)
		}
	}
}
