// Disk → Postgres memory seeder.
//
// The agent's memory file tree (e.g. /data/workspace-eng/memory/, which
// includes git-cloned vaults like cartridge-gg/memory via symlink) is
// the source of truth. The DB is a queryable index. This sweeper keeps
// the index in sync with disk by walking the tree, hashing each .md
// file, and re-indexing only the docs whose content changed since the
// last sweep.
//
// Project-agnostic: the sweeper takes the workspace root + agent +
// (optional) user scope. It makes no assumptions about vault layout —
// any directory of .md files works. Frontmatter + wikilinks parsing
// happens inside the store's IndexDocument hook (memory_docs.go).
package memory

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// DiskSeeder walks an agent's memory directory and upserts every .md
// file into the MemoryStore. Idempotent: unchanged files (matching
// hash already in DB) are skipped, so repeated sweeps are cheap.
//
// The store determines whether re-indexing also re-embeds chunks; the
// existing PG embedding cache means even forced re-indexing of an
// unchanged doc is cheap on the embed side. We still skip unchanged
// docs at the seeder layer to avoid the chunking + frontmatter +
// wikilinks work entirely.
type DiskSeeder struct {
	Store     store.MemoryStore
	Workspace string // absolute workspace root; we walk <workspace>/memory/**
	AgentID   string // UUID string of the agent owning these docs
	UserID    string // empty = shared/global; else per-user docs
	Log       *slog.Logger

	// MaxFileBytes guards against accidentally indexing a giant file.
	// 0 = use 5 MiB default. Files exceeding the cap are skipped with
	// a Warn log so the caller knows what was missed.
	MaxFileBytes int64
}

// Sweep walks the memory tree and re-indexes any changed docs.
// Returns the count of docs (re)indexed and skipped, and any walk
// error encountered (per-file errors are logged at Warn and don't
// abort the sweep).
type SweepResult struct {
	Indexed int
	Skipped int
	Failed  int
}

func (s *DiskSeeder) Sweep(ctx context.Context) (SweepResult, error) {
	if s.Store == nil || s.Workspace == "" || s.AgentID == "" {
		return SweepResult{}, fmt.Errorf("disk seeder: missing required field (store/workspace/agent)")
	}
	maxBytes := s.MaxFileBytes
	if maxBytes <= 0 {
		maxBytes = 5 << 20
	}
	memDir := filepath.Join(s.Workspace, "memory")
	info, err := os.Stat(memDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// No memory dir yet — nothing to seed. Not an error;
			// agents without a vault simply have an empty index.
			return SweepResult{}, nil
		}
		return SweepResult{}, fmt.Errorf("stat %s: %w", memDir, err)
	}
	if !info.IsDir() {
		return SweepResult{}, fmt.Errorf("%s is not a directory", memDir)
	}

	// Build a map of {path → hash} for docs already in the DB so we
	// can short-circuit unchanged files without reading them.
	existing, err := s.Store.ListDocuments(ctx, s.AgentID, s.UserID)
	if err != nil {
		return SweepResult{}, fmt.Errorf("list existing docs: %w", err)
	}
	dbHash := make(map[string]string, len(existing))
	for _, d := range existing {
		dbHash[d.Path] = d.Hash
	}

	var result SweepResult
	// Symlink handling: filepath.WalkDir does NOT follow symlinks. The
	// memory tree typically contains symlinks pointing at git-cloned
	// vault mirrors (e.g. <workspace>/memory/memory → ../mirrors/memory
	// for cartridge-gg/memory). Without explicit symlink resolution
	// the walk reaches each symlink, sees a non-directory entry, skips
	// it, and the entire vault content stays invisible — "started" log
	// fires but no .md ever gets indexed.
	//
	// Strategy: hand-recurse with os.ReadDir + os.Stat (which follows
	// symlinks). The "logical" path passed down through recursion uses
	// the symlink prefix (so memory/wiki/page1.md is what lands in the
	// DB, not the dereferenced mirrors/cartridge-gg-memory/wiki/page1.md
	// — agents reason about the vault layout, not the mirror's storage
	// location). visited[] guards against symlink cycles.
	visited := map[string]bool{}
	var walk func(logicalPath string) error
	walk = func(logicalPath string) error {
		// Stat (follows symlinks). If logicalPath is a symlink-to-dir
		// this returns the target's info, IsDir == true → we recurse.
		fi, err := os.Stat(logicalPath)
		if err != nil {
			s.logWarn("walk stat failed", "path", logicalPath, "err", err)
			return nil
		}
		// Cycle guard: resolve to canonical and skip if already visited.
		canon, lerr := filepath.EvalSymlinks(logicalPath)
		if lerr == nil {
			if visited[canon] {
				return nil
			}
			visited[canon] = true
		}
		if fi.IsDir() {
			entries, derr := os.ReadDir(logicalPath)
			if derr != nil {
				s.logWarn("readdir failed", "path", logicalPath, "err", derr)
				return nil
			}
			for _, e := range entries {
				if werr := walk(filepath.Join(logicalPath, e.Name())); werr != nil {
					return werr
				}
			}
			return nil
		}
		// Non-directory: only .md files matter.
		if !strings.HasSuffix(logicalPath, ".md") {
			return nil
		}
		if fi.Size() > maxBytes {
			s.logWarn("skip oversized file", "path", logicalPath, "size", fi.Size(), "max", maxBytes)
			result.Skipped++
			return nil
		}

		body, readErr := os.ReadFile(logicalPath)
		if readErr != nil {
			s.logWarn("read failed", "path", logicalPath, "err", readErr)
			result.Failed++
			return nil
		}

		relPath, _ := filepath.Rel(s.Workspace, logicalPath)
		relPath = filepath.ToSlash(relPath)

		newHash := ContentHash(string(body))
		if dbHash[relPath] == newHash {
			result.Skipped++
			return nil
		}

		if err := s.Store.PutDocument(ctx, s.AgentID, s.UserID, relPath, string(body)); err != nil {
			s.logWarn("put failed", "path", relPath, "err", err)
			result.Failed++
			return nil
		}
		if err := s.Store.IndexDocument(ctx, s.AgentID, s.UserID, relPath); err != nil {
			s.logWarn("index failed", "path", relPath, "err", err)
			result.Failed++
			return nil
		}
		result.Indexed++
		return nil
	}

	if walkErr := walk(memDir); walkErr != nil {
		return result, fmt.Errorf("walk %s: %w", memDir, walkErr)
	}
	return result, nil
}

func (s *DiskSeeder) logWarn(msg string, args ...any) {
	if s.Log == nil {
		return
	}
	s.Log.Warn(msg, args...)
}
