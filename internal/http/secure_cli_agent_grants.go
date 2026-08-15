package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Default reveal rate-limit: 10 calls/min per caller, burst 3.
// Per-instance limiter avoids cross-test state leakage when the test suite
// constructs multiple handlers in parallel.
const (
	envRevealRPM   = 10
	envRevealBurst = 3
)

// SecureCLIGrantHandler handles CRUD for per-agent secure CLI grants.
type SecureCLIGrantHandler struct {
	grants      store.SecureCLIAgentGrantStore
	tenantStore store.TenantStore
	msgBus      *bus.MessageBus
	envLimiter  *perKeyRateLimiter
}

// NewSecureCLIGrantHandler creates the handler. tenantStore may be nil (requireTenantAdmin
// handles that gracefully with a 501), but should always be provided in production.
func NewSecureCLIGrantHandler(gs store.SecureCLIAgentGrantStore, ts store.TenantStore, msgBus *bus.MessageBus) *SecureCLIGrantHandler {
	return &SecureCLIGrantHandler{
		grants:      gs,
		tenantStore: ts,
		msgBus:      msgBus,
		envLimiter:  newPerKeyRateLimiter(envRevealRPM, envRevealBurst),
	}
}

// SetEnvRevealLimiter overrides the env:reveal rate limiter. Intended for tests
// that need deterministic limits. Not safe to call concurrently with in-flight requests.
func (h *SecureCLIGrantHandler) SetEnvRevealLimiter(rpm, burst int) {
	h.envLimiter = newPerKeyRateLimiter(rpm, burst)
}

// HandleRevealEnvForTest exposes the reveal handler for integration tests that need
// to bypass the requireAuth middleware. The caller must inject auth context (UserID,
// TenantID, Role) manually. Not registered in any mux — test use only.
func (h *SecureCLIGrantHandler) HandleRevealEnvForTest(w http.ResponseWriter, r *http.Request) {
	h.handleRevealEnv(w, r)
}

// RegisterRoutes registers agent grant routes nested under cli-credentials.
func (h *SecureCLIGrantHandler) RegisterRoutes(mux *http.ServeMux) {
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return requireAuth(permissions.RoleAdmin, next)
	}
	mux.HandleFunc("GET /v1/cli-credentials/{id}/agent-grants", auth(h.handleList))
	mux.HandleFunc("POST /v1/cli-credentials/{id}/agent-grants", auth(h.handleCreate))
	mux.HandleFunc("GET /v1/cli-credentials/{id}/agent-grants/{grantId}", auth(h.handleGet))
	mux.HandleFunc("PUT /v1/cli-credentials/{id}/agent-grants/{grantId}", auth(h.handleUpdate))
	mux.HandleFunc("DELETE /v1/cli-credentials/{id}/agent-grants/{grantId}", auth(h.handleDelete))
	// POST keeps revealed secret material out of URL/history and avoids query caching.
	mux.HandleFunc("POST /v1/cli-credentials/{id}/agent-grants/{grantId}/env:reveal", auth(h.handleRevealEnv))
}

// grantCreateRequest is the typed DTO for grant creation.
// EnvVars is optional; plaintext values are encrypted by the store layer.
// Clients MUST NOT send encrypted_env — that field is never accepted from the wire.
type grantCreateRequest struct {
	AgentID        uuid.UUID        `json:"agent_id"`
	EnvVars        json.RawMessage  `json:"env_vars,omitempty"`
	DenyArgs       *json.RawMessage `json:"deny_args,omitempty"`
	DenyVerbose    *json.RawMessage `json:"deny_verbose,omitempty"`
	TimeoutSeconds *int             `json:"timeout_seconds,omitempty"`
	Tips           *string          `json:"tips,omitempty"`
	Enabled        *bool            `json:"enabled,omitempty"`
}

// populateGrantEnvFields sets sorted key names, env presence, and sanitized entries.
func populateGrantEnvFields(g *store.SecureCLIAgentGrant) {
	if len(g.EncryptedEnv) == 0 {
		g.EnvKeys = []string{}
		g.Env = nil
		g.EnvSet = false
		return
	}
	keys := store.SecureCLIEnvKeys(g.EncryptedEnv)
	g.EnvKeys = keys
	g.Env = store.SanitizeSecureCLIEnvJSON(g.EncryptedEnv)
	g.EnvSet = len(keys) > 0
}

