package tools

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SkillManageTool provides agent-driven skill lifecycle management.
// Complements publish_skill (directory-based) with a content-string interface
// so agents can create/patch/delete skills without pre-writing files to disk.
type SkillManageTool struct {
	skills  store.SkillManageStore
	base    string         // skills-store/ base directory (master tenant)
	dataDir string         // parent data dir for tenant-scoped skill paths
	loader  *skills.Loader // cache invalidation
}

func NewSkillManageTool(skills store.SkillManageStore, baseDir, dataDir string, loader *skills.Loader) *SkillManageTool {
	return &SkillManageTool{skills: skills, base: baseDir, dataDir: dataDir, loader: loader}
}

// isOwnerOfSkill returns true if the caller owns the skill identified by slug.
// Matches owner_id against three identities for backward compatibility (#915):
//   - ActorIDFromContext: current helper, merge-aware in DM, sender in groups
//   - UserIDFromContext:  legacy rows pre-#915 (group-scope) and merged tenant id
//   - SenderIDFromContext: raw channel sender (covers the pre-merge-aware ActorID window)
//
// If the slug does not exist, returns true (caller sees "skill not found" error
// from the downstream resolver — preserves the existing error surface).
func isOwnerOfSkill(ctx context.Context, skills store.SkillManageStore, slug string) bool {
	ownerID, found := skills.GetSkillOwnerIDBySlug(ctx, slug)
	if !found {
		return true
	}
	actorID := store.ActorIDFromContext(ctx)
	userID := store.UserIDFromContext(ctx)
	senderID := store.SenderIDFromContext(ctx)
	return ownerID == actorID || ownerID == userID || ownerID == senderID
}

func canManageSkill(ctx context.Context, skills store.SkillManageStore, info *store.SkillInfo) bool {
	if isOwnerOfSkill(ctx, skills, info.Slug) {
		return true
	}
	if info.ID == "" {
		return false
	}
	skillID, err := uuid.Parse(info.ID)
	if err != nil {
		return false
	}
	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return false
	}
	ok, err := skills.AgentCanManageSkill(ctx, skillID, agentID)
	if err != nil {
		slog.Warn("skill_manage: manage grant check failed", "skill", info.Slug, "agent_id", agentID, "error", err)
		return false
	}
	return ok
}

// tenantSkillsDir returns the skills-store directory scoped to the calling agent's tenant.
func (t *SkillManageTool) tenantSkillsDir(ctx context.Context) string {
	tid := store.TenantIDFromContext(ctx)
	slug := store.TenantSlugFromContext(ctx)
	return config.TenantSkillsStoreDir(t.dataDir, tid, slug)
}

func (t *SkillManageTool) Name() string { return "skill_manage" }

func (t *SkillManageTool) Description() string {
	return "Create, patch, or delete your own skills from content strings. " +
		"action=create: write a new skill from SKILL.md content and optional companion files. " +
		"action=patch: update an existing skill via find/replace and/or companion files (creates new immutable version). " +
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
				"description": "Exact text to find in the current SKILL.md. Required for content patch unless only 'files' or 'visibility' is being updated.",
			},
			"replace": map[string]any{
				"type":        "string",
				"description": "Replacement text. Required for patch.",
			},
			"visibility": map[string]any{
				"type":        "string",
				"enum":        []string{skills.VisibilityPrivate, skills.VisibilityPublic},
				"description": "Skill visibility. For create: defaults to 'private'. For patch: updates who can discover the skill without creating a new version.",
			},
			"files": map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "string"},
				"description":          "Optional companion files keyed by relative path under the skill root. SKILL.md must use 'content' or find/replace. Unsafe paths and system artifacts are rejected.",
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

const maxManagedSkillFileSize = 2 << 20

type managedSkillFile struct {
	Path    string
	Content []byte
}

