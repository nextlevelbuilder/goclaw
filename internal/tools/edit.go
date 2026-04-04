package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// EditTool performs search-and-replace edits on files.
// Supports context file interceptor and sandbox routing.
type EditTool struct {
	workspace       string
	restrict        bool
	deniedPrefixes  []string // path prefixes to deny access to (e.g. .goclaw)
	sandboxMgr      sandbox.Manager
	contextFileIntc *ContextFileInterceptor
	memIntc         *MemoryInterceptor
	permStore       store.ConfigPermissionStore // nil = no group write restriction
}

// DenyPaths adds path prefixes that edit must reject.
func (t *EditTool) DenyPaths(prefixes ...string) {
	t.deniedPrefixes = append(t.deniedPrefixes, prefixes...)
}

func (t *EditTool) SetContextFileInterceptor(intc *ContextFileInterceptor) {
	t.contextFileIntc = intc
}

func (t *EditTool) SetMemoryInterceptor(intc *MemoryInterceptor) {
	t.memIntc = intc
}

// SetConfigPermStore enables group write permission checks.
func (t *EditTool) SetConfigPermStore(s store.ConfigPermissionStore) {
	t.permStore = s
}

func NewEditTool(workspace string, restrict bool) *EditTool {
	return &EditTool{workspace: workspace, restrict: restrict}
}

func NewSandboxedEditTool(workspace string, restrict bool, mgr sandbox.Manager) *EditTool {
	return &EditTool{workspace: workspace, restrict: restrict, sandboxMgr: mgr}
}

func (t *EditTool) SetSandboxKey(key string) {}

func (t *EditTool) Name() string { return "edit" }
func (t *EditTool) Description() string {
	return "Edit a file by replacing exact text matches. Use old_string/new_string for precise edits without rewriting the entire file."
}

func (t *EditTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path (relative to workspace, or absolute)",
			},
			"old_string": map[string]any{
				"type":        "string",
				"description": "Exact text to find (must match uniquely unless replace_all is true)",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "Replacement text",
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "Replace all occurrences (default: false, requires unique match)",
			},
		},
		"required": []string{"path", "old_string", "new_string"},
	}
}

func (t *EditTool) Execute(ctx context.Context, args map[string]any) *Result {
	path, _ := args["path"].(string)
	oldStr, _ := args["old_string"].(string)
	newStr, _ := args["new_string"].(string)
	replaceAll, _ := args["replace_all"].(bool)

	if path == "" {
		return ErrorResult("path is required")
	}
	if oldStr == "" {
		return ErrorResult("old_string is required")
	}
	if oldStr == newStr {
		return ErrorResult("old_string and new_string are identical")
	}

	// Group write permission check
	if t.permStore != nil {
		if err := store.CheckFileWriterPermission(ctx, t.permStore); err != nil {
			return ErrorResult(err.Error())
		}
	}

	// Virtual FS: context files
	if t.contextFileIntc != nil {
		if content, handled, err := t.contextFileIntc.ReadFile(ctx, path); handled {
			if err != nil {
				return ErrorResult(fmt.Sprintf("failed to read context file: %v", err))
			}
			if content == "" {
				return ErrorResult(fmt.Sprintf("context file not found: %s", path))
			}
			newContent, result := applyEdit(content, oldStr, newStr, replaceAll)
			if result != nil {
				return result
			}
			if _, err := t.contextFileIntc.WriteFile(ctx, path, newContent); err != nil {
				return ErrorResult(fmt.Sprintf("failed to write context file: %v", err))
			}
			return SilentResult(fmt.Sprintf("Context file edited: %s", path))
		}
	}

	// Virtual FS: memory files
	if t.memIntc != nil {
		if content, handled, err := t.memIntc.ReadFile(ctx, path); handled {
			if err != nil {
				return ErrorResult(fmt.Sprintf("failed to read memory file: %v", err))
			}
			if content == "" {
				return ErrorResult(fmt.Sprintf("memory file not found: %s", path))
			}
			newContent, result := applyEdit(content, oldStr, newStr, replaceAll)
			if result != nil {
				return result
			}
			mwr, err := t.memIntc.WriteFile(ctx, path, newContent, false)
			if err != nil {
				return ErrorResult(fmt.Sprintf("failed to write memory file: %v", err))
			}
			msg := fmt.Sprintf("Memory file edited: %s", path)
			if mwr.KGTriggered {
				msg += "\n\n[Knowledge graph extraction triggered in background. The knowledge system may take a moment to fully update with new entities and relationships.]"
			}
			return SilentResult(msg)
		}
	}

	// Sandbox routing
	sandboxKey := ToolSandboxKeyFromCtx(ctx)
	if t.sandboxMgr != nil && sandboxKey != "" {
		return t.executeInSandbox(ctx, path, oldStr, newStr, replaceAll, sandboxKey)
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

	data, err := os.ReadFile(resolved)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read file: %v", err))
	}

	content := string(data)
	newContent, result := applyEdit(content, oldStr, newStr, replaceAll)
	if result != nil {
		return result
	}

	if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create directory: %v", err))
	}

	if err := os.WriteFile(resolved, []byte(newContent), 0644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write file: %v", err))
	}

	count := strings.Count(content, oldStr)
	return SilentResult(fmt.Sprintf("File edited: %s (%d replacement(s))", path, count))
}

func (t *EditTool) executeInSandbox(ctx context.Context, path, oldStr, newStr string, replaceAll bool, sandboxKey string) *Result {
	sb, err := t.sandboxMgr.Get(ctx, sandboxKey, t.workspace, SandboxConfigFromCtx(ctx))
	if err != nil {
		return ErrorResult(fmt.Sprintf("sandbox error: %v", err))
	}

	containerCwd, cwdErr := SandboxCwd(ctx, t.workspace, sandbox.DefaultContainerWorkdir)
	if cwdErr != nil {
		return ErrorResult(fmt.Sprintf("sandbox path mapping: %v", cwdErr))
	}
	containerPath := ResolveSandboxPath(path, containerCwd)

	bridge := sandbox.NewFsBridge(sb.ID(), sandbox.DefaultContainerWorkdir)
	content, err := bridge.ReadFile(ctx, containerPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read file: %v", err) + MaybeFsBridgeHint(err))
	}

	newContent, result := applyEdit(content, oldStr, newStr, replaceAll)
	if result != nil {
		return result
	}

	if err := bridge.WriteFile(ctx, containerPath, newContent, false); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write file: %v", err) + MaybeFsBridgeHint(err))
	}

	count := strings.Count(content, oldStr)
	return SilentResult(fmt.Sprintf("File edited: %s (%d replacement(s))", path, count))
}

// applyEdit performs the search-and-replace. Returns (newContent, nil) on success
// or ("", errorResult) on failure.
func applyEdit(content, oldStr, newStr string, replaceAll bool) (string, *Result) {
	count := strings.Count(content, oldStr)
	if count == 0 {
		return "", ErrorResult("old_string not found in file")
	}
	if !replaceAll && count > 1 {
		return "", ErrorResult(fmt.Sprintf("old_string found %d times — use replace_all=true or provide a more specific match", count))
	}

	if replaceAll {
		return strings.ReplaceAll(content, oldStr, newStr), nil
	}
	return strings.Replace(content, oldStr, newStr, 1), nil
}
