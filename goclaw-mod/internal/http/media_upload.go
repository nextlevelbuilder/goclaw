package http

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
)

const (
	// maxUploadSize is the default max upload size (50MB).
	maxUploadSize int64 = 50 * 1024 * 1024
)

// MediaUploadHandler handles media file uploads for WebSocket clients.
type MediaUploadHandler struct{}

// NewMediaUploadHandler creates a media upload handler.
func NewMediaUploadHandler() *MediaUploadHandler {
	return &MediaUploadHandler{}
}

// RegisterRoutes registers the upload endpoint.
func (h *MediaUploadHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/media/upload", h.auth(h.handleUpload))
}

func (h *MediaUploadHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

func (h *MediaUploadHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgFileTooLarge)})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgMissingFileField)})
		return
	}
	defer file.Close()

	// Sanitize filename: strip path, prevent traversal.
	origName := filepath.Base(header.Filename)
	if origName == "." || origName == "/" || strings.Contains(origName, "..") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidFilename)})
		return
	}

	ext := filepath.Ext(origName)
	if ext == "" {
		ext = ".bin"
	}

	// Save to temp with unique name.
	tmpName := fmt.Sprintf("ws_upload_%d%s", time.Now().UnixNano(), ext)
	tmpPath := filepath.Join(os.TempDir(), tmpName)

	out, err := os.Create(tmpPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to create temp file")})
		return
	}
	defer out.Close()

	if _, err := io.Copy(out, file); err != nil {
		os.Remove(tmpPath)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "failed to save file")})
		return
	}

	mimeType := media.DetectMIMEType(origName)

	writeJSON(w, http.StatusOK, map[string]any{
		"path":      tmpPath,
		"mime_type": mimeType,
		"filename":  origName,
	})
}
