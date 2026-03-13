package http

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// handleUpload processes a ZIP file upload containing a skill (must have SKILL.md at root).
func (h *SkillsHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgUserIDHeader)})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSkillUploadSize)

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "file is required: "+err.Error())})
		return
	}
	defer file.Close()

	// Save to temp file for zip processing
	tmp, err := os.CreateTemp("", "skill-upload-*.zip")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to create temp file")})
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hasher), file)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to save upload")})
		return
	}
	fileHash := fmt.Sprintf("%x", hasher.Sum(nil))

	// Open as zip
	zr, err := zip.OpenReader(tmp.Name())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "invalid ZIP file")})
		return
	}
	defer zr.Close()

	// Validate: must have SKILL.md at root or inside a single top-level directory.
	// Many ZIP tools wrap contents in a folder (e.g. "my-skill/SKILL.md").
	var skillMD *zip.File
	var stripPrefix string
	for _, f := range zr.File {
		name := strings.TrimPrefix(f.Name, "./")
		if name == "SKILL.md" {
			skillMD = f
			stripPrefix = ""
			break
		}
		// Allow one level of directory nesting: "dirname/SKILL.md"
		parts := strings.SplitN(name, "/", 3)
		if len(parts) == 2 && parts[1] == "SKILL.md" && !f.FileInfo().IsDir() {
			skillMD = f
			stripPrefix = parts[0] + "/"
			break
		}
	}
	if skillMD == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "ZIP must contain SKILL.md at root (or inside a single top-level directory)")})
		return
	}

	// Read and parse SKILL.md frontmatter
	skillContent, err := readZipFile(skillMD)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "failed to read SKILL.md")})
		return
	}
	if strings.TrimSpace(skillContent) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "SKILL.md is empty")})
		return
	}

	name, description, slug, frontmatter := skills.ParseSkillFrontmatter(skillContent)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "name in SKILL.md frontmatter")})
		return
	}
	if slug == "" {
		slug = skills.Slugify(name)
	}
	if !skills.SlugRegexp.MatchString(slug) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "slug")})
		return
	}

	// Check slug conflict with system skill
	if h.skills.IsSystemSkill(slug) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "slug conflicts with a system skill")})
		return
	}

	// Determine version (always increment — includes archived skills so re-upload gets v2+)
	version := h.skills.GetNextVersion(slug)

	// Extract to filesystem: baseDir/slug/version/
	destDir := filepath.Join(h.baseDir, slug, fmt.Sprintf("%d", version))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to create skill directory")})
		return
	}

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		// Skip symlinks in ZIP — prevent directory escape attacks
		if f.Mode()&os.ModeSymlink != 0 {
			continue
		}
		// Strip wrapper directory prefix if ZIP had one
		entryName := strings.TrimPrefix(f.Name, "./")
		if stripPrefix != "" {
			entryName = strings.TrimPrefix(entryName, stripPrefix)
			if entryName == "" {
				continue
			}
		}
		// Skip macOS/system artifacts
		if skills.IsSystemArtifact(entryName) {
			continue
		}
		// Security: prevent path traversal
		name := filepath.Clean(entryName)
		if strings.Contains(name, "..") {
			continue
		}
		destPath := filepath.Join(destDir, name)
		if !strings.HasPrefix(destPath, destDir+string(filepath.Separator)) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			continue
		}
		data, err := readZipFile(f)
		if err != nil {
			continue
		}
		os.WriteFile(destPath, []byte(data), 0644)
	}

	// Save metadata to DB
	desc := description
	skill := pg.SkillCreateParams{
		Name:        name,
		Slug:        slug,
		Description: &desc,
		OwnerID:     userID,
		Visibility:  "internal",
		Version:     version,
		FilePath:    destDir,
		FileSize:    size,
		FileHash:    &fileHash,
		Frontmatter: frontmatter,
	}

	id, err := h.skills.CreateSkillManaged(r.Context(), skill)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToCreate, "skill", err.Error())})
		return
	}

	h.skills.BumpVersion()
	emitAudit(h.msgBus, r, "skill.uploaded", "skill", slug)
	slog.Info("skill uploaded", "id", id, "slug", slug, "version", version, "size", header.Size)

	// Scan and check dependencies
	response := map[string]interface{}{
		"id":      id,
		"slug":    slug,
		"version": version,
		"name":    name,
	}
	manifest := skills.ScanSkillDeps(destDir)
	if manifest != nil && !manifest.IsEmpty() {
		ok, missing := skills.CheckSkillDeps(manifest)
		if !ok {
			// Set skill to archived due to missing deps
			_ = h.skills.UpdateSkill(id, map[string]any{"status": "archived"})
			response["deps_warning"] = "missing dependencies: " + skills.FormatMissing(missing)
		}
	}

	writeJSON(w, http.StatusCreated, response)
}
