package http

import (
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// MemoryHandler handles memory document management endpoints.
type MemoryHandler struct {
	store store.MemoryStore
	token string
}

// NewMemoryHandler creates a handler for memory management endpoints.
func NewMemoryHandler(s store.MemoryStore, token string) *MemoryHandler {
	return &MemoryHandler{store: s, token: token}
}

// RegisterRoutes registers all memory routes on the given mux.
func (h *MemoryHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/memory/documents", h.auth(h.handleListAllDocuments))
	mux.HandleFunc("GET /v1/agents/{agentID}/memory/documents", h.auth(h.handleListDocuments))
	mux.HandleFunc("GET /v1/agents/{agentID}/memory/documents/{path...}", h.auth(h.handleGetDocument))
	mux.HandleFunc("PUT /v1/agents/{agentID}/memory/documents/{path...}", h.auth(h.handlePutDocument))
	mux.HandleFunc("DELETE /v1/agents/{agentID}/memory/documents/{path...}", h.auth(h.handleDeleteDocument))
	mux.HandleFunc("GET /v1/agents/{agentID}/memory/chunks", h.auth(h.handleListChunks))
	mux.HandleFunc("POST /v1/agents/{agentID}/memory/index", h.auth(h.handleIndexDocument))
	mux.HandleFunc("POST /v1/agents/{agentID}/memory/index-all", h.auth(h.handleIndexAll))
	mux.HandleFunc("POST /v1/agents/{agentID}/memory/search", h.auth(h.handleSearch))
}

func (h *MemoryHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.token != "" {
			if extractBearerToken(r) != h.token {
				locale := extractLocale(r)
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": i18n.T(locale, i18n.MsgUnauthorized)})
				return
			}
		}
		role := resolveHTTPRole(r, h.token)
		ctx := store.WithRole(r.Context(), role)
		r = r.WithContext(ctx)
		next(w, r)
	}
}
