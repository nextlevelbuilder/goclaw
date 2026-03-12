package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
)

// ListFilesTool lists files in a directory, optionally through a sandbox container.
type ListFilesTool struct {
	workspace       string
	restrict        bool
	deniedPrefixes  []string // path prefixes to deny access to (e.g. .goclaw)
	sandboxMgr      sandbox.Manager
	contextFileIntc *ContextFileInterceptor // unused, satisfies InterceptorAware
	memIntc         *MemoryInterceptor      // nil = no memory routing
}

func (t *ListFilesTool) SetContextFileInterceptor(intc *ContextFileInterceptor) {
	t.contextFileIntc = intc
}

func (t *ListFilesTool) SetMemoryInterceptor(intc *MemoryInterceptor) {
	t.memIntc = intc
}

// DenyPaths adds path prefixes that list_files must reject/filter.
func (t *ListFilesTool) DenyPaths(prefixes ...string) {
	t.deniedPrefixes = append(t.deniedPrefixes, prefixes...)
}

func NewListFilesTool(workspace string, restrict bool) *ListFilesTool {
	return &ListFilesTool{workspace: workspace, restrict: restrict}
}

func NewSandboxedListFilesTool(workspace string, restrict bool, mgr sandbox.Manager) *ListFilesTool {
	return &ListFilesTool{workspace: workspace, restrict: restrict, sandboxMgr: mgr}
}

// SetSandboxKey is a no-op; sandbox key is now read from ctx (thread-safe).
func (t *ListFilesTool) SetSandboxKey(key string) {}

func (t *ListFilesTool) Name() string        { return "list_files" }
func (t *ListFilesTool) Description() string { return "List files and directories in a path" }
func (t *ListFilesTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Directory path to list (default: workspace root)",
			},
		},
	}
}

func (t *ListFilesTool) Execute(ctx context.Context, args map[string]any) *Result {
	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}

	// Virtual FS: route memory directory listing to DB
	if t.memIntc != nil {
		if listing, handled, err := t.memIntc.ListFiles(ctx, path); handled {
			if err != nil {
				return ErrorResult(fmt.Sprintf("failed to list memory files: %v", err))
			}
			if listing == "" {
				return SilentResult("No memory files stored yet")
			}
			return SilentResult(listing)
		}
	}

	// Sandbox routing (sandboxKey from ctx — thread-safe)
	sandboxKey := ToolSandboxKeyFromCtx(ctx)
	if t.sandboxMgr != nil && sandboxKey != "" {
		return t.executeInSandbox(ctx, path, sandboxKey)
	}

	// Host execution — use per-user workspace from context if available
	workspace := ToolWorkspaceFromCtx(ctx)
	if workspace == "" {
		workspace = t.workspace
	}
	resolved, err := resolvePath(path, workspace, effectiveRestrict(ctx, t.restrict))
	if err != nil {
		return ErrorResult(err.Error())
	}
	if err := checkDeniedPath(resolved, t.workspace, t.deniedPrefixes); err != nil {
		return ErrorResult(err.Error())
	}

	entries, err := os.ReadDir(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return SilentResult(fmt.Sprintf("Directory does not exist: %s", path))
		}
		return ErrorResult(fmt.Sprintf("failed to list directory: %v", err))
	}

	var sb strings.Builder
	for _, entry := range entries {
		// Filter out denied directories from listing
		if entry.IsDir() && len(t.deniedPrefixes) > 0 {
			entryPath := filepath.Join(resolved, entry.Name())
			if checkDeniedPath(entryPath, t.workspace, t.deniedPrefixes) != nil {
				continue
			}
		}

		info, _ := entry.Info()
		if entry.IsDir() {
			fmt.Fprintf(&sb, "[DIR]  %s/\n", entry.Name())
		} else if info != nil {
			fmt.Fprintf(&sb, "[FILE] %s (%d bytes)\n", entry.Name(), info.Size())
		} else {
			fmt.Fprintf(&sb, "[FILE] %s\n", entry.Name())
		}
	}

	return SilentResult(sb.String())
}

func (t *ListFilesTool) executeInSandbox(ctx context.Context, path, sandboxKey string) *Result {
	bridge, err := t.getFsBridge(ctx, sandboxKey)
	if err != nil {
		return ErrorResult(fmt.Sprintf("sandbox error: %v", err))
	}

	output, err := bridge.ListDir(ctx, path)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to list directory: %v", err))
	}

	return SilentResult(output)
}

func (t *ListFilesTool) getFsBridge(ctx context.Context, sandboxKey string) (*sandbox.FsBridge, error) {
	sb, err := t.sandboxMgr.Get(ctx, sandboxKey, t.workspace, SandboxConfigFromCtx(ctx))
	if err != nil {
		return nil, err
	}
	return sandbox.NewFsBridge(sb.ID(), "/workspace"), nil
}