// validateAndSerializeEnvVars validates env keys/values via denylist and returns serialized JSON.
// Returns (nil, 400 error response written) on denial, (jsonBytes, nil) on success.
// Never logs env values or keys in error paths.
func validateAndSerializeEnvVars(w http.ResponseWriter, locale string, raw json.RawMessage) ([]byte, bool) {
	envEntries, err := store.ParseSecureCLIEnv(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgGrantEnvValueInvalid, err.Error())})
		return nil, false
	}
	envVars := make(map[string]string, len(envEntries))
	for key, entry := range envEntries {
		envVars[key] = entry.Value
	}
	denied, valErr := crypto.ValidateGrantEnvVars(envVars)
	if valErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgGrantEnvValueInvalid, valErr.Error())})
		return nil, false
	}
	if len(denied) > 0 {
		sort.Strings(denied)
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":         i18n.T(locale, i18n.MsgGrantEnvDeniedKeys, strings.Join(denied, ", ")),
			"rejected_keys": strings.Join(denied, ","),
		})
		return nil, false
	}
	b, err := store.SerializeSecureCLIEnv(envEntries)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgGrantEnvValueInvalid, "serialization failed")})
		return nil, false
	}
	return b, true
}

func parseGrantPathIDs(w http.ResponseWriter, r *http.Request, locale string) (uuid.UUID, uuid.UUID, bool) {
	binaryID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "credential")})
		return uuid.Nil, uuid.Nil, false
	}
	grantID, err := uuid.Parse(r.PathValue("grantId"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "grant")})
		return uuid.Nil, uuid.Nil, false
	}
	return binaryID, grantID, true
}

func (h *SecureCLIGrantHandler) getGrantForBinary(w http.ResponseWriter, r *http.Request, locale string) (*store.SecureCLIAgentGrant, uuid.UUID, bool) {
	binaryID, grantID, ok := parseGrantPathIDs(w, r, locale)
	if !ok {
		return nil, uuid.Nil, false
	}
	g, err := h.grants.Get(r.Context(), grantID)
	if err != nil || g.BinaryID != binaryID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "grant", grantID.String())})
		return nil, uuid.Nil, false
	}
	return g, binaryID, true
}

func (h *SecureCLIGrantHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	binaryID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "credential")})
		return
	}
	grants, err := h.grants.ListByBinary(r.Context(), binaryID)
	if err != nil {
		slog.Error("secure_cli_grants.list", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "grants")})
		return
	}
	// Populate env metadata (keys only, no values) for each grant.
	for i := range grants {
		populateGrantEnvFields(&grants[i])
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": grants})
}

func (h *SecureCLIGrantHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	binaryID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "credential")})
		return
	}

	var req grantCreateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	if req.AgentID == uuid.Nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "agent_id")})
		return
	}
	if exists, err := h.grants.BinaryExists(r.Context(), binaryID); err != nil {
		slog.Error("secure_cli_grants.create.binary_scope", "binary_id", binaryID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "validate credential")})
		return
	} else if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "credential", binaryID.String())})
		return
	}
	if exists, err := h.grants.AgentExists(r.Context(), req.AgentID); err != nil {
		slog.Error("secure_cli_grants.create.agent_scope", "agent_id", req.AgentID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "validate agent")})
		return
	} else if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "agent", req.AgentID.String())})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	g := &store.SecureCLIAgentGrant{
		BinaryID:       binaryID,
		AgentID:        req.AgentID,
		DenyArgs:       req.DenyArgs,
		DenyVerbose:    req.DenyVerbose,
		TimeoutSeconds: req.TimeoutSeconds,
		Tips:           req.Tips,
		Enabled:        enabled,
	}
	if err := h.grants.Create(r.Context(), g); err != nil {
		slog.Error("secure_cli_grants.create", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "create grant")})
		return
	}

	// Encrypt and persist env vars separately to isolate plaintext handling.
	if len(req.EnvVars) > 0 {
		envJSON, ok := validateAndSerializeEnvVars(w, locale, req.EnvVars)
		if !ok {
			// Grant was created but env validation failed; clean it up to avoid orphan row.
			// Log rollback-delete failures so operators can clean up orphan rows.
			if delErr := h.grants.Delete(r.Context(), g.ID); delErr != nil {
				slog.Error("secure_cli_grants.create.rollback_delete",
					"grant_id", g.ID,
					"err", delErr,
					"note", "orphan grant row may exist after env validation failure",
				)
			}
			return
		}
		if err := h.grants.UpdateGrantEnv(r.Context(), g.ID, envJSON); err != nil {
			slog.Error("secure_cli_grants.create.set_env", "grant_id", g.ID, "error", err)
			// Log rollback-delete failures so operators can clean up orphan rows.
			if delErr := h.grants.Delete(r.Context(), g.ID); delErr != nil {
				slog.Error("secure_cli_grants.create.rollback_delete",
					"grant_id", g.ID,
					"err", delErr,
					"note", "orphan grant row may exist after env persist failure",
				)
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "persist grant env")})
			return
		}
		// Reflect the newly-persisted env bytes in the response so env_set/env_keys are accurate.
		g.EncryptedEnv = envJSON
	}

	h.emitCacheInvalidate(binaryID.String())
	populateGrantEnvFields(g)
	writeJSON(w, http.StatusCreated, g)
}

