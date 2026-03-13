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
)

// handleUpload processes a ZIP file upload containing a managed tool (must have TOOL.md at root).
func (h *ManagedToolsHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgUserIDHeader)})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxManagedToolUploadSize)

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "file is required: "+err.Error())})
		return
	}
	defer file.Close()

	// Save to temp file for zip processing
	tmp, err := os.CreateTemp("", "managed-tool-upload-*.zip")
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

	// Validate: must have TOOL.md at root or inside a single top-level directory.
	var toolMD *zip.File
	var stripPrefix string
	for _, f := range zr.File {
		name := strings.TrimPrefix(f.Name, "./")
		if name == "TOOL.md" {
			toolMD = f
			stripPrefix = ""
			break
		}
		// Allow one level of directory nesting: "dirname/TOOL.md"
		parts := strings.SplitN(name, "/", 3)
		if len(parts) == 2 && parts[1] == "TOOL.md" && !f.FileInfo().IsDir() {
			toolMD = f
			stripPrefix = parts[0] + "/"
			break
		}
	}
	if toolMD == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "ZIP must contain TOOL.md at root (or inside a single top-level directory)")})
		return
	}

	// Read and parse TOOL.md frontmatter
	toolContent, err := readZipFile(toolMD)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "failed to read TOOL.md")})
		return
	}
	if strings.TrimSpace(toolContent) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "TOOL.md is empty")})
		return
	}

	name, description, slug, frontmatter := skills.ParseSkillFrontmatter(toolContent)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "name in TOOL.md frontmatter")})
		return
	}
	if slug == "" {
		slug = skills.Slugify(name)
	}
	if !skills.SlugRegexp.MatchString(slug) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "slug")})
		return
	}

	// Extract optional runtime and entry_point from frontmatter
	var runtime, entryPoint *string
	if v, ok := frontmatter["runtime"]; ok && v != "" {
		runtime = &v
	}
	if v, ok := frontmatter["entry_point"]; ok && v != "" {
		entryPoint = &v
	}

	// Determine version (always increment)
	version := h.tools.GetNextVersion(slug)

	// Extract to filesystem: baseDir/slug/version/
	destDir := filepath.Join(h.baseDir, slug, fmt.Sprintf("%d", version))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to create tool directory")})
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
		cleanName := filepath.Clean(entryName)
		if strings.Contains(cleanName, "..") {
			continue
		}
		destPath := filepath.Join(destDir, cleanName)
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
	toolParams := store.ManagedToolCreateParams{
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
		Runtime:     runtime,
		EntryPoint:  entryPoint,
	}

	id, err := h.tools.CreateManagedTool(r.Context(), toolParams)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToCreate, "managed tool", err.Error())})
		return
	}

	h.tools.BumpVersion()
	emitAudit(h.msgBus, r, "managed-tool.uploaded", "managed_tool", slug)
	slog.Info("managed tool uploaded", "id", id, "slug", slug, "version", version, "size", header.Size)

	response := map[string]interface{}{
		"id":      id,
		"slug":    slug,
		"version": version,
		"name":    name,
	}

	writeJSON(w, http.StatusCreated, response)
}