// executeCreate writes a new skill from a SKILL.md content string.
func (t *SkillManageTool) executeCreate(ctx context.Context, args map[string]any) *Result {
	content, _ := args["content"].(string)
	if strings.TrimSpace(content) == "" {
		return ErrorResult("content is required for action=create")
	}
	if len(content) > maxSkillContentSize {
		return ErrorResult(fmt.Sprintf("content too large (%d bytes, max %d)", len(content), maxSkillContentSize))
	}
	companionFiles, err := parseManagedSkillFiles(args["files"])
	if err != nil {
		return ErrorResult(err.Error())
	}
	if err := validateManagedSkillTotalSize(companionFiles); err != nil {
		return ErrorResult(err.Error())
	}

	rawVisibility, _ := args["visibility"].(string)
	if err := skills.ValidateVisibility(rawVisibility); err != nil {
		return ErrorResult(err.Error())
	}
	visibility := skills.NormalizeVisibility(rawVisibility)

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

	// Version + destination (tenant-scoped)
	version := t.skills.GetNextVersion(ctx, slug)
	destDir := filepath.Join(t.tenantSkillsDir(ctx), slug, fmt.Sprintf("%d", version))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create skill directory: %v", err))
	}
	cleanupDest := true
	defer func() {
		if cleanupDest {
			_ = os.RemoveAll(destDir)
		}
	}()

	// Write SKILL.md
	contentBytes := []byte(content)
	skillPath := filepath.Join(destDir, "SKILL.md")
	if err := os.WriteFile(skillPath, contentBytes, 0644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write SKILL.md: %v", err))
	}
	if err := writeManagedSkillFiles(destDir, companionFiles); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write companion files: %v", err))
	}

	// Hash + size
	hasher := sha256.New()
	hasher.Write(contentBytes)
	fileHash := fmt.Sprintf("%x", hasher.Sum(nil))
	fileSize, err := dirSize(destDir)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to calculate skill size: %v", err))
	}

	// DB insert — owner = actor (real sender) so skill belongs to the individual
	// user rather than the group principal in group chats (#915).
	ownerID := store.ActorIDFromContext(ctx)
	if ownerID == "" {
		ownerID = "system"
	}
	desc := description
	id, err := t.skills.CreateSkillManaged(ctx, store.SkillCreateParams{
		Name:        name,
		Slug:        slug,
		Description: &desc,
		OwnerID:     ownerID,
		Visibility:  visibility,
		Version:     version,
		FilePath:    destDir,
		FileSize:    fileSize,
		FileHash:    &fileHash,
		Frontmatter: frontmatter,
	})
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to register skill: %v", err))
	}
	cleanupDest = false

	slog.Info("skill_manage: created", "id", id, "slug", slug, "version", version, "owner", ownerID)

	// Auto-grant to calling agent (granted-by = owner, same as CreateSkillManaged)
	granted := false
	agentID := store.AgentIDFromContext(ctx)
	if agentID != uuid.Nil {
		if err := t.skills.GrantToAgent(ctx, id, agentID, version, ownerID, true); err != nil {
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
			_ = t.skills.StoreMissingDeps(ctx, id, missing)
			depsWarning = skills.FormatMissing(missing)
		}
	}

	result := fmt.Sprintf("Skill %q created.\n- Slug: %s\n- Version: %d", name, slug, version)
	if len(companionFiles) > 0 {
		result += fmt.Sprintf("\n- Companion files: %d", len(companionFiles))
	}
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
	rawVisibility, _ := args["visibility"].(string)
	companionFiles, filesErr := parseManagedSkillFiles(args["files"])
	if filesErr != nil {
		return ErrorResult(filesErr.Error())
	}
	if slug == "" {
		return ErrorResult("slug is required for action=patch")
	}
	if err := skills.ValidateVisibility(rawVisibility); err != nil {
		return ErrorResult(err.Error())
	}
	// Patch requires at least one of: content edit (find), file payload, or visibility change.
	if find == "" && len(companionFiles) == 0 && rawVisibility == "" {
		return ErrorResult("patch requires either 'find' (content edit), 'files' (companion files), or 'visibility' (metadata update)")
	}

	info, ok := t.skills.GetSkill(ctx, slug)
	if !ok {
		return ErrorResult(fmt.Sprintf("skill %q not found or archived", slug))
	}
	if t.skills.IsSystemSkill(slug) {
		return ErrorResult(fmt.Sprintf("cannot manage system skill %q", slug))
	}

	// Ownership check: only the skill owner can patch.
	// Accept any of three identities the same human maps to:
	//   - actor (current merge-aware helper — preferred for new rows)
	//   - userID (legacy pre-#915 rows where owner_id was group principal
	//     or the merged tenant identity)
	//   - senderID (legacy rows from the pre-merge-aware ActorID window
	//     where DM owners got the raw channel sender)
	// A DM user merged to "viettx" with Telegram ID "386246614" matches all
	// three of their skills regardless of when they were created.
	if !canManageSkill(ctx, t.skills, info) {
		return ErrorResult(fmt.Sprintf("cannot manage skill %q: you are not the owner", slug))
	}

	// Visibility-only patch path: no content/files change, no new version.
	if find == "" && len(companionFiles) == 0 && rawVisibility != "" {
		skillID, err := uuid.Parse(info.ID)
		if err != nil {
			return ErrorResult(fmt.Sprintf("invalid skill ID in database: %v", err))
		}
		newVisibility := skills.NormalizeVisibility(rawVisibility)
		if err := t.skills.UpdateSkill(ctx, skillID, map[string]any{
			"visibility": newVisibility,
			"updated_at": time.Now(),
		}); err != nil {
			return ErrorResult(fmt.Sprintf("failed to update skill visibility: %v", err))
		}
		slog.Info("skill_manage: visibility updated", "slug", slug, "visibility", newVisibility)
		if t.loader != nil {
			t.loader.BumpVersion()
		}
		return NewResult(fmt.Sprintf("Skill %q visibility set to %s.", slug, newVisibility))
	}

	newVer, commitLock, lockErr := t.skills.GetNextVersionLocked(ctx, slug)
	if lockErr != nil {
		return ErrorResult(fmt.Sprintf("failed to lock version: %v", lockErr))
	}
	defer commitLock() //nolint:errcheck

	latestInfo, ok := t.skills.GetSkill(ctx, slug)
	if !ok {
		return ErrorResult(fmt.Sprintf("skill %q not found or archived", slug))
	}
	if t.skills.IsSystemSkill(slug) {
		return ErrorResult(fmt.Sprintf("cannot manage system skill %q", slug))
	}
	if !canManageSkill(ctx, t.skills, latestInfo) {
		return ErrorResult(fmt.Sprintf("cannot manage skill %q: you are not the owner", slug))
	}

	existingFiles, err := collectExistingManagedSkillCompanionFiles(latestInfo.BaseDir)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to inspect companion files: %v", err))
	}
	finalCompanionFiles := overlayManagedSkillFiles(existingFiles, companionFiles)
	if err := validateManagedSkillTotalSize(finalCompanionFiles); err != nil {
		return ErrorResult(err.Error())
	}

	// Read current SKILL.md from the latest version while the slug lock is held.
	current, err := os.ReadFile(latestInfo.Path)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read current SKILL.md: %v", err))
	}

	patched := string(current)
	if find != "" {
		patched = strings.Replace(patched, find, replace, 1)
	}
	if find != "" && patched == string(current) {
		return NewResult("no change: find text not found in current SKILL.md")
	}

	// Security scan on patched content
	violations, safe := skills.GuardSkillContent(patched)
	if !safe {
		return ErrorResult(skills.FormatGuardViolations(violations))
	}

	oldVer := latestInfo.Version
	destDir := filepath.Join(t.tenantSkillsDir(ctx), slug, fmt.Sprintf("%d", newVer))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create new version directory: %v", err))
	}
	cleanupDest := true
	defer func() {
		if cleanupDest {
			_ = os.RemoveAll(destDir)
		}
	}()

	// Write patched SKILL.md
	patchedBytes := []byte(patched)
	if err := os.WriteFile(filepath.Join(destDir, "SKILL.md"), patchedBytes, 0644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write patched SKILL.md: %v", err))
	}

	if err := writeManagedSkillFiles(destDir, finalCompanionFiles); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write companion files: %v", err))
	}

	// Hash + size
	hasher := sha256.New()
	hasher.Write(patchedBytes)
	fileHash := fmt.Sprintf("%x", hasher.Sum(nil))
	fileSize, err := dirSize(destDir)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to calculate skill size: %v", err))
	}

	// DB update
	skillID, err := uuid.Parse(info.ID)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid skill ID in database: %v", err))
	}
	updates := map[string]any{
		"version":    newVer,
		"file_path":  destDir,
		"file_size":  fileSize,
		"file_hash":  &fileHash,
		"updated_at": time.Now(),
	}
	if rawVisibility != "" {
		updates["visibility"] = skills.NormalizeVisibility(rawVisibility)
	}
	if err := t.skills.UpdateSkill(ctx, skillID, updates); err != nil {
		return ErrorResult(fmt.Sprintf("failed to update skill in database: %v", err))
	}
	cleanupDest = false

	slog.Info("skill_manage: patched", "slug", slug, "old_version", oldVer, "new_version", newVer, "companion_files", len(companionFiles))

	if t.loader != nil {
		t.loader.BumpVersion()
	}

	result := fmt.Sprintf("Skill %q patched. v%d → v%d. Changes active next turn.", slug, oldVer, newVer)
	if len(companionFiles) > 0 {
		result += fmt.Sprintf("\n- Companion files written: %d", len(companionFiles))
	}
	return NewResult(result)
}

