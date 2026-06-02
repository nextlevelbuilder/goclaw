package http

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// MemoryHandler handles memory document management endpoints.
type MemoryHandler struct {
	store  store.MemoryStore
	agents agentKeyResolver
}

// NewMemoryHandler creates a handler for memory management endpoints.
func NewMemoryHandler(s store.MemoryStore, agents ...store.AgentStore) *MemoryHandler {
	h := &MemoryHandler{store: s}
	if len(agents) > 0 {
		h.agents = agents[0]
	}
	return h
}

type agentKeyResolver interface {
	GetByKey(ctx context.Context, agentKey string) (*store.AgentData, error)
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
	return requireAuth("", next)
}

func (h *MemoryHandler) resolveAgentID(w http.ResponseWriter, r *http.Request) (string, bool) {
	rawID := r.PathValue("agentID")
	if id, err := uuid.Parse(rawID); err == nil {
		return id.String(), true
	}

	locale := store.LocaleFromContext(r.Context())
	if h.agents == nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "agent"))
		return "", false
	}

	agent, err := h.agents.GetByKey(r.Context(), rawID)
	if err != nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "agent", rawID))
		return "", false
	}
	return agent.ID.String(), true
}
