package http

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/vault"
)

// vaultDocListResponse wraps the document list with total count for pagination.
type vaultDocListResponse struct {
	Documents []store.VaultDocument `json:"documents"`
	Total     int                   `json:"total"`
}

// AgentLister is the subset of AgentStore needed by VaultHandler (rescan agent_key→UUID mapping).
type AgentLister interface {
	List(ctx context.Context, ownerID string) ([]store.AgentData, error)
}

// TeamLister is the subset of TeamStore needed by VaultHandler (rescan team validation).
type TeamLister interface {
	ListTeams(ctx context.Context) ([]store.TeamData, error)
}

// VaultHandler serves Knowledge Vault document and link endpoints.
type VaultHandler struct {
	store      store.VaultStore
	teamAccess store.TeamAccessStore // nil = skip team membership validation (e.g. lite edition)
	agents     AgentLister           // nil = rescan skips agent resolution
	teams      TeamLister            // nil = rescan skips team resolution
	workspace  string
	eventBus   eventbus.DomainEventBus
	rescanMu   sync.Map // key: tenantID → struct{}, per-tenant concurrency guard
}

func NewVaultHandler(s store.VaultStore, ta store.TeamAccessStore, workspace string, bus eventbus.DomainEventBus, agents AgentLister, teams TeamLister) *VaultHandler {
	return &VaultHandler{store: s, teamAccess: ta, agents: agents, teams: teams, workspace: workspace, eventBus: bus}
}

// validateTeamMembership checks that the requesting user belongs to the given team.
// Owner role bypasses this check. Returns false and writes 403 if unauthorized.
func (h *VaultHandler) validateTeamMembership(ctx context.Context, w http.ResponseWriter, teamID string) bool {
	if store.IsOwnerRole(ctx) {
		return true
	}
	if h.teamAccess == nil {
		return true // no team store = skip validation (lite edition)
	}
	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "user identity required"})
		return false
	}
	tid, err := uuid.Parse(teamID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid team_id"})
		return false
	}
	ok, err := h.teamAccess.HasTeamAccess(ctx, tid, userID)
	if err != nil || !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "not a member of the specified team"})
		return false
	}
	return true
}

// userAccessibleTeamIDs returns the team IDs accessible by the current non-owner user.
// Returns nil if no teams are found or team store is unavailable.
func (h *VaultHandler) userAccessibleTeamIDs(ctx context.Context) []string {
	userID := store.UserIDFromContext(ctx)
	if userID == "" || h.teamAccess == nil {
		return nil
	}
	teams, err := h.teamAccess.ListUserTeams(ctx, userID)
	if err != nil || len(teams) == 0 {
		return nil
	}
	ids := make([]string, len(teams))
	for i, t := range teams {
		ids[i] = t.ID.String()
	}
	return ids
}

// applyNonOwnerTeamScope restricts a non-owner vault list to personal + user's teams.
func (h *VaultHandler) applyNonOwnerTeamScope(ctx context.Context, opts *store.VaultListOptions) {
	if ids := h.userAccessibleTeamIDs(ctx); len(ids) > 0 {
		opts.TeamIDs = ids
	} else {
		empty := ""
		opts.TeamID = &empty
	}
}

func (h *VaultHandler) RegisterRoutes(mux *http.ServeMux) {
	// Cross-agent endpoint (agent_id optional query param).
	mux.HandleFunc("GET /v1/vault/documents", h.auth(h.handleListAllDocuments))
	// Per-agent endpoints.
	mux.HandleFunc("GET /v1/agents/{agentID}/vault/documents", h.auth(h.handleListDocuments))
	mux.HandleFunc("GET /v1/agents/{agentID}/vault/documents/{docID}", h.auth(h.handleGetDocument))
	mux.HandleFunc("POST /v1/agents/{agentID}/vault/documents", h.auth(h.handleCreateDocument))
	mux.HandleFunc("PUT /v1/agents/{agentID}/vault/documents/{docID}", h.auth(h.handleUpdateDocument))
	mux.HandleFunc("DELETE /v1/agents/{agentID}/vault/documents/{docID}", h.auth(h.handleDeleteDocument))
	mux.HandleFunc("POST /v1/vault/rescan", h.auth(h.handleRescan))
	mux.HandleFunc("POST /v1/vault/search", h.auth(h.handleSearchAll))
	mux.HandleFunc("POST /v1/agents/{agentID}/vault/search", h.auth(h.handleSearch))
	mux.HandleFunc("GET /v1/agents/{agentID}/vault/documents/{docID}/links", h.auth(h.handleGetLinks))
	mux.HandleFunc("POST /v1/agents/{agentID}/vault/links", h.auth(h.handleCreateLink))
	mux.HandleFunc("DELETE /v1/agents/{agentID}/vault/links/{linkID}", h.auth(h.handleDeleteLink))
}

