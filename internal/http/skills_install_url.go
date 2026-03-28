package http

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// githubRepoPattern matches GitHub repository URLs:
//
//	https://github.com/owner/repo
//	https://github.com/owner/repo/tree/branch
//	https://github.com/owner/repo/tree/branch/subdir
var githubRepoPattern = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+?)(?:\.git)?(?:/tree/([^/]+)(?:/(.+))?)?/?$`)

const (
	maxURLDownloadSize = 50 << 20 // 50 MB
	urlDownloadTimeout = 60 * time.Second
)

// skillPreviewItem is returned by the preview endpoint for each discovered skill.
type skillPreviewItem struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Dir         string `json:"dir"`
	HasScripts  bool   `json:"has_scripts"`
}

// handlePreviewURL downloads a repo/ZIP and returns the list of discovered skills
// without installing anything. The frontend uses this to let users pick which skills to install.
// Body: {"url": "https://github.com/owner/repo", "branch": "main"}
func (h *SkillsHandler) handlePreviewURL(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())

	var body struct {
		URL    string `json:"url"`
		Branch string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "url is required")})
		return
	}

	downloadURL, subdir := resolveDownloadURL(body.URL, body.Branch)
	if downloadURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "unsupported URL format — use a GitHub repo URL or direct .zip link")})
		return
	}

	zr, _, cleanup, errMsg, statusCode := downloadAndOpenZip(downloadURL)
	if errMsg != "" {
		writeJSON(w, statusCode, map[string]string{"error": errMsg})
		return
	}
	defer cleanup()

	wrapperPrefix := detectGitHubWrapper(zr)
	entries := findSkillEntries(zr, wrapperPrefix, subdir)
	if len(entries) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no SKILL.md found in repository"})
		return
	}

	var previews []skillPreviewItem
	for _, entry := range entries {
		content, err := readZipFile(entry.skillMDFile)
		if err != nil {
			continue
		}
		name, description, slug, _ := skills.ParseSkillFrontmatter(content)
		if name == "" && entry.skillDir != "" {
			name = filepath.Base(entry.skillDir)
		}
		if name == "" {
			continue
		}
		if slug == "" {
			slug = skills.Slugify(name)
		}

		// Check if skill has scripts/ directory
		hasScripts := false
		for _, f := range zr.File {
			entryName := strings.TrimPrefix(f.Name, "./")
			if strings.HasPrefix(entryName, entry.stripPrefix+"scripts/") {
				hasScripts = true
				break
			}
		}

		previews = append(previews, skillPreviewItem{
			Name:        name,
			Slug:        slug,
			Description: description,
			Dir:         entry.skillDir,
			HasScripts:  hasScripts,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"skills": previews, "total": len(previews)})
}

// handleInstallURL downloads a skill from a URL and installs selected skills.
// Body: {"url": "...", "branch": "...", "slugs": ["react-best-practices", "web-design"]}
// If slugs is empty/omitted, all discovered skills are installed.
func (h *SkillsHandler) handleInstallURL(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgUserIDHeader)})
		return
	}

	var body struct {
		URL    string   `json:"url"`
		Branch string   `json:"branch"`
		Slugs  []string `json:"slugs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "url is required")})
		return
	}

	downloadURL, subdir := resolveDownloadURL(body.URL, body.Branch)
	if downloadURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "unsupported URL format — use a GitHub repo URL or direct .zip link")})
		return
	}

	zr, zipSize, cleanup, errMsg, statusCode := downloadAndOpenZip(downloadURL)
	if errMsg != "" {
		writeJSON(w, statusCode, map[string]string{"error": errMsg})
		return
	}
	defer cleanup()

	wrapperPrefix := detectGitHubWrapper(zr)
	allEntries := findSkillEntries(zr, wrapperPrefix, subdir)
	if len(allEntries) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no SKILL.md found in repository"})
		return
	}

	// Filter by selected slugs if provided
	entries := allEntries
	if len(body.Slugs) > 0 {
		slugSet := make(map[string]bool, len(body.Slugs))
		for _, s := range body.Slugs {
			slugSet[s] = true
		}
		entries = nil
		for _, entry := range allEntries {
			content, err := readZipFile(entry.skillMDFile)
			if err != nil {
				continue
			}
			name, _, slug, _ := skills.ParseSkillFrontmatter(content)
			if name == "" && entry.skillDir != "" {
				name = filepath.Base(entry.skillDir)
			}
			if slug == "" && name != "" {
				slug = skills.Slugify(name)
			}
			if slugSet[slug] {
				entries = append(entries, entry)
			}
		}
		if len(entries) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "none of the selected skills were found"})
			return
		}
	}

	tenantSkillsBase := h.tenantSkillsDir(r)
	var installed []map[string]any
	var errors []string

	for _, entry := range entries {
		result, errMsg := h.installSkillFromZip(r, zr, entry, tenantSkillsBase, userID, zipSize)
		if errMsg != "" {
			errors = append(errors, fmt.Sprintf("%s: %s", entry.skillDir, errMsg))
			continue
		}
		installed = append(installed, result)
	}

	response := map[string]any{
		"installed": installed,
		"total":     len(installed),
	}
	if len(errors) > 0 {
		response["errors"] = errors
	}

	status := http.StatusCreated
	if len(installed) == 0 {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, response)
}

