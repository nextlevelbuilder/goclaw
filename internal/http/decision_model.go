package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/decisionmodel"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// DecisionModelHandler serves per-tenant decision-model config. Non-secret
// settings go to system_configs, the API key ONLY to config_secrets (AES-GCM).
// Every write passes endpoint SSRF validation, so the generic
// PUT /v1/system-configs/{key} refuses the decision_model.* prefix.
type DecisionModelHandler struct {
	systemConfigs store.SystemConfigStore
	configSecrets store.ConfigSecretsStore
	tenants       store.TenantStore
	// newDecider is swapped in tests; production builds a real Jev client.
	newDecider func(decisionmodel.Config) (decisionmodel.Decider, error)
}

func NewDecisionModelHandler(sc store.SystemConfigStore, cs store.ConfigSecretsStore, tenants store.TenantStore) *DecisionModelHandler {
	return &DecisionModelHandler{systemConfigs: sc, configSecrets: cs, tenants: tenants, newDecider: decisionmodel.NewJev}
}

// RegisterRoutes wires decision-model endpoints with RoleAdmin + tenant-admin auth.
func (h *DecisionModelHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/decision-model/config", requireAuth(permissions.RoleAdmin, h.requireTenantAdmin(h.handleGet)))
	mux.HandleFunc("POST /v1/decision-model/config", requireAuth(permissions.RoleAdmin, h.requireTenantAdmin(h.handleSave)))
	mux.HandleFunc("POST /v1/decision-model/verify", requireAuth(permissions.RoleAdmin, h.requireTenantAdmin(h.handleVerify)))
}

func (h *DecisionModelHandler) requireTenantAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pkgGatewayToken == "" && store.TenantIDFromContext(r.Context()) == store.MasterTenantID {
			next(w, r)
			return
		}
		if !requireTenantAdmin(w, r, h.tenants) {
			return
		}
		next(w, r)
	}
}

// decisionModelConfig is the wire shape for GET and POST. APIKey is "***" on
// GET when a key is stored; on POST "" or "***" keeps the stored key.
type decisionModelConfig struct {
	Enabled       bool   `json:"enabled"`
	Endpoint      string `json:"endpoint"`
	Model         string `json:"model"`
	TimeoutMS     int    `json:"timeout_ms"`
	MaxStateChars int    `json:"max_state_chars"`
	APIKey        string `json:"api_key"`
}

const maskedKey = "***"

func (h *DecisionModelHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if store.TenantIDFromContext(ctx) == uuid.Nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant context required"})
		return
	}
	cfg := decisionmodel.LoadConfig(ctx, h.systemConfigs)
	resp := decisionModelConfig{
		Endpoint:      cfg.Endpoint,
		Model:         cfg.Model,
		TimeoutMS:     int(cfg.Timeout / time.Millisecond),
		MaxStateChars: cfg.MaxStateChars,
	}
	if v, err := h.systemConfigs.Get(ctx, decisionmodel.KeyEnabled); err == nil {
		resp.Enabled = strings.TrimSpace(v) == "true"
	}
	if v, err := h.configSecrets.Get(ctx, decisionmodel.KeyAPIKey); err == nil && v != "" {
		resp.APIKey = maskedKey
	}
	writeJSON(w, http.StatusOK, resp)
}

// validate checks the non-secret fields. Returns an i18n-ready detail or "".
func (c *decisionModelConfig) validate() string {
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	c.Model = strings.TrimSpace(c.Model)
	if c.Endpoint == "" {
		c.Endpoint = decisionmodel.DefaultEndpoint
	}
	if c.Model == "" {
		c.Model = decisionmodel.DefaultModel
	}
	if c.TimeoutMS == 0 {
		c.TimeoutMS = int(decisionmodel.DefaultTimeout / time.Millisecond)
	}
	if c.MaxStateChars == 0 {
		c.MaxStateChars = decisionmodel.DefaultMaxStateChars
	}
	switch {
	case c.TimeoutMS < decisionmodel.MinTimeoutMS || c.TimeoutMS > decisionmodel.MaxTimeoutMS:
		return "timeout_ms must be between 100 and 5000"
	case c.MaxStateChars < decisionmodel.MinMaxStateChars || c.MaxStateChars > decisionmodel.MaxMaxStateChars:
		return "max_state_chars must be between 200 and 20000"
	case len(c.Model) > 100:
		return "model is too long"
	}
	if err := decisionmodel.ValidateEndpoint(c.Endpoint); err != nil {
		return err.Error()
	}
	return ""
}

