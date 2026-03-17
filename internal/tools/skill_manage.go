package tools

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// SkillManageTool provides agent-driven skill lifecycle management.
// Complements publish_skill (directory-based) with a content-string interface
// so agents can create/patch/delete skills without pre-writing files to disk.
type SkillManageTool struct {
	skills *pg.PGSkillStore
	base   string         // skills-store/ base directory
	loader *skills.Loader // cache invalidation
}

func NewSkillManageTool(skills *pg.PGSkillStore, baseDir string, loader *skills.Loader) *SkillManageTool {
	return &SkillManageTool{skills: skills, base: baseDir, loader: loader}
}

func (t *SkillManageTool) Name() string { return "skill_manage" }

func (t *SkillManageTool) Description() string {
	return "Create, patch, or delete your own skills from content strings. " +
		"action=create: write a new skill from SKILL.md content (content string, no directory needed). " +
		"action=patch: update an existing skill via find/replace (creates new immutable version). " +
		"action=delete: archive a skill so it is no longer discoverable. " +
		"Security scanner rejects dangerous patterns. You can only manage skills you own."
}

func (t *SkillManageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"create", "patch", "delete"},
				"description": "Operation to perform on the skill.",
			},
			"slug": map[string]any{
				"type":        "string",
				"description": "Unique skill identifier (lowercase alphanumeric + hyphens). Required for patch/delete. For create: auto-derived from 'name' frontmatter field if omitted.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Full SKILL.md content including YAML frontmatter (---\\nname: ...\\n---\\n# ...). Required for create.",
			},
			"find": map[string]any{
				"type":        "string",
				"description": "Exact text to find in the current SKILL.md. Required for patch.",
			},
			"replace": map[string]any{
				"type":        "string",
				"description": "Replacement text. Required for patch.",
			},
		},
		"required": []string{"action"},
	}
}

func (t *SkillManageTool) Execute(ctx context.Context, args map[string]any) *Result {
	action, _ := args["action"].(string)
	switch action {
	case "create":
		return t.executeCreate(ctx, args)
	case "patch":
		return t.executePatch(ctx, args)
	case "delete":
		return t.executeDelete(ctx, args)
	default:
		return ErrorResult("action must be one of: create, patch, delete")
	}
}

// maxSkillContentSize limits SKILL.md content to 100KB to prevent abuse.
const maxSkillContentSize = 100 * 1024