// downloadAndOpenZip downloads a URL into a temp file and opens it as a zip archive.
// Returns (reader, size, cleanup func, error message, http status code).
// Caller must call cleanup() when done.
func downloadAndOpenZip(downloadURL string) (*zip.ReadCloser, int64, func(), string, int) {
	client := &http.Client{Timeout: urlDownloadTimeout}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return nil, 0, func() {}, "failed to download: " + err.Error(), http.StatusBadGateway
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, func() {}, fmt.Sprintf("download returned HTTP %d", resp.StatusCode), http.StatusBadGateway
	}

	tmp, err := os.CreateTemp("", "skill-url-*.zip")
	if err != nil {
		return nil, 0, func() {}, "failed to create temp file", http.StatusInternalServerError
	}

	limited := io.LimitReader(resp.Body, maxURLDownloadSize+1)
	n, err := io.Copy(tmp, limited)
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, 0, func() {}, "download failed: " + err.Error(), http.StatusBadGateway
	}
	if n > maxURLDownloadSize {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, 0, func() {}, fmt.Sprintf("download exceeds %d MB limit", maxURLDownloadSize>>20), http.StatusBadRequest
	}
	tmp.Close()

	zr, err := zip.OpenReader(tmp.Name())
	if err != nil {
		os.Remove(tmp.Name())
		return nil, 0, func() {}, "downloaded file is not a valid ZIP", http.StatusBadRequest
	}

	tmpName := tmp.Name()
	cleanup := func() {
		zr.Close()
		os.Remove(tmpName)
	}
	return zr, n, cleanup, "", 0
}

// skillEntry represents a discovered skill within a ZIP archive.
type skillEntry struct {
	skillDir    string    // relative path to skill directory within ZIP (after wrapper strip)
	stripPrefix string    // full prefix to strip from ZIP entry names
	skillMDFile *zip.File // the SKILL.md zip entry
}

// resolveDownloadURL converts a URL into a downloadable ZIP URL.
// Returns (downloadURL, subdir) where subdir is an optional path filter within the repo.
func resolveDownloadURL(rawURL, branch string) (string, string) {
	if m := githubRepoPattern.FindStringSubmatch(rawURL); m != nil {
		owner, repo := m[1], m[2]
		ref := branch
		if ref == "" && m[3] != "" {
			ref = m[3]
		}
		if ref == "" {
			ref = "main"
		}
		subdir := m[4]
		return fmt.Sprintf("https://github.com/%s/%s/archive/refs/heads/%s.zip", owner, repo, ref), subdir
	}

	lower := strings.ToLower(rawURL)
	if strings.HasSuffix(lower, ".zip") {
		return rawURL, ""
	}

	return "", ""
}

// detectGitHubWrapper finds the common top-level directory that GitHub adds to archives.
func detectGitHubWrapper(zr *zip.ReadCloser) string {
	if len(zr.File) == 0 {
		return ""
	}
	var candidate string
	for _, f := range zr.File {
		name := strings.TrimPrefix(f.Name, "./")
		if !f.FileInfo().IsDir() {
			continue
		}
		name = strings.TrimSuffix(name, "/")
		if !strings.Contains(name, "/") {
			if candidate == "" || len(name) < len(candidate) {
				candidate = name
			}
		}
	}
	if candidate == "" {
		return ""
	}
	prefix := candidate + "/"
	for _, f := range zr.File {
		name := strings.TrimPrefix(f.Name, "./")
		if !strings.HasPrefix(name, prefix) && name != candidate+"/" {
			return ""
		}
	}
	return prefix
}

