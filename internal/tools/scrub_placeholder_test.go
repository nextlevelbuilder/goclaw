package tools

import (
	"context"
	"strings"
	"testing"
)

func TestScrubKeepsSecretPlaceholdersIntact(t *testing.T) {
	const ph = "{{secret:github-token}}"
	in := "use " + ph + " here, and api_key=abcdefgh12345678 must go"
	for name, got := range map[string]string{
		"plain": ScrubCredentials(in),
		"ctx":   ScrubCredentialsCtx(context.Background(), in),
	} {
		if !strings.Contains(got, ph) {
			t.Errorf("%s: placeholder was altered: %q", name, got)
		}
		if strings.Contains(got, "abcdefgh12345678") {
			t.Errorf("%s: credential next to a placeholder was not scrubbed: %q", name, got)
		}
	}
}

func TestScrubStillRedactsSecretKeyValueWithoutPlaceholder(t *testing.T) {
	got := ScrubCredentials("secret:abcdefghijkl")
	if strings.Contains(got, "abcdefghijkl") {
		t.Errorf("secret value not scrubbed: %q", got)
	}
}
