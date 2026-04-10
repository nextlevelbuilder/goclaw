package http

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// handleListAllDocuments lists vault documents across all agents in tenant.
// Optional query param agent_id to filter by specific agent.
func (h *VaultHandler) handleListAllDocuments(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context())
	agentID := r.URL.Query().Get("agent_id")
	opts := h.parseListOpts(r)

	// Validate team membership if specific team requested.
	if opts.TeamID != nil && *opts.TeamID != "" {
		if !h.validateTeamMembership(r.Context(), w, *opts.TeamID) {
			return
		}
	}
	// Non-owner without team_id filter: show personal + user's teams.
	if opts.TeamID == nil && !store.IsOwnerRole(r.Context()) {
		h.applyNonOwnerTeamScope(r.Context(), &opts)
	}

	docs, err := h.store.ListDocuments(r.Context(), tenantID.String(), agentID, opts)
	if err != nil {
		slog.Warn("vault.list_all failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if docs == nil {
		docs = []store.VaultDocument{}
	}
	total, cntErr := h.store.CountDocuments(r.Context(), tenantID.String(), agentID, opts)
	if cntErr != nil {
		slog.Warn("vault.count failed", "error", cntErr)
	}
	writeJSON(w, http.StatusOK, vaultDocListResponse{Documents: docs, Total: total})
}

// handleListDocuments lists vault documents for a specific agent.
func (h *VaultHandler) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context())
	agentID := r.PathValue("agentID")
	opts := h.parseListOpts(r)

	if opts.TeamID != nil && *opts.TeamID != "" {
		if !h.validateTeamMembership(r.Context(), w, *opts.TeamID) {
			return
		}
	}
	if opts.TeamID == nil && !store.IsOwnerRole(r.Context()) {
		h.applyNonOwnerTeamScope(r.Context(), &opts)
	}

	docs, err := h.store.ListDocuments(r.Context(), tenantID.String(), agentID, opts)
	if err != nil {
		slog.Warn("vault.list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if docs == nil {
		docs = []store.VaultDocument{}
	}
	total, cntErr := h.store.CountDocuments(r.Context(), tenantID.String(), agentID, opts)
	if cntErr != nil {
		slog.Warn("vault.count failed", "error", cntErr)
	}
	writeJSON(w, http.StatusOK, vaultDocListResponse{Documents: docs, Total: total})
}

// handleGetDocument returns a single vault document by ID, scoped to the agent.
func (h *VaultHandler) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context())
	agentID := r.PathValue("agentID")
	docID := r.PathValue("docID")

	doc, err := h.store.GetDocumentByID(r.Context(), tenantID.String(), docID)
	if err != nil {
		slog.Warn("vault.get failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if doc == nil || (agentID != "" && doc.AgentID != nil && *doc.AgentID != agentID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "document not found"})
		return
	}
	// Verify team boundary — non-owner must be team member to view team docs.
	if doc.TeamID != nil && *doc.TeamID != "" && !store.IsOwnerRole(r.Context()) {
		if !h.validateTeamMembership(r.Context(), w, *doc.TeamID) {
			return
		}
	}
	writeJSON(w, http.StatusOK, doc)
}

