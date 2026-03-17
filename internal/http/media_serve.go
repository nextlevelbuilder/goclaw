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
	token string
}

// NewMediaServeHandler creates a media serve handler.
func NewMediaServeHandler(store *media.Store, token string) *MediaServeHandler {
	return &MediaServeHandler{store: store, token: token}
}

// RegisterRoutes registers the media serve endpoint.
func (h *MediaServeHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/media/{id}", h.auth(h.handleServe))
}

func (h *MediaServeHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Accept token via Bearer header or ?token= query param (for <img src>).
		provided := extractBearerToken(r)
		if provided == "" {
			provided = r.URL.Query().Get("token")
		}
		if !requireAuthBearer(h.token, "", provided, w, r) {
			return
		}
		next(w, r)
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
