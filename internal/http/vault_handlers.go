package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// VaultHandler serves Knowledge Vault document and link endpoints.
type VaultHandler struct {
	store store.VaultStore
}

func NewVaultHandler(s store.VaultStore) *VaultHandler {
	return &VaultHandler{store: s}
}

func (h *VaultHandler) RegisterRoutes(mux *http.ServeMux) {
	// Cross-agent endpoint (agent_id optional query param).
	mux.HandleFunc("GET /v1/vault/documents", h.auth(h.handleListAllDocuments))
	// Per-agent endpoints.
	mux.HandleFunc("GET /v1/agents/{agentID}/vault/documents", h.auth(h.handleListDocuments))
	mux.HandleFunc("GET /v1/agents/{agentID}/vault/documents/{docID}", h.auth(h.handleGetDocument))
	mux.HandleFunc("POST /v1/agents/{agentID}/vault/search", h.auth(h.handleSearch))
	mux.HandleFunc("GET /v1/agents/{agentID}/vault/documents/{docID}/links", h.auth(h.handleGetLinks))
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
	return store.VaultListOptions{
		Scope:    r.URL.Query().Get("scope"),
		DocTypes: splitCSV(r.URL.Query().Get("doc_type")),
		Limit:    limit,
		Offset:   offset,
	}
}

// handleListAllDocuments lists vault documents across all agents in tenant.
// Optional query param agent_id to filter by specific agent.
func (h *VaultHandler) handleListAllDocuments(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context())
	agentID := r.URL.Query().Get("agent_id")
	opts := h.parseListOpts(r)

	docs, err := h.store.ListDocuments(r.Context(), tenantID.String(), agentID, opts)
	if err != nil {
		slog.Warn("vault.list_all failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if docs == nil {
		docs = []store.VaultDocument{}
	}
	writeJSON(w, http.StatusOK, docs)
}

// handleListDocuments lists vault documents for a specific agent.
func (h *VaultHandler) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context())
	agentID := r.PathValue("agentID")
	opts := h.parseListOpts(r)

	docs, err := h.store.ListDocuments(r.Context(), tenantID.String(), agentID, opts)
	if err != nil {
		slog.Warn("vault.list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if docs == nil {
		docs = []store.VaultDocument{}
	}
	writeJSON(w, http.StatusOK, docs)
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
	if doc == nil || doc.AgentID != agentID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "document not found"})
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// handleSearch runs hybrid FTS+vector search on vault documents.
func (h *VaultHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context())
	agentID := r.PathValue("agentID")

	var body struct {
		Query      string   `json:"query"`
		Scope      string   `json:"scope"`
		DocTypes   []string `json:"doc_types"`
		MaxResults int      `json:"max_results"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if body.Query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
		return
	}
	if body.MaxResults <= 0 {
		body.MaxResults = 10
	}

	results, err := h.store.Search(r.Context(), store.VaultSearchOptions{
		Query:      body.Query,
		AgentID:    agentID,
		TenantID:   tenantID.String(),
		Scope:      body.Scope,
		DocTypes:   body.DocTypes,
		MaxResults: body.MaxResults,
	})
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
		backlinks = []store.VaultLink{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"outlinks":  outLinks,
		"backlinks": backlinks,
	})
}

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