// executeCreate writes a new skill from a SKILL.md content string.
func (t *SkillManageTool) executeCreate(ctx context.Context, args map[string]any) *Result {
	content, _ := args["content"].(string)
	if strings.TrimSpace(content) == "" {
		return ErrorResult("content is required for action=create")
	}
	if len(content) > maxSkillContentSize {
		return ErrorResult(fmt.Sprintf("content too large (%d bytes, max %d)", len(content), maxSkillContentSize))
	}

	// Security scan before any disk write
	violations, safe := skills.GuardSkillContent(content)
	if !safe {
		return ErrorResult(skills.FormatGuardViolations(violations))
	}

	// Parse frontmatter
	name, description, slug, frontmatter := skills.ParseSkillFrontmatter(content)
	if name == "" {
		return ErrorResult("SKILL.md frontmatter must contain 'name' field")
	}

	// Allow slug override from args
	if argSlug, _ := args["slug"].(string); argSlug != "" {
		slug = argSlug
	}
	if slug == "" {
		slug = skills.Slugify(name)
	}
	if !skills.SlugRegexp.MatchString(slug) {
		return ErrorResult(fmt.Sprintf("invalid slug %q: must be lowercase alphanumeric with hyphens", slug))
	}
	if t.skills.IsSystemSkill(slug) {
		return ErrorResult(fmt.Sprintf("cannot manage system skill %q", slug))
	}

	// Version + destination
	version := t.skills.GetNextVersion(slug)
	destDir := filepath.Join(t.base, slug, fmt.Sprintf("%d", version))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create skill directory: %v", err))
	}

	// Write SKILL.md
	contentBytes := []byte(content)
	skillPath := filepath.Join(destDir, "SKILL.md")
	if err := os.WriteFile(skillPath, contentBytes, 0644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write SKILL.md: %v", err))
	}

	// Hash + size
	hasher := sha256.New()
	hasher.Write(contentBytes)
	fileHash := fmt.Sprintf("%x", hasher.Sum(nil))
	fileSize := int64(len(contentBytes))

	// DB insert
	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		userID = "system"
	}
	desc := description
	id, err := t.skills.CreateSkillManaged(ctx, pg.SkillCreateParams{
		Name:        name,
		Slug:        slug,
		Description: &desc,
		OwnerID:     userID,
		Visibility:  "private",
		Version:     version,
		FilePath:    destDir,
		FileSize:    fileSize,
		FileHash:    &fileHash,
		Frontmatter: frontmatter,
	})
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to register skill: %v", err))
	}

	slog.Info("skill_manage: created", "id", id, "slug", slug, "version", version, "owner", userID)

	// Auto-grant to calling agent
	granted := false
	agentID := store.AgentIDFromContext(ctx)
	if agentID != uuid.Nil {
		if err := t.skills.GrantToAgent(ctx, id, agentID, version, userID); err != nil {
			slog.Warn("skill_manage: auto-grant failed", "error", err)
		} else {
			granted = true
		}
	}

	if t.loader != nil {
		t.loader.BumpVersion()
	}

	// Dep scan (best-effort, warn only)
	var depsWarning string
	manifest := skills.ScanSkillDeps(destDir)
	if manifest != nil && !manifest.IsEmpty() {
		ok, missing := skills.CheckSkillDeps(manifest)
		if !ok {
			_ = t.skills.StoreMissingDeps(id, missing)
			depsWarning = skills.FormatMissing(missing)
		}
	}

	result := fmt.Sprintf("Skill %q created.\n- Slug: %s\n- Version: %d", name, slug, version)
	if granted {
		result += "\n- Granted to current agent"
	}
	result += "\n\nSkill will appear in search on next turn."
	if depsWarning != "" {
		result += fmt.Sprintf("\n\n⚠ Missing dependencies: %s", depsWarning)
	}
	return NewResult(result)
}

// executePatch applies a find/replace to the latest version and saves as a new version.
func (t *SkillManageTool) executePatch(ctx context.Context, args map[string]any) *Result {
	slug, _ := args["slug"].(string)
	find, _ := args["find"].(string)
	replace, _ := args["replace"].(string)
	if slug == "" {
		return ErrorResult("slug is required for action=patch")
	}
	if find == "" {
		return ErrorResult("find is required for action=patch")
	}

	info, ok := t.skills.GetSkill(slug)
	if !ok {
		return ErrorResult(fmt.Sprintf("skill %q not found or archived", slug))
	}
	if t.skills.IsSystemSkill(slug) {
		return ErrorResult(fmt.Sprintf("cannot manage system skill %q", slug))
	}

	// Ownership check: only the skill owner can patch
	userID := store.UserIDFromContext(ctx)
	if ownerID, found := t.skills.GetSkillOwnerIDBySlug(slug); found && ownerID != userID {
		return ErrorResult(fmt.Sprintf("cannot manage skill %q: you are not the owner", slug))
	}

	// Read current SKILL.md from latest version
	current, err := os.ReadFile(info.Path)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read current SKILL.md: %v", err))
	}

	patched := strings.Replace(string(current), find, replace, 1)
	if patched == string(current) {
		return NewResult("no change: find text not found in current SKILL.md")
	}

	// Security scan on patched content
	violations, safe := skills.GuardSkillContent(patched)
	if !safe {
		return ErrorResult(skills.FormatGuardViolations(violations))
	}

	oldVer := info.Version
	newVer, commitLock, lockErr := t.skills.GetNextVersionLocked(ctx, slug)
	if lockErr != nil {
		return ErrorResult(fmt.Sprintf("failed to lock version: %v", lockErr))
	}
	defer commitLock() //nolint:errcheck
	destDir := filepath.Join(t.base, slug, fmt.Sprintf("%d", newVer))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create new version directory: %v", err))
	}

	// Write patched SKILL.md
	patchedBytes := []byte(patched)
	if err := os.WriteFile(filepath.Join(destDir, "SKILL.md"), patchedBytes, 0644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write patched SKILL.md: %v", err))
	}

	// Copy any companion files from old version (scripts, assets, etc.)
	if err := copyOtherFiles(info.BaseDir, destDir); err != nil {
		slog.Warn("skill_manage: failed to copy companion files", "error", err)
	}

	// Hash + size
	hasher := sha256.New()
	hasher.Write(patchedBytes)
	fileHash := fmt.Sprintf("%x", hasher.Sum(nil))
	fileSize := int64(len(patchedBytes))

	// DB update
	skillID, err := uuid.Parse(info.ID)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid skill ID in database: %v", err))
	}
	if err := t.skills.UpdateSkill(skillID, map[string]any{
		"version":    newVer,
		"file_path":  destDir,
		"file_size":  fileSize,
		"file_hash":  &fileHash,
		"updated_at": time.Now(),
	}); err != nil {
		return ErrorResult(fmt.Sprintf("failed to update skill in database: %v", err))
	}

	slog.Info("skill_manage: patched", "slug", slug, "old_version", oldVer, "new_version", newVer)

	if t.loader != nil {
		t.loader.BumpVersion()
	}

	return NewResult(fmt.Sprintf("Skill %q patched. v%d → v%d. Changes active next turn.", slug, oldVer, newVer))
}

