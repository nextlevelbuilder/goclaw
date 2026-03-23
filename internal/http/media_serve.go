package http

import (
	"log/slog"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/media"
)

// MediaServeHandler serves persisted media files by ID.
type MediaServeHandler struct {
	store *media.Store
}

// NewMediaServeHandler creates a media serve handler.
func NewMediaServeHandler(store *media.Store) *MediaServeHandler {
	return &MediaServeHandler{store: store}
}

// RegisterRoutes registers the media serve endpoint.
func (h *MediaServeHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/media/{id}", h.auth(h.handleServe))
}

func (h *MediaServeHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Priority 1: short-lived signed file token (?ft=) — decoupled from gateway token.
		if ft := r.URL.Query().Get("ft"); ft != "" {
			mediaID := r.PathValue("id")
			if VerifyFileToken(ft, "/v1/media/"+mediaID, FileSigningKey()) {
				next(w, r)
				return
			}
			http.Error(w, "invalid or expired file token", http.StatusUnauthorized)
			return
		}
		// Priority 2: Bearer header (API clients only).
		provided := extractBearerToken(r)
		authedReq, ok := requireAuthBearer("", provided, w, r)
		if !ok {
			return
		}
		next(w, authedReq)
	}
}

func (h *MediaServeHandler) handleServe(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	id := r.PathValue("id")
	if id == "" || strings.Contains(id, "..") || strings.Contains(id, "/") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "invalid media id")})
		return
	}

	filePath, err := h.store.LoadPath(id)
	if err != nil {
		slog.Debug("media serve: not found", "id", id, "error", err)
		http.NotFound(w, r)
		return
	}

	ext := filepath.Ext(filePath)
	ct := mime.TypeByExtension(ext)
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")

	http.ServeFile(w, r, filePath)
}
