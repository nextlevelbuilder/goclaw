package http

import (
	"archive/tar"
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const maxWorkspaceFileSize = 50 << 20 // 50MB per file

// exportWorkspaceFiles walks wsPath and adds each file to tw under the "workspace/" prefix.
// Skips directories, hidden files (dot-prefixed), symlinks, and files exceeding maxWorkspaceFileSize.
func (h *AgentsHandler) exportWorkspaceFiles(ctx context.Context, tw *tar.Writer, wsPath string, progressFn func(ProgressEvent)) (int, int64, error) {
	return h.exportWorkspaceFilesWithPrefix(ctx, tw, wsPath, "workspace/", progressFn)
}

// exportWorkspaceFilesWithPrefix walks wsPath and adds each file to tw under tarPrefix.
// Skips directories, hidden files (dot-prefixed), symlinks, and files exceeding maxWorkspaceFileSize.
func (h *AgentsHandler) exportWorkspaceFilesWithPrefix(ctx context.Context, tw *tar.Writer, wsPath, tarPrefix string, progressFn func(ProgressEvent)) (int, int64, error) {
	info, err := os.Stat(wsPath)
	if err != nil || !info.IsDir() {
		return 0, 0, nil // no workspace dir = nothing to export
	}

	var count int
	var totalBytes int64

	walkErr := filepath.WalkDir(wsPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if fi.Size() > maxWorkspaceFileSize {
			slog.Debug("export: skipping large workspace file", "path", path, "size", fi.Size())
			return nil
		}

		rel, err := filepath.Rel(wsPath, path)
		if err != nil {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("export: failed to read workspace file", "path", path, "error", err)
			return nil
		}

		tarPath := tarPrefix + sanitizeRelPath(filepath.ToSlash(rel))
		if err := addToTar(tw, tarPath, data); err != nil {
			return err
		}

		count++
		totalBytes += fi.Size()
		if progressFn != nil && count%10 == 0 {
			progressFn(ProgressEvent{Phase: "workspace", Status: "running", Current: count})
		}
		return nil
	})

	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "workspace", Status: "done", Current: count, Total: count})
	}
	return count, totalBytes, walkErr
}

// extractWorkspaceFiles writes files from the archive into baseDir.
// Security: rejects path traversal and absolute paths.
// When overwrite is false, existing files are skipped.
func extractWorkspaceFiles(baseDir string, files map[string][]byte, overwrite bool) (int, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return 0, fmt.Errorf("create workspace dir: %w", err)
	}

	// Ensure baseDir ends with separator for prefix check
	baseDirClean := filepath.Clean(baseDir) + string(filepath.Separator)

	var count int
	for rel, data := range files {
		clean := filepath.Clean(rel)
		if strings.Contains(clean, "..") {
			continue
		}
		if filepath.IsAbs(clean) {
			continue
		}

		target := filepath.Join(baseDir, clean)
		// Verify target stays inside baseDir after path resolution
		if !strings.HasPrefix(filepath.Clean(target)+string(filepath.Separator), baseDirClean) {
			continue
		}

		if !overwrite {
			if _, err := os.Stat(target); err == nil {
				continue // skip existing
			}
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			continue
		}

		if err := os.WriteFile(target, data, 0644); err != nil {
			return count, fmt.Errorf("write %s: %w", rel, err)
		}
		count++
	}
	return count, nil
}