func (h *VaultHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

func (h *VaultHandler) parseListOpts(r *http.Request) store.VaultListOptions {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 20
	}
	if limit > 500 {
		limit = 500
	}
	opts := store.VaultListOptions{
		Scope:    r.URL.Query().Get("scope"),
		DocTypes: splitCSV(r.URL.Query().Get("doc_type")),
		Limit:    limit,
		Offset:   offset,
	}
	if teamID := r.URL.Query().Get("team_id"); teamID != "" {
		opts.TeamID = &teamID
	}
	return opts
}

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
	if doc == nil || (doc.AgentID != nil && *doc.AgentID != agentID) {
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

	writeJSON(w, http.StatusOK, map[string]any{
		"outlinks":  outLinks,
		"backlinks": backlinks,
	})
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
		AgentID:  &agentID,
		Path:     body.Path,
		Title:    body.Title,
		DocType:  body.DocType,
		Scope:    body.Scope,
		Metadata: body.Metadata,
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
	if err != nil || existing == nil || (existing.AgentID != nil && *existing.AgentID != agentID) {
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

// handleDeleteDocument deletes a vault document and its links.
func (h *VaultHandler) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context())
	agentID := r.PathValue("agentID")
	docID := r.PathValue("docID")

	existing, err := h.store.GetDocumentByID(r.Context(), tenantID.String(), docID)
	if err != nil || existing == nil || (existing.AgentID != nil && *existing.AgentID != agentID) {
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
	if from.AgentID != nil && *from.AgentID != agentID {
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

// handleRescan walks the entire tenant workspace and registers missing/changed files in vault.
// Infers agent/team ownership from directory structure: agents/{key}/, teams/{uuid}/, or root shared.
func (h *VaultHandler) handleRescan(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context()).String()

	// Per-tenant concurrency guard.
	if _, loaded := h.rescanMu.LoadOrStore(tenantID, struct{}{}); loaded {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "rescan already in progress"})
		return
	}
	defer h.rescanMu.Delete(tenantID)

	wsPath := h.resolveTenantWorkspace(r.Context())
	if wsPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workspace not available"})
		return
	}

	// Build agent_key→UUID map and team UUID set for path inference.
	agentMap, teamSet := h.buildRescanMaps(r.Context())

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	result, err := vault.RescanWorkspace(ctx, vault.RescanParams{
		TenantID:  tenantID,
		Workspace: wsPath,
		AgentMap:  agentMap,
		TeamSet:   teamSet,
	}, h.store, h.eventBus)
	if err != nil {
		slog.Warn("vault.rescan failed", "tenant", tenantID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// resolveTenantWorkspace returns the tenant-scoped workspace root.
func (h *VaultHandler) resolveTenantWorkspace(ctx context.Context) string {
	if h.workspace == "" {
		return ""
	}
	tenantID := store.TenantIDFromContext(ctx)
	slug := store.TenantSlugFromContext(ctx)
	return config.TenantWorkspace(h.workspace, tenantID, slug)
}

// buildRescanMaps pre-loads agent_key→UUID and team UUID sets for the current tenant.
func (h *VaultHandler) buildRescanMaps(ctx context.Context) (map[string]string, map[string]bool) {
	agentMap := make(map[string]string)
	teamSet := make(map[string]bool)

	if h.agents != nil {
		agents, err := h.agents.List(ctx, "")
		if err == nil {
			for _, a := range agents {
				agentMap[a.AgentKey] = a.ID.String()
			}
		}
	}
	if h.teams != nil {
		teams, err := h.teams.ListTeams(ctx)
		if err == nil {
			for _, t := range teams {
				teamSet[t.ID.String()] = true
			}
		}
	}
	return agentMap, teamSet
}

var allowedDocTypes = map[string]bool{"context": true, "memory": true, "note": true, "skill": true, "episodic": true, "media": true}
var allowedScopes = map[string]bool{"personal": true, "team": true, "shared": true}

func validDocType(dt string) bool { return allowedDocTypes[dt] }
func validScope(s string) bool    { return allowedScopes[s] }

// splitCSV splits a comma-separated string into a non-empty slice. Returns nil for empty input.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	return result
}