// handleCreateDocument creates a new vault document.
func (h *VaultHandler) handleCreateDocument(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	tenantID := store.TenantIDFromContext(r.Context())
	agentID := r.PathValue("agentID")

	var body struct {
		Path     string         `json:"path"`
		Title    string         `json:"title"`
		DocType  string         `json:"doc_type"`
		Scope    string         `json:"scope"`
		TeamID   string         `json:"team_id"`
		Metadata map[string]any `json:"metadata"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}
	if body.Path == "" || body.Title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path and title are required"})
		return
	}
	if strings.Contains(body.Path, "..") || strings.HasPrefix(body.Path, "/") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	if body.DocType == "" {
		body.DocType = "note"
	}
	if body.Scope == "" {
		body.Scope = "personal"
	}
	if !validDocType(body.DocType) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid doc_type"})
		return
	}
	if !validScope(body.Scope) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid scope"})
		return
	}

	doc := &store.VaultDocument{
		TenantID: tenantID.String(),
		Path:     body.Path,
		Title:    body.Title,
		DocType:  body.DocType,
		Scope:    body.Scope,
		Metadata: body.Metadata,
	}
	if agentID != "" {
		doc.AgentID = &agentID
	} else if doc.Scope == "personal" {
		doc.Scope = "shared" // no agent → shared scope
	}
	if body.TeamID != "" {
		if !h.validateTeamMembership(r.Context(), w, body.TeamID) {
			return
		}
		doc.TeamID = &body.TeamID
		if body.Scope == "personal" {
			doc.Scope = "team"
		}
	}
	if err := h.store.UpsertDocument(r.Context(), doc); err != nil {
		slog.Warn("vault.create failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Re-fetch by ID (set via RETURNING) — unambiguous even when same path exists across teams.
	created, _ := h.store.GetDocumentByID(r.Context(), tenantID.String(), doc.ID)
	if created != nil {
		writeJSON(w, http.StatusCreated, created)
	} else {
		writeJSON(w, http.StatusCreated, doc)
	}
}

// handleUpdateDocument updates an existing vault document.
func (h *VaultHandler) handleUpdateDocument(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	tenantID := store.TenantIDFromContext(r.Context())
	agentID := r.PathValue("agentID")
	docID := r.PathValue("docID")

	existing, err := h.store.GetDocumentByID(r.Context(), tenantID.String(), docID)
	if err != nil || existing == nil || (agentID != "" && existing.AgentID != nil && *existing.AgentID != agentID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "document not found"})
		return
	}

	var body struct {
		Title    *string        `json:"title"`
		DocType  *string        `json:"doc_type"`
		Scope    *string        `json:"scope"`
		TeamID   *string        `json:"team_id"` // nil=no change, ""=clear, "uuid"=set
		Metadata map[string]any `json:"metadata"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}

	if body.Title != nil {
		existing.Title = *body.Title
	}
	if body.DocType != nil {
		existing.DocType = *body.DocType
	}
	if body.Scope != nil {
		existing.Scope = *body.Scope
	}
	if body.TeamID != nil {
		// Only owner/admin can change team assignment.
		if !store.IsOwnerRole(r.Context()) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only owner can change document team assignment"})
			return
		}
		if *body.TeamID == "" {
			existing.TeamID = nil
			existing.Scope = "personal"
		} else {
			existing.TeamID = body.TeamID
			existing.Scope = "team"
		}
	}
	if body.Metadata != nil {
		existing.Metadata = body.Metadata
	}

	if err := h.store.UpsertDocument(r.Context(), existing); err != nil {
		slog.Warn("vault.update failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	updated, _ := h.store.GetDocumentByID(r.Context(), tenantID.String(), docID)
	if updated != nil {
		writeJSON(w, http.StatusOK, updated)
	} else {
		writeJSON(w, http.StatusOK, existing)
	}
}

// handleDeleteDocument deletes a vault document by ID.
func (h *VaultHandler) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context())
	agentID := r.PathValue("agentID")
	docID := r.PathValue("docID")

	existing, err := h.store.GetDocumentByID(r.Context(), tenantID.String(), docID)
	if err != nil || existing == nil || (agentID != "" && existing.AgentID != nil && *existing.AgentID != agentID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "document not found"})
		return
	}

	// Verify team boundary before deletion.
	if existing.TeamID != nil && *existing.TeamID != "" && !store.IsOwnerRole(r.Context()) {
		if !h.validateTeamMembership(r.Context(), w, *existing.TeamID) {
			return
		}
	}

	// DeleteDocument without RunContext applies no team_id filter (broad match on tenant+agent+path).
	// This is safe because we pre-validated team membership above and use server-derived existing.Path.
	// Use the doc's actual agent_id (may be empty for team/shared docs).
	deleteAgentID := ""
	if existing.AgentID != nil {
		deleteAgentID = *existing.AgentID
	}
	if err := h.store.DeleteDocument(r.Context(), tenantID.String(), deleteAgentID, existing.Path); err != nil {
		slog.Warn("vault.delete failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
