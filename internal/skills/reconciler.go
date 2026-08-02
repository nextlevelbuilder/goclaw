package skills

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ManagedSkillStore is the minimal interface needed by the reconciler.
// PGSkillStore satisfies it; other backends may omit it and the reconcile step
// is skipped at setup.
type ManagedSkillStore interface {
	// SkillExists reports whether a skills row already exists for the slug in
	// the current tenant (any status, including deleted).
	SkillExists(ctx context.Context, slug string) (bool, error)
	// CreateSkillManaged registers a skill row (tenant-scoped via ctx).
	CreateSkillManaged(ctx context.Context, p store.SkillCreateParams) (uuid.UUID, error)
	// BumpVersion invalidates cached skill listings after a change.
	BumpVersion()
}

// Reconciler registers on-disk managed skills (skills-store) that are missing
// from the database so they become visible to agents. Skills are the
// DB-registered, agent-filtered list; a skill placed directly into a tenant's
// skills-store directory without a skills row would otherwise be silently
// invisible in <available_skills> and skill_search.
type Reconciler struct {
	store ManagedSkillStore
}

// NewReconciler creates a reconciler backed by the given skill store.
func NewReconciler(s ManagedSkillStore) *Reconciler {
	return &Reconciler{store: s}
}

// Reconcile scans managedDir (a single tenant's skills-store, e.g. the master
// tenant's <dataDir>/skills-store) for <slug>/<version>/SKILL.md directories
// and registers any slug that has no skills row. Registration is idempotent:
// existing rows (active, archived, or deleted) are left untouched and their
// versions are never bumped. Returns the number of skills registered.
func (r *Reconciler) Reconcile(ctx context.Context, tenantID uuid.UUID, managedDir string) (int, error) {
	if managedDir == "" {
		return 0, nil
	}
	ctx = store.WithTenantID(ctx, tenantID)

	entries, err := os.ReadDir(managedDir)
	if err != nil {
		return 0, nil // directory may not exist yet (fresh install)
	}

	registered := 0
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), "_") {
			continue // _shared/ is a helper dir, not a skill
		}
		slug := e.Name()

		version, versionDir := latestManagedVersion(managedDir, slug)
		if version < 0 {
			continue
		}
		skillFile := filepath.Join(versionDir, "SKILL.md")
		data, err := os.ReadFile(skillFile)
		if err != nil {
			slog.Debug("skills reconcile: skip dir without SKILL.md", "slug", slug)
			continue
		}

		exists, err := r.store.SkillExists(ctx, slug)
		if err != nil {
			slog.Warn("skills reconcile: existence check failed", "slug", slug, "error", err)
			continue
		}
		if exists {
			continue
		}

		// Register the on-disk skill with metadata parsed from its frontmatter.
		name := slug
		description := ""
		if meta := parseMetadata(skillFile); meta != nil {
			if meta.Name != "" {
				name = meta.Name
			}
			description = meta.Description
		}
		fmMap := map[string]string{}
		if fm := extractFrontmatter(string(data)); fm != "" {
			fmMap = parseSimpleYAML(fm)
		}

		// Only an explicit public visibility can be honored: private requires an
		// owner match and internal requires agent grants, neither of which the
		// reconciler can establish. Public keeps the skill usable by all agents
		// in the tenant (still tenant-scoped — never cross-tenant).
		visibility := VisibilityPublic
		if v := strings.ToLower(strings.TrimSpace(fmMap["visibility"])); v != "" && v == VisibilityPublic {
			visibility = VisibilityPublic
		} else if v != "" {
			slog.Info("skills reconcile: registering as public because reconciler cannot grant "+v,
				"slug", slug, "frontmatter_visibility", v)
		}

		hash := fmt.Sprintf("%x", sha256.Sum256(data))
		desc := description
		_, err = r.store.CreateSkillManaged(ctx, store.SkillCreateParams{
			Name:        name,
			Slug:        slug,
			Description: &desc,
			OwnerID:     "system",
			Visibility:  visibility,
			Status:      "active",
			Version:     version,
			FilePath:    versionDir,
			FileSize:    int64(len(data)),
			FileHash:    &hash,
			Frontmatter: fmMap,
		})
		if err != nil {
			slog.Warn("skills reconcile: registration failed", "slug", slug, "error", err)
			continue
		}
		registered++
		slog.Info("skills reconcile: registered on-disk skill", "slug", slug, "version", version)
	}

	if registered > 0 {
		r.store.BumpVersion()
	}
	return registered, nil
}

// latestManagedVersion returns the highest-numbered version subdirectory for a
// skill slug within a managed skills directory (structure: <dir>/<slug>/<version>/).
// Mirrors the Loader's version-resolution so the reconciler doesn't need a Loader
// instance. Returns (version, path) or (-1, "") if no valid version exists.
func latestManagedVersion(managedDir, slug string) (int, string) {
	slugDir := filepath.Join(managedDir, slug)
	entries, err := os.ReadDir(slugDir)
	if err != nil {
		return -1, ""
	}

	var versions []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		v, err := strconv.Atoi(e.Name())
		if err != nil || v < 1 {
			continue
		}
		versions = append(versions, v)
	}
	if len(versions) == 0 {
		return -1, ""
	}

	sort.Sort(sort.Reverse(sort.IntSlice(versions)))
	latestVer := versions[0]
	return latestVer, filepath.Join(slugDir, strconv.Itoa(latestVer))
}