// executeDelete archives a skill in the DB and moves its directory to .trash/.
func (t *SkillManageTool) executeDelete(ctx context.Context, args map[string]any) *Result {
	slug, _ := args["slug"].(string)
	if slug == "" {
		return ErrorResult("slug is required for action=delete")
	}

	info, ok := t.skills.GetSkill(ctx, slug)
	if !ok {
		return ErrorResult(fmt.Sprintf("skill %q not found or already archived", slug))
	}
	if t.skills.IsSystemSkill(slug) {
		return ErrorResult(fmt.Sprintf("cannot manage system skill %q", slug))
	}

	// Ownership check: only the skill owner can delete.
	// Same three-identity match as the patch flow above (#915).
	if !canManageSkill(ctx, t.skills, info) {
		return ErrorResult(fmt.Sprintf("cannot manage skill %q: you are not the owner", slug))
	}

	// Soft-delete on disk: move to .trash/<slug>.<unix-timestamp>
	skillsDir := t.tenantSkillsDir(ctx)
	trashDir := filepath.Join(skillsDir, ".trash")
	if err := os.MkdirAll(trashDir, 0755); err != nil {
		slog.Warn("skill_manage: failed to create .trash dir", "error", err)
	} else {
		timestamp := fmt.Sprintf("%d", time.Now().Unix())
		src := filepath.Join(skillsDir, slug)
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
	if err := t.skills.DeleteSkill(ctx, skillID); err != nil {
		return ErrorResult(fmt.Sprintf("failed to archive skill in database: %v", err))
	}

	slog.Info("skill_manage: deleted", "slug", slug, "id", info.ID)

	if t.loader != nil {
		t.loader.BumpVersion()
	}

	return NewResult(fmt.Sprintf("Skill %q deleted and removed from search.", slug))
}

// maxCopySize limits total companion file copy to 20MB (matching publish_skill).
const maxCopySize = 20 << 20

func parseManagedSkillFiles(raw any) ([]managedSkillFile, error) {
	if raw == nil {
		return nil, nil
	}
	files, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("files must be an object mapping relative paths to string content")
	}
	out := make([]managedSkillFile, 0, len(files))
	for rawPath, rawContent := range files {
		content, ok := rawContent.(string)
		if !ok {
			return nil, fmt.Errorf("files[%q] must be a string", rawPath)
		}
		cleanPath, err := validateManagedSkillFilePath(rawPath)
		if err != nil {
			return nil, err
		}
		if len(content) > maxManagedSkillFileSize {
			return nil, fmt.Errorf("file %q too large (%d bytes, max %d)", cleanPath, len(content), maxManagedSkillFileSize)
		}
		out = append(out, managedSkillFile{Path: cleanPath, Content: []byte(content)})
	}
	return out, nil
}

