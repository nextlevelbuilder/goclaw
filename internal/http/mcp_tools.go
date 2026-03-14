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

	// Try runtime Manager first — returns names only (no descriptions available).
	var tools []mcpbridge.ToolInfo
	if h.mgr != nil {
		if names := h.mgr.ServerToolNames(srv.Name); len(names) > 0 {
			tools = make([]mcpbridge.ToolInfo, len(names))
			for i, n := range names {
				tools[i] = mcpbridge.ToolInfo{Name: n}
			}
		}
	}

	// Fallback: on-demand discovery (returns names + descriptions).
	if len(tools) == 0 && srv.Transport != "" {
		var args []string
		var env, headers map[string]string
		_ = json.Unmarshal(srv.Args, &args)
		_ = json.Unmarshal(srv.Env, &env)
		_ = json.Unmarshal(srv.Headers, &headers)

		discovered, err := mcpbridge.DiscoverTools(r.Context(), srv.Transport, srv.Command, args, env, srv.URL, headers)
		if err != nil {
			slog.Warn("mcp.discover_tools", "server", srv.Name, "error", err)
		} else {
			tools = discovered
		}
	}

	if tools == nil {
		tools = []mcpbridge.ToolInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}
