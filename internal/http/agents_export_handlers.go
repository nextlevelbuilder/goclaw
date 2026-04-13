package http

import (
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// handleExportPreview returns lightweight counts per exportable section.
func (h *AgentsHandler) handleExportPreview(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())

	ag, status, err := h.lookupAccessibleAgent(r)
	if err != nil {
		writeError(w, status, protocol.ErrNotFound, err.Error())
		return
	}
	if !h.canExport(ag, userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgNoAccess, "agent"))
		return
	}

	counts, err := pg.ExportPreviewCounts(r.Context(), h.db, ag.ID)
	if err != nil {
		slog.Error("agents.export.preview", "agent_id", ag.ID, "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "failed to fetch preview counts"))
		return
	}

	// Count workspace files (filesystem, not DB)
	var workspaceFiles int
	if ag.Workspace != "" {
		wsPath := config.ExpandHome(ag.Workspace)
		if info, statErr := os.Stat(wsPath); statErr == nil && info.IsDir() {
			filepath.WalkDir(wsPath, func(_ string, d fs.DirEntry, _ error) error { //nolint:errcheck
				if d.IsDir() || strings.HasPrefix(d.Name(), ".") || d.Type()&fs.ModeSymlink != 0 {
					return nil
				}
				if fi, err := d.Info(); err == nil && fi.Size() <= maxWorkspaceFileSize {
					workspaceFiles++
				}
				return nil
			})
		}
	}

	type previewResponse struct {
		*pg.ExportPreview
		WorkspaceFiles int `json:"workspace_files"`
	}
	writeJSON(w, http.StatusOK, previewResponse{ExportPreview: counts, WorkspaceFiles: workspaceFiles})
}

// handleExport dispatches to SSE streaming or direct download based on ?stream= param.
func (h *AgentsHandler) handleExport(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())

	ag, status, err := h.lookupAccessibleAgent(r)
	if err != nil {
		writeError(w, status, protocol.ErrNotFound, err.Error())
		return
	}
	if !h.canExport(ag, userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgNoAccess, "agent"))
		return
	}

	sections := parseExportSections(r.URL.Query().Get("sections"))
	stream := r.URL.Query().Get("stream") == "true"

	if stream {
		h.handleExportSSE(w, r, ag, sections)
	} else {
		h.handleExportDirect(w, r, ag, sections)
	}
}

// handleExportDownload serves a previously-prepared export archive by token.
func (h *AgentsHandler) handleExportDownload(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())

	token := r.PathValue("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "token"))
		return
	}

	entry, ok := lookupExportToken(token)
	if !ok {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "export token", token))
		return
	}

	// Verify token belongs to requesting user (or system owner)
	if entry.userID != userID && !h.isOwnerUser(userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgNoAccess, "export download"))
		return
	}

	f, err := os.Open(entry.filePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, entry.fileName))
	io.Copy(w, f) //nolint:errcheck
}

// handleExportSSE streams build progress as SSE events then sends a download token on completion.
func (h *AgentsHandler) handleExportSSE(w http.ResponseWriter, r *http.Request, ag *store.AgentData, sections map[string]bool) {
	flusher := initSSE(w)
	if flusher == nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "streaming not supported")
		return
	}

	tmpFile, err := os.CreateTemp("", "goclaw-export-*.tar.gz")
	if err != nil {
		sendSSE(w, flusher, "error", ProgressEvent{Phase: "init", Status: "error", Detail: "failed to create temp file"})
		return
	}
	tmpPath := tmpFile.Name()

	progressFn := func(ev ProgressEvent) {
		sendSSE(w, flusher, "progress", ev)
	}

	buildErr := h.writeExportArchive(r.Context(), tmpFile, ag, sections, progressFn)
	tmpFile.Close()

	if buildErr != nil {
		slog.Error("agents.export.sse", "agent_id", ag.ID, "error", buildErr)
		sendSSE(w, flusher, "error", ProgressEvent{Phase: "archive", Status: "error", Detail: buildErr.Error()})
		os.Remove(tmpPath)
		return
	}

	userID := store.UserIDFromContext(r.Context())
	token := h.generateExportToken(ag.ID.String(), userID, tmpPath, exportFileName(ag.AgentKey))
	sendSSE(w, flusher, "complete", map[string]string{
		"download_url": "/v1/agents/" + ag.ID.String() + "/export/download/" + token,
	})
}

// handleExportDirect streams the tar.gz archive directly to the response body.
func (h *AgentsHandler) handleExportDirect(w http.ResponseWriter, r *http.Request, ag *store.AgentData, sections map[string]bool) {
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, exportFileName(ag.AgentKey)))

	if err := h.writeExportArchive(r.Context(), w, ag, sections, nil); err != nil {
		// Headers already written; log only — cannot send JSON error at this point.
		slog.Error("agents.export.direct", "agent_id", ag.ID, "error", err)
	}
}

// canExport checks if userID has permission to export the given agent.
// Export is restricted to agent owner and system owner by design.
func (h *AgentsHandler) canExport(ag *store.AgentData, userID string) bool {
	if ag.OwnerID == userID {
		return true
	}
	if h.isOwnerUser(userID) {
		return true
	}
	return false
}

// generateExportToken is an alias for storeExportToken (backward compat within AgentsHandler).
func (h *AgentsHandler) generateExportToken(entityID, userID, filePath, fileName string) string {
	return storeExportToken(entityID, userID, filePath, fileName)
}

// parseExportSections parses the ?sections= query param.
// Defaults to config + context_files when empty.
// Use sections=all to include every section including episodic.
func parseExportSections(raw string) map[string]bool {
	if raw == "" {
		return map[string]bool{"config": true, "context_files": true}
	}
	if strings.TrimSpace(raw) == "all" {
		return allExportSections
	}
	out := make(map[string]bool)
	for s := range strings.SplitSeq(raw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out[s] = true
		}
	}
	return out
}

// exportFileName builds the tar.gz filename for a given agent key.
// Strips characters unsafe for Content-Disposition headers.
func exportFileName(agentKey string) string {
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, agentKey)
	return fmt.Sprintf("agent-%s-%s.tar.gz", safe, time.Now().UTC().Format("20060102"))
}