// findSkillEntries discovers all SKILL.md files in the ZIP, returning one entry per skill.
func findSkillEntries(zr *zip.ReadCloser, wrapperPrefix, subdir string) []skillEntry {
	var entries []skillEntry
	seen := map[string]bool{}

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.TrimPrefix(f.Name, "./")

		inner := name
		if wrapperPrefix != "" {
			if !strings.HasPrefix(inner, wrapperPrefix) {
				continue
			}
			inner = strings.TrimPrefix(inner, wrapperPrefix)
		}

		if subdir != "" {
			subdirPrefix := subdir + "/"
			if !strings.HasPrefix(inner, subdirPrefix) && inner != subdir+"/SKILL.md" {
				continue
			}
		}

		base := filepath.Base(inner)
		if base != "SKILL.md" {
			continue
		}

		skillDir := filepath.Dir(inner)
		if skillDir == "." {
			skillDir = ""
		}

		if seen[skillDir] {
			continue
		}
		seen[skillDir] = true

		fullPrefix := wrapperPrefix
		if skillDir != "" {
			fullPrefix += skillDir + "/"
		}

		entries = append(entries, skillEntry{
			skillDir:    skillDir,
			stripPrefix: fullPrefix,
			skillMDFile: f,
		})
	}

	return entries
}

// installSkillFromZip extracts and installs a single skill from the ZIP archive.
func (h *SkillsHandler) installSkillFromZip(
	r *http.Request,
	zr *zip.ReadCloser,
	entry skillEntry,
	tenantSkillsBase string,
	userID string,
	totalSize int64,
) (map[string]any, string) {
	locale := store.LocaleFromContext(r.Context())

	skillContent, err := readZipFile(entry.skillMDFile)
	if err != nil {
		return nil, "failed to read SKILL.md"
	}
	if strings.TrimSpace(skillContent) == "" {
		return nil, "SKILL.md is empty"
	}

	name, description, slug, frontmatter := skills.ParseSkillFrontmatter(skillContent)
	if name == "" {
		if entry.skillDir != "" {
			name = filepath.Base(entry.skillDir)
		} else {
			return nil, "name missing in SKILL.md frontmatter"
		}
	}
	if slug == "" {
		slug = skills.Slugify(name)
	}
	if !skills.SlugRegexp.MatchString(slug) {
		return nil, i18n.T(locale, i18n.MsgInvalidSlug, "slug")
	}

	if h.skills.IsSystemSkill(slug) {
		return nil, "slug conflicts with a system skill"
	}

	version := h.skills.GetNextVersion(r.Context(), slug)
	destDir := filepath.Join(tenantSkillsBase, slug, fmt.Sprintf("%d", version))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, "failed to create skill directory"
	}

	hasher := sha256.New()

	for _, f := range zr.File {
		if f.FileInfo().IsDir() || f.Mode()&os.ModeSymlink != 0 {
			continue
		}
		entryName := strings.TrimPrefix(f.Name, "./")
		if !strings.HasPrefix(entryName, entry.stripPrefix) {
			continue
		}
		relName := strings.TrimPrefix(entryName, entry.stripPrefix)
		if relName == "" {
			continue
		}
		if skills.IsSystemArtifact(relName) {
			continue
		}
		cleanName := filepath.Clean(relName)
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
		hasher.Write([]byte(data))
		os.WriteFile(destPath, []byte(data), 0644)
	}

	fileHash := fmt.Sprintf("%x", hasher.Sum(nil))

	desc := description
	skill := store.SkillCreateParams{
		Name:        name,
		Slug:        slug,
		Description: &desc,
		OwnerID:     userID,
		Visibility:  "internal",
		Version:     version,
		FilePath:    destDir,
		FileSize:    totalSize,
		FileHash:    &fileHash,
		Frontmatter: frontmatter,
	}

	id, err := h.skills.CreateSkillManaged(r.Context(), skill)
	if err != nil {
		return nil, "failed to create skill: " + err.Error()
	}

	h.skills.BumpVersion()
	emitAudit(h.msgBus, r, "skill.installed", "skill", slug)
	slog.Info("skill installed from URL", "id", id, "slug", slug, "version", version)

	result := map[string]any{
		"id":      id,
		"slug":    slug,
		"version": version,
		"name":    name,
	}

	manifest := skills.ScanSkillDeps(destDir)
	if manifest != nil && !manifest.IsEmpty() {
		ok, missing := skills.CheckSkillDeps(manifest)
		if !ok {
			_ = h.skills.UpdateSkill(r.Context(), id, map[string]any{"status": "archived"})
			result["deps_warning"] = "missing dependencies: " + skills.FormatMissing(missing)
		}
	}

	return result, ""
}
