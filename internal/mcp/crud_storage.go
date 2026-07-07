package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerStorageCRUDTools registers the goclaw_storage_* MCP tools, closing
// the `goclaw storage` CLI-vs-MCP coverage gap. Path validation (traversal,
// symlink escape, tenant-isolation hiding, protected top-level dirs) mirrors
// internal/http/storage.go's handleList/handleSize/handleDelete/handleMove
// and their isHiddenPath/validateExistingStoragePath/validateStorageParent
// helpers — duplicated because this MCP surface does not depend on
// internal/http (which already imports internal/mcp; the reverse import
// would cycle). delete/move refuse to touch protectedDirs (skills,
// skills-store, media, tenants) same as the HTTP handler.
func registerStorageCRUDTools(srv *mcpserver.MCPServer, cfg *config.Config) {
	srv.AddTool(mcpgo.NewTool("goclaw_storage_list",
		mcpgo.WithDescription("List files and directories under the tenant's data directory."),
		mcpgo.WithString("path", mcpgo.Description("Subpath to scope the listing to; empty lists the data dir root.")),
		mcpgo.WithNumber("depth", mcpgo.Description("Max depth to walk (1-20, default 3).")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleStorageList(cfg))

	srv.AddTool(mcpgo.NewTool("goclaw_storage_size",
		mcpgo.WithDescription("Compute total size and file count under the tenant's data directory (or a subpath)."),
		mcpgo.WithString("path", mcpgo.Description("Subpath to scope the calculation to; empty sizes the whole data dir.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleStorageSize(cfg))

	srv.AddTool(mcpgo.NewTool("goclaw_storage_delete",
		mcpgo.WithDescription("Delete a file or directory under the tenant's data directory. Refuses protected top-level dirs (skills, skills-store, media, tenants)."),
		mcpgo.WithString("path", mcpgo.Required(), mcpgo.Description("Path to delete, relative to the data dir root.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleStorageDelete(cfg))

	srv.AddTool(mcpgo.NewTool("goclaw_storage_move",
		mcpgo.WithDescription("Move/rename a file or directory within the tenant's data directory. Refuses protected top-level dirs and existing destinations."),
		mcpgo.WithString("from", mcpgo.Required(), mcpgo.Description("Source path, relative to the data dir root.")),
		mcpgo.WithString("to", mcpgo.Required(), mcpgo.Description("Destination path, relative to the data dir root.")),
	), handleStorageMove(cfg))
}

// storageProtectedDirs mirrors internal/http/storage.go's protectedDirs.
var storageProtectedDirs = []string{"skills", "skills-store", "media", "tenants"}

func storageTopLevelPath(rel string) string {
	if before, _, ok := strings.Cut(rel, "/"); ok {
		return before
	}
	return rel
}

func storageIsProtectedPath(rel string) bool {
	top := storageTopLevelPath(rel)
	for _, d := range storageProtectedDirs {
		if strings.EqualFold(top, d) {
			return true
		}
	}
	return false
}

// storageIsHiddenPath mirrors isHiddenPath: master tenant must not see the
// cross-tenant isolation root ("tenants/") in its own listing.
func storageIsHiddenPath(ctx context.Context, rel string) bool {
	if rel == "" {
		return false
	}
	if store.TenantIDFromContext(ctx) != store.MasterTenantID {
		return false
	}
	return strings.EqualFold(storageTopLevelPath(rel), "tenants")
}

func storageTenantBaseDir(ctx context.Context, cfg *config.Config) string {
	tid := store.TenantIDFromContext(ctx)
	slug := store.TenantSlugFromContext(ctx)
	return config.TenantDataDir(cfg.DataDir, tid, slug)
}

// storageEvalSymlinkOrClean mirrors evalSymlinkOrClean.
func storageEvalSymlinkOrClean(path string) string {
	if realPath, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(realPath)
	}
	return filepath.Clean(path)
}

// storagePathWithinDir mirrors pathWithinDir.
func storagePathWithinDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// storageIsHiddenRealPath mirrors isHiddenRealPath.
func storageIsHiddenRealPath(ctx context.Context, base, realPath string) bool {
	if store.TenantIDFromContext(ctx) != store.MasterTenantID {
		return false
	}
	realTenantRoot, err := filepath.EvalSymlinks(filepath.Join(base, "tenants"))
	if err != nil {
		return false
	}
	return storagePathWithinDir(filepath.Clean(realPath), filepath.Clean(realTenantRoot))
}

// storageValidateExistingPath mirrors validateExistingStoragePath: resolves
// symlinks and confirms the real path stays within base and isn't the
// hidden cross-tenant isolation root.
func storageValidateExistingPath(ctx context.Context, base, absPath string) bool {
	realBase := storageEvalSymlinkOrClean(base)
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return false
	}
	realPath = filepath.Clean(realPath)
	if !storagePathWithinDir(realPath, realBase) {
		return false
	}
	return !storageIsHiddenRealPath(ctx, base, realPath)
}

// storageValidateParent mirrors validateStorageParent: walks up from parent
// until it finds a real (symlink-resolved) ancestor, confirming every
// resolvable ancestor stays within base and isn't the hidden isolation root.
func storageValidateParent(ctx context.Context, base, parent string) bool {
	realBase := storageEvalSymlinkOrClean(base)
	current := filepath.Clean(parent)
	for {
		if realParent, err := filepath.EvalSymlinks(current); err == nil {
			realParent = filepath.Clean(realParent)
			if !storagePathWithinDir(realParent, realBase) {
				return false
			}
			return !storageIsHiddenRealPath(ctx, base, realParent)
		}
		next := filepath.Dir(current)
		if next == current {
			return false
		}
		current = next
	}
}

func handleStorageList(cfg *config.Config) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		subPath := req.GetString("path", "")
		if strings.Contains(subPath, "..") {
			return mcpgo.NewToolResultError("storage.list: invalid path"), nil
		}

		base := storageTenantBaseDir(ctx, cfg)
		rootDir := base
		if subPath != "" {
			if storageIsHiddenPath(ctx, subPath) {
				return mcpgo.NewToolResultError("storage.list: path not found: " + subPath), nil
			}
			rootDir = filepath.Join(base, filepath.Clean(subPath))
			if !strings.HasPrefix(rootDir, base) {
				return mcpgo.NewToolResultError("storage.list: invalid path"), nil
			}
		}

		maxDepth := intArg(req, "depth", 3)
		if maxDepth < 1 || maxDepth > 20 {
			maxDepth = 3
		}

		type fileEntry struct {
			Path        string `json:"path"`
			Name        string `json:"name"`
			IsDir       bool   `json:"isDir"`
			Size        int64  `json:"size"`
			HasChildren bool   `json:"hasChildren,omitempty"`
			Protected   bool   `json:"protected"`
		}
		var entries []fileEntry

		filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if path == rootDir {
				return nil
			}
			rel, _ := filepath.Rel(base, path)
			if storageIsHiddenPath(ctx, rel) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if skills.IsSystemArtifact(rel) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			relToRoot, _ := filepath.Rel(rootDir, path)
			depth := strings.Count(relToRoot, string(filepath.Separator)) + 1

			if d.IsDir() && depth > maxDepth {
				e := fileEntry{Path: rel, Name: d.Name(), IsDir: true, Protected: storageIsProtectedPath(rel)}
				if dirEntries, err := os.ReadDir(path); err == nil && len(dirEntries) > 0 {
					e.HasChildren = true
				}
				entries = append(entries, e)
				return filepath.SkipDir
			}

			entry := fileEntry{Path: rel, Name: d.Name(), IsDir: d.IsDir()}
			if !d.IsDir() {
				if info, err := d.Info(); err == nil {
					entry.Size = info.Size()
				}
			}
			if d.IsDir() && depth == maxDepth {
				if dirEntries, err := os.ReadDir(path); err == nil && len(dirEntries) > 0 {
					entry.HasChildren = true
				}
			}
			entry.Protected = storageIsProtectedPath(rel)
			entries = append(entries, entry)
			return nil
		})

		if entries == nil {
			entries = []fileEntry{}
		}
		return jsonToolResult(map[string]any{"files": entries, "baseDir": base})
	}
}

func handleStorageSize(cfg *config.Config) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		subPath := req.GetString("path", "")
		if strings.Contains(subPath, "..") {
			return mcpgo.NewToolResultError("storage.size: invalid path"), nil
		}

		base := storageTenantBaseDir(ctx, cfg)
		sizeBase := base
		if subPath != "" {
			if storageIsHiddenPath(ctx, subPath) {
				return mcpgo.NewToolResultError("storage.size: path not found: " + subPath), nil
			}
			sizeBase = filepath.Join(base, filepath.Clean(subPath))
			if !strings.HasPrefix(sizeBase, base) {
				return mcpgo.NewToolResultError("storage.size: invalid path"), nil
			}
		}

		var total int64
		var fileCount int
		filepath.WalkDir(sizeBase, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(base, path)
			if storageIsHiddenPath(ctx, rel) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if skills.IsSystemArtifact(rel) {
				return nil
			}
			if info, err := d.Info(); err == nil {
				total += info.Size()
				fileCount++
			}
			return nil
		})

		return jsonToolResult(map[string]any{"total": total, "files": fileCount})
	}
}

func handleStorageDelete(cfg *config.Config) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		relPath, err := req.RequireString("path")
		if err != nil {
			return toolError("storage.delete", err)
		}
		if strings.Contains(relPath, "..") {
			return mcpgo.NewToolResultError("storage.delete: invalid path"), nil
		}
		if storageIsProtectedPath(relPath) {
			return mcpgo.NewToolResultError("storage.delete: cannot delete a protected directory"), nil
		}

		base := storageTenantBaseDir(ctx, cfg)
		absPath := filepath.Join(base, filepath.Clean(relPath))
		if !strings.HasPrefix(absPath, base+string(filepath.Separator)) {
			return mcpgo.NewToolResultError("storage.delete: invalid path"), nil
		}

		info, err := os.Lstat(absPath)
		if err != nil {
			return mcpgo.NewToolResultError("storage.delete: not found: " + relPath), nil
		}
		if !storageValidateExistingPath(ctx, base, absPath) {
			return mcpgo.NewToolResultError("storage.delete: not found: " + relPath), nil
		}

		switch {
		case info.Mode()&os.ModeSymlink != 0:
			err = os.Remove(absPath)
		case info.IsDir():
			err = os.RemoveAll(absPath)
		default:
			err = os.Remove(absPath)
		}
		if err != nil {
			return toolError("storage.delete", err)
		}
		return jsonToolResult(map[string]string{"status": "deleted", "path": relPath})
	}
}

