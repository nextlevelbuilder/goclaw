package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/usage/pricing"
)

type UsageCapsHandler struct {
	store   store.UsageCapStore
	tenants store.TenantStore
	client  *http.Client
}

func NewUsageCapsHandler(s store.UsageCapStore, tenants store.TenantStore) *UsageCapsHandler {
	return &UsageCapsHandler{store: s, tenants: tenants, client: &http.Client{Timeout: 30 * time.Second}}
}

func (h *UsageCapsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/usage-caps/policies", h.auth(h.handleListPolicies))
	mux.HandleFunc("POST /v1/usage-caps/policies", h.writeAuth(h.handleCreatePolicy))
	mux.HandleFunc("PATCH /v1/usage-caps/policies/{id}", h.writeAuth(h.handleUpdatePolicy))
	mux.HandleFunc("DELETE /v1/usage-caps/policies/{id}", h.writeAuth(h.handleDeletePolicy))
	mux.HandleFunc("GET /v1/usage-caps/utilization", h.auth(h.handleUtilization))
	mux.HandleFunc("GET /v1/usage-caps/events", h.auth(h.handleEvents))
	mux.HandleFunc("POST /v1/model-pricing/sync-openrouter", h.masterAuth(h.handleSyncOpenRouter))
	mux.HandleFunc("GET /v1/model-pricing", h.auth(h.handleListPricing))
	mux.HandleFunc("PUT /v1/model-pricing/overrides", h.writeAuth(h.handlePutOverride))
	mux.HandleFunc("GET /v1/model-pricing/overrides", h.auth(h.handleListOverrides))
	mux.HandleFunc("DELETE /v1/model-pricing/overrides/{id}", h.writeAuth(h.handleDeleteOverride))
}

func (h *UsageCapsHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(permissions.RoleAdmin, next)
}

func (h *UsageCapsHandler) writeAuth(next http.HandlerFunc) http.HandlerFunc {
	return h.auth(func(w http.ResponseWriter, r *http.Request) {
		if !requireTenantAdmin(w, r, h.tenants) {
			return
		}
		next(w, r)
	})
}

func (h *UsageCapsHandler) masterAuth(next http.HandlerFunc) http.HandlerFunc {
	return h.auth(func(w http.ResponseWriter, r *http.Request) {
		if !requireMasterScope(w, r) {
			return
		}
		next(w, r)
	})
}

func writeUsageCapError(w http.ResponseWriter, r *http.Request, status int, key string, args ...any) {
	writeJSON(w, status, map[string]string{"error": i18n.T(store.LocaleFromContext(r.Context()), key, args...)})
}

func (h *UsageCapsHandler) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	scope := store.UsageCapScope{TenantID: tenantIDOrMaster(r)}
	policies, err := h.store.ListUsageCapPolicies(r.Context(), scope, true)
	if err != nil {
		writeUsageCapError(w, r, http.StatusInternalServerError, i18n.MsgUsageCapsListPoliciesFailed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"policies": policies})
}

func (h *UsageCapsHandler) handleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	var body policyBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeUsageCapError(w, r, http.StatusBadRequest, i18n.MsgInvalidJSON)
		return
	}
	p, err := body.toPolicy(tenantIDOrMaster(r))
	if err != nil {
		writeUsageCapError(w, r, http.StatusBadRequest, i18n.MsgInvalidRequest, err.Error())
		return
	}
	if err := h.store.CreateUsageCapPolicy(r.Context(), &p); err != nil {
		slog.Warn("usage_caps.create_policy_failed", "error", err)
		writeUsageCapError(w, r, http.StatusBadRequest, i18n.MsgUsageCapPolicyValidationFailed)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (h *UsageCapsHandler) handleUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeUsageCapError(w, r, http.StatusBadRequest, i18n.MsgInvalidID, "policy")
		return
	}
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeUsageCapError(w, r, http.StatusBadRequest, i18n.MsgInvalidJSON)
		return
	}
	patch, err := policyPatchFromBody(bodyBytes)
	if err != nil {
		writeUsageCapError(w, r, http.StatusBadRequest, i18n.MsgInvalidRequest, err.Error())
		return
	}
	p, err := h.store.UpdateUsageCapPolicy(r.Context(), tenantIDOrMaster(r), id, patch)
	if err != nil {
		if errors.Is(err, store.ErrUsageCapPolicyManaged) {
			writeUsageCapError(w, r, http.StatusConflict, i18n.MsgUsageCapPolicyManaged)
			return
		}
		slog.Warn("usage_caps.update_policy_failed", "error", err)
		writeUsageCapError(w, r, http.StatusBadRequest, i18n.MsgUsageCapPolicyValidationFailed)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *UsageCapsHandler) handleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeUsageCapError(w, r, http.StatusBadRequest, i18n.MsgInvalidID, "policy")
		return
	}
	if err := h.store.DeleteUsageCapPolicy(r.Context(), tenantIDOrMaster(r), id); err != nil {
		if errors.Is(err, store.ErrUsageCapPolicyManaged) {
			writeUsageCapError(w, r, http.StatusConflict, i18n.MsgUsageCapPolicyManaged)
			return
		}
		writeUsageCapError(w, r, http.StatusInternalServerError, i18n.MsgUsageCapsDeletePolicyFailed)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *UsageCapsHandler) handleUtilization(w http.ResponseWriter, r *http.Request) {
	rows, err := h.store.ListUsageCapUtilization(r.Context(), tenantIDOrMaster(r))
	if err != nil {
		writeUsageCapError(w, r, http.StatusInternalServerError, i18n.MsgUsageCapsUtilizationFailed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows})
}

