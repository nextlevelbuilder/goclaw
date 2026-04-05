package http

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// handleMCPImport imports a MCP servers archive (POST /v1/mcp/import).
func (h *MCPHandler) handleMCPImport(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())

	if h.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "db not configured")})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImportBodySize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "multipart parse: "+err.Error())})
		return
	}

	f, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, "missing 'file' field")})
		return
	}
	defer f.Close()

	stream := r.URL.Query().Get("stream") == "true"
	if stream {
		flusher := initSSE(w)
		if flusher == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
			return
		}
		progressFn := func(ev ProgressEvent) { sendSSE(w, flusher, "progress", ev) }
		summary, importErr := h.doMCPImport(r.Context(), f, userID, progressFn)
		if importErr != nil {
			sendSSE(w, flusher, "error", ProgressEvent{Phase: "import", Status: "error", Detail: importErr.Error()})
			return
		}
		sendSSE(w, flusher, "complete", summary)
		return
	}

	summary, err := h.doMCPImport(r.Context(), f, userID, nil)
	if err != nil {
		slog.Error("mcp.import", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}
	writeJSON(w, http.StatusCreated, summary)
}

// MCPImportSummary is returned after a successful MCP import.
type MCPImportSummary struct {
	ServersImported int `json:"servers_imported"`
	ServersSkipped  int `json:"servers_skipped"`
	GrantsApplied   int `json:"grants_applied"`
}

// doMCPImport parses the MCP tar.gz and creates servers + grants.
func (h *MCPHandler) doMCPImport(ctx context.Context, r io.Reader, userID string, progressFn func(ProgressEvent)) (*MCPImportSummary, error) {
	entries, err := readTarGzEntries(r)
	if err != nil {
		return nil, err
	}

	summary := &MCPImportSummary{}

	// Parse servers
	var servers []pg.MCPServerExport
	if raw, ok := entries["servers.jsonl"]; ok {
		servers, err = parseJSONL[pg.MCPServerExport](raw)
		if err != nil {
			return nil, fmt.Errorf("parse servers.jsonl: %w", err)
		}
	}

	// Parse grants
	var grants []pg.MCPGrantWithKey
	if raw, ok := entries["grants.jsonl"]; ok {
		grants, err = parseJSONL[pg.MCPGrantWithKey](raw)
		if err != nil {
			slog.Warn("mcp.import: parse grants.jsonl failed", "error", err)
		}
	}

	// Import servers — build name → uuid.UUID map for grant wiring
	tid := importTenantID(ctx)
	serverNameToUUID := make(map[string]uuid.UUID, len(servers))

	for i, srv := range servers {
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "server", Status: "running", Current: i + 1, Total: len(servers), Detail: srv.Name})
		}
		id, created, err := pg.ImportMCPServer(ctx, h.db, srv, userID)
		if err != nil {
			slog.Warn("mcp.import: create server", "name", srv.Name, "error", err)
			continue
		}
		serverNameToUUID[srv.Name] = id
		if created {
			summary.ServersImported++
		} else {
			summary.ServersSkipped++
		}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "server", Status: "done", Detail: srv.Name})
		}
	}

	h.emitCacheInvalidate()

	// Apply grants
	for i, g := range grants {
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "grant", Status: "running", Current: i + 1, Total: len(grants)})
		}

		serverID, ok := serverNameToUUID[g.ServerName]
		if !ok {
			// Server may have pre-existed — look it up (tenant-scoped)
			if err := h.db.QueryRowContext(ctx,
				"SELECT id FROM mcp_servers WHERE name = $1 AND tenant_id = $2", g.ServerName, tid,
			).Scan(&serverID); err != nil {
				slog.Warn("mcp.import.grant: server not found", "server", g.ServerName)
				continue
			}
		}

		var agentID uuid.UUID
		if err := h.db.QueryRowContext(ctx,
			"SELECT id FROM agents WHERE agent_key = $1 AND tenant_id = $2", g.AgentKey, tid,
		).Scan(&agentID); err != nil {
			slog.Warn("mcp.import.grant: agent not found", "key", g.AgentKey)
			continue
		}

		if err := pg.ImportMCPGrant(ctx, h.db, serverID, agentID, g, userID); err != nil {
			slog.Warn("mcp.import.grant: insert", "server", g.ServerName, "agent", g.AgentKey, "error", err)
			continue
		}
		summary.GrantsApplied++
	}

	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "grants", Status: "done", Current: summary.GrantsApplied, Total: len(grants)})
	}

	return summary, nil
}
