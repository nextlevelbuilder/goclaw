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
	walkErr := filepath.WalkDir(memDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Permission / symlink errors on individual entries should
			// log + skip, not abort the entire sweep.
			s.logWarn("walk error", "path", path, "err", err)
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		// Symlinks: WalkDir doesn't follow by default, but our memory
		// dir often IS a symlink (e.g. memory/memory → ../mirrors/memory).
		// The walk descends into the directory the symlink points to
		// when the path passed to WalkDir is itself a symlink, which is
		// what we want (we Stat'd memDir above and it was a directory).

		fi, statErr := d.Info()
		if statErr != nil {
			s.logWarn("stat failed", "path", path, "err", statErr)
			result.Failed++
			return nil
		}
		if fi.Size() > maxBytes {
			s.logWarn("skip oversized file", "path", path, "size", fi.Size(), "max", maxBytes)
			result.Skipped++
			return nil
		}

		body, readErr := os.ReadFile(path)
		if readErr != nil {
			s.logWarn("read failed", "path", path, "err", readErr)
			result.Failed++
			return nil
		}

		relPath, _ := filepath.Rel(s.Workspace, path)
		// Normalize separators on the off-chance Windows ever runs
		// this; PG store canonicalizes on "/".
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
		// IndexDocument runs frontmatter + wikilinks + chunking +
		// embedding. Failures here mean we have the doc in
		// memory_documents but not searchable yet — log and continue,
		// the next sweep will retry.
		if err := s.Store.IndexDocument(ctx, s.AgentID, s.UserID, relPath); err != nil {
			s.logWarn("index failed", "path", relPath, "err", err)
			result.Failed++
			return nil
		}
		result.Indexed++
		return nil
	})

	if walkErr != nil {
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
