// Package skills — Shell-in-prompt execution (CP-07).
// Replaces !`command` patterns in SKILL.md with command output at load time.
// Security: only local/trusted skills allowed — MCP and plugin sources blocked.
package skills

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var shellPattern = regexp.MustCompile("!`([^`]+)`")

// ExecuteShellInPrompt replaces !`command` patterns in skill markdown
// with the command's stdout output.
//
// Security rules:
//   - source "mcp", "remote", "plugin" → shell commands stripped (not executed)
//   - source "workspace", "global", "builtin", "personal" → executed
//   - Each command has a 5-second timeout
//   - Output is truncated at 2000 chars
func ExecuteShellInPrompt(markdown string, source string, workDir string) string {
	// Block remote/untrusted sources
	if isUntrustedSource(source) {
		return StripShellCommands(markdown)
	}

	return shellPattern.ReplaceAllStringFunc(markdown, func(match string) string {
		if len(match) < 4 {
			return match
		}
		cmd := match[2 : len(match)-1] // extract between !` and `

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		proc := exec.CommandContext(ctx, "sh", "-c", cmd)
		if workDir != "" {
			proc.Dir = workDir
		}
		out, err := proc.Output()
		if err != nil {
			return fmt.Sprintf("[shell error: %v]", err)
		}

		result := strings.TrimSpace(string(out))
		if len(result) > 2000 {
			result = result[:2000] + "\n... (truncated)"
		}
		return result
	})
}

// StripShellCommands removes !`...` patterns from untrusted markdown.
func StripShellCommands(markdown string) string {
	return shellPattern.ReplaceAllString(markdown, "[shell command removed — untrusted source]")
}

// HasShellCommands checks if markdown contains shell commands.
func HasShellCommands(markdown string) bool {
	return shellPattern.MatchString(markdown)
}

func isUntrustedSource(source string) bool {
	switch source {
	case "mcp", "remote", "plugin", "marketplace":
		return true
	}
	return false
}
