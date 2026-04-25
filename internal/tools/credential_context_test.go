package tools

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestGenerateCredentialContext_BlockedSectionScopedToMarker pins the wording
// that scopes the "blocked command" guidance to credentialed-CLI errors only.
// The LLM must see: (a) a header that says "credentialed CLI command",
// (b) the literal `[CREDENTIALED EXEC]` marker, and (c) the qualifier
// "credentialed CLI operation" — not the bare phrase from previous wording.
func TestGenerateCredentialContext_BlockedSectionScopedToMarker(t *testing.T) {
	creds := []store.SecureCLIBinary{{
		BinaryName:  "gh",
		Description: "GitHub CLI",
	}}

	out := GenerateCredentialContext(creds)

	wantContains := []string{
		"### When a credentialed CLI command is blocked:",
		"[CREDENTIALED EXEC]",
		"credentialed CLI operation",
	}
	for _, s := range wantContains {
		if !strings.Contains(out, s) {
			t.Errorf("expected output to contain %q, but it did not.\nOutput:\n%s", s, out)
		}
	}

	// Old unqualified wording must not survive — that wording over-generalized
	// to plain shell exec failures and caused unjustified pre-refusals.
	dontWant := "Tell the user: \"This operation requires admin approval"
	if strings.Contains(out, dontWant) {
		t.Errorf("output still contains unqualified wording %q.\nOutput:\n%s", dontWant, out)
	}
}
