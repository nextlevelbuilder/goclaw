package tools

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
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

func effectiveSandboxWorkspace(ctx context.Context, globalWorkspace string) (string, error) {
	if ws := ToolWorkspaceFromCtx(ctx); ws != "" {
		return canonicalSandboxWorkspace(ws), nil
	}
	if globalWorkspace != "" && store.IsMasterScope(ctx) {
		slog.Warn("security.sandbox_global_workspace_fallback",
			"workspace", globalWorkspace,
			"tenant_id", store.TenantIDFromContext(ctx),
			"agent_id", store.AgentIDFromContext(ctx))
		return canonicalSandboxWorkspace(globalWorkspace), nil
	}
	return "", fmt.Errorf("sandbox workspace unavailable for tenant-scoped execution")
}

func canonicalSandboxWorkspace(workspace string) string {
	clean := filepath.Clean(workspace)
	if real, err := filepath.EvalSymlinks(clean); err == nil {
		return real
	}
	return clean
}

func sandboxCwdForHostPath(hostCwd, mountWorkspace, containerBase string) (string, error) {
	if hostCwd == "" {
		hostCwd = mountWorkspace
	}
	if containerBase == "" {
		containerBase = "/workspace"
	}
	cleanMount := filepath.Clean(mountWorkspace)
	cleanCwd := filepath.Clean(hostCwd)
	rel, err := filepath.Rel(cleanMount, cleanCwd)
	if err != nil || strings.HasPrefix(filepath.Clean(rel), "..") {
		return "", fmt.Errorf("working directory %q is outside sandbox mount %q", hostCwd, mountWorkspace)
	}
	if rel == "." {
		return filepath.ToSlash(containerBase), nil
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
