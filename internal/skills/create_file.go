package skills

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Errors returned by CreateFromContent.
var (
	ErrSkillNameRequired  = errors.New("name is required in SKILL.md frontmatter")
	ErrSkillSlugInvalid   = errors.New("invalid skill slug")
	ErrSkillSlugConflict  = errors.New("slug conflicts with a system skill")
	ErrSkillGuardRejected = errors.New("skill content failed security scan")
)

// CreateFromContent creates a new managed skill from a single SKILL.md's
// content. This is the single-file equivalent of the web UI's ZIP-based
// skill upload (SkillsHandler.handleUpload in
// internal/http/skills_upload.go) — same frontmatter parsing, security
// guard, slug validation, and version-1 directory layout, minus multi-file
// extraction and dependency scanning (not meaningful for a text-only
// caller like an MCP tool). Name/slug/description are parsed from the
// content's YAML frontmatter.
func CreateFromContent(ctx context.Context, manage store.SkillManageStore, tenantSkillsDir, content, ownerID string) (id uuid.UUID, slug string, err error) {
	if violations, safe := GuardSkillContent(content); !safe {
		return uuid.Nil, "", fmt.Errorf("%w: %s", ErrSkillGuardRejected, FormatGuardViolations(violations))
	}

	name, description, parsedSlug, frontmatter := ParseSkillFrontmatter(content)
	if name == "" {
		return uuid.Nil, "", ErrSkillNameRequired
	}
	slug = parsedSlug
	if slug == "" {
		slug = Slugify(name)
	}
	if !SlugRegexp.MatchString(slug) {
		return uuid.Nil, "", ErrSkillSlugInvalid
	}
	if manage.IsSystemSkill(slug) {
		return uuid.Nil, "", ErrSkillSlugConflict
	}

	version := manage.GetNextVersion(ctx, slug)
	destDir := filepath.Join(tenantSkillsDir, slug, strconv.Itoa(version))
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return uuid.Nil, "", err
	}
	if err := os.WriteFile(filepath.Join(destDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		_ = os.RemoveAll(destDir)
		return uuid.Nil, "", err
	}

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	desc := description
	id, err = manage.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name:        name,
		Slug:        slug,
		Description: &desc,
		OwnerID:     ownerID,
		Visibility:  "internal",
		Status:      "active",
		Version:     version,
		FilePath:    destDir,
		FileSize:    int64(len(content)),
		FileHash:    &hash,
		Frontmatter: frontmatter,
	})
	if err != nil {
		_ = os.RemoveAll(destDir)
		return uuid.Nil, "", err
	}
	manage.BumpVersion()
	return id, slug, nil
}
