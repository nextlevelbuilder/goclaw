package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// GenerateCredentialContext builds a TOOLS.md supplement from enabled secure CLI configs.
// This context is injected into the agent's system prompt so the LLM knows:
// - Which CLIs are available with pre-configured auth
// - That these CLIs run in Direct Exec Mode (no shell operators)
// - Which operations are blocked per CLI
// Returns empty string if no credentialed CLIs are configured.
func GenerateCredentialContext(creds []store.SecureCLIBinary) string {
	if len(creds) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\n## Credentialed CLI Tools\n\n")
	b.WriteString("The following CLI tools have pre-configured authentication.\n")
	b.WriteString("Credentials are injected automatically — do NOT attempt to provide or read credentials.\n\n")
	b.WriteString("⚠️ CRITICAL: These tools run in DIRECT EXEC MODE (no shell).\n")
	b.WriteString("- Do NOT use shell operators: ;  &&  ||  |  >  >>  <  $()  ``\n")
	b.WriteString("- Do NOT use environment variables: $VAR, ${VAR}\n")
	b.WriteString("- Each exec() call runs ONE command only\n")
	b.WriteString("- Use --json or --format=json for structured output\n")
	b.WriteString("- Parse JSON output directly — do NOT pipe to jq\n\n")
	b.WriteString("### Available CLIs:\n\n")

	hasGit := false
	for _, c := range creds {
		b.WriteString(fmt.Sprintf("**%s** — %s\n", c.BinaryName, c.Description))
		if blocked := summarizeDenyPatterns(c.DenyArgs); blocked != "" {
			b.WriteString(fmt.Sprintf("  Blocked: %s\n", blocked))
		}
		if c.Tips != "" {
			b.WriteString(fmt.Sprintf("  Tip: %s\n", c.Tips))
		}
		b.WriteString("\n")
		if c.AdapterName != nil && *c.AdapterName == "git" {
			hasGit = true
		}
	}

	if hasGit {
		// Git adapter has fixed, predictable semantics worth surfacing to the
		// LLM up front. Keeps the agent from attempting workarounds (writing
		// to ~/.gitconfig, exporting GIT_USERNAME, etc.) when a subcommand is
		// outside the auto-auth set.
		b.WriteString("### git (adapter-managed):\n")
		b.WriteString("- Auto-authenticated subcommands: `clone`, `fetch`, `pull`, `push`, `submodule`.\n")
		b.WriteString("- Other subcommands (status, log, diff, commit, branch, …) run WITHOUT credentials — safe for read-only repo work.\n")
		b.WriteString("- `git config --global` is denied by policy.\n")
		b.WriteString("- Auth is host-scoped: a credential for `github.com` will NOT authenticate to `gitlab.com`.\n")
		b.WriteString("- Do NOT attempt to print, copy, or modify the credential — it is injected per-process only.\n\n")
	}

	b.WriteString("### When a credentialed CLI command is blocked:\n")
	b.WriteString("This section applies ONLY to commands that return a `[CREDENTIALED EXEC]` error.\n")
	b.WriteString("Tell the user: \"This credentialed CLI operation is blocked by policy and may require admin approval.\"\n")
	b.WriteString("Do NOT attempt workarounds to bypass blocked credentialed CLI commands.\n")
	return b.String()
}

// summarizeDenyPatterns converts regex deny patterns to a human-readable summary.
// E.g. ["auth\\s+", "ssh-key", "repo\\s+delete"] -> "auth, ssh-key, repo delete"
func summarizeDenyPatterns(patternsJSON json.RawMessage) string {
	if len(patternsJSON) == 0 {
		return ""
	}
	var patterns []string
	if err := json.Unmarshal(patternsJSON, &patterns); err != nil || len(patterns) == 0 {
		return ""
	}
	// Convert regex patterns to readable form by stripping common regex syntax
	readable := make([]string, 0, len(patterns))
	replacer := strings.NewReplacer(`\s+`, " ", `\s*`, " ", `\b`, "", `\w+`, "*")
	for _, p := range patterns {
		readable = append(readable, replacer.Replace(p))
	}
	return strings.Join(readable, ", ")
}
