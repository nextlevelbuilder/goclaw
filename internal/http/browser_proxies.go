package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/browser"
)

// BrowserProxiesHandler provides HTTP endpoints for managing the browser proxy pool.
type BrowserProxiesHandler struct {
	proxy  *browser.ProxyManager
	logger *slog.Logger
}

// NewBrowserProxiesHandler creates a BrowserProxiesHandler.
func NewBrowserProxiesHandler(pm *browser.ProxyManager, l *slog.Logger) *BrowserProxiesHandler {
	if l == nil {
		l = slog.Default()
	}
	return &BrowserProxiesHandler{proxy: pm, logger: l}
}

// RegisterRoutes registers proxy pool HTTP routes.
func (h *BrowserProxiesHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/browser/proxies", requireAuth("", h.handleList))
	mux.HandleFunc("POST /v1/browser/proxies", requireAuth("", h.handleCreate))
	mux.HandleFunc("DELETE /v1/browser/proxies/{id}", requireAuth("", h.handleDelete))
	mux.HandleFunc("PATCH /v1/browser/proxies/{id}/toggle", requireAuth("", h.handleToggle))
	mux.HandleFunc("POST /v1/browser/proxies/health", requireAuth("", h.handleHealthCheck))
}

// handleList returns all proxies for the tenant with passwords masked.
func (h *BrowserProxiesHandler) handleList(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context()).String()
	proxies, err := h.proxy.List(r.Context(), tenantID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Mask passwords in response
	type proxyResponse struct {
		ID             string  `json:"id"`
		Name           string  `json:"name"`
		URL            string  `json:"url"`
		Username       string  `json:"username,omitempty"`
		Password       string  `json:"password,omitempty"`
		Geo            string  `json:"geo,omitempty"`
		IsEnabled      bool    `json:"isEnabled"`
		IsHealthy      bool    `json:"isHealthy"`
		FailCount      int     `json:"failCount"`
		LastHealthCheck *string `json:"lastHealthCheck,omitempty"`
		CreatedAt      string  `json:"createdAt"`
	}

	result := make([]proxyResponse, 0, len(proxies))
	for _, p := range proxies {
		pr := proxyResponse{
			ID:        p.ID,
			Name:      p.Name,
			URL:       p.URL,
			Username:  p.Username,
			Geo:       p.Geo,
			IsEnabled: p.IsEnabled,
			IsHealthy: p.IsHealthy,
			FailCount: p.FailCount,
			CreatedAt: p.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}
		if p.Password != "" {
			pr.Password = "***"
		}
		if p.LastHealthCheck != nil {
			t := p.LastHealthCheck.Format("2006-01-02T15:04:05Z")
			pr.LastHealthCheck = &t
		}
		result = append(result, pr)
	}

	writeJSON(w, http.StatusOK, map[string]any{"proxies": result})
}

// handleCreate adds a new proxy with URL validation.
func (h *BrowserProxiesHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Username string `json:"username"`
		Password string `json:"password"`
		Geo      string `json:"geo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Name == "" || req.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and url are required"})
		return
	}

	// Validate proxy URL format (C-04: block injection via malformed URLs)
	if err := browser.ValidateProxyURL(req.URL); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	tenantID := store.TenantIDFromContext(r.Context()).String()
	proxy := &store.BrowserProxy{
		TenantID: tenantID,
		Name:     req.Name,
		URL:      req.URL,
		Username: req.Username,
		Password: req.Password,
		Geo:      req.Geo,
	}

	if err := h.proxy.Add(r.Context(), proxy); err != nil {
		h.logger.Warn("failed to add proxy", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "id": proxy.ID})
}

// handleDelete removes a proxy by ID, scoped to the requesting tenant.
func (h *BrowserProxiesHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}

	tenantID := store.TenantIDFromContext(r.Context()).String()
	if err := h.proxy.Remove(r.Context(), id, tenantID); err != nil {
		h.logger.Warn("failed to remove proxy", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleToggle enables or disables a proxy, scoped to the requesting tenant.
func (h *BrowserProxiesHandler) handleToggle(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	tenantID := store.TenantIDFromContext(r.Context()).String()
	if err := h.proxy.SetEnabled(r.Context(), id, tenantID, req.Enabled); err != nil {
		h.logger.Warn("failed to toggle proxy", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleHealthCheck runs health checks on all proxies for the tenant.
func (h *BrowserProxiesHandler) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context()).String()
	if err := h.proxy.RunHealthCheck(r.Context(), tenantID); err != nil {
		h.logger.Warn("health check failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
