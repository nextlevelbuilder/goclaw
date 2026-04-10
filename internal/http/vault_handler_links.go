package http

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// handleSearchAll runs tenant-wide search (agent_id optional in body).
func (h *VaultHandler) handleSearchAll(w http.ResponseWriter, r *http.Request) {
	h.doSearch(w, r, "")
}

// handleSearch runs hybrid FTS+vector search scoped to a specific agent.
func (h *VaultHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	h.doSearch(w, r, r.PathValue("agentID"))
}

// doSearch is the shared search implementation for both per-agent and tenant-wide endpoints.
func (h *VaultHandler) doSearch(w http.ResponseWriter, r *http.Request, agentID string) {
	locale := extractLocale(r)
	tenantID := store.TenantIDFromContext(r.Context())

	var body struct {
		Query      string   `json:"query"`
		AgentID    string   `json:"agent_id"`
		Scope      string   `json:"scope"`
		DocTypes   []string `json:"doc_types"`
		MaxResults int      `json:"max_results"`
		TeamID     string   `json:"team_id"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}
	if body.Query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
		return
	}
	if body.MaxResults <= 0 {
		body.MaxResults = 10
	}
	// Body agent_id only used when path doesn't provide one (tenant-wide endpoint).
	if agentID == "" {
		agentID = body.AgentID
	}

	searchOpts := store.VaultSearchOptions{
		Query:      body.Query,
		AgentID:    agentID,
		TenantID:   tenantID.String(),
		Scope:      body.Scope,
		DocTypes:   body.DocTypes,
		MaxResults: body.MaxResults,
	}
	if body.TeamID != "" {
		if !h.validateTeamMembership(r.Context(), w, body.TeamID) {
			return
		}
		searchOpts.TeamID = &body.TeamID
	} else if !store.IsOwnerRole(r.Context()) {
		if ids := h.userAccessibleTeamIDs(r.Context()); len(ids) > 0 {
			searchOpts.TeamIDs = ids
		} else {
			empty := ""
			searchOpts.TeamID = &empty
		}
	}

	results, err := h.store.Search(r.Context(), searchOpts)
	if err != nil {
		slog.Warn("vault.search failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if results == nil {
		results = []store.VaultSearchResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

// handleGetLinks returns outgoing links and backlinks for a vault document.
func (h *VaultHandler) handleGetLinks(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context())
	_ = r.PathValue("agentID") // agent scoping done at document level
	docID := r.PathValue("docID")

	outLinks, err := h.store.GetOutLinks(r.Context(), tenantID.String(), docID)
	if err != nil {
		slog.Warn("vault.outlinks failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	backlinks, err := h.store.GetBacklinks(r.Context(), tenantID.String(), docID)
	if err != nil {
		slog.Warn("vault.backlinks failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if outLinks == nil {
		outLinks = []store.VaultLink{}
	}
	if backlinks == nil {
		backlinks = []store.VaultBacklink{}
	}

	// Filter backlinks by team boundary — derive team context from the target document
	// itself (not a query param) so clients don't need to supply it correctly.
	isOwner := store.IsOwnerRole(r.Context())
	if !isOwner {
		targetDoc, _ := h.store.GetDocumentByID(r.Context(), tenantID.String(), docID)
		var currentTeamID string
		if targetDoc != nil && targetDoc.TeamID != nil {
			currentTeamID = *targetDoc.TeamID
		}
		filtered := make([]store.VaultBacklink, 0, len(backlinks))
		for _, bl := range backlinks {
			if currentTeamID != "" {
				if bl.TeamID != nil && *bl.TeamID != currentTeamID {
					continue
				}
			} else {
				if bl.TeamID != nil && *bl.TeamID != "" {
					continue
				}
			}
			filtered = append(filtered, bl)
		}
		backlinks = filtered
	}

	// Resolve target doc titles for outlinks so the UI can display names instead of IDs.
	docNames := make(map[string]string, len(outLinks))
	for _, l := range outLinks {
		if _, seen := docNames[l.ToDocID]; seen {
			continue
		}
		if d, err := h.store.GetDocumentByID(r.Context(), tenantID.String(), l.ToDocID); err == nil && d != nil {
			name := d.Title
			if name == "" {
				if idx := strings.LastIndex(d.Path, "/"); idx >= 0 {
					name = d.Path[idx+1:]
				} else {
					name = d.Path
				}
			}
			docNames[l.ToDocID] = name
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"outlinks":  outLinks,
		"backlinks": backlinks,
		"doc_names": docNames,
	})
}

// handleCreateLink creates a link between two vault documents.
func (h *VaultHandler) handleCreateLink(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	tenantID := store.TenantIDFromContext(r.Context())

	var body struct {
		FromDocID string `json:"from_doc_id"`
		ToDocID   string `json:"to_doc_id"`
		LinkType  string `json:"link_type"`
		Context   string `json:"context"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}
	if body.FromDocID == "" || body.ToDocID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from_doc_id and to_doc_id are required"})
		return
	}
	if body.LinkType == "" {
		body.LinkType = "reference"
	}

	// Verify both docs exist, same tenant, and at least source belongs to this agent.
	agentID := r.PathValue("agentID")
	from, _ := h.store.GetDocumentByID(r.Context(), tenantID.String(), body.FromDocID)
	to, _ := h.store.GetDocumentByID(r.Context(), tenantID.String(), body.ToDocID)
	if from == nil || to == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "one or both documents not found"})
		return
	}
	if agentID != "" && from.AgentID != nil && *from.AgentID != agentID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "source document does not belong to this agent"})
		return
	}
	// Block cross-team linking (both team docs must be in same team).
	if from.TeamID != nil && to.TeamID != nil && *from.TeamID != *to.TeamID {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot link documents from different teams"})
		return
	}

	link := &store.VaultLink{
		FromDocID: body.FromDocID,
		ToDocID:   body.ToDocID,
		LinkType:  body.LinkType,
		Context:   body.Context,
	}
	if err := h.store.CreateLink(r.Context(), link); err != nil {
		slog.Warn("vault.create_link failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, link)
}

// handleBatchGetLinks returns all outlinks for a batch of doc IDs in one query.
func (h *VaultHandler) handleBatchGetLinks(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	tenantID := store.TenantIDFromContext(r.Context())

	var body struct {
		DocIDs []string `json:"doc_ids"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}
	if len(body.DocIDs) == 0 {
		writeJSON(w, http.StatusOK, []store.VaultLink{})
		return
	}
	if len(body.DocIDs) > 500 {
		body.DocIDs = body.DocIDs[:500]
	}

	links, err := h.store.GetOutLinksBatch(r.Context(), tenantID.String(), body.DocIDs)
	if err != nil {
		slog.Warn("vault.batch_links failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if links == nil {
		links = []store.VaultLink{}
	}
	writeJSON(w, http.StatusOK, links)
}

// handleDeleteLink deletes a vault link.
func (h *VaultHandler) handleDeleteLink(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context())
	linkID := r.PathValue("linkID")

	if err := h.store.DeleteLink(r.Context(), tenantID.String(), linkID); err != nil {
		slog.Warn("vault.delete_link failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
