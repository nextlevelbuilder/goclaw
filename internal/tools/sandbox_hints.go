package tools

import "strings"

// sandboxHint is appended to tool error output when a command fails inside
// a Docker sandbox container due to a missing binary/tool (exit code 127).
const sandboxHint = "\n\n[SANDBOX] This command ran inside a Docker sandbox container. " +
	"The required tool/binary is not installed in the sandbox image. " +
	"Tell the user this failed due to sandbox environment limitations — " +
	"they can install the binary in the sandbox image or disable sandbox mode for this agent."

// isBinaryNotFound returns true if the error output and exit code indicate
// a missing binary. Exit code 127 is the POSIX convention for "command not found".
func isBinaryNotFound(exitCode int, output string) bool {
	if exitCode == 127 {
		return true
	}
	// Fallback: match stderr patterns from non-standard shells.
	lower := strings.ToLower(output)
	return strings.Contains(lower, "not found") &&
		(strings.Contains(lower, "command") || strings.Contains(lower, "sh:"))
}

// MaybeSandboxHint returns the sandbox hint suffix if the error looks like
// a missing binary. Returns empty string otherwise.
// Only call this from sandbox execution paths — never from host execution.
func MaybeSandboxHint(exitCode int, output string) string {
	if isBinaryNotFound(exitCode, output) {
		return sandboxHint
	}
	return ""
}
