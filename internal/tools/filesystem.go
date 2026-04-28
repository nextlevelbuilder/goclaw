package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// virtualSystemFiles are files dynamically injected into the system prompt.
// They don't exist on disk — if the model tries to read them, return a hint.
var virtualSystemFiles = map[string]string{
	bootstrap.TeamFile:         "TEAM.md is already loaded in your system prompt. Refer to the TEAM.md section in your context above for team member information.",
	bootstrap.AvailabilityFile: "AVAILABILITY.md is already loaded in your system prompt. Refer to the AVAILABILITY.md section in your context above for agent availability information.",
}

// ReadFileTool reads file contents, optionally through a sandbox container.
type ReadFileTool struct {
	workspace        string
	restrict         bool
	allowedPrefixes  []string                // extra allowed path prefixes (e.g. skills dirs)
	deniedPrefixes   []string                // path prefixes to deny access to (e.g. .goclaw)
	sandboxMgr       sandbox.Manager         // nil = direct host access
	contextFileIntc  *ContextFileInterceptor // nil = no virtual FS routing
	memIntc          *MemoryInterceptor      // nil = no memory routing
	permStore        store.ConfigPermissionStore // nil = no group read restriction
	vaultIntc        *VaultInterceptor           // nil = no vault lazy sync
}

// SetContextFileInterceptor enables virtual FS routing for context files.
func (t *ReadFileTool) SetContextFileInterceptor(intc *ContextFileInterceptor) {
	t.contextFileIntc = intc
}

// SetMemoryInterceptor enables virtual FS routing for memory files.
func (t *ReadFileTool) SetMemoryInterceptor(intc *MemoryInterceptor) {
	t.memIntc = intc
}

// SetConfigPermStore enables group read restriction for SOUL.md/AGENTS.md.
func (t *ReadFileTool) SetConfigPermStore(s store.ConfigPermissionStore) {
	t.permStore = s
}

// SetVaultInterceptor enables lazy vault hash sync on file reads.
func (t *ReadFileTool) SetVaultInterceptor(v *VaultInterceptor) {
	t.vaultIntc = v
}

func NewReadFileTool(workspace string, restrict bool) *ReadFileTool {
	return &ReadFileTool{workspace: workspace, restrict: restrict}
}

// AllowPaths adds extra path prefixes that read_file is allowed to access
// even when restrict_to_workspace is true (e.g. skills directories).
func (t *ReadFileTool) AllowPaths(prefixes ...string) {
	t.allowedPrefixes = append(t.allowedPrefixes, prefixes...)
}

// DenyPaths adds path prefixes that read_file must reject (e.g. hidden dirs).
func (t *ReadFileTool) DenyPaths(prefixes ...string) {
	t.deniedPrefixes = append(t.deniedPrefixes, prefixes...)
}

func NewSandboxedReadFileTool(workspace string, restrict bool, mgr sandbox.Manager) *ReadFileTool {
	return &ReadFileTool{workspace: workspace, restrict: restrict, sandboxMgr: mgr}
}

// SetSandboxKey is a no-op; sandbox key is now read from ctx (thread-safe).
func (t *ReadFileTool) SetSandboxKey(key string) {}

