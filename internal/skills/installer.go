package skills

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

const maxInstallDirSize = 20 << 20 // 20 MB

// SkillInstaller orchestrates skill installation from a fetched directory
// into skills-store, the database, and the in-memory loader cache.
// Used by both CLI commands and (future) agent tool.
type SkillInstaller struct {
	store   *pg.PGSkillStore
	baseDir string // skills-store/ root directory
	loader  *Loader
}

// SkillInstallResult holds the outcome of a skill installation.
type SkillInstallResult struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Version     int       `json:"version"`
	DepsWarning string    `json:"deps_warning,omitempty"`
}

// NewInstaller creates a SkillInstaller.
func NewInstaller(store *pg.PGSkillStore, baseDir string, loader *Loader) *SkillInstaller {
	return &SkillInstaller{store: store, baseDir: baseDir, loader: loader}
}

// Install validates, copies, registers, and hot-reloads a skill from srcDir.
// ownerID should be "github:owner/repo" for GitHub-installed skills.
// The caller is responsible for cleaning up srcDir after Install returns.
func (si *SkillInstaller) Install(ctx context.Context, srcDir, ownerID string) (*SkillInstallResult, error) {
	// 1. Read and validate SKILL.md.
	skillPath := filepath.Join(srcDir, "SKILL.md")
	content, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, fmt.Errorf("SKILL.md not found in skill directory: %w", err)
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("SKILL.md is empty")
	}

	// 2. Security scan SKILL.md content.
	if violations, safe := GuardSkillContent(string(content)); !safe {
		return nil, fmt.Errorf("skill rejected by security scanner:\n%s", FormatGuardViolations(violations))
	}

	// 3. Parse frontmatter.
	name, description, slug, frontmatter := ParseSkillFrontmatter(string(content))
	if name == "" {
		return nil, fmt.Errorf("SKILL.md frontmatter must contain 'name' field")
	}
	if slug == "" {
		slug = Slugify(name)
	}
	if !SlugRegexp.MatchString(slug) {
		return nil, fmt.Errorf("invalid slug %q: must be lowercase alphanumeric with hyphens", slug)
	}

	// 3. Reject system skill overwrite.
	if si.store.IsSystemSkill(slug) {
		return nil, fmt.Errorf("slug %q conflicts with a system skill — cannot overwrite", slug)
	}

	// 4. Compute content hash.
	hash := fmt.Sprintf("%x", sha256.Sum256(content))

	// 5. Check directory size.
	size, err := installDirSize(srcDir)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate directory size: %w", err)
	}
	if size > maxInstallDirSize {
		return nil, fmt.Errorf("skill directory too large: %d bytes (max %d MB)", size, maxInstallDirSize>>20)
	}

	// 6. Acquire locked version to prevent race conditions.
	version, commitVersion, err := si.store.GetNextVersionLocked(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("acquire version lock: %w", err)
	}
	// Release the advisory lock after we're done with copy + DB insert.
	defer commitVersion() //nolint:errcheck

	destDir := filepath.Join(si.baseDir, slug, strconv.Itoa(version))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("create destination: %w", err)
	}

	// 7. Copy files. Cleanup on failure.
	if err := CopyDir(srcDir, destDir); err != nil {
		os.RemoveAll(destDir)
		return nil, fmt.Errorf("copy skill files: %w", err)
	}

	// 8. Register in DB.
	desc := description
	params := store.SkillCreateParams{
		Name:        name,
		Slug:        slug,
		Description: &desc,
		OwnerID:     ownerID,
		Visibility:  "public",
		Status:      "active",
		Version:     version,
		FilePath:    destDir,
		FileSize:    size,
		FileHash:    &hash,
		Frontmatter: frontmatter,
	}
	id, err := si.store.CreateSkillManaged(ctx, params)
	if err != nil {
		os.RemoveAll(destDir) // cleanup orphaned files
		return nil, fmt.Errorf("register skill in DB: %w", err)
	}

	slog.Info("skill installed", "id", id, "slug", slug, "version", version, "owner", ownerID)

	// 9. Scan and install dependencies.
	var depsWarning string
	manifest := ScanSkillDeps(destDir)
	if manifest != nil && !manifest.IsEmpty() {
		ok, missing := CheckSkillDeps(manifest)
		if !ok {
			_ = si.store.StoreMissingDeps(ctx, id, missing)
			// Auto-install missing deps.
			installResult, _ := InstallDeps(ctx, manifest, missing)
			if installResult != nil && len(installResult.Errors) > 0 {
				depsWarning = fmt.Sprintf("some deps failed: %v", installResult.Errors)
			}
		}
	}

	// 10. Hot-reload.
	if si.loader != nil {
		si.loader.BumpVersion()
	}

	return &SkillInstallResult{
		ID:          id,
		Name:        name,
		Slug:        slug,
		Version:     version,
		DepsWarning: depsWarning,
	}, nil
}

// Uninstall soft-deletes a skill by slug and refreshes the loader cache.
func (si *SkillInstaller) Uninstall(ctx context.Context, slug string) error {
	// Look up skill by slug.
	info, ok := si.store.GetSkill(ctx, slug)
	if !ok {
		return fmt.Errorf("skill %q not found", slug)
	}
	id, err := uuid.Parse(info.ID)
	if err != nil {
		return fmt.Errorf("invalid skill ID: %w", err)
	}

	if err := si.store.DeleteSkill(ctx, id); err != nil {
		return fmt.Errorf("delete skill: %w", err)
	}

	slog.Info("skill uninstalled", "slug", slug, "id", id)

	if si.loader != nil {
		si.loader.BumpVersion()
	}
	return nil
}

// installDirSize returns total size of all files in a directory.
func installDirSize(path string) (int64, error) {
	var total int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}
