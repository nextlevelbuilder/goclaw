package http

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/nextlevelbuilder/goclaw/internal/backup"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// handleRestore accepts a multipart tar.gz upload and restores a tenant via SSE.
// Query params: tenant_id, tenant_slug, mode (upsert|replace|new), dry_run
// Only system owners may restore (cross-tenant operation).
func (h *TenantBackupHandler) handleRestore(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := extractLocale(r)

	if !h.isOwnerUser(userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized,
			i18n.T(locale, i18n.MsgNoAccess, "tenant restore"))
		return
	}

	q := r.URL.Query()
	mode := q.Get("mode")
	if mode == "" {
		mode = "upsert"
	}
	dryRun := q.Get("dry_run") == "true" || q.Get("dry_run") == "1"

	// For "new" mode the tenant does not need to exist yet.
	tenantID, tenantSlug, ok := h.resolveTenant(w, r)
	if !ok && mode != "new" {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRestoreSize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgFileTooLarge))
		return
	}

	file, _, err := r.FormFile("archive")
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgMissingFileField))
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "goclaw-tenant-restore-*.tar.gz")
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError))
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	written, copyErr := copyWithLimit(tmp, file, maxRestoreSize)
	tmp.Close()
	if copyErr != nil || written == 0 {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgFileTooLarge))
		return
	}

	flusher := initSSE(w)
	if flusher == nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "streaming not supported")
		return
	}

	dataDir := config.TenantDataDir(h.cfg.ResolvedDataDir(), tenantID, tenantSlug)
	wsDir := config.TenantWorkspace(h.cfg.WorkspacePath(), tenantID, tenantSlug)

	opts := backup.TenantRestoreOptions{
		DB:            h.db,
		ArchivePath:   tmpPath,
		TenantID:      tenantID,
		TenantSlug:    tenantSlug,
		DataDir:       dataDir,
		WorkspacePath: wsDir,
		Mode:          mode,
		Force:         true, // authenticated as owner above
		DryRun:        dryRun,
		ProgressFn: func(phase, detail string) {
			sendSSE(w, flusher, "progress", ProgressEvent{Phase: phase, Status: "running", Detail: detail})
		},
	}

	result, runErr := backup.TenantRestore(r.Context(), opts)
	if runErr != nil {
		slog.Error("tenant.restore.sse", "error", runErr, "user", userID)
		sendSSE(w, flusher, "error", ProgressEvent{Phase: "restore", Status: "error", Detail: runErr.Error()})
		return
	}

	sendSSE(w, flusher, "complete", map[string]any{
		"tenant_id":       result.TenantID,
		"tables_restored": result.TablesRestored,
		"files_extracted": result.FilesExtracted,
		"warnings":        result.Warnings,
		"dry_run":         dryRun,
	})
}
