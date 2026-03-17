package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// SecureCLIHandler handles secure CLI binary credential CRUD endpoints.
type SecureCLIHandler struct {
	store  store.SecureCLIStore
	token  string
	msgBus *bus.MessageBus
}

// NewSecureCLIHandler creates a handler for secure CLI credential management.
func NewSecureCLIHandler(s store.SecureCLIStore, token string, msgBus *bus.MessageBus) *SecureCLIHandler {
	return &SecureCLIHandler{store: s, token: token, msgBus: msgBus}
}

// RegisterRoutes registers all secure CLI routes on the given mux.
func (h *SecureCLIHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/cli-credentials", h.auth(h.handleList))
	mux.HandleFunc("POST /v1/cli-credentials", h.auth(h.handleCreate))
	mux.HandleFunc("GET /v1/cli-credentials/presets", h.auth(h.handlePresets))
	mux.HandleFunc("GET /v1/cli-credentials/{id}", h.auth(h.handleGet))
	mux.HandleFunc("PUT /v1/cli-credentials/{id}", h.auth(h.handleUpdate))
	mux.HandleFunc("DELETE /v1/cli-credentials/{id}", h.auth(h.handleDelete))
	mux.HandleFunc("POST /v1/cli-credentials/{id}/test", h.auth(h.handleDryRun))
}

func (h *SecureCLIHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(h.token, permissions.RoleAdmin, next)
}

func (h *SecureCLIHandler) emitCacheInvalidate(key string) {
	if h.msgBus == nil {
		return
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: "secure_cli", Key: key},
	})
}

func (h *SecureCLIHandler) handleList(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	result, err := h.store.List(r.Context())
	if err != nil {
		slog.Error("secure_cli.list", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "CLI credentials")})
		return
	}
	// Mask encrypted env — never send raw credentials to UI
	for i := range result {
		result[i].EncryptedEnv = nil
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result})
}

// secureCLICreateRequest supports both preset-based and custom creation.
type secureCLICreateRequest struct {
	Preset         string          `json:"preset,omitempty"`          // auto-fill from preset
	BinaryName     string          `json:"binary_name"`
	BinaryPath     *string         `json:"binary_path,omitempty"`
	Description    string          `json:"description"`
	Env            map[string]string `json:"env"`                     // plaintext env vars (encrypted by store)
	DenyArgs       json.RawMessage `json:"deny_args,omitempty"`
	DenyVerbose    json.RawMessage `json:"deny_verbose,omitempty"`
	TimeoutSeconds int             `json:"timeout_seconds,omitempty"`
	Tips           string          `json:"tips,omitempty"`
	AgentID        *uuid.UUID      `json:"agent_id,omitempty"`
	Enabled        bool            `json:"enabled"`
}

func (h *SecureCLIHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	var req secureCLICreateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	// Apply preset defaults if specified
	if req.Preset != "" {
		preset := tools.GetPreset(req.Preset)
		if preset == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown preset: " + req.Preset})
			return
		}
		if req.BinaryName == "" {
			req.BinaryName = preset.BinaryName
		}
		if req.Description == "" {
			req.Description = preset.Description
		}
		if len(req.DenyArgs) == 0 {
			req.DenyArgs, _ = json.Marshal(preset.DenyArgs)
		}
		if len(req.DenyVerbose) == 0 {
			req.DenyVerbose, _ = json.Marshal(preset.DenyVerbose)
		}
		if req.TimeoutSeconds <= 0 {
			req.TimeoutSeconds = preset.Timeout
		}
		if req.Tips == "" {
			req.Tips = preset.Tips
		}
	}

	if req.BinaryName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "binary_name")})
		return
	}
	if len(req.Env) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "env")})
		return
	}

	// Serialize env as JSON bytes (store layer encrypts)
	envJSON, err := json.Marshal(req.Env)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid env"})
		return
	}

	b := &store.SecureCLIBinary{
		BinaryName:     req.BinaryName,
		BinaryPath:     req.BinaryPath,
		Description:    req.Description,
		EncryptedEnv:   envJSON,
		DenyArgs:       req.DenyArgs,
		DenyVerbose:    req.DenyVerbose,
		TimeoutSeconds: req.TimeoutSeconds,
		Tips:           req.Tips,
		AgentID:        req.AgentID,
		Enabled:        req.Enabled,
		CreatedBy:      store.UserIDFromContext(r.Context()),
	}
	if b.TimeoutSeconds <= 0 {
		b.TimeoutSeconds = 30
	}

	if err := h.store.Create(r.Context(), b); err != nil {
		slog.Error("secure_cli.create", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	tools.ResetCredentialScrubValues() // clear stale scrub values
	b.EncryptedEnv = nil               // don't return credentials
	emitAudit(h.msgBus, r, "secure_cli.created", "secure_cli", b.ID.String())
	h.emitCacheInvalidate(b.ID.String())
	writeJSON(w, http.StatusCreated, b)
}

func (h *SecureCLIHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "credential")})
		return
	}

	b, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "credential", id.String())})
		return
	}

	b.EncryptedEnv = nil // don't expose credentials
	writeJSON(w, http.StatusOK, b)
}

