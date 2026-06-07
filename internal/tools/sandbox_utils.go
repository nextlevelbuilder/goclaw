package tools

import (
	"context"
	"fmt"
	"path"
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
	return path.Join(filepath.ToSlash(containerBase), filepath.ToSlash(rel)), nil
}

// ResolveSandboxPath resolves a tool-provided path (relative or absolute)
// against the sandbox container CWD. Escapes are rejected to containerCwd so a
// tool scoped to /workspace/agent-a cannot address /workspace/agent-b.
func ResolveSandboxPath(filePath, containerCwd string) string {
	cwd := path.Clean(containerCwd)
	if cwd == "." || cwd == "/" {
		cwd = "/workspace"
	}
	var resolved string
	if strings.HasPrefix(filePath, "/") {
		resolved = path.Clean(filePath)
	} else {
		resolved = path.Clean(path.Join(cwd, filePath))
	}
	if resolved == cwd || strings.HasPrefix(resolved, cwd+"/") {
		return resolved
	}
	return cwd
}
