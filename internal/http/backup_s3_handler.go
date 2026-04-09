package http

import (
	"context"
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

// BackupS3Handler handles S3 integration endpoints for backup/restore.
// All routes require admin role + owner user.
type BackupS3Handler struct {
	cfg     *config.Config
	dsn     string
	version string
	secrets store.ConfigSecretsStore
	isOwner func(string) bool
}

// NewBackupS3Handler creates a handler for S3 backup endpoints.
func NewBackupS3Handler(cfg *config.Config, dsn, version string, secrets store.ConfigSecretsStore, isOwner func(string) bool) *BackupS3Handler {
	return &BackupS3Handler{cfg: cfg, dsn: dsn, version: version, secrets: secrets, isOwner: isOwner}
}

// RegisterRoutes registers S3 backup routes on the given mux.
func (h *BackupS3Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/system/backup/s3/config",
		requireAuth(permissions.RoleAdmin, h.handleGetConfig))
	mux.HandleFunc("PUT /v1/system/backup/s3/config",
		requireAuth(permissions.RoleAdmin, h.handleSaveConfig))
	mux.HandleFunc("GET /v1/system/backup/s3/list",
		requireAuth(permissions.RoleAdmin, h.handleList))
	mux.HandleFunc("POST /v1/system/backup/s3/upload",
		requireAuth(permissions.RoleAdmin, h.handleUpload))
	mux.HandleFunc("POST /v1/system/backup/s3/backup",
		requireAuth(permissions.RoleAdmin, h.handleBackupAndUpload))
}

// handleGetConfig returns the current S3 config with access_key_id masked.
// secret_access_key is NEVER returned.
func (h *BackupS3Handler) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := extractLocale(r)

	if !h.isOwnerUser(userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized,
			i18n.T(locale, i18n.MsgNoAccess, "s3 config"))
		return
	}

	cfg, err := backup.LoadS3Config(r.Context(), h.secrets)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, err.Error())
		return
	}
	if cfg == nil {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"configured":    true,
		"bucket":        cfg.Bucket,
		"region":        cfg.Region,
		"endpoint":      cfg.Endpoint,
		"prefix":        cfg.Prefix,
		"access_key_id": maskAccessKey(cfg.AccessKeyID),
	})
}

// handleSaveConfig saves S3 credentials and tests the connection.
func (h *BackupS3Handler) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := extractLocale(r)

	if !h.isOwnerUser(userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized,
			i18n.T(locale, i18n.MsgNoAccess, "s3 config"))
		return
	}

	var cfg backup.S3Config
	if !bindJSON(w, r, locale, &cfg) {
		return
	}

	if err := backup.ValidateS3Config(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, err.Error())
		return
	}

	// Set defaults before testing.
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.Prefix == "" {
		cfg.Prefix = "backups/"
	}

	// Test connection before saving.
	client, err := backup.NewS3Client(&cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			fmt.Sprintf("invalid s3 config: %v", err))
		return
	}
	if err := client.TestConnection(r.Context()); err != nil {
		slog.Warn("backup.s3.connection_test_failed", "error", err)
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			fmt.Sprintf("s3 connection test failed: %v", err))
		return
	}

	if err := backup.SaveS3Config(r.Context(), h.secrets, &cfg); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			fmt.Sprintf("save s3 config: %v", err))
		return
	}

	slog.Info("backup.s3.config_saved", "bucket", cfg.Bucket, "region", cfg.Region)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "bucket": cfg.Bucket})
}

// handleList returns available backups in S3 sorted newest first.
func (h *BackupS3Handler) handleList(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := extractLocale(r)

	if !h.isOwnerUser(userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized,
			i18n.T(locale, i18n.MsgNoAccess, "s3 list"))
		return
	}

	client, err := h.s3ClientFromSecrets(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, err.Error())
		return
	}

	entries, err := client.ListBackups(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			fmt.Sprintf("list s3 backups: %v", err))
		return
	}

	if entries == nil {
		entries = []backup.BackupEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"backups": entries})
}

