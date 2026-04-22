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
	workspace       string
	restrict        bool
	allowedPrefixes []string                    // extra allowed path prefixes (cross-drive on Windows)
	deniedPrefixes  []string                    // path prefixes to deny access to (e.g. .goclaw)
	sandboxMgr      sandbox.Manager
	contextFileIntc *ContextFileInterceptor     // nil = no virtual FS routing
	memIntc         *MemoryInterceptor          // nil = no memory routing
	permStore       store.ConfigPermissionStore // nil = no group write restriction
	workspaceIntc   *WorkspaceInterceptor       // nil = no team workspace validation
	vaultIntc       *VaultInterceptor           // nil = no vault registration
}

// AllowPaths adds extra path prefixes that write_file is allowed to access
// even when restrict_to_workspace is true (e.g. cross-drive on Windows).
func (t *WriteFileTool) AllowPaths(prefixes ...string) {
	t.allowedPrefixes = append(t.allowedPrefixes, prefixes...)
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

// SetConfigPermStore enables group write permission checks.
func (t *WriteFileTool) SetConfigPermStore(s store.ConfigPermissionStore) {
	t.permStore = s
}

// SetWorkspaceInterceptor enables team workspace validation and event broadcasting.
func (t *WriteFileTool) SetWorkspaceInterceptor(intc *WorkspaceInterceptor) {
	t.workspaceIntc = intc
}

// SetVaultInterceptor enables vault document registration on file writes.
func (t *WriteFileTool) SetVaultInterceptor(v *VaultInterceptor) {
	t.vaultIntc = v
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
	return "Write content to a file, creating directories as needed. " +
		"IMPORTANT: content longer than ~12000 characters may be truncated by the API. " +
		"For large files, use the edit tool to build the file in sections, or split into multiple write_file calls with append=true."
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
			"append": map[string]any{
				"type":        "boolean",
				"description": "Append content to the file instead of overwriting. Use this to build large files in chunks.",
			},
			"deliver": map[string]any{
				"type":        "boolean",
				"description": "Deliver this file to the user as an attachment. Defaults to true. Set to false ONLY for intermediate/temporary files the user will never see (e.g. config, cache, temp scripts). For any file the user requested or should receive, keep true (default).",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, args map[string]any) *Result {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	appendMode, _ := args["append"].(bool)
	deliver := true
	if v, ok := args["deliver"].(bool); ok {
		deliver = v
	}
	if path == "" {
		return ErrorResult("path is required")
	}

	// Group write permission check
	if t.permStore != nil {
		if err := store.CheckFileWriterPermission(ctx, t.permStore); err != nil {
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
		if mwr, err := t.memIntc.WriteFile(ctx, path, content, appendMode); mwr.Handled {
			if err != nil {
				return ErrorResult(fmt.Sprintf("failed to write memory file: %v", err))
			}
			msg := fmt.Sprintf("Memory file written: %s (%d bytes)", path, len(content))
			if mwr.KGTriggered {
				msg += "\n\n[Knowledge graph extraction triggered in background. The knowledge system may take a moment to fully update with new entities and relationships.]"
			}
			if mwr.PreviousContent != "" {
				prev := mwr.PreviousContent
				prevRunes := []rune(prev)
				if len(prevRunes) > 4000 {
					prev = string(prevRunes[:4000]) + "\n... (truncated)"
				}
				msg += fmt.Sprintf("\n\n⚠️ WARNING: This file had existing content (%d chars) that was replaced. "+
					"If the old content below contains information not present in your new version, "+
					"please re-write the file to merge both.\n\n"+
					"--- PREVIOUS CONTENT ---\n%s\n--- END PREVIOUS CONTENT ---",
					len([]rune(mwr.PreviousContent)), prev)
			}
			return SilentResult(msg)
		}
	}

	// Sandbox routing (sandboxKey from ctx — thread-safe)
	sandboxKey := ToolSandboxKeyFromCtx(ctx)
	if t.sandboxMgr != nil && sandboxKey != "" {
		return t.executeInSandbox(ctx, path, content, sandboxKey, deliver, appendMode)
	}

	// Host execution — use per-user workspace from context if available
	workspace := ToolWorkspaceFromCtx(ctx)
	if workspace == "" {
		workspace = t.workspace
	}
	allowed := allowedWithTeamWorkspace(ctx, t.allowedPrefixes)
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

	if appendMode {
		f, err := os.OpenFile(resolved, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return ErrorResult(fmt.Sprintf("failed to open file for append: %v", err))
		}
		_, err = f.WriteString(content)
		f.Close()
		if err != nil {
			return ErrorResult(fmt.Sprintf("failed to append to file: %v", err))
		}
	} else if err := os.WriteFile(resolved, []byte(content), 0644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write file: %v", err))
	}

	if t.workspaceIntc != nil {
		t.workspaceIntc.AfterWrite(ctx, resolved, "write")
	}

	if t.vaultIntc != nil {
		go t.vaultIntc.AfterWrite(context.WithoutCancel(ctx), resolved, content)
	}

	verb := "written"
	if appendMode {
		verb = "appended"
	}
	msg := fmt.Sprintf("File %s: %s (%d bytes)", verb, path, len(content))
	if deliver {
		msg += ". File will be automatically delivered to the user — do NOT send it again via message tool."
	}
	result := SilentResult(msg)
	result.Deliverable = content
	if deliver {
		result.Media = []bus.MediaFile{{Path: resolved, Filename: filepath.Base(resolved)}}
		// Track delivered path so message tool's self-send guard can detect duplicates.
		if dm := DeliveredMediaFromCtx(ctx); dm != nil {
			dm.Mark(resolved)
		}
	}
	return result
}

func (t *WriteFileTool) executeInSandbox(ctx context.Context, path, content, sandboxKey string, deliver, appendMode bool) *Result {
	bridge, err := t.getFsBridge(ctx, sandboxKey)
	if err != nil {
		return ErrorResult(fmt.Sprintf("sandbox error: %v", err))
	}

	containerCwd, cwdErr := SandboxCwd(ctx, t.workspace, sandbox.DefaultContainerWorkdir)
	if cwdErr != nil {
		return ErrorResult(fmt.Sprintf("sandbox path mapping: %v", cwdErr))
	}
	containerPath := ResolveSandboxPath(path, containerCwd)

	if err := bridge.WriteFile(ctx, containerPath, content, appendMode); err != nil {
		verb := "write"
		if appendMode {
			verb = "append to"
		}
		return ErrorResult(fmt.Sprintf("failed to %s file: %v", verb, err) + MaybeFsBridgeHint(err))
	}

	verb := "written"
	if appendMode {
		verb = "appended"
	}
	msg := fmt.Sprintf("File %s: %s (%d bytes)", verb, path, len(content))
	if deliver {
		msg += ". File will be automatically delivered to the user — do NOT send it again via message tool."
	}
	result := SilentResult(msg)
	result.Deliverable = content
	if deliver {
		// Sandbox workspace is bind-mounted — resolve to host path for delivery
		workspace := ToolWorkspaceFromCtx(ctx)
		if workspace == "" {
			workspace = t.workspace
		}
		hostPath := filepath.Join(workspace, path)
		result.Media = []bus.MediaFile{{Path: hostPath, Filename: filepath.Base(hostPath)}}
		if dm := DeliveredMediaFromCtx(ctx); dm != nil {
			dm.Mark(hostPath)
		}
	}
	return result
}

func (t *WriteFileTool) getFsBridge(ctx context.Context, sandboxKey string) (*sandbox.FsBridge, error) {
	sb, err := t.sandboxMgr.Get(ctx, sandboxKey, t.workspace, SandboxConfigFromCtx(ctx))
	if err != nil {
		return nil, err
	}
	return sandbox.NewFsBridge(sb.ID(), sandbox.DefaultContainerWorkdir), nil
}
