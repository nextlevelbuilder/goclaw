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
	store          store.VaultStore
	teamAccess     store.TeamAccessStore // nil = skip team membership validation (e.g. lite edition)
	agents         AgentLister           // nil = rescan skips agent resolution
	teams          TeamLister            // nil = rescan skips team resolution
	workspace      string
	eventBus       eventbus.DomainEventBus
	enrichProgress *vault.EnrichProgress // nil = enrichment progress SSE disabled
	rescanMu       sync.Map              // key: tenantID → struct{}, per-tenant concurrency guard
}

// SetEnrichProgress injects the enrichment progress tracker for SSE streaming.
func (h *VaultHandler) SetEnrichProgress(p *vault.EnrichProgress) { h.enrichProgress = p }

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
	// Cross-agent endpoints (agentID = "" from PathValue when no {agentID} in path).
	mux.HandleFunc("GET /v1/vault/documents", h.auth(h.handleListAllDocuments))
	mux.HandleFunc("POST /v1/vault/documents", h.auth(h.handleCreateDocument))
	mux.HandleFunc("GET /v1/vault/documents/{docID}", h.auth(h.handleGetDocument))
	mux.HandleFunc("PUT /v1/vault/documents/{docID}", h.auth(h.handleUpdateDocument))
	mux.HandleFunc("DELETE /v1/vault/documents/{docID}", h.auth(h.handleDeleteDocument))
	mux.HandleFunc("GET /v1/vault/documents/{docID}/links", h.auth(h.handleGetLinks))
	mux.HandleFunc("POST /v1/vault/links", h.auth(h.handleCreateLink))
	mux.HandleFunc("DELETE /v1/vault/links/{linkID}", h.auth(h.handleDeleteLink))
	mux.HandleFunc("POST /v1/vault/links/batch", h.auth(h.handleBatchGetLinks))
	mux.HandleFunc("POST /v1/vault/upload", h.auth(h.handleUpload))
	mux.HandleFunc("POST /v1/vault/rescan", h.auth(h.handleRescan))
	mux.HandleFunc("POST /v1/vault/search", h.auth(h.handleSearchAll))
	mux.HandleFunc("GET /v1/vault/enrichment/status", h.auth(h.handleEnrichmentStatus))
	// Per-agent endpoints (backward compat — same handlers, agentID from path).
	mux.HandleFunc("GET /v1/agents/{agentID}/vault/documents", h.auth(h.handleListDocuments))
	mux.HandleFunc("GET /v1/agents/{agentID}/vault/documents/{docID}", h.auth(h.handleGetDocument))
	mux.HandleFunc("POST /v1/agents/{agentID}/vault/documents", h.auth(h.handleCreateDocument))
	mux.HandleFunc("PUT /v1/agents/{agentID}/vault/documents/{docID}", h.auth(h.handleUpdateDocument))
	mux.HandleFunc("DELETE /v1/agents/{agentID}/vault/documents/{docID}", h.auth(h.handleDeleteDocument))
	mux.HandleFunc("GET /v1/agents/{agentID}/vault/documents/{docID}/links", h.auth(h.handleGetLinks))
	mux.HandleFunc("POST /v1/agents/{agentID}/vault/search", h.auth(h.handleSearch))
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

	// Signal enrichment progress so WS subscribers see running=true immediately.
	if h.enrichProgress != nil && (result.New+result.Updated) > 0 {
		h.enrichProgress.Start(result.New+result.Updated, store.TenantIDFromContext(r.Context()))
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

// handleEnrichmentStatus returns the current enrichment pipeline progress as JSON.
func (h *VaultHandler) handleEnrichmentStatus(w http.ResponseWriter, r *http.Request) {
	if h.enrichProgress == nil {
		writeJSON(w, http.StatusOK, vault.EnrichEvent{})
		return
	}
	writeJSON(w, http.StatusOK, h.enrichProgress.Status())
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