// handleUpload uploads an existing local backup file to S3 via SSE progress stream.
// Body: {"backup_token": "<token>"} — token from a previous backup operation.
func (h *BackupS3Handler) handleUpload(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := extractLocale(r)

	if !h.isOwnerUser(userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized,
			i18n.T(locale, i18n.MsgNoAccess, "s3 upload"))
		return
	}

	var req struct {
		BackupToken string `json:"backup_token"`
	}
	if !bindJSON(w, r, locale, &req) {
		return
	}

	// Only accept backup_token — never arbitrary file paths (prevents file exfiltration)
	if req.BackupToken == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, "backup_token is required"))
		return
	}

	archivePath := ""
	fileName := ""

	if req.BackupToken != "" {
		entry, ok := lookupExportToken(req.BackupToken)
		if !ok {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgNotFound, "backup token", req.BackupToken))
			return
		}
		archivePath = entry.filePath
		fileName = entry.fileName
	}

	if archivePath == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgRequired, "backup_token or backup_path"))
		return
	}

	client, err := h.s3ClientFromSecrets(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, err.Error())
		return
	}

	flusher := initSSE(w)
	if flusher == nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "streaming not supported")
		return
	}

	s3Key, uploadErr := uploadFileToS3(r.Context(), client, archivePath, fileName, h.version)
	if uploadErr != nil {
		slog.Error("backup.s3.upload_failed", "error", uploadErr)
		sendSSE(w, flusher, "error", ProgressEvent{Phase: "upload", Status: "error", Detail: uploadErr.Error()})
		return
	}

	sendSSE(w, flusher, "complete", map[string]any{"s3_key": s3Key, "status": "uploaded"})
}

// handleBackupAndUpload creates a new backup then uploads it to S3 in one step.
func (h *BackupS3Handler) handleBackupAndUpload(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := extractLocale(r)

	if !h.isOwnerUser(userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized,
			i18n.T(locale, i18n.MsgNoAccess, "s3 backup"))
		return
	}

	var req struct {
		ExcludeDB    bool `json:"exclude_db"`
		ExcludeFiles bool `json:"exclude_files"`
	}
	_ = decodeJSONOptional(r, &req)

	client, err := h.s3ClientFromSecrets(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, err.Error())
		return
	}

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
	defer os.Remove(tmpPath)

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
		slog.Error("backup.s3.backup_failed", "error", runErr)
		sendSSE(w, flusher, "error", ProgressEvent{Phase: "backup", Status: "error", Detail: runErr.Error()})
		return
	}

	sendSSE(w, flusher, "progress", ProgressEvent{Phase: "upload", Status: "running", Detail: "uploading to S3"})

	s3Key, uploadErr := uploadFileToS3(r.Context(), client, tmpPath, "", h.version)
	if uploadErr != nil {
		slog.Error("backup.s3.upload_failed", "error", uploadErr)
		sendSSE(w, flusher, "error", ProgressEvent{Phase: "upload", Status: "error", Detail: uploadErr.Error()})
		return
	}

	sendSSE(w, flusher, "complete", map[string]any{
		"s3_key":         s3Key,
		"total_bytes":    manifest.Stats.TotalBytes,
		"schema_version": manifest.SchemaVersion,
		"status":         "uploaded",
	})
}

// s3ClientFromSecrets loads S3 config from secrets store and returns a client.
// Returns a descriptive error if S3 is not configured.
func (h *BackupS3Handler) s3ClientFromSecrets(r *http.Request) (*backup.S3Client, error) {
	cfg, err := backup.LoadS3Config(r.Context(), h.secrets)
	if err != nil {
		return nil, fmt.Errorf("load s3 config: %w", err)
	}
	if cfg == nil {
		return nil, fmt.Errorf("s3 not configured — use PUT /v1/system/backup/s3/config first")
	}
	return backup.NewS3Client(cfg)
}

// isOwnerUser returns true if userID belongs to a configured system owner.
func (h *BackupS3Handler) isOwnerUser(userID string) bool {
	return userID != "" && h.isOwner != nil && h.isOwner(userID)
}

// maskAccessKey masks an AWS access key ID, showing only the first 4 chars + "***".
func maskAccessKey(key string) string {
	if len(key) <= 4 {
		return "***"
	}
	return key[:4] + "***"
}

// uploadFileToS3 opens a local file and uploads it to S3, returning the final S3 key.
func uploadFileToS3(ctx context.Context, client *backup.S3Client, filePath, fileName, version string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open backup file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat backup file: %w", err)
	}

	if fileName == "" {
		ts := time.Now().UTC().Format("20060102-150405")
		if version != "" {
			fileName = fmt.Sprintf("backup-%s-v%s.tar.gz", ts, version)
		} else {
			fileName = fmt.Sprintf("backup-%s.tar.gz", ts)
		}
	}

	if err := client.Upload(ctx, fileName, f, info.Size()); err != nil {
		return "", err
	}
	return fileName, nil
}
