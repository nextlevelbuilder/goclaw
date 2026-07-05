package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// MCPToolLister returns discovered tool names/info for a specific MCP server.
type MCPToolLister interface {
	ServerToolNames(serverName string) []string
	// ServerToolInfos returns the original (bare, unprefixed) tool name and
	// real description for each tool of an already-connected server. Bare
	// names match the shape returned by mcp.DiscoverTools, so callers don't
	// have to special-case "already connected" vs. "on-demand discovery".
	ServerToolInfos(serverName string) []mcp.ToolInfo
}

// MCPPoolEvictor evicts pooled connections for a tenant+server (called on credential rotation).
type MCPPoolEvictor interface {
	Evict(tenantID uuid.UUID, serverName string)
	// EvictServer evicts both the shared and all per-user connections for a server.
	// Called when OAuth tokens or grants change so next request reconnects with fresh credentials.
	EvictServer(tenantID uuid.UUID, serverName string)
}

// MCPHandler handles MCP server management HTTP endpoints.
type MCPHandler struct {
	store         store.MCPServerStore
	msgBus        *bus.MessageBus
	mgr           MCPToolLister            // optional, nil when Manager not available
	poolEvictor   MCPPoolEvictor           // optional, nil when pool not available
	db            *sql.DB                  // for export/import direct queries
	oauthProvider MCPOAuthTokenProvider    // optional, nil when OAuth not configured
	oauthStore    store.MCPOAuthTokenStore // optional, nil when OAuth not configured
}

// MCPOAuthTokenProvider retrieves a valid OAuth Bearer token for an MCP server.
// Mirrors mcp.OAuthTokenProvider to avoid a circular import.
type MCPOAuthTokenProvider interface {
	GetValidToken(ctx context.Context, serverID, tenantID uuid.UUID, userID string) (string, error)
}

// NewMCPHandler creates a handler for MCP server management endpoints.
func NewMCPHandler(s store.MCPServerStore, msgBus *bus.MessageBus, mgr MCPToolLister) *MCPHandler {
	return &MCPHandler{store: s, msgBus: msgBus, mgr: mgr}
}

// SetPoolEvictor sets the pool evictor for credential rotation handling.
func (h *MCPHandler) SetPoolEvictor(e MCPPoolEvictor) { h.poolEvictor = e }

// SetOAuthProvider wires in an OAuth token provider so the tool-list endpoint
// can inject a Bearer token when doing on-demand discovery for OAuth servers.
func (h *MCPHandler) SetOAuthProvider(p MCPOAuthTokenProvider) { h.oauthProvider = p }

// SetOAuthStore wires in the OAuth token store so the update handler can purge
// stored tokens when a server's URL or OAuth config changes (the old tokens
// were minted for a different resource/AS and can no longer be used).
func (h *MCPHandler) SetOAuthStore(s store.MCPOAuthTokenStore) { h.oauthStore = s }

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
	// Server CRUD (reads: viewer+, writes: admin+)
	mux.HandleFunc("GET /v1/mcp/servers", h.auth(h.handleListServers))
	mux.HandleFunc("POST /v1/mcp/servers", h.adminAuth(h.handleCreateServer))
	mux.HandleFunc("GET /v1/mcp/servers/{id}", h.auth(h.handleGetServer))
	mux.HandleFunc("PUT /v1/mcp/servers/{id}", h.adminAuth(h.handleUpdateServer))
	mux.HandleFunc("DELETE /v1/mcp/servers/{id}", h.adminAuth(h.handleDeleteServer))

	// Test connection (admin+ — infra operation)
	mux.HandleFunc("POST /v1/mcp/servers/test", h.adminAuth(h.handleTestConnection))

	// Reconnect (admin+ — evict pooled connection)
	mux.HandleFunc("POST /v1/mcp/servers/{id}/reconnect", h.adminAuth(h.handleReconnectServer))

	// Server tools (read-only: viewer+)
	mux.HandleFunc("GET /v1/mcp/servers/{id}/tools", h.auth(h.handleListServerTools))

	// Agent grants (reads: viewer+, writes: admin+)
	mux.HandleFunc("GET /v1/mcp/servers/{id}/grants", h.auth(h.handleListServerGrants))
	mux.HandleFunc("POST /v1/mcp/servers/{id}/grants/agent", h.adminAuth(h.handleGrantAgent))
	mux.HandleFunc("DELETE /v1/mcp/servers/{id}/grants/agent/{agentID}", h.adminAuth(h.handleRevokeAgent))
	mux.HandleFunc("GET /v1/mcp/grants/agent/{agentID}", h.auth(h.handleListAgentGrants))

	// User grants (admin+)
	mux.HandleFunc("POST /v1/mcp/servers/{id}/grants/user", h.adminAuth(h.handleGrantUser))
	mux.HandleFunc("DELETE /v1/mcp/servers/{id}/grants/user/{userID}", h.adminAuth(h.handleRevokeUser))

	// Access requests (create: viewer+, list: viewer+, review: admin+)
	mux.HandleFunc("POST /v1/mcp/requests", h.auth(h.handleCreateRequest))
	mux.HandleFunc("GET /v1/mcp/requests", h.auth(h.handleListPendingRequests))
	mux.HandleFunc("POST /v1/mcp/requests/{id}/review", h.adminAuth(h.handleReviewRequest))
	// Export / Import (admin+)
	mux.HandleFunc("GET /v1/mcp/export/preview", h.adminAuth(h.handleMCPExportPreview))
	mux.HandleFunc("GET /v1/mcp/export", h.adminAuth(h.handleMCPExport))
	mux.HandleFunc("POST /v1/mcp/import", h.adminAuth(h.handleMCPImport))
}

