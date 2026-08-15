package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/backup"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// backupInProgress prevents concurrent backup/restore operations.
var backupInProgress atomic.Bool

// BackupHandler handles system backup endpoints.
// All routes require admin role; download/preflight routes further require owner.
type BackupHandler struct {
	cfg      *config.Config
	dsn      string
	version  string
	isOwner  func(string) bool
}

// NewBackupHandler creates a handler for system backup endpoints.
func NewBackupHandler(cfg *config.Config, dsn, version string, isOwner func(string) bool) *BackupHandler {
	return &BackupHandler{cfg: cfg, dsn: dsn, version: version, isOwner: isOwner}
}

// RegisterRoutes registers system backup routes on the given mux.
func (h *BackupHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/system/backup",
		requireAuth(permissions.RoleAdmin, h.handleBackup))
	mux.HandleFunc("GET /v1/system/backup/preflight",
		requireAuth(permissions.RoleAdmin, h.handlePreflight))
	mux.HandleFunc("GET /v1/system/backup/download/{token}",
		requireAuth(permissions.RoleAdmin, h.handleDownload))
}

// preflightHTTPResponse is the flat JSON shape consumed by the web UI.
type preflightHTTPResponse struct {
	PgDumpAvailable    bool     `json:"pg_dump_available"`
	DiskSpaceOK        bool     `json:"disk_space_ok"`
	DbSizeBytes        int64    `json:"db_size_bytes"`
	DbSizeHuman        string   `json:"db_size_human"`
	FreeDiskBytes      int64    `json:"free_disk_bytes"`
	FreeDiskHuman      string   `json:"free_disk_human"`
	DataDirSizeBytes   int64    `json:"data_dir_size_bytes"`
	DataDirSizeHuman   string   `json:"data_dir_size_human"`
	WorkspaceSizeBytes int64    `json:"workspace_size_bytes"`
	WorkspaceSizeHuman string   `json:"workspace_size_human"`
	Warnings           []string `json:"warnings"`
}

// handlePreflight returns a preflight check result for the backup operation.
func (h *BackupHandler) handlePreflight(w http.ResponseWriter, r *http.Request) {
	result := backup.RunPreflight(r.Context(), h.dsn, h.cfg.ResolvedDataDir(), h.cfg.WorkspacePath())
	resp := preflightHTTPResponse{
		PgDumpAvailable:    result.PgDumpAvailable,
		DiskSpaceOK:        result.DiskSpaceOK,
		DbSizeBytes:        result.DbSizeBytes,
		DbSizeHuman:        backup.FormatBytes(result.DbSizeBytes),
		FreeDiskBytes:      result.FreeDiskBytes,
		FreeDiskHuman:      backup.FormatBytes(result.FreeDiskBytes),
		DataDirSizeBytes:   result.DataDirSizeBytes,
		DataDirSizeHuman:   backup.FormatBytes(result.DataDirSizeBytes),
		WorkspaceSizeBytes: result.WorkspaceSizeBytes,
		WorkspaceSizeHuman: backup.FormatBytes(result.WorkspaceSizeBytes),
		Warnings:           result.Warnings,
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleBackup runs the backup as an SSE-streamed operation.
// Request body (JSON, optional): {"exclude_db": false, "exclude_files": false}
func (h *BackupHandler) handleBackup(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := extractLocale(r)

	if !h.isOwnerUser(userID) {
		slog.Warn("security.backup_owner_denied", "user_id", userID)
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized,
			i18n.T(locale, i18n.MsgNoAccess, "system backup"))
		return
	}

	if !backupInProgress.CompareAndSwap(false, true) {
		writeError(w, http.StatusConflict, protocol.ErrInternal, "a backup or restore operation is already in progress")
		return
	}
	defer backupInProgress.Store(false)

	var req struct {
		ExcludeDB    bool `json:"exclude_db"`
		ExcludeFiles bool `json:"exclude_files"`
	}
	// Ignore decode errors — all fields have safe zero-value defaults.
	_ = decodeJSONOptional(r, &req)

	flusher := initSSE(w)
	if flusher == nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "streaming not supported")
		return
	}

	tmpFile, err := os.CreateTemp("", "goclaw-backup-*.tar.gz")
	if err != nil {
		sendSSE(w, flusher, "error", ProgressEvent{Phase: "init", Status: "error", Detail: "failed to create temp file"})
		return
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	ts := time.Now().UTC().Format("20060102-150405")
	fileName := fmt.Sprintf("backup-%s.tar.gz", ts)

	opts := backup.Options{
		DSN:           h.dsn,
		DataDir:       h.cfg.ResolvedDataDir(),
		WorkspacePath: h.cfg.WorkspacePath(),
		OutputPath:    tmpPath,
		CreatedBy:     userID,
		GoclawVersion: h.version,
		ExcludeDB:     req.ExcludeDB,
		ExcludeFiles:  req.ExcludeFiles,
		ProgressFn: func(phase, detail string) {
			sendSSE(w, flusher, "progress", ProgressEvent{Phase: phase, Status: "running", Detail: detail})
		},
	}

	manifest, runErr := backup.Run(r.Context(), opts)
	if runErr != nil {
		slog.Error("system.backup.sse", "error", runErr)
		sendSSE(w, flusher, "error", ProgressEvent{Phase: "backup", Status: "error", Detail: runErr.Error()})
		os.Remove(tmpPath)
		return
	}

	token := storeExportToken("system", userID, tmpPath, fileName)
	sendSSE(w, flusher, "complete", map[string]any{
		"download_url":   "/v1/system/backup/download/" + token,
		"file_name":      fileName,
		"total_bytes":    manifest.Stats.TotalBytes,
		"schema_version": manifest.SchemaVersion,
	})
}

// handleDownload serves a previously-prepared backup archive by token.
func (h *BackupHandler) handleDownload(w http.ResponseWriter, r *http.Request) {
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

// isOwnerUser returns true if userID belongs to a configured system owner.
func (h *BackupHandler) isOwnerUser(userID string) bool {
	return userID != "" && h.isOwner != nil && h.isOwner(userID)
}

// decodeJSONOptional decodes the request body into dest.
// Returns nil (no error) when body is absent or empty — all fields keep zero values.
func decodeJSONOptional(r *http.Request, dest any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return json.NewDecoder(r.Body).Decode(dest)
}
