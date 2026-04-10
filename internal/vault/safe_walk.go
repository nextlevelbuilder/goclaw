package vault

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WalkEntry represents an eligible file found during workspace walk.
type WalkEntry struct {
	RelPath string    // workspace-relative, forward-slash separated
	AbsPath string    // absolute filesystem path
	Size    int64     // file size in bytes
	ModTime time.Time // last modification time
}

// WalkStats holds metrics from a workspace walk.
type WalkStats struct {
	TotalWalked     int
	Eligible        int
	SkippedSymlinks int
	SkippedExcluded int
	SkippedTooLarge int
	Truncated       bool
}

// WalkOptions configures resource limits for workspace walking.
type WalkOptions struct {
	MaxFiles      int   // max eligible files to return (0 = unlimited)
	MaxTotalBytes int64 // max cumulative file size in bytes (0 = unlimited)
	MaxFileBytes  int64 // skip individual files larger than this (0 = unlimited)
}

// DefaultWalkOptions returns safe defaults for production use.
func DefaultWalkOptions() WalkOptions {
	return WalkOptions{
		MaxFiles:      5000,
		MaxTotalBytes: 500 * 1024 * 1024, // 500MB
		MaxFileBytes:  50 * 1024 * 1024,  // 50MB per file
	}
}

// SafeWalkWorkspace walks root directory collecting eligible files.
// Symlinks are skipped unconditionally. Excluded paths are filtered.
// Resource limits (file count, total size, context deadline) are enforced.
func SafeWalkWorkspace(ctx context.Context, root string, opts WalkOptions) ([]WalkEntry, WalkStats, error) {
	root = filepath.Clean(root)
	var (
		entries    []WalkEntry
		stats      WalkStats
		totalBytes int64
		walkCount  int
	)

	// Early context check before starting walk.
	if ctx.Err() != nil {
		return nil, stats, ctx.Err()
	}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}

		// Check context periodically.
		walkCount++
		if walkCount%100 == 0 {
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}

		// Skip ALL symlinks unconditionally (both files and dirs).
		if d.Type()&os.ModeSymlink != 0 {
			stats.SkippedSymlinks++
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Compute workspace-relative path (forward-slash).
		relPath, relErr := filepath.Rel(root, path)
		if relErr != nil || relPath == "." {
			return nil
		}
		relPath = filepath.ToSlash(relPath)

		// Boundary check: path must stay inside root.
		if strings.HasPrefix(relPath, "..") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Directory-level exclusions: skip entire subtree.
		if d.IsDir() {
			if isExcludedDir(relPath) {
				stats.SkippedExcluded++
				return filepath.SkipDir
			}
			return nil
		}

		stats.TotalWalked++

		// File-level exclusions.
		if isExcludedPath(relPath) {
			stats.SkippedExcluded++
			return nil
		}

		// File info for size check.
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil // skip unreadable
		}

		// Per-file size limit.
		if opts.MaxFileBytes > 0 && info.Size() > opts.MaxFileBytes {
			stats.SkippedTooLarge++
			return nil
		}

		// Total size limit.
		if opts.MaxTotalBytes > 0 && totalBytes+info.Size() > opts.MaxTotalBytes {
			stats.Truncated = true
			return filepath.SkipAll
		}

		// File count limit.
		if opts.MaxFiles > 0 && len(entries) >= opts.MaxFiles {
			stats.Truncated = true
			return filepath.SkipAll
		}

		totalBytes += info.Size()
		stats.Eligible++
		entries = append(entries, WalkEntry{
			RelPath: relPath,
			AbsPath: path,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
		return nil
	})

	// Context cancellation is expected, not an error for partial results.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return entries, stats, err
	}
	return entries, stats, err
}

// isExcludedDir returns true if an entire directory subtree should be skipped.
// relPath is the walk-relative path of the directory (e.g. "agents/my-bot/web-fetch").
func isExcludedDir(relPath string) bool {
	// Get the directory's own name (last segment).
	dirName := filepath.Base(relPath)

	// Skip memory/ at any depth.
	if dirName == "memory" {
		return true
	}

	// Skip hidden dirs (. prefix) EXCEPT .uploads (user content).
	if strings.HasPrefix(dirName, ".") && dirName != ".uploads" {
		return true
	}

	// Skip web-fetch/ at any depth — content from external URLs may be dangerous.
	if dirName == "web-fetch" {
		return true
	}

	return false
}

// contextFiles are root-level bootstrap files managed by ContextFileInterceptor.
var contextFiles = map[string]bool{
	"SOUL.md":         true,
	"IDENTITY.md":     true,
	"USER.md":         true,
	"BOOTSTRAP.md":    true,
	"AGENTS.md":       true,
	"TOOLS.md":        true,
	"CAPABILITIES.md": true,
	"MEMORY.md":       true,
	"AGENTS_CORE.md":  true,
	"AGENTS_TASK.md":  true,
}

// isExcludedPath returns true if a file should be excluded from vault registration.
// Defense-in-depth: checks ALL parent directory segments for exclusions.
func isExcludedPath(relPath string) bool {
	// Check every directory segment in the path.
	dir := filepath.Dir(relPath)
	for dir != "." && dir != "/" {
		seg := filepath.Base(dir)
		if seg == "memory" || seg == "web-fetch" {
			return true
		}
		if strings.HasPrefix(seg, ".") && seg != ".uploads" {
			return true
		}
		dir = filepath.Dir(dir)
	}

	base := filepath.Base(relPath)

	// SQLite database files.
	if strings.HasSuffix(base, ".db") || strings.HasSuffix(base, ".db-wal") || strings.HasSuffix(base, ".db-shm") {
		return true
	}

	// Root-level context files only (not nested).
	if !strings.Contains(relPath, "/") && contextFiles[base] {
		return true
	}

	// Hidden files at root level.
	if strings.HasPrefix(base, ".") && !strings.Contains(relPath, "/") {
		return true
	}

	return false
}
