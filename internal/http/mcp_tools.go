package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// handleTestConnection tests an MCP server connection without saving it.
func (h *MCPHandler) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	var req struct {
		ServerID  *uuid.UUID        `json:"server_id,omitempty"`
		Transport string            `json:"transport"`
		Command   string            `json:"command"`
		Args      []string          `json:"args"`
		URL       string            `json:"url"`
		Headers   map[string]string `json:"headers"`
		Env       map[string]string `json:"env"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	if req.Transport == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "transport")})
		return
	}
	if err := mcpbridge.ValidateServerConfig(req.Transport, req.Command, req.Args, req.URL); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// For an OAuth server, "test OAuth" means test with the OAuth token only:
	// Authorization comes solely from the GLOBAL token — never from a body header
	// fallback. Non-OAuth servers keep testing with the body headers/env as provided.
	if req.ServerID != nil && h.store != nil {
		if srv, err2 := h.store.GetServer(r.Context(), *req.ServerID); err2 == nil && srv != nil && mcpbridge.IsOAuthActive(srv.Settings) {
			// Drop any body-supplied Authorization — no fallback for OAuth servers.
			delete(req.Headers, "Authorization")
			if h.oauthProvider != nil {
				tenantID := store.TenantIDFromContext(r.Context())
				if token, err3 := h.oauthProvider.GetValidToken(r.Context(), *req.ServerID, tenantID, ""); err3 == nil && token != "" {
					if req.Headers == nil {
						req.Headers = make(map[string]string)
					}
					req.Headers["Authorization"] = "Bearer " + token
				}
			}
		}
	}

	tools, err := mcpbridge.DiscoverTools(r.Context(), req.Transport, req.Command, req.Args, req.Env, req.URL, req.Headers)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"tool_count": len(tools),
	})
}

// handleListServerTools lists tools for a specific MCP server.
func (h *MCPHandler) handleListServerTools(w http.ResponseWriter, r *http.Request) {
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

	// Try runtime Manager first — returns the original (bare, unprefixed) MCP
	// tool names with their real descriptions, sourced from the live
	// *BridgeTool for each registered tool. This must match the bare-name
	// shape returned by the DiscoverTools fallback below: tool_allow grants
	// saved from whichever shape this endpoint happens to return are matched
	// against bare names by ListToolsForAgent's tool_cache lookups
	// (internal/mcp/manager.go), so returning the registered/prefixed name
	// here (as before) silently broke prompt-preview schemas for any server
	// that was already live-connected when its grants were configured.
	var tools []mcpbridge.ToolInfo
	if h.mgr != nil {
		if infos := h.mgr.ServerToolInfos(srv.Name); len(infos) > 0 {
			tools = infos
		}
	}

	// Fallback: on-demand discovery (returns names + descriptions).
	if len(tools) == 0 && srv.Transport != "" {
		var args []string
		var env, headers map[string]string
		_ = json.Unmarshal(srv.Args, &args)
		_ = json.Unmarshal(srv.Env, &env)
		_ = json.Unmarshal(srv.Headers, &headers)

		discover := true
		// OAuth servers: Authorization must come from the OAuth token. If no valid
		// token is available (not authorized yet), do NOT discover via the server-level
		// header fallback — return empty so the UI reflects "authorization required"
		// instead of exposing tools the user can't actually call.
		if mcpbridge.IsOAuthActive(srv.Settings) {
			token := ""
			if h.oauthProvider != nil {
				tenantID := store.TenantIDFromContext(r.Context())
				// Tool discovery is server-wide (the tool set is the same for every
				// user), so always use the GLOBAL token — matching how srv.Headers/Env
				// here are server-level, regardless of require_user_credentials.
				token, _ = h.oauthProvider.GetValidToken(r.Context(), srv.ID, tenantID, "")
			}
			if token == "" {
				discover = false
			} else {
				if headers == nil {
					headers = make(map[string]string)
				}
				headers["Authorization"] = "Bearer " + token
			}
		}

		if discover {
			discovered, err := mcpbridge.DiscoverTools(r.Context(), srv.Transport, srv.Command, args, env, srv.URL, headers)
			if err != nil {
				slog.Warn("mcp.discover_tools", "server", srv.Name, "error", err)
			} else {
				tools = discovered
			}
		}
	}

	if tools == nil {
		tools = []mcpbridge.ToolInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}
