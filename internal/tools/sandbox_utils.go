package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// MapHostPathToSandbox maps a host workspace path to its corresponding path inside
// the sandbox container (e.g. mapping /app/workspace/agent-ws/user-id to /workspace/user-id).
// It ensures that the path is within the global workspace mount.
func MapHostPathToSandbox(ctx context.Context, hostPath, globalWorkspace string) (string, error) {
	containerBase := ToolSandboxDirFromCtx(ctx)
	if containerBase == "" {
		containerBase = "/workspace" // standard fallback
	}

	// If hostPath matches globalWorkspace exactly, it's the root.
	if filepath.Clean(hostPath) == filepath.Clean(globalWorkspace) {
		return containerBase, nil
	}

	// Calculate relative path from global workspace mount.
	rel, err := filepath.Rel(globalWorkspace, hostPath)
	if err != nil || strings.HasPrefix(filepath.Clean(rel), "..") {
		return "", fmt.Errorf("path (%s) is outside the global sandbox mount (%s)", hostPath, globalWorkspace)
	}

	// Join with container root (usually /workspace).
	return filepath.Join(containerBase, rel), nil
}
