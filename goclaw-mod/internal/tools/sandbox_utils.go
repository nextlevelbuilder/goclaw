package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	delegationArtifactInputsDirName  = "inputs"
	delegationArtifactOutputsDirName = "outputs"
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

func sandboxContainerWorkdir(ctx context.Context) string {
	if cfg := SandboxConfigFromCtx(ctx); cfg != nil {
		return cfg.ContainerWorkdir()
	}
	return sandbox.DefaultContainerWorkdir
}

// acquireToolSandbox attaches the delegation input exchange only to the exact
// delegated run that owns it. The input/output sibling check prevents a forged
// context from turning this runtime-only option into general host mounting.
func acquireToolSandbox(
	ctx context.Context,
	manager sandbox.Manager,
	key, workspace string,
) (sandbox.Sandbox, error) {
	cfg := SandboxConfigFromCtx(ctx)
	inputRoot := DelegationArtifactInputsFromCtx(ctx)
	if inputRoot == "" {
		return manager.Get(ctx, key, workspace, cfg)
	}
	delegationID, err := uuid.Parse(DelegationIDFromCtx(ctx))
	if err != nil {
		return nil, fmt.Errorf("delegation artifact sandbox context is invalid")
	}

	outputRoot := ToolWorkspaceFromCtx(ctx)
	inputCanonical, err := canonicalRealDirectory(inputRoot)
	if err != nil {
		return nil, fmt.Errorf("delegation artifact input mount is unavailable")
	}
	outputCanonical, err := canonicalRealDirectory(outputRoot)
	if err != nil {
		return nil, fmt.Errorf("delegation artifact output mount is unavailable")
	}
	if filepath.Base(inputCanonical) != delegationArtifactInputsDirName ||
		filepath.Base(outputCanonical) != delegationArtifactOutputsDirName ||
		filepath.Dir(inputCanonical) != filepath.Dir(outputCanonical) {
		return nil, fmt.Errorf("delegation artifact sandbox context is invalid")
	}
	exchangeRoot := filepath.Dir(inputCanonical)
	if filepath.Base(exchangeRoot) != delegationID.String() ||
		filepath.Base(filepath.Dir(exchangeRoot)) != "delegations" ||
		filepath.Base(filepath.Dir(filepath.Dir(exchangeRoot))) != "collaboration" {
		return nil, fmt.Errorf("delegation artifact sandbox context is invalid")
	}

	mount := sandbox.ReadOnlyMount{
		Name:        "inputs",
		HostPath:    inputCanonical,
		Destination: path.Join(sandboxContainerWorkdir(ctx), "inputs"),
	}
	return manager.Get(
		ctx,
		key,
		workspace,
		cfg,
		sandbox.WithWorkspaceAccessOverride(sandbox.AccessRW),
		sandbox.WithReadOnlyMounts(mount),
	)
}

func canonicalRealDirectory(raw string) (string, error) {
	if raw == "" || !filepath.IsAbs(raw) {
		return "", fmt.Errorf("directory path must be absolute")
	}
	clean := filepath.Clean(raw)
	info, err := os.Lstat(clean)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("directory path is not a real directory")
	}
	real, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", fmt.Errorf("directory path is not canonical")
	}
	return filepath.Clean(real), nil
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
