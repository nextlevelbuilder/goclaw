package tools

import "strings"

// --- Hint messages for LLM consumption ---

const hintBinaryNotFound = "\n\n[SANDBOX] This command ran inside a Docker sandbox container. " +
	"The required tool/binary is not installed in the sandbox image. " +
	"Tell the user this failed due to sandbox environment limitations — " +
	"they can install the binary in the sandbox image or disable sandbox mode for this agent."

const hintPermissionDenied = "\n\n[SANDBOX] Permission denied inside sandbox container. " +
	"The workspace may be mounted as read-only (workspace_access: ro). " +
	"Check the agent's sandbox configuration or tell the user to change workspace_access to rw."

const hintNetworkDisabled = "\n\n[SANDBOX] Network operation failed — sandbox networking is disabled (--network none). " +
	"If this agent needs internet access, tell the user to enable network_enabled in the agent's sandbox configuration."

const hintReadOnlyFS = "\n\n[SANDBOX] Write failed — target path is outside the mounted workspace volume. " +
	"The sandbox filesystem is read-only except for the workspace mount. " +
	"Ensure all file operations use paths within the workspace directory."

const hintNoSuchFile = "\n\n[SANDBOX] File or directory not found inside sandbox container. " +
	"The sandbox has a minimal filesystem — only the workspace mount and installed packages are available. " +
	"Verify the path exists within the workspace, or install required files in the sandbox image."

const hintResourceLimit = "\n\n[SANDBOX] Sandbox resource limit reached (disk space or memory). " +
	"The container has restricted resources. Tell the user to increase sandbox limits " +
	"or clean up temporary files in the workspace."

// --- Detection functions ---

// isBinaryNotFound returns true if the error indicates a missing binary.
// Exit code 127 is the POSIX convention for "command not found".
func isBinaryNotFound(exitCode int, output string) bool {
	if exitCode == 127 {
		return true
	}
	lower := strings.ToLower(output)
	return strings.Contains(lower, "not found") &&
		(strings.Contains(lower, "command") || strings.Contains(lower, "sh:"))
}

func isPermissionDenied(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "permission denied") || strings.Contains(lower, "eacces")
}

func isNetworkDisabled(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "network is unreachable") ||
		strings.Contains(lower, "name resolution")
	// Note: "connection refused" is NOT matched here — it indicates a service
	// not running, not network disabled. --network none produces "network is
	// unreachable" or DNS resolution failures, not "connection refused".
}

func isReadOnlyFS(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "read-only file system") || strings.Contains(lower, "erofs")
}

func isResourceLimit(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "no space left") ||
		strings.Contains(lower, "enospc") ||
		strings.Contains(lower, "cannot allocate memory")
}

// --- Public API ---

// MaybeSandboxHint returns an LLM-actionable hint suffix for sandbox exec errors.
// Checks multiple error patterns in priority order. Returns empty string if no
// pattern matches. Only call from sandbox execution paths.
func MaybeSandboxHint(exitCode int, output string) string {
	if isBinaryNotFound(exitCode, output) {
		return hintBinaryNotFound
	}
	if isPermissionDenied(output) {
		return hintPermissionDenied
	}
	if isNetworkDisabled(output) {
		return hintNetworkDisabled
	}
	if isReadOnlyFS(output) {
		return hintReadOnlyFS
	}
	if isResourceLimit(output) {
		return hintResourceLimit
	}
	return ""
}

// MaybeFsBridgeHint returns an LLM-actionable hint for FsBridge errors
// (filesystem tool sandbox operations). FsBridge errors are Go errors,
// not exit codes, so we match against err.Error() text.
func MaybeFsBridgeHint(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if isPermissionDenied(msg) {
		return hintPermissionDenied
	}
	if isReadOnlyFS(msg) {
		return hintReadOnlyFS
	}
	if strings.Contains(strings.ToLower(msg), "no such file") {
		return hintNoSuchFile
	}
	if isResourceLimit(msg) {
		return hintResourceLimit
	}
	return ""
}
