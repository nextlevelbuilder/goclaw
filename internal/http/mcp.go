package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// MCPToolLister returns discovered tool names for a specific MCP server.
type MCPToolLister interface {
	ServerToolNames(serverName string) []string
}

// MCPHandler handles MCP server management HTTP endpoints.
type MCPHandler struct {
	store  store.MCPServerStore
	token  string
	msgBus *bus.MessageBus
	mgr    MCPToolLister // optional, nil when Manager not available
}

// NewMCPHandler creates a handler for MCP server management endpoints.
func NewMCPHandler(s store.MCPServerStore, token string, msgBus *bus.MessageBus, mgr MCPToolLister) *MCPHandler {
	return &MCPHandler{store: s, token: token, msgBus: msgBus, mgr: mgr}
}

func (h *MCPHandler) emitCacheInvalidate() {
	if h.msgBus == nil {
		return
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindMCP},
	})
}

// RegisterRoutes registers all MCP management routes on the given mux.
func (h *MCPHandler) RegisterRoutes(mux *http.ServeMux) {
	// Server CRUD
	mux.HandleFunc("GET /v1/mcp/servers", h.auth(h.handleListServers))
	mux.HandleFunc("POST /v1/mcp/servers", h.auth(h.handleCreateServer))
	mux.HandleFunc("GET /v1/mcp/servers/{id}", h.auth(h.handleGetServer))
	mux.HandleFunc("PUT /v1/mcp/servers/{id}", h.auth(h.handleUpdateServer))
	mux.HandleFunc("DELETE /v1/mcp/servers/{id}", h.auth(h.handleDeleteServer))

	// Test connection (no save)
	mux.HandleFunc("POST /v1/mcp/servers/test", h.auth(h.handleTestConnection))

	// Server tools (runtime-discovered)
	mux.HandleFunc("GET /v1/mcp/servers/{id}/tools", h.auth(h.handleListServerTools))

	// Agent grants
	mux.HandleFunc("GET /v1/mcp/servers/{id}/grants", h.auth(h.handleListServerGrants))
	mux.HandleFunc("POST /v1/mcp/servers/{id}/grants/agent", h.auth(h.handleGrantAgent))
	mux.HandleFunc("DELETE /v1/mcp/servers/{id}/grants/agent/{agentID}", h.auth(h.handleRevokeAgent))
	mux.HandleFunc("GET /v1/mcp/grants/agent/{agentID}", h.auth(h.handleListAgentGrants))

	// User grants
	mux.HandleFunc("POST /v1/mcp/servers/{id}/grants/user", h.auth(h.handleGrantUser))
	mux.HandleFunc("DELETE /v1/mcp/servers/{id}/grants/user/{userID}", h.auth(h.handleRevokeUser))

	// Access requests
	mux.HandleFunc("POST /v1/mcp/requests", h.auth(h.handleCreateRequest))
	mux.HandleFunc("GET /v1/mcp/requests", h.auth(h.handleListPendingRequests))
	mux.HandleFunc("POST /v1/mcp/requests/{id}/review", h.auth(h.handleReviewRequest))
}

func (h *MCPHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(h.token, "", next)
}

// --- Server CRUD ---

// mcpServerWithCounts extends MCPServerData with agent grant count for list responses.
type mcpServerWithCounts struct {
	store.MCPServerData
	AgentCount int `json:"agent_count"`
}

func (h *MCPHandler) handleListServers(w http.ResponseWriter, r *http.Request) {
	servers, err := h.store.ListServers(r.Context())
	if err != nil {
		slog.Error("mcp.list_servers", "error", err)
		locale := store.LocaleFromContext(r.Context())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "servers")})
		return
	}

	// Enrich with agent grant counts
	counts, _ := h.store.CountAgentGrantsByServer(r.Context())
	result := make([]mcpServerWithCounts, len(servers))
	for i, srv := range servers {
		result[i] = mcpServerWithCounts{MCPServerData: srv, AgentCount: counts[srv.ID]}
	}

	writeJSON(w, http.StatusOK, map[string]any{"servers": result})
}

func (h *MCPHandler) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	var srv store.MCPServerData
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&srv); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if srv.Name == "" || srv.Transport == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "name and transport")})
		return
	}
	if !isValidSlug(srv.Name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "name")})
		return
	}

	userID := store.UserIDFromContext(r.Context())
	if userID != "" {
		srv.CreatedBy = userID
	}

	if err := h.store.CreateServer(r.Context(), &srv); err != nil {
		slog.Error("mcp.create_server", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.emitCacheInvalidate()
	emitAudit(h.msgBus, r, "mcp_server.created", "mcp_server", srv.ID.String())
	writeJSON(w, http.StatusCreated, srv)
}

func (h *MCPHandler) handleGetServer(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "server")})
		return
	}

	srv, err := h.store.GetServer(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "server", id.String())})
		return
	}

	writeJSON(w, http.StatusOK, srv)
}

func (h *MCPHandler) handleUpdateServer(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "server")})
		return
	}

	var updates map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if name, ok := updates["name"]; ok {
		if s, _ := name.(string); !isValidSlug(s) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "name")})
			return
		}
	}

	if err := h.store.UpdateServer(r.Context(), id, updates); err != nil {
		slog.Error("mcp.update_server", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.emitCacheInvalidate()
	emitAudit(h.msgBus, r, "mcp_server.updated", "mcp_server", id.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *MCPHandler) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "server")})
		return
	}

	if err := h.store.DeleteServer(r.Context(), id); err != nil {
		slog.Error("mcp.delete_server", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.emitCacheInvalidate()
	emitAudit(h.msgBus, r, "mcp_server.deleted", "mcp_server", id.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