// executeDelete archives a skill in the DB and moves its directory to .trash/.
func (t *SkillManageTool) executeDelete(ctx context.Context, args map[string]any) *Result {
	slug, _ := args["slug"].(string)
	if slug == "" {
		return ErrorResult("slug is required for action=delete")
	}

	info, ok := t.skills.GetSkill(slug)
	if !ok {
		return ErrorResult(fmt.Sprintf("skill %q not found or already archived", slug))
	}
	if t.skills.IsSystemSkill(slug) {
		return ErrorResult(fmt.Sprintf("cannot manage system skill %q", slug))
	}

	// Ownership check: only the skill owner can delete
	deleteUserID := store.UserIDFromContext(ctx)
	if ownerID, found := t.skills.GetSkillOwnerIDBySlug(slug); found && ownerID != deleteUserID {
		return ErrorResult(fmt.Sprintf("cannot manage skill %q: you are not the owner", slug))
	}

	// Soft-delete on disk: move to .trash/<slug>.<unix-timestamp>
	trashDir := filepath.Join(t.base, ".trash")
	if err := os.MkdirAll(trashDir, 0755); err != nil {
		slog.Warn("skill_manage: failed to create .trash dir", "error", err)
	} else {
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		src := filepath.Join(t.base, slug)
		dst := filepath.Join(trashDir, slug+"."+timestamp)
		if err := os.Rename(src, dst); err != nil {
			// Cross-device rename fails on some setups — log and continue (DB archive is primary)
			slog.Warn("skill_manage: disk move to .trash failed", "slug", slug, "error", err)
		}
	}

	// DB archive
	skillID, err := uuid.Parse(info.ID)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid skill ID in database: %v", err))
	}
	if err := t.skills.DeleteSkill(skillID); err != nil {
		return ErrorResult(fmt.Sprintf("failed to archive skill in database: %v", err))
	}

	slog.Info("skill_manage: deleted", "slug", slug, "id", info.ID)

	if t.loader != nil {
		t.loader.BumpVersion()
	}

	return NewResult(fmt.Sprintf("Skill %q archived and removed from search.", slug))
}

// maxCopySize limits total companion file copy to 20MB (matching publish_skill).
const maxCopySize = 20 << 20

// copyOtherFiles copies all files from srcDir to dstDir except SKILL.md.
// Used by patch to carry companion files (scripts, assets) into the new version directory.
// Uses WalkDir (not Walk) so symlinks are detected via DirEntry.Type() before Stat follows them.
// Enforces a 20MB total size limit.
func copyOtherFiles(srcDir, dstDir string) error {
	var totalSize int64
	return filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip symlinks — WalkDir exposes the raw type before following
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." || rel == "SKILL.md" {
			return nil
		}
		// Skip path traversal attempts
		if strings.Contains(rel, "..") {
			return nil
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dstDir, rel), 0755)
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		totalSize += fi.Size()
		if totalSize > maxCopySize {
			return fmt.Errorf("companion files exceed %d bytes limit", maxCopySize)
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		dst, err := os.Create(filepath.Join(dstDir, rel))
		if err != nil {
			return err
		}
		defer dst.Close()
		_, err = io.Copy(dst, src)
		return err
	})
}
