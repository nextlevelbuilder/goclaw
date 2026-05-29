package http

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const (
	uploadDepsInstallTimeout = 5 * time.Minute
	maxUploadManagerAgentIDs = 100
)

var (
	installUploadedSkillDeps = skills.InstallDeps
	checkUploadedSkillDeps   = skills.CheckSkillDeps
)

// handleUpload processes a ZIP file upload containing a skill (must have SKILL.md at root).
func (h *SkillsHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgUserIDHeader)})
		return
	}

	preParseLimitMB := h.resolvePreParseUploadLimitMB(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, skillUploadBodyBytes(preParseLimitMB))

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "file is required: "+err.Error())})
		return
	}
	defer file.Close()

	managerAgentIDs, err := parseUploadManagerAgentIDs(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, err.Error())})
		return
	}

	// Save to temp file for zip processing
	tmp, err := os.CreateTemp("", "skill-upload-*.zip")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to create temp file")})
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	size, err := io.Copy(tmp, file)
	if err != nil {
		tmp.Close()
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, skillUploadTooLargeMessage(maxBytesErr.Limit, preParseLimitMB))})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to save upload")})
		return
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to finalize upload")})
		return
	}
	if err := tmp.Close(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to finalize upload")})
		return
	}

	// Open as zip
	zr, err := zip.OpenReader(tmpName)
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

	// Security guard: scan for malicious content BEFORE any disk/DB write
	violations, safe := skills.GuardSkillContent(skillContent)
	if !safe {
		slog.Warn("security.skills.upload_rejected",
			"user_id", userID,
			"violations", len(violations),
			"first_rule", violations[0].Reason)
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":      i18n.T(locale, i18n.MsgInvalidRequest, "skill content failed security scan"),
			"violations": skills.FormatGuardViolations(violations),
		})
		return
	}

	name, description, slug, frontmatter := skills.ParseSkillFrontmatter(skillContent)
	uploadLimitMB := h.resolveSkillUploadLimitMB(r.Context(), frontmatter)
	if size > skillUploadLimitBytes(uploadLimitMB) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, skillUploadTooLargeMessage(size, uploadLimitMB))})
		return
	}
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

	// Compute content hash of SKILL.md for idempotency check.
	// Using SKILL.md content (not ZIP hash) so content-identical uploads are deduplicated
	// even when packaged into different ZIP files (e.g. multi-skill split upload).
	skillHash := fmt.Sprintf("%x", sha256.Sum256([]byte(skillContent)))

	tenantSkillsBase := h.tenantSkillsDir(r)
	uploadLock := h.skillUploadLock(filepath.Join(tenantSkillsBase, slug))
	uploadLock.Lock()
	defer uploadLock.Unlock()

	// Check whether content is unchanged from the current stored version.
	// Performed under lock to avoid TOCTOU race where concurrent uploads
	// could both pass the hash check before either creates a new version.
	existingHash, existingVer, skillExists := h.skills.GetSkillHashBySlug(r.Context(), slug)
	if skillExists && existingHash != "" && existingHash == skillHash {
		response := map[string]any{
			"slug":    slug,
			"version": existingVer,
			"name":    name,
			"status":  "unchanged",
		}
		if len(managerAgentIDs) > 0 {
			if existing, ok := h.skills.GetSkill(r.Context(), slug); ok && existing.ID != "" {
				if existingID, err := uuid.Parse(existing.ID); err == nil {
					grantErrors := h.grantUploadedSkillManagers(r.Context(), existingID, managerAgentIDs, existingVer, userID)
					if len(grantErrors) > 0 {
						response["grant_errors"] = grantErrors
					}
				}
			}
		}
		writeJSON(w, http.StatusOK, response)
		return
	}

	// Determine version (always increment — includes archived skills so re-upload gets v2+)
	version := h.skills.GetNextVersion(r.Context(), slug)

	// Extract to filesystem: tenant-scoped skills-store/slug/version/
	destDir := filepath.Join(tenantSkillsBase, slug, fmt.Sprintf("%d", version))
	if err := os.MkdirAll(destDir, 0755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to create skill directory")})
		return
	}

	wroteSkillMD := false
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
			os.RemoveAll(destDir)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to create skill file directory")})
			return
		}
		if err := copyZipFileToPath(f, destPath); err != nil {
			os.RemoveAll(destDir)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "failed to read ZIP entry")})
			return
		}
		if cleanName == "SKILL.md" {
			wroteSkillMD = true
		}
	}
	if !wroteSkillMD {
		os.RemoveAll(destDir)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "ZIP must contain a writable SKILL.md")})
		return
	}

	// Save metadata to DB
	desc := description
	skill := store.SkillCreateParams{
		Name:        name,
		Slug:        slug,
		Description: &desc,
		OwnerID:     userID,
		Visibility:  "internal",
		Version:     version,
		FilePath:    destDir,
		FileSize:    size,
		FileHash:    &skillHash, // SKILL.md content hash for idempotency (not ZIP hash)
		Frontmatter: frontmatter,
	}

	// Scan and check dependencies
	// is_new is true only when no previous version of this skill existed (first upload).
	isNew := !skillExists
	response := map[string]any{"slug": slug, "version": version, "name": name, "status": "active", "is_new": isNew}
	depState := uploadSkillDepState{}
	depsCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), uploadDepsInstallTimeout)
	defer cancel()

	manifest := skills.ScanSkillDeps(destDir)
	if manifest != nil && !manifest.IsEmpty() {
		if ok, missing := checkUploadedSkillDeps(manifest); !ok {
			depState = h.reconcileUploadedSkillDeps(
				depsCtx,
				slug,
				manifest,
				missing,
				canAutoInstallUploadedSkillDeps(r.Context()),
			)
			skill.Status = depState.status
			skill.MissingDeps = depState.missing
			maps.Copy(response, depState.response)
		}
	}

	// Use depsCtx (non-cancellable) so the DB write completes even if the
	// client disconnects during the dep-install window.
	id, err := h.skills.CreateSkillManaged(depsCtx, skill)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToCreate, "skill", err.Error())})
		return
	}
	response["id"] = id
	if len(managerAgentIDs) > 0 {
		grantErrors := h.grantUploadedSkillManagers(depsCtx, id, managerAgentIDs, version, userID)
		if len(grantErrors) > 0 {
			response["grant_errors"] = grantErrors
		}
	}

	h.skills.BumpVersion()
	h.emitCacheInvalidate(bus.CacheKindSkills, id.String(), uuid.Nil)
	emitAudit(h.msgBus, r, "skill.uploaded", "skill", slug)
	slog.Info("skill uploaded", "id", id, "slug", slug, "version", version, "size", header.Size, "status", skill.Status)
	depState.emit(h, slug)

	writeJSON(w, http.StatusCreated, response)
}