func (h *SecureCLIGrantHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	g, _, ok := h.getGrantForBinary(w, r, locale)
	if !ok {
		return
	}
	populateGrantEnvFields(g)
	writeJSON(w, http.StatusOK, g)
}

func (h *SecureCLIGrantHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	g, binaryID, ok := h.getGrantForBinary(w, r, locale)
	if !ok {
		return
	}

	// Decode into a raw map to distinguish absent vs null env_vars.
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&raw); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	// Build typed field updates (allowlist: deny_args, deny_verbose, timeout_seconds, tips, enabled).
	updates := map[string]any{"updated_at": time.Now()}
	allowedScalar := map[string]bool{
		"deny_args": true, "deny_verbose": true, "timeout_seconds": true,
		"tips": true, "enabled": true,
	}
	for k, v := range raw {
		if k == "env_vars" {
			continue // handled separately below
		}
		if allowedScalar[k] {
			var decoded any
			// Return 400 on unmarshal failure; silent discard means admin
			// thinks they applied a change (e.g. enabled: "false") but the grant is unchanged.
			if err := json.Unmarshal(v, &decoded); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": i18n.T(locale, i18n.MsgGrantEnvValueInvalid, "field "+k+": "+err.Error()),
				})
				return
			}
			updates[k] = decoded
		}
	}
	// 3-state env_vars semantics: absent=skip, null=clear, {...}=replace.
	// Empty map is treated as clear, same as null.
	// TS type: absent | null | Record<string,string> — see ui/web/src/types/cli-credential.ts.
	var envJSON []byte
	envPresent := false
	if envRaw, present := raw["env_vars"]; present {
		envPresent = true
		var envPtr *map[string]string
		if string(envRaw) != "null" {
			envEntries, err := store.ParseSecureCLIEnv(envRaw)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgGrantEnvValueInvalid, "env_vars must be a string map or env entries")})
				return
			}
			m := make(map[string]string, len(envEntries))
			for key, entry := range envEntries {
				m[key] = entry.Value
			}
			envPtr = &m
		}
		// envPtr == nil → clear; envPtr != nil → replace.
		// Note: envPtr pointing to an empty map ({}) is treated as clear (same as null) —
		// envJSON stays nil and UpdateGrantEnv(nil) removes the override.
		if envPtr != nil && len(*envPtr) > 0 {
			j, ok := validateAndSerializeEnvVars(w, locale, envRaw)
			if !ok {
				return
			}
			envJSON = j
		}
	}

	if err := h.grants.Update(r.Context(), g.ID, updates); err != nil {
		slog.Error("secure_cli_grants.update", "grant_id", g.ID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "update grant")})
		return
	}

	if envPresent {
		if err := h.grants.UpdateGrantEnv(r.Context(), g.ID, envJSON); err != nil {
			slog.Error("secure_cli_grants.update.set_env", "grant_id", g.ID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "update grant env")})
			return
		}
	}

	h.emitCacheInvalidate(binaryID.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *SecureCLIGrantHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	g, binaryID, ok := h.getGrantForBinary(w, r, locale)
	if !ok {
		return
	}
	if err := h.grants.Delete(r.Context(), g.ID); err != nil {
		slog.Error("secure_cli_grants.delete", "grant_id", g.ID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "delete grant")})
		return
	}

	h.emitCacheInvalidate(binaryID.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleRevealEnv decrypts and returns the grant's env vars in plaintext.
//
// Security posture:
//   - POST method (not GET) defeats HTTP caching and browser prefetch/CSRF.
//   - requireTenantAdmin + implicit tenant_id SQL filter (in store.Get).
//   - Rate limited to 10 reveals/min per caller.
//   - Cache-Control: no-store ensures response is not cached by intermediaries.
//   - Audit log emitted with actor, tenant, grant, timestamp.
//   - Plaintext values NEVER logged; only grant_id/tenant_id appear in logs.
func (h *SecureCLIGrantHandler) handleRevealEnv(w http.ResponseWriter, r *http.Request) {
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	ctx := r.Context()

	// Reject contexts where the tenant_id SQL filter in store.Get would not bind
	// to a real tenant — that would leak env vars across tenant boundaries.
	// We check tenant_id directly (not store.IsMasterScope) because the shared
	// IsMasterScope predicate also returns true for owner role with an explicit
	// tenant_id, which is a legitimate caller here (the SQL filter still binds).
	if tid := store.TenantIDFromContext(ctx); tid == uuid.Nil || tid == store.MasterTenantID {
		locale := store.LocaleFromContext(ctx)
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": i18n.T(locale, i18n.MsgPermissionDenied, "reveal env (master scope not allowed)"),
		})
		return
	}

	locale := store.LocaleFromContext(ctx)

	// Rate limit: 10 reveals/min per authenticated caller (context UserID).
	// Require non-empty UserID from authenticated context.
	// If UserID is empty, auth middleware failed to populate it — reject rather
	// than fall back to a spoofable header or IP address.
	callerID := store.UserIDFromContext(ctx)
	if callerID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": i18n.T(locale, i18n.MsgPermissionDenied, "reveal env (missing user context)"),
		})
		return
	}
	rlKey := "uid:" + callerID
	if !h.envLimiter.Allow(rlKey) {
		slog.Warn("security.rate_limited", "endpoint", "env:reveal", "key", rlKey)
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": i18n.T(locale, i18n.MsgGrantEnvRevealLimit)})
		return
	}

	// store.Get enforces tenant_id filter; helper also enforces URL parent-child hierarchy.
	g, binaryID, ok := h.getGrantForBinary(w, r, locale)
	if !ok {
		return
	}

	tenantID := store.TenantIDFromContext(ctx)
	// callerID is already declared above (used as rate limit key).
	// Audit log (INFO): routine audited read. Per CLAUDE.md, security.* Warn is reserved
	// for suspicious events. Routine reveals are Info under audit.* prefix.
	// Failure paths (rate-limit, 404) remain Warn under security.*.
	slog.Info("audit.cli_credential.env.reveal",
		"caller_id", callerID,
		"tenant_id", tenantID,
		"grant_id", g.ID,
		"binary_id", binaryID,
		"reason", "reveal-env",
		"ts", time.Now().UTC(),
	)

	// Prevent HTTP/proxy caching of the secret response.
	w.Header().Set("Cache-Control", "no-store, no-cache")
	w.Header().Set("Pragma", "no-cache")

	// EncryptedEnv at this point contains the decrypted plaintext JSON (store.Get decrypts on read).
	if len(g.EncryptedEnv) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"env_vars": map[string]string{}})
		return
	}
	envVars, err := store.FlattenSecureCLIEnv(g.EncryptedEnv)
	if err != nil {
		slog.Error("secure_cli_grants.reveal.parse", "grant_id", g.ID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "parse grant env")})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"env_vars": envVars})
}

func (h *SecureCLIGrantHandler) emitCacheInvalidate(key string) {
	if h.msgBus == nil {
		return
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: map[string]any{"scope": "secure_cli", "key": key},
	})
}
