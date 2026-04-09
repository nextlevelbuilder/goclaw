package http

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"

	"github.com/nextlevelbuilder/goclaw/internal/backup"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const maxRestoreSize = 10 << 30 // 10 GB

// RestoreHandler handles the POST /v1/system/restore endpoint.
type RestoreHandler struct {
	cfg     *config.Config
	dsn     string
	isOwner func(string) bool
}

// NewRestoreHandler creates a handler for system restore endpoints.
func NewRestoreHandler(cfg *config.Config, dsn string, isOwner func(string) bool) *RestoreHandler {
	return &RestoreHandler{cfg: cfg, dsn: dsn, isOwner: isOwner}
}

// RegisterRoutes registers the restore route on the given mux.
func (h *RestoreHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/system/restore",
		requireAuth(permissions.RoleAdmin, h.handleRestore))
}

// handleRestore accepts a multipart tar.gz upload and streams restore progress via SSE.
// Query params: skip_db=true, skip_files=true, dry_run=true
func (h *RestoreHandler) handleRestore(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := extractLocale(r)

	if !h.isOwnerUser(userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized,
			i18n.T(locale, i18n.MsgNoAccess, "system restore"))
		return
	}

	if !backupInProgress.CompareAndSwap(false, true) {
		writeError(w, http.StatusConflict, protocol.ErrInternal, "a backup or restore operation is already in progress")
		return
	}
	defer backupInProgress.Store(false)

	q := r.URL.Query()
	skipDB := q.Get("skip_db") == "true" || q.Get("skip_db") == "1"
	skipFiles := q.Get("skip_files") == "true" || q.Get("skip_files") == "1"
	dryRun := q.Get("dry_run") == "true" || q.Get("dry_run") == "1"

	// Preflight: verify psql is available for PG builds (no-op for SQLite builds).
	if !skipDB && h.dsn != "" {
		if err := checkPsqlAvailable(); err != nil {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
				fmt.Sprintf("psql not found on PATH: %v", err))
			return
		}
	}

	// Check active connections before accepting upload (only meaningful for PG).
	if !dryRun && !skipDB && h.dsn != "" {
		conns, connErr := backup.CheckActiveConnections(r.Context(), h.dsn)
		if connErr == nil && conns > 0 {
			writeError(w, http.StatusConflict, protocol.ErrInvalidRequest,
				fmt.Sprintf("%d active DB connection(s) detected; stop the gateway and all clients before restoring", conns))
			return
		}
	}

	// Parse multipart upload.
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

	// Save upload to a temp file so we can seek / re-read.
	tmp, err := os.CreateTemp("", "goclaw-restore-*.tar.gz")
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError))
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	written, err := copyWithLimit(tmp, file, maxRestoreSize)
	tmp.Close()
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgFileTooLarge))
		return
	}
	if written == 0 {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "archive is empty")
		return
	}

	// Switch to SSE for progress streaming.
	flusher := initSSE(w)
	if flusher == nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "streaming not supported")
		return
	}

	opts := backup.RestoreOptions{
		ArchivePath:   tmpPath,
		DSN:           h.dsn,
		DataDir:       h.cfg.ResolvedDataDir(),
		WorkspacePath: h.cfg.WorkspacePath(),
		DryRun:        dryRun,
		SkipDB:        skipDB,
		SkipFiles:     skipFiles,
		Force:         true, // HTTP caller already authenticated as owner/admin
		ProgressFn: func(phase, detail string) {
			sendSSE(w, flusher, "progress", ProgressEvent{Phase: phase, Status: "running", Detail: detail})
		},
	}

	result, runErr := backup.Restore(r.Context(), opts)
	if runErr != nil {
		slog.Error("system.restore.sse", "error", runErr, "user", userID)
		sendSSE(w, flusher, "error", ProgressEvent{
			Phase:  "restore",
			Status: "error",
			Detail: runErr.Error(),
		})
		return
	}

	sendSSE(w, flusher, "complete", map[string]any{
		"manifest_version":  result.ManifestVersion,
		"schema_version":    result.SchemaVersion,
		"database_restored": result.DatabaseRestored,
		"files_extracted":   result.FilesExtracted,
		"bytes_extracted":   result.BytesExtracted,
		"warnings":          result.Warnings,
		"dry_run":           dryRun,
	})
}

// isOwnerUser returns true if userID belongs to a configured system owner.
func (h *RestoreHandler) isOwnerUser(userID string) bool {
	return userID != "" && h.isOwner != nil && h.isOwner(userID)
}

// checkPsqlAvailable verifies that psql is on PATH (PG builds only).
// For SQLite builds this is a no-op returning nil.
func checkPsqlAvailable() error {
	_, err := exec.LookPath("psql")
	return err
}

// copyWithLimit copies at most limit bytes from src to dst.
// Returns an error if the source exceeds limit.
func copyWithLimit(dst io.Writer, src io.Reader, limit int64) (int64, error) {
	n, err := io.Copy(dst, io.LimitReader(src, limit+1))
	if err != nil {
		return n, err
	}
	if n > limit {
		return n, fmt.Errorf("upload exceeds %s limit", formatBytes(limit))
	}
	return n, nil
}

// formatBytes formats a byte count as a human-readable string.
func formatBytes(b int64) string {
	if b >= 1<<30 {
		return strconv.FormatInt(b>>30, 10) + " GB"
	}
	return strconv.FormatInt(b>>20, 10) + " MB"
}
