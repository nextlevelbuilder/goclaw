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

// 14. Phase 5: when the git adapter is enabled on at least one preset, the
// generated TOOLS.md supplement includes the git-adapter usage block so the
// LLM knows which subcommands auto-authenticate and which run un-credentialed.
func TestGenerateCredentialContext_IncludesGit(t *testing.T) {
	git := "git"
	creds := []store.SecureCLIBinary{{
		BinaryName:  "git",
		Description: "git VCS",
		AdapterName: &git,
	}}

	out := GenerateCredentialContext(creds)

	wantContains := []string{
		"### git (adapter-managed):",
		"Auto-authenticated subcommands",
		"`clone`, `fetch`, `pull`, `push`, `submodule`",
		"host-scoped",
		"git config --global",
	}
	for _, s := range wantContains {
		if !strings.Contains(out, s) {
			t.Errorf("expected output to contain %q, but it did not.\nOutput:\n%s", s, out)
		}
	}
}

// When NO preset has adapter_name=="git", the git block must NOT appear —
// keeps TOOLS.md context minimal when only passthrough binaries are wired.
func TestGenerateCredentialContext_OmitsGitWhenAbsent(t *testing.T) {
	creds := []store.SecureCLIBinary{{
		BinaryName:  "gh",
		Description: "GitHub CLI",
	}}
	out := GenerateCredentialContext(creds)
	if strings.Contains(out, "### git (adapter-managed):") {
		t.Errorf("git block leaked into non-git context.\nOutput:\n%s", out)
	}
}
