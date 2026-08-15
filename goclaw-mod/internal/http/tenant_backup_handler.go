package http

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/backup"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TenantBackupHandler handles tenant-scoped backup/restore endpoints.
// Permission: system owner OR tenant admin.
type TenantBackupHandler struct {
	db      *sql.DB
	cfg     *config.Config
	tenants store.TenantStore
	isOwner func(string) bool
	version string
}

// NewTenantBackupHandler creates a handler for tenant backup/restore endpoints.
func NewTenantBackupHandler(db *sql.DB, cfg *config.Config, tenants store.TenantStore, version string, isOwner func(string) bool) *TenantBackupHandler {
	return &TenantBackupHandler{
		db:      db,
		cfg:     cfg,
		tenants: tenants,
		isOwner: isOwner,
		version: version,
	}
}

// RegisterRoutes registers tenant backup routes on the given mux.
func (h *TenantBackupHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/tenant/backup",
		requireAuth(permissions.RoleAdmin, h.handleBackup))
	mux.HandleFunc("GET /v1/tenant/backup/preflight",
		requireAuth(permissions.RoleAdmin, h.handlePreflight))
	mux.HandleFunc("GET /v1/tenant/backup/download/{token}",
		requireAuth(permissions.RoleAdmin, h.handleDownload))
	mux.HandleFunc("POST /v1/tenant/restore",
		requireAuth(permissions.RoleAdmin, h.handleRestore))
}

// handlePreflight returns basic readiness info for a tenant backup.
func (h *TenantBackupHandler) handlePreflight(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": tenantID.String(),
		"ready":     true,
	})
}

// handleBackup runs a tenant backup and streams progress via SSE.
// Query: tenant_id=<uuid> or tenant_slug=<slug>
func (h *TenantBackupHandler) handleBackup(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := extractLocale(r)

	tenantID, tenantSlug, ok := h.resolveTenant(w, r)
	if !ok {
		return
	}

	if !h.authorised(r, userID, tenantID) {
		slog.Warn("security.tenant_backup_denied", "user_id", userID, "tenant_id", tenantID)
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized,
			i18n.T(locale, i18n.MsgNoAccess, "tenant backup"))
		return
	}

	flusher := initSSE(w)
	if flusher == nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "streaming not supported")
		return
	}

	tmpFile, err := os.CreateTemp("", "goclaw-tenant-backup-*.tar.gz")
	if err != nil {
		sendSSE(w, flusher, "error", ProgressEvent{Phase: "init", Status: "error", Detail: "failed to create temp file"})
		return
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	ts := time.Now().UTC().Format("20060102-150405")
	fileName := fmt.Sprintf("tenant-backup-%s-%s.tar.gz", tenantSlug, ts)

	dataDir := config.TenantDataDir(h.cfg.ResolvedDataDir(), tenantID, tenantSlug)
	wsDir := config.TenantWorkspace(h.cfg.WorkspacePath(), tenantID, tenantSlug)

	opts := backup.TenantBackupOptions{
		DB:            h.db,
		TenantID:      tenantID,
		TenantSlug:    tenantSlug,
		DataDir:       dataDir,
		WorkspacePath: wsDir,
		OutputPath:    tmpPath,
		CreatedBy:     userID,
		ProgressFn: func(phase, detail string) {
			sendSSE(w, flusher, "progress", ProgressEvent{Phase: phase, Status: "running", Detail: detail})
		},
	}

	manifest, runErr := backup.TenantBackup(r.Context(), opts)
	if runErr != nil {
		slog.Error("tenant.backup.sse", "error", runErr, "tenant", tenantID)
		sendSSE(w, flusher, "error", ProgressEvent{Phase: "backup", Status: "error", Detail: runErr.Error()})
		os.Remove(tmpPath)
		return
	}

	token := storeExportToken("tenant:"+tenantID.String(), userID, tmpPath, fileName)
	sendSSE(w, flusher, "complete", map[string]any{
		"download_url":   "/v1/tenant/backup/download/" + token,
		"file_name":      fileName,
		"tenant_id":      tenantID.String(),
		"schema_version": manifest.SchemaVersion,
		"table_counts":   manifest.TableCounts,
	})
}

// handleDownload serves a previously-prepared tenant backup archive by token.
func (h *TenantBackupHandler) handleDownload(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := extractLocale(r)

	token := r.PathValue("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgRequired, "token"))
		return
	}

	entry, ok := lookupExportToken(token)
	if !ok {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound,
			i18n.T(locale, i18n.MsgNotFound, "backup token", token))
		return
	}

	if entry.userID != userID && !h.isOwnerUser(userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized,
			i18n.T(locale, i18n.MsgNoAccess, "backup download"))
		return
	}

	f, err := os.Open(entry.filePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError))
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, entry.fileName))
	http.ServeContent(w, r, entry.fileName, time.Time{}, f)
}