func (h *MCPHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

func (h *MCPHandler) adminAuth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(permissions.RoleAdmin, next)
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

	// Security validation: command+args for stdio, URL for HTTP transports
	var args []string
	if len(srv.Args) > 0 {
		if err := json.Unmarshal(srv.Args, &args); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "args must be a string array")})
			return
		}
	}
	if err := mcp.ValidateServerConfig(srv.Transport, srv.Command, args, srv.URL); err != nil {
		userID := store.UserIDFromContext(r.Context())
		slog.Warn("security.mcp.server_rejected",
			"user_id", userID,
			"reason", err.Error(),
			"transport", srv.Transport)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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

// oauthFingerprint captures the MCP-server OAuth settings that, when changed,
// invalidate previously minted tokens (different client, AS, grant, or scope).
type oauthFingerprint struct {
	AuthType      string
	ClientID      string
	AuthEndpoint  string
	TokenEndpoint string
	GrantType     string
	Scope         string
	UseDCR        bool // toggling DCR changes how tokens are obtained → must purge
}

// extractOAuthFingerprint parses settings.oauth into a comparable fingerprint.
// An empty/absent oauth block yields the zero value, so toggling OAuth on/off is
// correctly detected as a change (UpdateServer replaces the whole settings blob).
func extractOAuthFingerprint(settings json.RawMessage) oauthFingerprint {
	if len(settings) == 0 {
		return oauthFingerprint{}
	}
	var s struct {
		OAuth struct {
			AuthType      string `json:"auth_type"`
			ClientID      string `json:"client_id"`
			AuthEndpoint  string `json:"auth_endpoint"`
			TokenEndpoint string `json:"token_endpoint"`
			GrantType     string `json:"grant_type"`
			Scope         string `json:"scope"`
			UseDCR        *bool  `json:"use_dcr"`
		} `json:"oauth"`
	}
	if err := json.Unmarshal(settings, &s); err != nil {
		return oauthFingerprint{}
	}
	// Absent use_dcr means discover+DCR (matching handleStart), so normalize nil→true
	// to avoid a spurious purge when a legacy server gains an explicit use_dcr=true.
	useDCR := true
	if s.OAuth.UseDCR != nil {
		useDCR = *s.OAuth.UseDCR
	}
	return oauthFingerprint{
		AuthType:      s.OAuth.AuthType,
		ClientID:      s.OAuth.ClientID,
		AuthEndpoint:  s.OAuth.AuthEndpoint,
		TokenEndpoint: s.OAuth.TokenEndpoint,
		GrantType:     s.OAuth.GrantType,
		Scope:         s.OAuth.Scope,
		UseDCR:        useDCR,
	}
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

	// Allowlist: only permit known MCP server columns.
	updates = filterAllowedKeys(updates, mcpServerAllowedFields)

	// Security validation: validate updated fields
	// For updates, we need to consider the existing server + updated fields
	existingSrv, _ := h.store.GetServer(r.Context(), id)
	if existingSrv != nil {
		// Determine effective values (update or existing)
		transport := existingSrv.Transport
		if t, ok := updates["transport"].(string); ok {
			transport = t
		}
		command := existingSrv.Command
		if c, ok := updates["command"].(string); ok {
			command = c
		}
		url := existingSrv.URL
		if u, ok := updates["url"].(string); ok {
			url = u
		}
		// Parse args from updates or existing
		var args []string
		if argsRaw, ok := updates["args"]; ok {
			if argsSlice, ok := argsRaw.([]any); ok {
				for _, a := range argsSlice {
					if s, ok := a.(string); ok {
						args = append(args, s)
					}
				}
			}
		} else if len(existingSrv.Args) > 0 {
			_ = json.Unmarshal(existingSrv.Args, &args)
		}

		if err := mcp.ValidateServerConfig(transport, command, args, url); err != nil {
			userID := store.UserIDFromContext(r.Context())
			slog.Warn("security.mcp.server_update_rejected",
				"user_id", userID,
				"server_id", id,
				"reason", err.Error(),
				"transport", transport)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}

	// Read server name before update for pool eviction
	var serverName string
	if existingSrv != nil {
		serverName = existingSrv.Name
	}

	if err := h.store.UpdateServer(r.Context(), id, updates); err != nil {
		slog.Error("mcp.update_server", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// When the URL or OAuth config changes, the previously minted OAuth tokens
	// were issued for a different resource/AS and are no longer usable. Purge them
	// (global + per-user), drop the refresher's in-memory cache, and force the pool
	// to reconnect so a stale Bearer token is never replayed against the new target.
	tid := store.TenantIDFromContext(r.Context())
	oauthInvalidated := false
	if existingSrv != nil {
		urlChanged := false
		if newURL, ok := updates["url"].(string); ok && strings.TrimSpace(newURL) != existingSrv.URL {
			urlChanged = true
		}
		oauthChanged := false
		if rawSettings, ok := updates["settings"]; ok {
			newSettings, _ := json.Marshal(rawSettings)
			if extractOAuthFingerprint(newSettings) != extractOAuthFingerprint(existingSrv.Settings) {
				oauthChanged = true
			}
		}
		if urlChanged || oauthChanged {
			if h.oauthStore != nil {
				if err := h.oauthStore.DeleteServerOAuthTokens(r.Context(), id, tid); err != nil {
					slog.Warn("mcp.oauth_purge_on_config_change_failed", "server_id", id, "error", err)
				}
			}
			if inv, ok := h.oauthProvider.(interface{ InvalidateServer(uuid.UUID) }); ok {
				inv.InvalidateServer(id)
			}
			if h.poolEvictor != nil && serverName != "" {
				h.poolEvictor.EvictServer(tid, serverName)
			}
			oauthInvalidated = true
			slog.Info("mcp.oauth_tokens_purged_on_config_change", "server_id", id, "url_changed", urlChanged, "oauth_changed", oauthChanged)
		}
	}

	// Evict pool connections when credentials change (force reconnect with new creds).
	// Use EvictServer (shared + all per-user connections) so per-user connections that
	// inherited the server-level headers/api_key as their base don't keep stale values —
	// matching the OAuth-config-change path above. Skipped when EvictServer already ran.
	if !oauthInvalidated && h.poolEvictor != nil && serverName != "" {
		_, hasKey := updates["api_key"]
		_, hasHeaders := updates["headers"]
		_, hasEnv := updates["env"]
		if hasKey || hasHeaders || hasEnv {
			h.poolEvictor.EvictServer(tid, serverName)
		}
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

func (h *MCPHandler) handleReconnectServer(w http.ResponseWriter, r *http.Request) {
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

	if h.poolEvictor != nil {
		tid := store.TenantIDFromContext(r.Context())
		h.poolEvictor.Evict(tid, srv.Name)
	}

	h.emitCacheInvalidate()
	emitAudit(h.msgBus, r, "mcp_server.reconnected", "mcp_server", id.String())
	slog.Info("mcp.server.reconnect_requested", "server", srv.Name, "id", id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "reconnected"})
}