func handleStorageMove(cfg *config.Config) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		fromRel, err := req.RequireString("from")
		if err != nil {
			return toolError("storage.move", err)
		}
		toRel, err := req.RequireString("to")
		if err != nil {
			return toolError("storage.move", err)
		}
		if strings.Contains(fromRel, "..") || strings.Contains(toRel, "..") {
			return mcpgo.NewToolResultError("storage.move: invalid path"), nil
		}
		if storageIsProtectedPath(fromRel) || storageIsProtectedPath(toRel) {
			return mcpgo.NewToolResultError("storage.move: cannot move a protected directory"), nil
		}

		base := storageTenantBaseDir(ctx, cfg)

		srcAbs := filepath.Join(base, filepath.Clean(fromRel))
		if !strings.HasPrefix(srcAbs, base+string(filepath.Separator)) {
			return mcpgo.NewToolResultError("storage.move: invalid path"), nil
		}
		srcReal, err := filepath.EvalSymlinks(srcAbs)
		if err != nil {
			return mcpgo.NewToolResultError("storage.move: source not found"), nil
		}
		baseReal := storageEvalSymlinkOrClean(base)
		srcReal = filepath.Clean(srcReal)
		if !storagePathWithinDir(srcReal, baseReal) || storageIsHiddenRealPath(ctx, base, srcReal) {
			return mcpgo.NewToolResultError("storage.move: invalid path"), nil
		}

		destAbs := filepath.Join(base, filepath.Clean(toRel))
		if !strings.HasPrefix(destAbs, base+string(filepath.Separator)) {
			return mcpgo.NewToolResultError("storage.move: invalid path"), nil
		}
		destDir := filepath.Dir(destAbs)
		if !storageValidateParent(ctx, base, destDir) {
			return mcpgo.NewToolResultError("storage.move: invalid destination path"), nil
		}
		if err := os.MkdirAll(destDir, 0750); err != nil {
			return toolError("storage.move", err)
		}
		if !storageValidateParent(ctx, base, destDir) {
			return mcpgo.NewToolResultError("storage.move: invalid destination path"), nil
		}
		if _, err := os.Stat(destAbs); err == nil {
			return mcpgo.NewToolResultError("storage.move: a file already exists at the destination"), nil
		}
		if err := os.Rename(srcAbs, destAbs); err != nil {
			return toolError("storage.move", err)
		}
		return jsonToolResult(map[string]any{"from": fromRel, "to": toRel})
	}
}