func (h *UsageCapsHandler) handleEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := h.store.ListUsageCapEvents(r.Context(), tenantIDOrMaster(r), limit)
	if err != nil {
		writeUsageCapError(w, r, http.StatusInternalServerError, i18n.MsgUsageCapsEventsFailed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (h *UsageCapsHandler) handleSyncOpenRouter(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	entries, err := pricing.FetchOpenRouterCatalog(ctx, h.client)
	if err != nil {
		slog.Warn("usage_pricing.openrouter_sync", "error", err)
		writeUsageCapError(w, r, http.StatusBadGateway, i18n.MsgUsagePricingSyncOpenRouterFailed, err.Error())
		return
	}
	count, err := h.store.UpsertPricingCatalog(r.Context(), entries)
	if err != nil {
		writeUsageCapError(w, r, http.StatusInternalServerError, i18n.MsgUsagePricingStoreCatalogFailed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": count})
}

func (h *UsageCapsHandler) handleListPricing(w http.ResponseWriter, r *http.Request) {
	rows, err := h.store.ListPricingCatalog(r.Context(), store.UsagePricingQuery{
		ModelID: r.URL.Query().Get("model"),
		Limit:   queryInt(r, "limit", 100),
	})
	if err != nil {
		writeUsageCapError(w, r, http.StatusInternalServerError, i18n.MsgUsagePricingListFailed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": rows})
}

func (h *UsageCapsHandler) handlePutOverride(w http.ResponseWriter, r *http.Request) {
	var body overrideBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeUsageCapError(w, r, http.StatusBadRequest, i18n.MsgInvalidJSON)
		return
	}
	providerID, err := uuid.Parse(body.ProviderID)
	if err != nil || body.ModelID == "" {
		writeUsageCapError(w, r, http.StatusBadRequest, i18n.MsgUsagePricingProviderModelRequired)
		return
	}
	o := &store.UsagePricingOverride{
		TenantID: tenantIDOrMaster(r), ProviderID: providerID,
		ProviderType: body.ProviderType, ModelID: body.ModelID,
		Pricing: body.Pricing, Enabled: body.Enabled == nil || *body.Enabled,
	}
	if err := h.store.PutPricingOverride(r.Context(), o); err != nil {
		slog.Warn("usage_pricing.put_override_failed", "error", err)
		writeUsageCapError(w, r, http.StatusBadRequest, i18n.MsgUsagePricingOverrideValidationFailed)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func (h *UsageCapsHandler) handleListOverrides(w http.ResponseWriter, r *http.Request) {
	var providerID uuid.UUID
	if raw := r.URL.Query().Get("provider_id"); raw != "" {
		var err error
		providerID, err = uuid.Parse(raw)
		if err != nil {
			writeUsageCapError(w, r, http.StatusBadRequest, i18n.MsgInvalidID, "provider")
			return
		}
	}
	rows, err := h.store.ListPricingOverrides(r.Context(), store.UsagePricingQuery{TenantID: tenantIDOrMaster(r), ProviderID: providerID})
	if err != nil {
		writeUsageCapError(w, r, http.StatusInternalServerError, i18n.MsgUsagePricingListOverridesFailed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"overrides": rows})
}

func (h *UsageCapsHandler) handleDeleteOverride(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeUsageCapError(w, r, http.StatusBadRequest, i18n.MsgInvalidID, "override")
		return
	}
	if err := h.store.DeletePricingOverride(r.Context(), tenantIDOrMaster(r), id); err != nil {
		writeUsageCapError(w, r, http.StatusInternalServerError, i18n.MsgUsagePricingDeleteOverrideFailed)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type policyBody struct {
	AgentID       *string  `json:"agent_id"`
	ProviderID    *string  `json:"provider_id"`
	ProviderType  *string  `json:"provider_type"`
	ModelID       *string  `json:"model_id"`
	Window        string   `json:"window"`
	MaxTokens     *int64   `json:"max_tokens"`
	MaxCostMicros *int64   `json:"max_cost_micros"`
	MaxCostUSD    *float64 `json:"max_cost_usd"`
	Enabled       *bool    `json:"enabled"`
	Priority      *int     `json:"priority"`
}

type overrideBody struct {
	ProviderID   string                   `json:"provider_id"`
	ProviderType string                   `json:"provider_type"`
	ModelID      string                   `json:"model_id"`
	Pricing      store.UsagePricingFields `json:"pricing"`
	Enabled      *bool                    `json:"enabled"`
}

func (b policyBody) toPolicy(tenantID uuid.UUID) (store.UsageCapPolicy, error) {
	p := store.UsageCapPolicy{TenantID: tenantID, Window: b.Window, Enabled: true, Priority: 100}
	if b.Enabled != nil {
		p.Enabled = *b.Enabled
	}
	if b.Priority != nil {
		p.Priority = *b.Priority
	}
	if p.Window == "" {
		p.Window = store.UsageCapWindowDay
	}
	var err error
	p.AgentID, err = parseOptionalUUIDStrict("agent_id", b.AgentID)
	if err != nil {
		return p, err
	}
	p.ProviderID, err = parseOptionalUUIDStrict("provider_id", b.ProviderID)
	if err != nil {
		return p, err
	}
	if b.ProviderType != nil {
		p.ProviderType = *b.ProviderType
	}
	if b.ModelID != nil {
		p.ModelID = *b.ModelID
	}
	p.MaxTokens = b.MaxTokens
	p.MaxCostMicros = maxCostMicros(b.MaxCostMicros, b.MaxCostUSD)
	return p, nil
}

func (b policyBody) toPatch() (store.UsageCapPolicyPatch, error) {
	var patch store.UsageCapPolicyPatch
	if b.AgentID != nil {
		v, err := parseOptionalUUIDStrict("agent_id", b.AgentID)
		if err != nil {
			return patch, err
		}
		patch.AgentID = &v
	}
	if b.ProviderID != nil {
		v, err := parseOptionalUUIDStrict("provider_id", b.ProviderID)
		if err != nil {
			return patch, err
		}
		patch.ProviderID = &v
	}
	patch.ProviderType = b.ProviderType
	patch.ModelID = b.ModelID
	if b.Window != "" {
		patch.Window = &b.Window
	}
	if b.MaxTokens != nil {
		patch.MaxTokens = &b.MaxTokens
	}
	if b.MaxCostMicros != nil || b.MaxCostUSD != nil {
		v := maxCostMicros(b.MaxCostMicros, b.MaxCostUSD)
		patch.MaxCostMicros = &v
	}
	patch.Enabled = b.Enabled
	patch.Priority = b.Priority
	return patch, nil
}

func policyPatchFromBody(bodyBytes []byte) (store.UsageCapPolicyPatch, error) {
	var body policyBody
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return store.UsageCapPolicyPatch{}, err
	}
	patch, err := body.toPatch()
	if err != nil {
		return patch, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(bodyBytes, &raw); err == nil {
		if isJSONNull(raw["max_tokens"]) {
			var v *int64
			patch.MaxTokens = &v
		}
		if isJSONNull(raw["max_cost_micros"]) || isJSONNull(raw["max_cost_usd"]) {
			var v *int64
			patch.MaxCostMicros = &v
		}
	}
	return patch, nil
}

func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

func tenantIDOrMaster(r *http.Request) uuid.UUID {
	if tid := store.TenantIDFromContext(r.Context()); tid != uuid.Nil {
		return tid
	}
	return store.MasterTenantID
}

func queryInt(r *http.Request, key string, fallback int) int {
	n, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func parseOptionalUUIDStrict(field string, raw *string) (*uuid.UUID, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(*raw)
	if err != nil {
		return nil, fmt.Errorf("invalid %s", field)
	}
	return &id, nil
}

func maxCostMicros(micros *int64, usd *float64) *int64 {
	if micros != nil {
		return micros
	}
	if usd == nil {
		return nil
	}
	v := int64(*usd * 1_000_000)
	return &v
}
