package skills

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// SystemSkillStore is the minimal interface needed by the seeder.
type SystemSkillStore interface {
	UpsertSystemSkill(ctx context.Context, p pg.SkillCreateParams) (uuid.UUID, bool, error)
	GetNextVersion(slug string) int
	BumpVersion()
}

// Seeder seeds system/bundled skills into the database.
type Seeder struct {
	bundledDir string           // source: /app/bundled-skills/ or skills/ (dev)
	managedDir string           // destination: skills-store/ directory
	store      SystemSkillStore // DB operations
}

// NewSeeder creates a new system skill seeder.
func NewSeeder(bundledDir, managedDir string, store SystemSkillStore) *Seeder {
	return &Seeder{
		bundledDir: bundledDir,
		managedDir: managedDir,
		store:      store,
	}
}

// Seed scans bundledDir, upserts each skill into DB, copies files to managedDir.
// Returns (seeded, skipped, error).
func (s *Seeder) Seed(ctx context.Context) (int, int, error) {
	entries, err := os.ReadDir(s.bundledDir)
	if err != nil {
		return 0, 0, fmt.Errorf("read bundled dir: %w", err)
	}

	seeded, skipped := 0, 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		slug := e.Name()

		// Skip _shared/ directories (not skills, just shared code)
		if strings.HasPrefix(slug, "_") {
			s.copySharedDir(slug)
			continue
		}

		skillDir := filepath.Join(s.bundledDir, slug)
		skillFile := filepath.Join(skillDir, "SKILL.md")

		data, err := os.ReadFile(skillFile)
		if err != nil {
			slog.Debug("seeder: skip dir without SKILL.md", "slug", slug)
			continue
		}

		// Parse metadata
		content := string(data)
		meta := parseMetadata(skillFile)
		name := slug
		description := ""
		if meta != nil {
			if meta.Name != "" {
				name = meta.Name
			}
			description = meta.Description
		}

		// Compute hash of SKILL.md content
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))

		// Scan dependencies
		manifest := ScanSkillDeps(skillDir)

		// Check dependency availability
		status := "active"
		var depsWarning string
		if manifest != nil && !manifest.IsEmpty() {
			ok, missing := CheckSkillDeps(manifest)
			if !ok {
				status = "archived"
				depsWarning = FormatMissing(missing)
				slog.Warn("seeder: skill deps missing, setting archived",
					"slug", slug, "missing", depsWarning)
			}
		}

		// Build frontmatter map (includes deps info for UI)
		fm := extractFrontmatter(content)
		fmMap := make(map[string]string)
		if fm != "" {
			fmMap = parseSimpleYAML(fm)
		}
		if depsWarning != "" {
			fmMap["_deps_missing"] = depsWarning
		}

		version := s.store.GetNextVersion(slug)
		destDir := filepath.Join(s.managedDir, slug, fmt.Sprintf("%d", version))

		desc := description
		p := pg.SkillCreateParams{
			Name:        name,
			Slug:        slug,
			Description: &desc,
			OwnerID:     "system",
			Visibility:  "public",
			Status:      status,
			Version:     version,
			FilePath:    destDir,
			FileSize:    int64(len(data)),
			FileHash:    &hash,
			Frontmatter: fmMap,
		}

		id, changed, err := s.store.UpsertSystemSkill(ctx, p)
		if err != nil {
			slog.Error("seeder: failed to upsert skill", "slug", slug, "error", err)
			continue
		}

		if !changed {
			skipped++
			continue
		}

		// Copy skill directory to managed dir
		if err := copyDir(skillDir, destDir); err != nil {
			slog.Error("seeder: failed to copy skill files", "slug", slug, "error", err)
			continue
		}

		slog.Info("seeder: skill seeded", "id", id, "slug", slug, "version", version, "status", status)
		seeded++
	}

	if seeded > 0 {
		s.store.BumpVersion()
	}
	return seeded, skipped, nil
}

// copySharedDir copies a _shared/ directory to managedDir.
func (s *Seeder) copySharedDir(name string) {
	src := filepath.Join(s.bundledDir, name)
	dst := filepath.Join(s.managedDir, name)

	// Only copy if source exists and dest doesn't (or source is newer)
	srcInfo, err := os.Stat(src)
	if err != nil {
		return
	}
	dstInfo, _ := os.Stat(dst)
	if dstInfo != nil && dstInfo.ModTime().After(srcInfo.ModTime()) {
		return
	}

	if err := copyDir(src, dst); err != nil {
		slog.Warn("seeder: failed to copy shared dir", "name", name, "error", err)
	}
}

// copyDir recursively copies a directory tree.
// Resolves the top-level path so symlinks (e.g. scripts/office -> ../../_shared/office)
// are followed and their contents are copied as real files.
func copyDir(src, dst string) error {
	resolved, err := filepath.EvalSymlinks(src)
	if err != nil {
		resolved = src
	}

	return filepath.Walk(resolved, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(resolved, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}
