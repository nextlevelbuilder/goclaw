package http

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// parseImportSections parses the ?include= query param (comma-separated section names).
// Defaults to all sections if empty.
func parseImportSections(raw string) map[string]bool {
	all := map[string]bool{
		"config":          true,
		"context_files":   true,
		"memory":          true,
		"knowledge_graph": true,
		"cron":            true,
		"user_profiles":   true,
		"user_overrides":  true,
		"workspace":       true,
		"team":            true,
		"episodic":        true,
		"evolution":       true,
		"vault":           true,
	}
	if raw == "" {
		return all
	}
	out := make(map[string]bool)
	for s := range strings.SplitSeq(raw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out[s] = true
		}
	}
	return out
}

// canImport checks if userID has permission to import agents (system owner only for now).
func (h *AgentsHandler) canImport(userID string) bool {
	return h.isOwnerUser(userID)
}

// handleImportPreview parses the archive manifest and returns it without importing.
func (h *AgentsHandler) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())

	if !h.canImport(userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgNoAccess, "import"))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImportBodySize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "multipart parse: "+err.Error()))
		return
	}

	f, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "missing 'file' field"))
		return
	}
	defer f.Close()

	arc, err := readImportArchive(f)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "archive parse: "+err.Error()))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"manifest":              arc.manifest,
		"context_files":         len(arc.contextFiles),
		"user_context_files":    len(arc.userContextFiles),
		"memory_docs":           len(arc.memoryGlobal) + countUserMemory(arc.memoryUsers),
		"kg_entities":           len(arc.kgEntities),
		"kg_relations":          len(arc.kgRelations),
		"cron_jobs":             len(arc.cronJobs),
		"user_profiles":         len(arc.userProfiles),
		"user_overrides":        len(arc.userOverrides),
		"workspace_files":       len(arc.workspaceFiles),
		"episodic_summaries":    len(arc.episodicSummaries),
		"evolution_metrics":     len(arc.evolutionMetrics),
		"evolution_suggestions": len(arc.evolutionSuggestions),
		"vault_documents":       len(arc.vaultDocuments),
		"vault_links":           len(arc.vaultLinks),
		"team":                  arc.teamMeta != nil,
	})
}

// handleImport creates a new agent from an uploaded archive.
func (h *AgentsHandler) handleImport(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())

	if !h.canImport(userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgNoAccess, "import agent"))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImportBodySize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "multipart parse: "+err.Error()))
		return
	}

	f, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "missing 'file' field"))
		return
	}
	defer f.Close()

	arc, err := readImportArchive(f)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "archive parse: "+err.Error()))
		return
	}

	stream := r.URL.Query().Get("stream") == "true"
	if stream {
		flusher := initSSE(w)
		if flusher == nil {
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "streaming not supported")
			return
		}
		progressFn := func(ev ProgressEvent) { sendSSE(w, flusher, "progress", ev) }
		summary, importErr := h.doImportNewAgent(r.Context(), r, arc, progressFn)
		if importErr != nil {
			sendSSE(w, flusher, "error", ProgressEvent{Phase: "import", Status: "error", Detail: importErr.Error()})
			return
		}
		sendSSE(w, flusher, "complete", summary)
		return
	}

	summary, err := h.doImportNewAgent(r.Context(), r, arc, nil)
	if err != nil {
		slog.Error("agents.import", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, summary)
}

// handleMergeImport merges archive data into an existing agent.
func (h *AgentsHandler) handleMergeImport(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())

	ag, status, err := h.lookupAccessibleAgent(r)
	if err != nil {
		writeError(w, status, protocol.ErrNotFound, err.Error())
		return
	}
	// Require agent owner or system owner
	if ag.OwnerID != userID && !h.isOwnerUser(userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgNoAccess, "merge import"))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImportBodySize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "multipart parse: "+err.Error()))
		return
	}

	f, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "missing 'file' field"))
		return
	}
	defer f.Close()

	arc, err := readImportArchive(f)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "archive parse: "+err.Error()))
		return
	}

	sections := parseImportSections(r.URL.Query().Get("include"))
	stream := r.URL.Query().Get("stream") == "true"

	if stream {
		flusher := initSSE(w)
		if flusher == nil {
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "streaming not supported")
			return
		}
		progressFn := func(ev ProgressEvent) { sendSSE(w, flusher, "progress", ev) }
		summary, mergeErr := h.doMergeImport(r.Context(), ag, arc, sections, progressFn)
		if mergeErr != nil {
			slog.Error("agents.merge_import.sse", "agent_id", ag.ID, "error", mergeErr)
			sendSSE(w, flusher, "error", map[string]any{"phase": "merge", "detail": mergeErr.Error(), "rolled_back": false})
			return
		}
		sendSSE(w, flusher, "complete", summary)
		return
	}

	summary, err := h.doMergeImport(r.Context(), ag, arc, sections, nil)
	if err != nil {
		slog.Error("agents.merge_import", "agent_id", ag.ID, "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, summary)
}
