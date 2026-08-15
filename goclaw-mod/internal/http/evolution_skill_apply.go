package http

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// applySkillDraft creates a managed skill from a SuggestSkillAdd suggestion.
// Uses draftOverride if provided, otherwise falls back to the suggestion's parameters.skill_draft.
func (h *EvolutionHandler) applySkillDraft(ctx context.Context, sg store.EvolutionSuggestion, draftOverride, reviewedBy string) error {
	if h.skillStore == nil || h.skillLoader == nil {
		return fmt.Errorf("skill creation not available")
	}

	// Resolve draft content: request override > suggestion parameters.
	draft := draftOverride
	if draft == "" {
		var params map[string]any
		if err := json.Unmarshal(sg.Parameters, &params); err == nil {
			draft, _ = params["skill_draft"].(string)
		}
	}
	if draft == "" {
		return fmt.Errorf("no skill_draft content found")
	}

	// Security scan before any disk write.
	violations, safe := skills.GuardSkillContent(draft)
	if !safe {
		return fmt.Errorf("skill draft failed security scan: %s", skills.FormatGuardViolations(violations))
	}

	// Parse frontmatter for metadata.
	name, description, slug, frontmatter := skills.ParseSkillFrontmatter(draft)
	if name == "" {
		return fmt.Errorf("skill draft missing 'name' in frontmatter")
	}
	if slug == "" {
		slug = skills.Slugify(name)
	}

	// Resolve tenant-scoped destination directory.
	tenantID := store.TenantIDFromContext(ctx)
	tenantSlug := store.TenantSlugFromContext(ctx)
	baseDir := config.TenantSkillsStoreDir(h.dataDir, tenantID, tenantSlug)

	version := h.skillStore.GetNextVersion(ctx, slug)
	destDir := filepath.Join(baseDir, slug, fmt.Sprintf("%d", version))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create skill directory: %w", err)
	}

	// Write SKILL.md file.
	contentBytes := []byte(draft)
	if err := os.WriteFile(filepath.Join(destDir, "SKILL.md"), contentBytes, 0644); err != nil {
		return fmt.Errorf("write SKILL.md: %w", err)
	}

	// DB insert.
	hasher := sha256.New()
	hasher.Write(contentBytes)
	fileHash := fmt.Sprintf("%x", hasher.Sum(nil))
	desc := description

	id, err := h.skillStore.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name:        name,
		Slug:        slug,
		Description: &desc,
		OwnerID:     reviewedBy,
		Visibility:  "private",
		Version:     version,
		FilePath:    destDir,
		FileSize:    int64(len(contentBytes)),
		FileHash:    &fileHash,
		Frontmatter: frontmatter,
	})
	if err != nil {
		return fmt.Errorf("register skill: %w", err)
	}

	// Bump loader to pick up new skill.
	h.skillLoader.BumpVersion()

	// Mark suggestion as applied.
	if err := h.suggestions.UpdateSuggestionStatus(ctx, sg.ID, "applied", reviewedBy); err != nil {
		slog.Warn("evolution.skill_apply: status update failed", "error", err)
	}

	slog.Info("evolution.skill_apply: created", "skill_id", id, "slug", slug, "version", version, "suggestion", sg.ID)
	return nil
}
