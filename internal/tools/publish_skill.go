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

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

const maxSkillDirSize = 20 << 20 // 20 MB

// PublishSkillTool registers a skill directory in the database,
// making it discoverable and grantable to agents.
type PublishSkillTool struct {
	skills *pg.PGSkillStore
	base   string         // skills-store/ directory
	loader *skills.Loader // cache invalidation
}

func NewPublishSkillTool(skills *pg.PGSkillStore, baseDir string, loader *skills.Loader) *PublishSkillTool {
	return &PublishSkillTool{skills: skills, base: baseDir, loader: loader}
}

func (t *PublishSkillTool) Name() string { return "publish_skill" }

func (t *PublishSkillTool) Description() string {
	return "Register a skill directory in the system database so it becomes discoverable, searchable, and grantable to agents. " +
		"Use the skill-creator skill to create the skill first, then call this tool to publish it. " +
		"The directory must contain a SKILL.md file with name in its YAML frontmatter. " +
		"The skill is auto-granted to the calling agent."
}

func (t *PublishSkillTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to skill directory containing SKILL.md (absolute or relative to workspace)",
			},
		},
		"required": []string{"path"},
	}
}

func (t *PublishSkillTool) Execute(ctx context.Context, args map[string]any) *Result {
	rawPath, _ := args["path"].(string)
	if rawPath == "" {
		return ErrorResult("path is required")
	}

	// Resolve path: absolute or relative to workspace
	dir := rawPath
	if !filepath.IsAbs(dir) {
		ws := ToolWorkspaceFromCtx(ctx)
		if ws == "" {
			return ErrorResult("relative path provided but no workspace available")
		}
		dir = filepath.Join(ws, dir)
	}
	dir = filepath.Clean(dir)

	// Validate SKILL.md exists
	skillPath := filepath.Join(dir, "SKILL.md")
	content, err := os.ReadFile(skillPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("cannot read SKILL.md: %v", err))
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return ErrorResult("SKILL.md is empty")
	}

	// Parse frontmatter
	name, description, slug, frontmatter := skills.ParseSkillFrontmatter(string(content))
	if name == "" {
		return ErrorResult("SKILL.md frontmatter must contain 'name' field")
	}
	if slug == "" {
		slug = skills.Slugify(name)
	}
	if !skills.SlugRegexp.MatchString(slug) {
		return ErrorResult(fmt.Sprintf("invalid slug %q: must be lowercase alphanumeric with hyphens", slug))
	}

	// Check system skill conflict
	if t.skills.IsSystemSkill(slug) {
		return ErrorResult(fmt.Sprintf("slug %q conflicts with a system skill", slug))
	}

	// Compute hash + size
	hasher := sha256.New()
	hasher.Write(content)
	fileHash := fmt.Sprintf("%x", hasher.Sum(nil))
	fileSize, err := dirSize(dir)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to calculate directory size: %v", err))
	}
	if fileSize > maxSkillDirSize {
		return ErrorResult(fmt.Sprintf("skill directory exceeds size limit (%d MB)", maxSkillDirSize>>20))
	}

	// Version + destination
	version := t.skills.GetNextVersion(slug)
	destDir := filepath.Join(t.base, slug, fmt.Sprintf("%d", version))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create destination: %v", err))
	}

	// Copy directory
	if err := copySkillDir(dir, destDir); err != nil {
		return ErrorResult(fmt.Sprintf("failed to copy skill files: %v", err))
	}

	// Insert into DB
	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		userID = "system" // fallback for agent-only contexts
	}
	desc := description
	params := pg.SkillCreateParams{
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
	}

	id, err := t.skills.CreateSkillManaged(ctx, params)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to register skill: %v", err))
	}

	slog.Info("skill published", "id", id, "slug", slug, "version", version, "owner", userID)

	// Auto-grant to calling agent
	agentID := store.AgentIDFromContext(ctx)
	if agentID != uuid.Nil {
		if err := t.skills.GrantToAgent(ctx, id, agentID, version, userID); err != nil {
			slog.Warn("publish_skill: auto-grant failed", "error", err)
		}
	}

	// Bump loader cache
	if t.loader != nil {
		t.loader.BumpVersion()
	}

	// Scan deps
	var depsWarning string
	manifest := skills.ScanSkillDeps(destDir)
	if manifest != nil && !manifest.IsEmpty() {
		ok, missing := skills.CheckSkillDeps(manifest)
		if !ok {
			_ = t.skills.StoreMissingDeps(id, missing)
			depsWarning = skills.FormatMissing(missing)
		}
	}

	// Build result
	result := fmt.Sprintf("Skill %q published successfully.\n- ID: %s\n- Slug: %s\n- Version: %d", name, id, slug, version)
	if agentID != uuid.Nil {
		result += "\n- Granted to current agent"
	}
	if depsWarning != "" {
		result += fmt.Sprintf("\n\n⚠ Missing dependencies: %s\nTry installing them with exec (e.g. pip install <pkg> or npm install <pkg>). If you cannot install system binaries, inform the user.", depsWarning)
	}

	return NewResult(result)
}

// copySkillDir recursively copies src to dst, skipping symlinks and system artifacts.
func copySkillDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		// Security: skip path traversal
		if strings.Contains(rel, "..") {
			return filepath.SkipDir
		}

		// Skip symlinks
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		// Skip system artifacts
		if skills.IsSystemArtifact(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		destPath := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		// Copy file
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.Create(destPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

// dirSize returns total size of all files in a directory.
func dirSize(path string) (int64, error) {
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
