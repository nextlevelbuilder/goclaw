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
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// SystemSkillStore is the minimal interface needed by the seeder.
type SystemSkillStore interface {
	UpsertSystemSkill(ctx context.Context, p pg.SkillCreateParams) (uuid.UUID, bool, string, error)
	GetNextVersion(slug string) int
	BumpVersion()
	UpdateSkill(ctx context.Context, id uuid.UUID, updates map[string]interface{}) error
	StoreMissingDeps(id uuid.UUID, missing []string) error
}

// seededSkill tracks a skill that was seeded and needs async dep checking.
type seededSkill struct {
	id      uuid.UUID
	slug    string
	baseDir string // managed dir path for ScanSkillDeps
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

// Seed upserts skill records into DB and copies files to managedDir.
// Does NOT check dependencies (non-blocking). Call CheckDepsAsync after startup.
// All skills are seeded as status="active" initially; async check may archive some.
func (s *Seeder) Seed(ctx context.Context) (seeded int, skipped int, skills []seededSkill, err error) {
	entries, err := os.ReadDir(s.bundledDir)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("read bundled dir: %w", err)
	}

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

		// Build frontmatter map
		fm := extractFrontmatter(content)
		fmMap := make(map[string]string)
		if fm != "" {
			fmMap = parseSimpleYAML(fm)
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
			Status:      "active",
			Version:     version,
			FilePath:    destDir,
			FileSize:    int64(len(data)),
			FileHash:    &hash,
			Frontmatter: fmMap,
		}

		id, changed, actualDir, upsertErr := s.store.UpsertSystemSkill(ctx, p)
		if upsertErr != nil {
			slog.Error("seeder: failed to upsert skill", "slug", slug, "error", upsertErr)
			continue
		}

		if !changed {
			// Use the existing file_path from DB — destDir is GetNextVersion+1 which doesn't exist yet.
			// Also check if the managed dir is intact: a previous copy may have failed mid-way due to
			// symlink-to-directory errors, leaving scripts/ empty. Detect by checking if the bundled
			// scripts/ dir has content but the managed scripts/ dir is missing or empty.
			if needsReCopy(skillDir, actualDir) {
				slog.Info("seeder: managed dir incomplete, re-copying", "slug", slug, "dir", actualDir)
				if err := CopyDir(skillDir, actualDir); err != nil {
					slog.Error("seeder: failed to re-copy skill files", "slug", slug, "error", err)
				}
			}
			skipped++
			skills = append(skills, seededSkill{id: id, slug: slug, baseDir: actualDir})
			continue
		}

		// Copy skill directory to managed dir
		if err := CopyDir(skillDir, destDir); err != nil {
			slog.Error("seeder: failed to copy skill files", "slug", slug, "error", err)
			continue
		}

		slog.Info("seeder: skill seeded", "id", id, "slug", slug, "version", version)
		skills = append(skills, seededSkill{id: id, slug: slug, baseDir: actualDir})
		seeded++
	}

	if seeded > 0 {
		s.store.BumpVersion()
	}
	return seeded, skipped, skills, nil
}

// CheckDepsAsync checks dependencies for seeded skills in a background goroutine.
// Emits WS events per-skill so the UI can update in realtime.
// After each check, bumps the skills cache version so the next agent turn picks up changes.
func (s *Seeder) CheckDepsAsync(skills []seededSkill, msgBus *bus.MessageBus) {
	go func() {
		checked := 0
		for _, sk := range skills {
			manifest := ScanSkillDeps(sk.baseDir)
			if manifest == nil || manifest.IsEmpty() {
				emitDepEvent(msgBus, sk.slug, "active", nil)
				checked++
				continue
			}

			ok, missing := CheckSkillDeps(manifest)
			// Always persist missing deps so UI can display them per-skill
			_ = s.store.StoreMissingDeps(sk.id, missing)
			status := "active"
			if !ok {
				status = "archived"
				_ = s.store.UpdateSkill(store.WithCrossTenant(context.Background()), sk.id, map[string]interface{}{"status": "archived"})
				s.store.BumpVersion()
				slog.Warn("seeder: skill deps missing", "slug", sk.slug, "missing", FormatMissing(missing))
			}

			emitDepEvent(msgBus, sk.slug, status, missing)
			checked++
		}

		// Emit completion event
		if msgBus != nil {
			msgBus.Broadcast(bus.Event{
				Name: protocol.EventSkillDepsComplete,
				Payload: map[string]interface{}{
					"count": checked,
				},
			})
		}
		slog.Info("seeder: async dep check complete", "checked", checked)
	}()
}

func emitDepEvent(msgBus *bus.MessageBus, slug, status string, missing []string) {
	if msgBus == nil {
		return
	}
	payload := map[string]interface{}{
		"slug":   slug,
		"status": status,
	}
	if len(missing) > 0 {
		payload["missing"] = missing
	}
	msgBus.Broadcast(bus.Event{
		Name:    protocol.EventSkillDepsChecked,
		Payload: payload,
	})
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

	if err := CopyDir(src, dst); err != nil {
		slog.Warn("seeder: failed to copy shared dir", "name", name, "error", err)
	}
}

// copyDir recursively copies a directory tree.
// Resolves the top-level path and any mid-tree symlinks pointing to directories
// so local module symlinks (e.g. scripts/office -> ../../_shared/office)
// are copied as real directories rather than left as dangling entries.
// CopyDir recursively copies a directory tree.
func CopyDir(src, dst string) error {
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

		// filepath.Walk uses Lstat; a symlink to a directory won't have IsDir()=true.
		// Detect and recurse into directory symlinks so local modules are fully copied.
		if info.Mode()&os.ModeSymlink != 0 {
			if realInfo, statErr := os.Stat(path); statErr == nil && realInfo.IsDir() {
				return CopyDir(path, target)
			}
			return nil // skip broken symlinks
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

// needsReCopy returns true when the managed copy's scripts/ is missing or has fewer
// entries than the bundled source — symptom of a previous failed copy caused by a
// symlink-to-directory stopping filepath.Walk early (e.g. scripts/office/ symlink).
func needsReCopy(bundledDir, managedDir string) bool {
	srcScripts := filepath.Join(bundledDir, "scripts")
	srcEntries, err := os.ReadDir(srcScripts)
	if err != nil || len(srcEntries) == 0 {
		return false // bundled has no scripts; nothing to check
	}
	dstScripts := filepath.Join(managedDir, "scripts")
	dstEntries, err := os.ReadDir(dstScripts)
	if err != nil {
		return true // dst scripts dir missing
	}
	return len(dstEntries) < len(srcEntries)
}