func validateManagedSkillFilePath(rawPath string) (string, error) {
	return skills.ValidateSkillTargetPath(rawPath, false)
}

func collectExistingManagedSkillCompanionFiles(srcDir string) ([]managedSkillFile, error) {
	var out []managedSkillFile
	var totalSize int64
	err := filepath.WalkDir(srcDir, func(filePath string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(srcDir, filePath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || rel == "SKILL.md" {
			return nil
		}
		cleanPath := path.Clean(rel)
		if cleanPath == "." || strings.HasPrefix(cleanPath, "../") || cleanPath == ".." || strings.HasPrefix(cleanPath, "/") {
			return fmt.Errorf("existing companion file %q escapes skill root", rel)
		}
		if skills.IsSystemArtifact(cleanPath) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		totalSize += info.Size()
		if totalSize > maxCopySize {
			return fmt.Errorf("companion files exceed %d bytes limit", maxCopySize)
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		out = append(out, managedSkillFile{Path: cleanPath, Content: data})
		return nil
	})
	return out, err
}

func overlayManagedSkillFiles(existing, payload []managedSkillFile) []managedSkillFile {
	byPath := make(map[string]managedSkillFile, len(existing)+len(payload))
	order := make([]string, 0, len(existing)+len(payload))
	for _, file := range existing {
		if _, exists := byPath[file.Path]; !exists {
			order = append(order, file.Path)
		}
		byPath[file.Path] = file
	}
	for _, file := range payload {
		if _, exists := byPath[file.Path]; !exists {
			order = append(order, file.Path)
		}
		byPath[file.Path] = file
	}
	out := make([]managedSkillFile, 0, len(order))
	for _, filePath := range order {
		out = append(out, byPath[filePath])
	}
	return out
}

func validateManagedSkillTotalSize(files []managedSkillFile) error {
	var total int64
	for _, file := range files {
		total += int64(len(file.Content))
		if total > maxCopySize {
			return fmt.Errorf("companion files exceed %d bytes limit", maxCopySize)
		}
	}
	return nil
}

func writeManagedSkillFiles(destDir string, files []managedSkillFile) error {
	for _, file := range files {
		destPath := filepath.Join(destDir, filepath.FromSlash(file.Path))
		cleanDest := filepath.Clean(destPath)
		if !strings.HasPrefix(cleanDest, destDir+string(filepath.Separator)) {
			return fmt.Errorf("file %q escapes skill root", file.Path)
		}
		if err := os.MkdirAll(filepath.Dir(cleanDest), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(cleanDest, file.Content, 0644); err != nil {
			return err
		}
	}
	return nil
}