func (h *DecisionModelHandler) decode(w http.ResponseWriter, r *http.Request) (decisionModelConfig, bool) {
	locale := store.LocaleFromContext(r.Context())
	if store.TenantIDFromContext(r.Context()) == uuid.Nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant context required"})
		return decisionModelConfig{}, false
	}
	var req decisionModelConfig
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return decisionModelConfig{}, false
	}
	if msg := req.validate(); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, msg)})
		return decisionModelConfig{}, false
	}
	return req, true
}

// resolveKey returns the submitted key, or the stored one for "" / "***".
func (h *DecisionModelHandler) resolveKey(ctx context.Context, submitted string) string {
	submitted = strings.TrimSpace(submitted)
	if submitted != "" && submitted != maskedKey {
		return submitted
	}
	v, _ := h.configSecrets.Get(ctx, decisionmodel.KeyAPIKey)
	return v
}

func (h *DecisionModelHandler) handleSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	req, ok := h.decode(w, r)
	if !ok {
		return
	}
	newKey := strings.TrimSpace(req.APIKey)
	if req.Enabled && h.resolveKey(ctx, newKey) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "api_key")})
		return
	}

	// Secret first: if it fails, enabled is not flipped on without a key.
	if newKey != "" && newKey != maskedKey {
		if err := h.configSecrets.Set(ctx, decisionmodel.KeyAPIKey, newKey); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save api_key"})
			return
		}
	}
	for _, kv := range [][2]string{
		{decisionmodel.KeyEndpoint, req.Endpoint},
		{decisionmodel.KeyModel, req.Model},
		{decisionmodel.KeyTimeoutMS, strconv.Itoa(req.TimeoutMS)},
		{decisionmodel.KeyMaxStateChars, strconv.Itoa(req.MaxStateChars)},
		{decisionmodel.KeyEnabled, strconv.FormatBool(req.Enabled)}, // last: enable only after the rest is saved
	} {
		if err := h.systemConfigs.Set(ctx, kv[0], kv[1]); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save " + kv[0]})
			return
		}
	}
	req.APIKey = ""
	if h.resolveKey(ctx, "") != "" {
		req.APIKey = maskedKey
	}
	writeJSON(w, http.StatusOK, req)
}

// handleVerify sends one real typed question with the submitted (unsaved)
// settings, so a human can confirm endpoint + key before enabling.
func (h *DecisionModelHandler) handleVerify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	req, ok := h.decode(w, r)
	if !ok {
		return
	}
	key := h.resolveKey(ctx, req.APIKey)
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "api_key")})
		return
	}
	d, err := h.newDecider(decisionmodel.Config{
		Endpoint:      req.Endpoint,
		APIKey:        key,
		Model:         req.Model,
		Timeout:       time.Duration(req.TimeoutMS) * time.Millisecond,
		MaxStateChars: req.MaxStateChars,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, err.Error())})
		return
	}
	start := time.Now()
	dec, err := d.Decide(ctx, decisionmodel.State{
		Message: "Please fix the failing Go unit test in internal/foo/foo_test.go.",
		Options: map[string]string{
			"coder":  "Writes and fixes source code and tests.",
			"writer": "Writes marketing copy and blog posts.",
		},
	})
	latency := time.Since(start).Milliseconds()
	if err != nil {
		msg := err.Error()
		if errors.Is(err, decisionmodel.ErrUnavailable) {
			msg = strings.TrimPrefix(msg, decisionmodel.ErrUnavailable.Error()+": ")
		}
		writeJSON(w, http.StatusOK, map[string]any{"valid": false, "error": msg, "latency_ms": latency})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"valid":      true,
		"model":      dec.Model,
		"choice":     dec.Option,
		"confidence": dec.Confidence,
		"latency_ms": latency,
	})
}
