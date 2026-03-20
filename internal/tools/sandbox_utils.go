package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// SandboxCwd maps the current effective workspace (from context) to its
// corresponding path inside the sandbox container. The sandbox mounts the
// global workspace root at containerBase (usually "/workspace"). This function
// computes the relative path from globalWorkspace to the context workspace
// and joins it with containerBase.
//
// Example: globalWorkspace="/app/workspace", ctx workspace="/app/workspace/agent-a/user-123"
// → returns "/workspace/agent-a/user-123"
func SandboxCwd(ctx context.Context, globalWorkspace, containerBase string) (string, error) {
	ws := ToolWorkspaceFromCtx(ctx)
	if ws == "" {
		// No per-request workspace — fall back to container root.
		return containerBase, nil
	}

	rel, err := filepath.Rel(globalWorkspace, ws)
	if err != nil || strings.HasPrefix(filepath.Clean(rel), "..") {
		return "", fmt.Errorf("workspace %q is outside global mount %q", ws, globalWorkspace)
	}

	if rel == "." {
		return containerBase, nil
	}
	return filepath.Join(containerBase, rel), nil
}

// ResolveSandboxPath resolves a tool-provided path (relative or absolute)
// against the sandbox container CWD. If the path is relative, it is joined
// with containerCwd. Absolute paths are returned as-is (the sandbox
// filesystem already restricts access to the mounted volume).
func ResolveSandboxPath(path, containerCwd string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(containerCwd, path)
}
