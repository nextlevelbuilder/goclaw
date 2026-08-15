package http

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// VaultGraphHandler serves lightweight graph endpoints for visualization.
type VaultGraphHandler struct {
	vaultGraph store.VaultGraphStore
	kgGraph    store.KGGraphStore
	teamAccess store.TeamAccessStore // nil = skip team validation (lite edition)
}

// NewVaultGraphHandler creates a new graph handler.
func NewVaultGraphHandler(vg store.VaultGraphStore, kg store.KGGraphStore, ta store.TeamAccessStore) *VaultGraphHandler {
	return &VaultGraphHandler{vaultGraph: vg, kgGraph: kg, teamAccess: ta}
}

// RegisterRoutes registers graph visualization routes.
func (h *VaultGraphHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/vault/graph", h.auth(h.handleVaultGraph))
	if h.kgGraph != nil {
		mux.HandleFunc("GET /v1/agents/{agentID}/kg/graph/compact", h.auth(h.handleKGGraphCompact))
	}
}

func (h *VaultGraphHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

// --- vault graph ---

type vaultGraphResponse struct {
	Nodes      []store.GraphNode `json:"nodes"`
	Edges      []store.GraphEdge `json:"edges"`
	TotalNodes int               `json:"total_nodes"`
	TotalEdges int               `json:"total_edges"`
}

func (h *VaultGraphHandler) handleVaultGraph(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := store.TenantIDFromContext(ctx).String()

	agentID := r.URL.Query().Get("agent_id")
	teamID := r.URL.Query().Get("team_id")

	limit := parseGraphLimit(r.URL.Query().Get("limit"))

	opts := store.VaultGraphListOptions{Limit: limit}

	// Team scope validation.
	if teamID != "" {
		if !h.validateTeamMembership(ctx, w, teamID) {
			return
		}
		opts.TeamID = &teamID
	} else if !store.IsOwnerRole(ctx) {
		h.applyNonOwnerTeamScope(ctx, &opts)
	}

	nodes, totalNodes, err := h.vaultGraph.ListGraphNodes(ctx, tenantID, agentID, opts)
	if err != nil {
		slog.Warn("vault_graph.nodes failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if nodes == nil {
		nodes = []store.GraphNode{}
	}

	edges, totalEdges, err := h.vaultGraph.ListGraphEdges(ctx, tenantID, agentID, opts)
	if err != nil {
		slog.Warn("vault_graph.edges failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if edges == nil {
		edges = []store.GraphEdge{}
	}

	writeJSON(w, http.StatusOK, vaultGraphResponse{
		Nodes:      nodes,
		Edges:      edges,
		TotalNodes: totalNodes,
		TotalEdges: totalEdges,
	})
}

// --- KG graph compact ---

type kgGraphCompactResponse struct {
	Nodes      []store.KGGraphNode `json:"nodes"`
	Edges      []store.KGGraphEdge `json:"edges"`
	TotalNodes int                 `json:"total_nodes"`
	TotalEdges int                 `json:"total_edges"`
}

func (h *VaultGraphHandler) handleKGGraphCompact(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agentID := r.PathValue("agentID")
	userID := r.URL.Query().Get("user_id")

	limit := parseGraphLimit(r.URL.Query().Get("limit"))

	nodes, totalNodes, err := h.kgGraph.ListKGGraphNodes(ctx, agentID, userID, limit)
	if err != nil {
		slog.Warn("kg_graph_compact.nodes failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if nodes == nil {
		nodes = []store.KGGraphNode{}
	}

	edges, totalEdges, err := h.kgGraph.ListKGGraphEdges(ctx, agentID, userID, limit*3)
	if err != nil {
		slog.Warn("kg_graph_compact.edges failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if edges == nil {
		edges = []store.KGGraphEdge{}
	}

	writeJSON(w, http.StatusOK, kgGraphCompactResponse{
		Nodes:      nodes,
		Edges:      edges,
		TotalNodes: totalNodes,
		TotalEdges: totalEdges,
	})
}

// --- helpers ---

func parseGraphLimit(s string) int {
	limit, _ := strconv.Atoi(s)
	if limit <= 0 {
		limit = 2000
	}
	if limit > 10000 {
		limit = 10000
	}
	return limit
}

// validateTeamMembership checks team membership for non-owner users.
func (h *VaultGraphHandler) validateTeamMembership(ctx context.Context, w http.ResponseWriter, teamID string) bool {
	if store.IsOwnerRole(ctx) {
		return true
	}
	if h.teamAccess == nil {
		return true
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

// applyNonOwnerTeamScope restricts a non-owner graph query to personal + user's teams.
func (h *VaultGraphHandler) applyNonOwnerTeamScope(ctx context.Context, opts *store.VaultGraphListOptions) {
	if h.teamAccess == nil {
		return
	}
	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		return
	}
	teams, err := h.teamAccess.ListUserTeams(ctx, userID)
	if err != nil || len(teams) == 0 {
		empty := ""
		opts.TeamID = &empty
		return
	}
	ids := make([]string, len(teams))
	for i, t := range teams {
		ids[i] = t.ID.String()
	}
	opts.TeamIDs = ids
}