func (t *ReadFileTool) Name() string { return "read_file" }
func (t *ReadFileTool) Description() string {
	return "Read the contents of a file. For large files, use offset and limit to read specific line ranges."
}
func (t *ReadFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "File path (relative to workspace, or absolute)",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "Start reading from this line number (0-indexed). Defaults to 0.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of lines to return. Omit to read until output cap.",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, args map[string]any) *Result {
	path, _ := args["path"].(string)
	if path == "" {
		return ErrorResult("path is required")
	}

	// Group read restriction: block non-writers from reading SOUL.md/AGENTS.md
	if t.permStore != nil {
		base := filepath.Base(path)
		if base == bootstrap.SoulFile || base == bootstrap.AgentsFile {
			if err := store.CheckFileWriterPermission(ctx, t.permStore); err != nil {
				return ErrorResult(fmt.Sprintf("permission denied: %s is restricted in this group", base))
			}
		}
	}

	// Virtual FS: route context files to DB
	if t.contextFileIntc != nil {
		if content, handled, err := t.contextFileIntc.ReadFile(ctx, path); handled {
			if err != nil {
				return ErrorResult(fmt.Sprintf("failed to read context file: %v", err))
			}
			if content == "" {
				return ErrorResult(fmt.Sprintf("context file not found: %s", path))
			}
			return SilentResult(content)
		}
	}

	// Virtual system files: TEAM.md, DELEGATION.md, AVAILABILITY.md are injected
	// into the system prompt and don't exist on disk. Return a helpful hint.
	baseName := filepath.Base(path)
	if hint, ok := virtualSystemFiles[baseName]; ok {
		return SilentResult(hint)
	}

	// Virtual FS: route memory files to DB
	if t.memIntc != nil {
		if content, handled, err := t.memIntc.ReadFile(ctx, path); handled {
			if err != nil {
				return ErrorResult(fmt.Sprintf("failed to read memory file: %v", err))
			}
			if content == "" {
				return SilentResult(fmt.Sprintf("(memory file %s does not exist yet — it will be created when memory is saved)", path))
			}
			return SilentResult(content + "\n\n[Source: database, not filesystem]")
		}
	}

	// Sandbox routing (sandboxKey from ctx — thread-safe)
	sandboxKey := ToolSandboxKeyFromCtx(ctx)
	if t.sandboxMgr != nil && sandboxKey != "" {
		return t.executeInSandbox(ctx, path, sandboxKey, args)
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

	// Block binary files — reading them wastes context with garbled data.
	if isBinaryFileExt(resolved) {
		ext := strings.ToLower(filepath.Ext(resolved))
		return ErrorResult(fmt.Sprintf("cannot read binary file (%s). Use the appropriate tool: read_image for images, read_document for documents, read_audio for audio, read_video for video.", ext))
	}

	// Vault lazy sync: update hash if file was modified outside the agent.
	if t.vaultIntc != nil {
		go t.vaultIntc.BeforeRead(context.WithoutCancel(ctx), resolved)
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		msg := fmt.Sprintf("failed to read file: %v", err)
		if os.IsNotExist(err) {
			if teamWs := ToolTeamWorkspaceFromCtx(ctx); teamWs != "" && !strings.HasPrefix(resolved, teamWs) {
				msg += fmt.Sprintf("\nHint: file may be in the team workspace. Try: read_file(path=\"%s/%s\")", teamWs, path)
			}
		}
		return ErrorResult(msg)
	}

	return t.paginateOutput(string(data), args)
}

func (t *ReadFileTool) executeInSandbox(ctx context.Context, path, sandboxKey string, args map[string]any) *Result {
	bridge, err := t.getFsBridge(ctx, sandboxKey)
	if err != nil {
		return ErrorResult(fmt.Sprintf("sandbox error: %v", err))
	}

	containerCwd, cwdErr := SandboxCwd(ctx, t.workspace, sandbox.DefaultContainerWorkdir)
	if cwdErr != nil {
		return ErrorResult(fmt.Sprintf("sandbox path mapping: %v", cwdErr))
	}
	containerPath := ResolveSandboxPath(path, containerCwd)

	data, err := bridge.ReadFile(ctx, containerPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read file: %v", err) + MaybeFsBridgeHint(err))
	}

	return t.paginateOutput(data, args)
}

func (t *ReadFileTool) getFsBridge(ctx context.Context, sandboxKey string) (*sandbox.FsBridge, error) {
	sb, err := t.sandboxMgr.Get(ctx, sandboxKey, t.workspace, SandboxConfigFromCtx(ctx))
	if err != nil {
		return nil, err
	}
	return sandbox.NewFsBridge(sb.ID(), sandbox.DefaultContainerWorkdir), nil
}

// readFileMaxChars is the output cap for read_file. Large files require offset/limit pagination.
const readFileMaxChars = 50000

// paginateOutput applies offset/limit slicing and output capping to file content.
// Returns a SilentResult with pagination metadata when the output is truncated.
func (t *ReadFileTool) paginateOutput(content string, args map[string]any) *Result {
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	// Parse offset (0-indexed line number).
	offset := 0
	if v, ok := args["offset"]; ok {
		switch n := v.(type) {
		case float64:
			offset = int(n)
		case int:
			offset = n
		}
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= totalLines {
		return SilentResult(fmt.Sprintf("(offset %d exceeds file length of %d lines)", offset, totalLines))
	}

	// Parse limit (max lines to return).
	limit := 0 // 0 = no explicit limit
	if v, ok := args["limit"]; ok {
		switch n := v.(type) {
		case float64:
			limit = int(n)
		case int:
			limit = n
		}
	}

	// Slice lines by offset and limit.
	sliced := lines[offset:]
	if limit > 0 && limit < len(sliced) {
		sliced = sliced[:limit]
	}

	output := strings.Join(sliced, "\n")
	shownLines := len(sliced)
	endLine := offset + shownLines

	// Check output char cap.
	runeCount := len([]rune(output))
	if runeCount <= readFileMaxChars {
		// Fits within cap — add line info if offset was used or file was partially read.
		if offset > 0 || endLine < totalLines {
			output += fmt.Sprintf("\n\n[Showing lines %d-%d of %d total]", offset, endLine-1, totalLines)
		}
		return SilentResult(output)
	}

	// Output exceeds cap — truncate at line boundary within budget.
	charCount := 0
	truncIdx := len(sliced)
	for i, line := range sliced {
		charCount += len([]rune(line)) + 1 // +1 for newline
		if charCount > readFileMaxChars {
			truncIdx = i
			break
		}
	}
	if truncIdx < 1 {
		truncIdx = 1
	}

	output = strings.Join(sliced[:truncIdx], "\n")
	shownLines = truncIdx
	nextOffset := offset + shownLines

	output += fmt.Sprintf("\n\n[Output capped. File has %d lines, showed %d (lines %d-%d). Use offset=%d to continue reading.]",
		totalLines, shownLines, offset, offset+shownLines-1, nextOffset)

	return SilentResult(output)
}

// allowedWithTeamWorkspace returns the READ-allowed prefixes with team workspace,
// team root, and tenant-specific paths appended if present in context.
// Thread-safe: creates a new slice per request.
// Merge order: base (global) → tenant paths → team workspace (leaf scope) → team root.
// Team root is the team-wide directory without UserChatLayer suffix; it lets
// any agent in the team read files generated by peers under different chat scopes.
//
// Use this variant for read operations (read_file, read_image, list_files, send_file).
// For write operations, use allowedWriteWithTeamWorkspace instead — team root is
// intentionally excluded from write to prevent cross-chat write leakage.
func allowedWithTeamWorkspace(ctx context.Context, base []string) []string {
	return buildAllowedPrefixes(ctx, base, true)
}

// allowedWriteWithTeamWorkspace returns the WRITE-allowed prefixes. Same as the
// read variant but WITHOUT team root — writes must stay within the agent's leaf
// scope (team workspace = own chat dir for isolated mode, team root itself for
// shared mode). This prevents an agent in chat A from writing into chat B's
// workspace through a cross-chat absolute path.
//
// Use this variant for write/mutation operations (write_file, edit, shell).
func allowedWriteWithTeamWorkspace(ctx context.Context, base []string) []string {
	return buildAllowedPrefixes(ctx, base, false)
}

// buildAllowedPrefixes merges base + tenant paths + team workspace, optionally
// including team root. Extracted to share the slice-building logic between read
// and write variants without duplication.
func buildAllowedPrefixes(ctx context.Context, base []string, includeTeamRoot bool) []string {
	tenantPaths := TenantAllowedPathsFromCtx(ctx)
	teamWs := ToolTeamWorkspaceFromCtx(ctx)
	var teamRoot string
	if includeTeamRoot {
		teamRoot = ToolTeamRootFromCtx(ctx)
	}

	if len(tenantPaths) == 0 && teamWs == "" && teamRoot == "" {
		return base
	}

	capacity := len(base) + len(tenantPaths)
	if teamWs != "" {
		capacity++
	}
	if teamRoot != "" && teamRoot != teamWs {
		capacity++
	}

	out := make([]string, 0, capacity)
	out = append(out, base...)
	out = append(out, tenantPaths...)
	if teamWs != "" {
		out = append(out, teamWs)
	}
	if teamRoot != "" && teamRoot != teamWs {
		out = append(out, teamRoot)
	}
	return out
}

// resolvePathWithAllowed is like resolvePath but also allows paths under extra prefixes.
func resolvePathWithAllowed(path, workspace string, restrict bool, allowedPrefixes []string) (string, error) {
	resolved, err := resolvePath(path, workspace, restrict)
	if err == nil {
		return resolved, nil
	}
	// If restricted and denied, check if path falls under an allowed prefix.
	// Resolve symlinks in the candidate path for safe comparison.
	cleaned := filepath.Clean(path)
	absPath, _ := filepath.Abs(cleaned)
	real, evalErr := filepath.EvalSymlinks(absPath)
	if evalErr != nil {
		// Try resolving parent for non-existent files
		parentReal, parentErr := filepath.EvalSymlinks(filepath.Dir(absPath))
		if parentErr != nil {
			return "", err
		}
		real = filepath.Join(parentReal, filepath.Base(absPath))
	}
	for _, prefix := range allowedPrefixes {
		absPrefix, _ := filepath.Abs(prefix)
		prefixReal, prefixErr := filepath.EvalSymlinks(absPrefix)
		if prefixErr != nil {
			prefixReal = absPrefix
		}
		if isPathInside(real, prefixReal) {
			slog.Debug("read_file: allowed by prefix", "path", real, "prefix", prefixReal)
			return real, nil
		}
	}
	slog.Warn("read_file: access denied", "path", cleaned, "workspace", workspace, "allowedPrefixes", allowedPrefixes)
	return "", err
}

// checkDeniedPath returns an error if the resolved path falls under any denied prefix.
// Denied prefixes are relative to the workspace (e.g. ".goclaw" denies workspace/.goclaw/).
// The resolved path should already be canonical (from resolvePath with restrict=true).
func checkDeniedPath(resolved, workspace string, deniedPrefixes []string) error {
	if len(deniedPrefixes) == 0 {
		return nil
	}
	absResolved, _ := filepath.Abs(resolved)
	absWorkspace, _ := filepath.Abs(workspace)
	// Resolve workspace to canonical form for consistent comparison.
	wsReal, err := filepath.EvalSymlinks(absWorkspace)
	if err != nil {
		wsReal = absWorkspace
	}
	for _, prefix := range deniedPrefixes {
		denied := filepath.Join(wsReal, prefix)
		if isPathInside(absResolved, denied) {
			return fmt.Errorf("access denied: path %s is restricted", prefix)
		}
	}
	return nil
}

// binaryFileExts are file extensions that should not be read as text.
// Reading these wastes context with garbled binary data.
var binaryFileExts = map[string]bool{
	// Images
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
	".bmp": true, ".ico": true, ".tiff": true, ".tif": true,
	// Audio
	".mp3": true, ".wav": true, ".ogg": true, ".flac": true, ".aac": true, ".m4a": true,
	// Video
	".mp4": true, ".avi": true, ".mov": true, ".mkv": true, ".webm": true,
	// Archives
	".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".7z": true, ".rar": true,
	// Documents (binary)
	".pdf": true, ".docx": true, ".xlsx": true, ".pptx": true,
	// Executables
	".exe": true, ".dll": true, ".so": true, ".dylib": true,
}

// isBinaryFileExt returns true if the file extension indicates a binary file.
func isBinaryFileExt(path string) bool {
	return binaryFileExts[strings.ToLower(filepath.Ext(path))]
}

// resolvePath resolves a path relative to the workspace and validates it.
// When restrict=true, resolves symlinks to canonical paths and rejects
// paths that escape the workspace boundary (symlink/hardlink attacks).
func resolvePath(path, workspace string, restrict bool) (string, error) {
	var resolved string
	if filepath.IsAbs(path) {
		resolved = filepath.Clean(path)
	} else {
		resolved = filepath.Clean(filepath.Join(workspace, path))
	}

	if !restrict {
		return resolved, nil
	}

	// Resolve workspace to canonical path (follow symlinks in workspace path itself).
	absWorkspace, _ := filepath.Abs(workspace)
	wsReal, err := filepath.EvalSymlinks(absWorkspace)
	if err != nil {
		wsReal = absWorkspace // workspace doesn't exist yet — use as-is
	}

	// Resolve the target path to canonical form (follows all symlinks).
	absResolved, _ := filepath.Abs(resolved)
	real, err := filepath.EvalSymlinks(absResolved)
	if err != nil {
		if os.IsNotExist(err) {
			// Check if the path itself is a symlink (broken/dangling).
			// Lstat doesn't follow symlinks, so it succeeds even for broken ones.
			if linfo, lerr := os.Lstat(absResolved); lerr == nil && linfo.Mode()&os.ModeSymlink != 0 {
				// It's a broken symlink — read target and validate.
				target, readErr := os.Readlink(absResolved)
				if readErr != nil {
					return "", fmt.Errorf("access denied: cannot resolve symlink")
				}
				if !filepath.IsAbs(target) {
					target = filepath.Join(filepath.Dir(absResolved), target)
				}
				target = filepath.Clean(target)

				// Resolve through existing ancestors to catch chained symlinks
				// (e.g. link1 → link2 → /outside) where intermediate targets escape.
				resolved, resolveErr := resolveThroughExistingAncestors(target)
				if resolveErr != nil {
					slog.Warn("security.broken_symlink_resolve_failed", "path", path, "target", target)
					return "", fmt.Errorf("access denied: cannot resolve broken symlink target")
				}
				if !isPathInside(resolved, wsReal) {
					slog.Warn("security.broken_symlink_escape", "path", path, "target", resolved, "workspace", wsReal)
					return "", fmt.Errorf("access denied: broken symlink target outside workspace")
				}
				real = resolved
			} else {
				// Truly non-existent file (not a symlink): walk up to find the
				// deepest existing ancestor so nested new dirs (e.g. posts/file.md)
				// are allowed as long as an ancestor is inside the workspace.
				ancestorReal, ancestorErr := resolveThroughExistingAncestors(absResolved)
				if ancestorErr != nil {
					return "", fmt.Errorf("access denied: cannot resolve path")
				}
				real = ancestorReal
			}
		} else {
			// Permission error or other — reject.
			slog.Warn("security.path_resolve_failed", "path", path, "error", err)
			return "", fmt.Errorf("access denied: cannot resolve path")
		}
	}

	// Validate canonical path stays within canonical workspace.
	if !isPathInside(real, wsReal) {
		slog.Warn("security.path_escape", "path", path, "resolved", real, "workspace", wsReal)
		return "", fmt.Errorf("access denied: path outside workspace")
	}

	// Reject paths with mutable symlink components (TOCTOU symlink rebind risk).
	// A symlink in the path whose parent directory is writable could be replaced
	// between resolution time and actual file operation.
	if hasMutableSymlinkParent(real) {
		slog.Warn("security.mutable_symlink_parent", "path", path, "resolved", real)
		return "", fmt.Errorf("access denied: path contains mutable symlink component")
	}

	// Reject hardlinked files (nlink > 1) to prevent hardlink-based escapes.
	if err := checkHardlink(real); err != nil {
		return "", err
	}

	return real, nil
}

// isPathInside checks whether child is inside or equal to parent directory.
// On Windows, comparison is case-insensitive since NTFS paths are case-insensitive.
func isPathInside(child, parent string) bool {
	// Windows paths are case-insensitive; normalize to lowercase for comparison.
	if runtime.GOOS == "windows" {
		child = strings.ToLower(child)
		parent = strings.ToLower(parent)
	}
	if child == parent {
		return true
	}
	return strings.HasPrefix(child, parent+string(filepath.Separator))
}

// resolveThroughExistingAncestors resolves a path by finding the deepest
// existing ancestor, canonicalizing it with EvalSymlinks, then appending
// the remaining non-existent components. This handles broken symlinks
// whose targets contain intermediate symlinks that escape the workspace.
func resolveThroughExistingAncestors(target string) (string, error) {
	// Try full resolution first (target exists and all symlinks resolve)
	if real, err := filepath.EvalSymlinks(target); err == nil {
		return real, nil
	}

	// Walk up to find the deepest existing ancestor
	current := target
	var tail []string
	for {
		parent := filepath.Dir(current)
		if parent == current {
			// Reached filesystem root without finding existing dir
			break
		}
		tail = append([]string{filepath.Base(current)}, tail...)
		current = parent

		if realParent, err := filepath.EvalSymlinks(current); err == nil {
			// Found existing ancestor — canonicalize and rebuild
			result := realParent
			for _, component := range tail {
				result = filepath.Join(result, component)
			}
			return result, nil
		}
	}
	return filepath.Clean(target), nil
}