func (h *SecureCLIHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "credential")})
		return
	}

	var updates map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	// Allowlist of updatable fields to prevent column injection
	allowed := map[string]bool{
		"binary_name": true, "binary_path": true, "description": true,
		"env": true, "deny_args": true, "deny_verbose": true,
		"timeout_seconds": true, "tips": true, "agent_id": true, "enabled": true,
	}
	for k := range updates {
		if !allowed[k] {
			delete(updates, k)
		}
	}

	// If env is updated, serialize as JSON string for the store encrypt path
	if envVal, ok := updates["env"]; ok {
		if envMap, isMap := envVal.(map[string]any); isMap {
			envJSON, _ := json.Marshal(envMap)
			updates["encrypted_env"] = string(envJSON)
			delete(updates, "env")
		}
	}

	if err := h.store.Update(r.Context(), id, updates); err != nil {
		slog.Error("secure_cli.update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	tools.ResetCredentialScrubValues() // clear stale scrub values
	emitAudit(h.msgBus, r, "secure_cli.updated", "secure_cli", id.String())
	h.emitCacheInvalidate(id.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *SecureCLIHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "credential")})
		return
	}

	if err := h.store.Delete(r.Context(), id); err != nil {
		slog.Error("secure_cli.delete", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	tools.ResetCredentialScrubValues() // clear stale scrub values
	emitAudit(h.msgBus, r, "secure_cli.deleted", "secure_cli", id.String())
	h.emitCacheInvalidate(id.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *SecureCLIHandler) handlePresets(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"presets": tools.CLIPresets})
}

// dryRunRequest tests commands against deny patterns.
type dryRunRequest struct {
	TestCommands []string `json:"test_commands"`
}

type dryRunResult struct {
	Command     string  `json:"command"`
	Allowed     bool    `json:"allowed"`
	MatchedDeny *string `json:"matched_deny"`
}

func (h *SecureCLIHandler) handleDryRun(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "credential")})
		return
	}

	b, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "credential", id.String())})
		return
	}

	var req dryRunRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	// Parse deny patterns from config
	var denyArgs, denyVerbose []string
	_ = json.Unmarshal(b.DenyArgs, &denyArgs)
	_ = json.Unmarshal(b.DenyVerbose, &denyVerbose)
	allPatterns := append(denyArgs, denyVerbose...)

	results := make([]dryRunResult, 0, len(req.TestCommands))
	for _, cmd := range req.TestCommands {
		result := dryRunResult{Command: cmd, Allowed: true}
		// Strip binary name prefix to get just the args portion
		argsStr := cmd
		if strings.HasPrefix(cmd, b.BinaryName+" ") {
			argsStr = cmd[len(b.BinaryName)+1:]
		}
		for _, p := range allPatterns {
			re, err := regexp.Compile(p)
			if err != nil {
				continue
			}
			if re.MatchString(argsStr) {
				result.Allowed = false
				result.MatchedDeny = &p
				break
			}
		}
		results = append(results, result)
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}
