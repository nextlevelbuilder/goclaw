package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// WriteFileTool writes content to a file, optionally through a sandbox container.
type WriteFileTool struct {
	workspace        string
	restrict         bool
	deniedPrefixes   []string // path prefixes to deny access to (e.g. .goclaw)
	sandboxMgr       sandbox.Manager
	contextFileIntc  *ContextFileInterceptor // nil = no virtual FS routing
	memIntc          *MemoryInterceptor      // nil = no memory routing
	groupWriterCache *store.GroupWriterCache // nil = no group write restriction
	workspaceIntc    *WorkspaceInterceptor   // nil = no team workspace validation
}

// DenyPaths adds path prefixes that write_file must reject.
func (t *WriteFileTool) DenyPaths(prefixes ...string) {
	t.deniedPrefixes = append(t.deniedPrefixes, prefixes...)
}

// SetContextFileInterceptor enables virtual FS routing for context files.
func (t *WriteFileTool) SetContextFileInterceptor(intc *ContextFileInterceptor) {
	t.contextFileIntc = intc
}

// SetMemoryInterceptor enables virtual FS routing for memory files.
func (t *WriteFileTool) SetMemoryInterceptor(intc *MemoryInterceptor) {
	t.memIntc = intc
}

// SetGroupWriterCache enables group write permission checks.
func (t *WriteFileTool) SetGroupWriterCache(c *store.GroupWriterCache) {
	t.groupWriterCache = c
}

// SetWorkspaceInterceptor enables team workspace validation and event broadcasting.
func (t *WriteFileTool) SetWorkspaceInterceptor(intc *WorkspaceInterceptor) {
	t.workspaceIntc = intc
}

func NewWriteFileTool(workspace string, restrict bool) *WriteFileTool {
	return &WriteFileTool{workspace: workspace, restrict: restrict}
}

func NewSandboxedWriteFileTool(workspace string, restrict bool, mgr sandbox.Manager) *WriteFileTool {
	return &WriteFileTool{workspace: workspace, restrict: restrict, sandboxMgr: mgr}
}

// SetSandboxKey is a no-op; sandbox key is now read from ctx (thread-safe).
func (t *WriteFileTool) SetSandboxKey(key string) {}

func (t *WriteFileTool) Name() string { return "write_file" }
func (t *WriteFileTool) Description() string {
	return "Write content to a file, creating directories as needed"
}
func (t *WriteFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path (relative to workspace, or absolute)",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Content to write",
			},
			"deliver": map[string]any{
				"type":        "boolean",
				"description": "Deliver this file to the user as an attachment. Defaults to true. Set to false for intermediate/temporary files (e.g. config, cache, temp scripts).",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, args map[string]any) *Result {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	deliver := true
	if v, ok := args["deliver"].(bool); ok {
		deliver = v
	}
	if path == "" {
		return ErrorResult("path is required")
	}

	// Group write permission check
	if t.groupWriterCache != nil {
		if err := store.CheckGroupWritePermission(ctx, t.groupWriterCache); err != nil {
			return ErrorResult(err.Error())
		}
	}

	// Virtual FS: route context files to DB
	if t.contextFileIntc != nil {
		if handled, err := t.contextFileIntc.WriteFile(ctx, path, content); handled {
			if err != nil {
				return ErrorResult(fmt.Sprintf("failed to write context file: %v", err))
			}
			return SilentResult(fmt.Sprintf("Context file written: %s (%d bytes)", path, len(content)))
		}
	}

	// Virtual FS: route memory files to DB
	if t.memIntc != nil {
		if mwr, err := t.memIntc.WriteFile(ctx, path, content); mwr.Handled {
			if err != nil {
				return ErrorResult(fmt.Sprintf("failed to write memory file: %v", err))
			}
			msg := fmt.Sprintf("Memory file written: %s (%d bytes)", path, len(content))
			if mwr.KGTriggered {
				msg += "\n\n[Knowledge graph extraction triggered in background. The knowledge system may take a moment to fully update with new entities and relationships.]"
			}
			return SilentResult(msg)
		}
	}

	// Sandbox routing (sandboxKey from ctx — thread-safe)
	sandboxKey := ToolSandboxKeyFromCtx(ctx)
	if t.sandboxMgr != nil && sandboxKey != "" {
		return t.executeInSandbox(ctx, path, content, sandboxKey, deliver)
	}

	// Host execution — use per-user workspace from context if available
	workspace := ToolWorkspaceFromCtx(ctx)
	if workspace == "" {
		workspace = t.workspace
	}
	allowed := allowedWithTeamWorkspace(ctx, nil)
	resolved, err := resolvePathWithAllowed(path, workspace, effectiveRestrict(ctx, t.restrict), allowed)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if err := checkDeniedPath(resolved, t.workspace, t.deniedPrefixes); err != nil {
		return ErrorResult(err.Error())
	}

	// Team workspace validation + delete-on-empty.
	if t.workspaceIntc != nil {
		isDelete, intcErr := t.workspaceIntc.HandleWrite(ctx, resolved, content)
		if intcErr != nil {
			return ErrorResult(intcErr.Error())
		}
		if isDelete {
			if err := os.Remove(resolved); err != nil && !os.IsNotExist(err) {
				return ErrorResult(fmt.Sprintf("failed to delete file: %v", err))
			}
			t.workspaceIntc.AfterWrite(ctx, resolved, "delete")
			return SilentResult(fmt.Sprintf("File deleted: %s", path))
		}
	}

	if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create directory: %v", err))
	}

	if err := os.WriteFile(resolved, []byte(content), 0644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write file: %v", err))
	}

	if t.workspaceIntc != nil {
		t.workspaceIntc.AfterWrite(ctx, resolved, "write")
	}

	result := SilentResult(fmt.Sprintf("File written: %s (%d bytes)", path, len(content)))
	result.Deliverable = content
	if deliver {
		result.Media = []bus.MediaFile{{Path: resolved}}
	}
	return result
}

func (t *WriteFileTool) executeInSandbox(ctx context.Context, path, content, sandboxKey string, deliver bool) *Result {
	bridge, err := t.getFsBridge(ctx, sandboxKey)
	if err != nil {
		return ErrorResult(fmt.Sprintf("sandbox error: %v", err))
	}

	if err := bridge.WriteFile(ctx, path, content); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write file: %v", err))
	}

	result := SilentResult(fmt.Sprintf("File written: %s (%d bytes)", path, len(content)))
	result.Deliverable = content
	if deliver {
		// Sandbox workspace is bind-mounted — resolve to host path for delivery
		workspace := ToolWorkspaceFromCtx(ctx)
		if workspace == "" {
			workspace = t.workspace
		}
		hostPath := filepath.Join(workspace, path)
		result.Media = []bus.MediaFile{{Path: hostPath}}
	}
	return result
}

func (t *WriteFileTool) getFsBridge(ctx context.Context, sandboxKey string) (*sandbox.FsBridge, error) {
	sb, err := t.sandboxMgr.Get(ctx, sandboxKey, t.workspace, SandboxConfigFromCtx(ctx))
	if err != nil {
		return nil, err
	}
	return sandbox.NewFsBridge(sb.ID(), "/workspace"), nil
}
