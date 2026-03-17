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

	for _, c := range creds {
		b.WriteString(fmt.Sprintf("**%s** — %s\n", c.BinaryName, c.Description))
		if blocked := summarizeDenyPatterns(c.DenyArgs); blocked != "" {
			b.WriteString(fmt.Sprintf("  Blocked: %s\n", blocked))
		}
		if c.Tips != "" {
			b.WriteString(fmt.Sprintf("  Tip: %s\n", c.Tips))
		}
		b.WriteString("\n")
	}

	b.WriteString("### When a command is blocked:\n")
	b.WriteString("Tell the user: \"This operation requires admin approval and cannot be performed automatically.\"\n")
	b.WriteString("Do NOT attempt workarounds to bypass blocked commands.\n")
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