func parseUploadManagerAgentIDs(r *http.Request) ([]uuid.UUID, error) {
	raw := strings.TrimSpace(r.FormValue("manager_agent_ids"))
	if raw == "" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, fmt.Errorf("manager_agent_ids must be a JSON array")
	}
	if len(values) > maxUploadManagerAgentIDs {
		return nil, fmt.Errorf("manager_agent_ids exceeds limit of %d", maxUploadManagerAgentIDs)
	}
	out := make([]uuid.UUID, 0, len(values))
	seen := make(map[uuid.UUID]bool, len(values))
	for _, value := range values {
		id, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil || id == uuid.Nil {
			return nil, fmt.Errorf("invalid manager_agent_ids value")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

func (h *SkillsHandler) grantUploadedSkillManagers(ctx context.Context, skillID uuid.UUID, agentIDs []uuid.UUID, version int, userID string) []string {
	if version <= 0 {
		version = 1
	}
	var errs []string
	for _, agentID := range agentIDs {
		if err := h.skills.GrantToAgent(ctx, skillID, agentID, version, userID, true); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", agentID, err))
		}
	}
	return errs
}

func canAutoInstallUploadedSkillDeps(ctx context.Context) bool {
	return store.IsOwnerRole(ctx) || store.TenantIDFromContext(ctx) == store.MasterTenantID
}

func uploadDepErrors(result *skills.InstallResult, installErr error) []string {
	var errors []string
	if installErr != nil {
		errors = append(errors, installErr.Error())
	}
	if result != nil && len(result.Errors) > 0 {
		errors = append(errors, result.Errors...)
	}
	return errors
}

func (h *SkillsHandler) emitUploadDepInstalling(slug string, count int) {
	if h.msgBus == nil {
		return
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventSkillDepsInstalling,
		Payload: map[string]any{"skill": slug, "count": count},
	})
}

func (h *SkillsHandler) emitUploadDepChecked(slug, status string, missing []string) {
	if h.msgBus == nil {
		return
	}
	payload := map[string]any{
		"slug":   slug,
		"status": status,
	}
	if len(missing) > 0 {
		payload["missing"] = missing
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventSkillDepsChecked,
		Payload: payload,
	})
}

func (h *SkillsHandler) emitUploadDepInstalled(slug string, result *skills.InstallResult) {
	if h.msgBus == nil {
		return
	}
	payload := map[string]any{"skill": slug}
	if result != nil {
		payload["result"] = result
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventSkillDepsInstalled,
		Payload: payload,
	})
}

func (h *SkillsHandler) reconcileUploadedSkillDeps(
	ctx context.Context,
	slug string,
	manifest *skills.SkillManifest,
	missing []string,
	allowAutoInstall bool,
) uploadSkillDepState {
	response := map[string]any{}
	finalStatus := "archived"
	finalMissing := append([]string(nil), missing...)
	state := uploadSkillDepState{installCount: len(missing), checked: true}
	var installResult *skills.InstallResult
	var installErr error

	if allowAutoInstall {
		installResult, installErr = installUploadedSkillDeps(ctx, manifest, missing)
		if ok, checkedMissing := checkUploadedSkillDeps(manifest); ok {
			finalStatus = "active"
			finalMissing = nil
			response["deps_installed"] = true
			slog.Info("skill deps auto-installed", "skill", slug, "installed", missing)
		} else {
			finalMissing = checkedMissing
			slog.Warn("skill deps auto-install failed", "skill", slug, "missing", finalMissing, "errors", uploadDepErrors(installResult, installErr))
		}
		state.installResult = installResult
	} else {
		response["deps_warning"] = "missing dependencies: " + skills.FormatMissing(finalMissing)
		state.installCount = 0
	}

	if finalStatus == "archived" {
		if _, exists := response["deps_warning"]; !exists {
			response["deps_warning"] = "auto-install failed for: " + skills.FormatMissing(finalMissing)
		}
		response["missing_deps"] = finalMissing
		if errors := uploadDepErrors(installResult, installErr); len(errors) > 0 {
			response["deps_errors"] = errors
		}
	}
	response["status"] = finalStatus
	state.status = finalStatus
	state.missing = finalMissing
	state.response = response
	return state
}

type uploadSkillDepState struct {
	status        string
	missing       []string
	response      map[string]any
	installCount  int
	installResult *skills.InstallResult
	checked       bool
}

func (s uploadSkillDepState) emit(h *SkillsHandler, slug string) {
	if !s.checked {
		return
	}
	if s.installCount > 0 {
		h.emitUploadDepInstalling(slug, s.installCount)
		h.emitUploadDepInstalled(slug, s.installResult)
	}
	h.emitUploadDepChecked(slug, s.status, s.missing)
}
