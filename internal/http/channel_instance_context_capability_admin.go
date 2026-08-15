package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type channelContextPath struct {
	Instance  *store.ChannelInstanceData
	ScopeType string
	ScopeKey  string
}

func (h *ChannelInstancesHandler) resolveContextPath(w http.ResponseWriter, r *http.Request) (channelContextPath, bool) {
	inst, ok := h.resolveInstance(w, r)
	if !ok {
		return channelContextPath{}, false
	}
	scopeType := r.PathValue("scopeType")
	scopeKey := r.PathValue("scopeKey")
	if !validateHTTPChannelScope(scopeType, scopeKey) {
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "invalid scope"))
		return channelContextPath{}, false
	}
	return channelContextPath{Instance: inst, ScopeType: scopeType, ScopeKey: scopeKey}, true
}

func validateHTTPChannelScope(scopeType, scopeKey string) bool {
	scope := store.ChannelContextScope{ChannelInstanceName: "placeholder", ScopeType: scopeType, ScopeKey: scopeKey}
	return scope.Valid()
}

func (h *ChannelInstancesHandler) requireContextAdmin(w http.ResponseWriter, r *http.Request) bool {
	return requireTenantAdmin(w, r, h.tenantStore)
}

type contextMCPGrantRequest struct {
	Enabled         *bool           `json:"enabled,omitempty"`
	ToolAllow       json.RawMessage `json:"tool_allow,omitempty"`
	ToolDeny        json.RawMessage `json:"tool_deny,omitempty"`
	ConfigOverrides json.RawMessage `json:"config_overrides,omitempty"`
}

func (h *ChannelInstancesHandler) handleListContextMCPGrants(w http.ResponseWriter, r *http.Request) {
	if h.mcpContextStore == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "MCP context store is not available"})
		return
	}
	if !h.requireContextAdmin(w, r) {
		return
	}
	path, ok := h.resolveContextPath(w, r)
	if !ok {
		return
	}
	grants, err := h.mcpContextStore.ListContextGrants(r.Context(), path.Instance.ID, path.ScopeType, path.ScopeKey)
	if err != nil {
		slog.Error("channel_context.mcp_grants.list", "error", err)
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "MCP context grants"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": grants})
}

func (h *ChannelInstancesHandler) handleUpsertContextMCPGrant(w http.ResponseWriter, r *http.Request) {
	if h.mcpContextStore == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "MCP context store is not available"})
		return
	}
	if !h.requireContextAdmin(w, r) {
		return
	}
	path, ok := h.resolveContextPath(w, r)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	serverID, err := uuid.Parse(r.PathValue("serverID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "server"))
		return
	}
	var req contextMCPGrantRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON))
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	grant := &store.MCPContextGrant{
		ChannelInstanceID: path.Instance.ID,
		ScopeType:         path.ScopeType,
		ScopeKey:          path.ScopeKey,
		ServerID:          serverID,
		Enabled:           enabled,
		ToolAllow:         req.ToolAllow,
		ToolDeny:          req.ToolDeny,
		ConfigOverrides:   req.ConfigOverrides,
		GrantedBy:         store.UserIDFromContext(r.Context()),
	}
	if err := h.mcpContextStore.UpsertContextGrant(r.Context(), grant); err != nil {
		slog.Error("channel_context.mcp_grants.upsert", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, err.Error())
		return
	}
	emitAudit(h.msgBus, r, "channel_context.mcp_grant_upserted", "mcp_server", serverID.String())
	writeJSON(w, http.StatusOK, map[string]any{"status": "updated"})
}

func (h *ChannelInstancesHandler) handleDeleteContextMCPGrant(w http.ResponseWriter, r *http.Request) {
	if h.mcpContextStore == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "MCP context store is not available"})
		return
	}
	if !h.requireContextAdmin(w, r) {
		return
	}
	path, ok := h.resolveContextPath(w, r)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	serverID, err := uuid.Parse(r.PathValue("serverID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "server"))
		return
	}
	if err := h.mcpContextStore.DeleteContextGrant(r.Context(), path.Instance.ID, path.ScopeType, path.ScopeKey, serverID); err != nil {
		slog.Error("channel_context.mcp_grants.delete", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, err.Error())
		return
	}
	emitAudit(h.msgBus, r, "channel_context.mcp_grant_deleted", "mcp_server", serverID.String())
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

type contextMCPCredentialSummary struct {
	ChannelInstanceID uuid.UUID `json:"channel_instance_id"`
	ScopeType         string    `json:"scope_type"`
	ScopeKey          string    `json:"scope_key"`
	ServerID          uuid.UUID `json:"server_id"`
	HasAPIKey         bool      `json:"has_api_key"`
	HasHeaders        bool      `json:"has_headers"`
	HasEnv            bool      `json:"has_env"`
	CreatedBy         string    `json:"created_by"`
}

func summarizeMCPCredentials(creds store.MCPContextCredentials) contextMCPCredentialSummary {
	return contextMCPCredentialSummary{
		ChannelInstanceID: creds.ChannelInstanceID,
		ScopeType:         creds.ScopeType,
		ScopeKey:          creds.ScopeKey,
		ServerID:          creds.ServerID,
		HasAPIKey:         creds.APIKey != "",
		HasHeaders:        len(creds.Headers) > 0,
		HasEnv:            len(creds.Env) > 0,
		CreatedBy:         creds.CreatedBy,
	}
}

func (h *ChannelInstancesHandler) handleListContextMCPCredentials(w http.ResponseWriter, r *http.Request) {
	if h.mcpContextStore == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "MCP context store is not available"})
		return
	}
	if !h.requireContextAdmin(w, r) {
		return
	}
	path, ok := h.resolveContextPath(w, r)
	if !ok {
		return
	}
	creds, err := h.mcpContextStore.ListContextCredentials(r.Context(), path.Instance.ID, path.ScopeType, path.ScopeKey)
	if err != nil {
		slog.Error("channel_context.mcp_credentials.list", "error", err)
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "MCP context credentials"))
		return
	}
	out := make([]contextMCPCredentialSummary, 0, len(creds))
	for _, c := range creds {
		out = append(out, summarizeMCPCredentials(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
}

func (h *ChannelInstancesHandler) handleSetContextMCPCredentials(w http.ResponseWriter, r *http.Request) {
	if h.mcpContextStore == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "MCP context store is not available"})
		return
	}
	if !h.requireContextAdmin(w, r) {
		return
	}
	path, ok := h.resolveContextPath(w, r)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	serverID, err := uuid.Parse(r.PathValue("serverID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "server"))
		return
	}
	var creds store.MCPContextCredentials
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&creds); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON))
		return
	}
	creds.ChannelInstanceID = path.Instance.ID
	creds.ScopeType = path.ScopeType
	creds.ScopeKey = path.ScopeKey
	creds.ServerID = serverID
	creds.CreatedBy = store.UserIDFromContext(r.Context())
	if err := h.mcpContextStore.SetContextCredentials(r.Context(), &creds); err != nil {
		slog.Error("channel_context.mcp_credentials.set", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, err.Error())
		return
	}
	emitAudit(h.msgBus, r, "channel_context.mcp_credentials_set", "mcp_server", serverID.String())
	writeJSON(w, http.StatusOK, map[string]any{"status": "updated"})
}

func (h *ChannelInstancesHandler) handleDeleteContextMCPCredentials(w http.ResponseWriter, r *http.Request) {
	if h.mcpContextStore == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "MCP context store is not available"})
		return
	}
	if !h.requireContextAdmin(w, r) {
		return
	}
	path, ok := h.resolveContextPath(w, r)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	serverID, err := uuid.Parse(r.PathValue("serverID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "server"))
		return
	}
	if err := h.mcpContextStore.DeleteContextCredentials(r.Context(), path.Instance.ID, path.ScopeType, path.ScopeKey, serverID); err != nil {
		slog.Error("channel_context.mcp_credentials.delete", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, err.Error())
		return
	}
	emitAudit(h.msgBus, r, "channel_context.mcp_credentials_deleted", "mcp_server", serverID.String())
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

type contextCLIGrantRequest struct {
	Enabled        *bool            `json:"enabled,omitempty"`
	EnvVars        json.RawMessage  `json:"env_vars,omitempty"`
	DenyArgs       *json.RawMessage `json:"deny_args,omitempty"`
	DenyVerbose    *json.RawMessage `json:"deny_verbose,omitempty"`
	TimeoutSeconds *int             `json:"timeout_seconds,omitempty"`
	Tips           *string          `json:"tips,omitempty"`
}

func (h *ChannelInstancesHandler) handleListContextCLIGrants(w http.ResponseWriter, r *http.Request) {
	if h.cliContextStore == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "Secure CLI context store is not available"})
		return
	}
	if !h.requireContextAdmin(w, r) {
		return
	}
	path, ok := h.resolveContextPath(w, r)
	if !ok {
		return
	}
	grants, err := h.cliContextStore.ListContextGrants(r.Context(), path.Instance.ID, path.ScopeType, path.ScopeKey)
	if err != nil {
		slog.Error("channel_context.cli_grants.list", "error", err)
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "CLI context grants"))
		return
	}
	for i := range grants {
		populateContextGrantEnvFields(&grants[i])
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": grants})
}

func populateContextGrantEnvFields(g *store.SecureCLIContextGrant) {
	if len(g.EncryptedEnv) == 0 {
		g.EnvKeys = []string{}
		g.Env = nil
		g.EnvSet = false
		return
	}
	g.EnvKeys = store.SecureCLIEnvKeys(g.EncryptedEnv)
	g.Env = store.SanitizeSecureCLIEnvJSON(g.EncryptedEnv)
	g.EnvSet = len(g.EnvKeys) > 0
}

func (h *ChannelInstancesHandler) handleUpsertContextCLIGrant(w http.ResponseWriter, r *http.Request) {
	if h.cliContextStore == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "Secure CLI context store is not available"})
		return
	}
	if !h.requireContextAdmin(w, r) {
		return
	}
	path, ok := h.resolveContextPath(w, r)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	binaryID, err := uuid.Parse(r.PathValue("binaryID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "credential"))
		return
	}
	var req contextCLIGrantRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON))
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	var envJSON []byte
	if len(req.EnvVars) > 0 {
		var ok bool
		envJSON, ok = validateAndSerializeEnvVars(w, locale, req.EnvVars)
		if !ok {
			return
		}
	}
	grant := &store.SecureCLIContextGrant{
		ChannelInstanceID: path.Instance.ID,
		ScopeType:         path.ScopeType,
		ScopeKey:          path.ScopeKey,
		BinaryID:          binaryID,
		DenyArgs:          req.DenyArgs,
		DenyVerbose:       req.DenyVerbose,
		TimeoutSeconds:    req.TimeoutSeconds,
		Tips:              req.Tips,
		EncryptedEnv:      envJSON,
		Enabled:           enabled,
		GrantedBy:         store.UserIDFromContext(r.Context()),
	}
	if err := h.cliContextStore.UpsertContextGrant(r.Context(), grant); err != nil {
		slog.Error("channel_context.cli_grants.upsert", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, err.Error())
		return
	}
	emitAudit(h.msgBus, r, "channel_context.cli_grant_upserted", "secure_cli", binaryID.String())
	writeJSON(w, http.StatusOK, map[string]any{"status": "updated"})
}

func (h *ChannelInstancesHandler) handleDeleteContextCLIGrant(w http.ResponseWriter, r *http.Request) {
	if h.cliContextStore == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "Secure CLI context store is not available"})
		return
	}
	if !h.requireContextAdmin(w, r) {
		return
	}
	path, ok := h.resolveContextPath(w, r)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	binaryID, err := uuid.Parse(r.PathValue("binaryID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "credential"))
		return
	}
	if err := h.cliContextStore.DeleteContextGrant(r.Context(), path.Instance.ID, path.ScopeType, path.ScopeKey, binaryID); err != nil {
		slog.Error("channel_context.cli_grants.delete", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, err.Error())
		return
	}
	emitAudit(h.msgBus, r, "channel_context.cli_grant_deleted", "secure_cli", binaryID.String())
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}

type contextCLICredentialRequest struct {
	EnvVars        json.RawMessage `json:"env_vars"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	CredentialType *string         `json:"credential_type,omitempty"`
	HostScope      *string         `json:"host_scope,omitempty"`
}

type contextCLICredentialSummary struct {
	ChannelInstanceID uuid.UUID       `json:"channel_instance_id"`
	ScopeType         string          `json:"scope_type"`
	ScopeKey          string          `json:"scope_key"`
	BinaryID          uuid.UUID       `json:"binary_id"`
	EnvSet            bool            `json:"env_set"`
	EnvKeys           []string        `json:"env_keys"`
	Metadata          json.RawMessage `json:"metadata,omitempty"`
	CredentialType    *string         `json:"credential_type,omitempty"`
	HostScope         *string         `json:"host_scope,omitempty"`
	CreatedBy         string          `json:"created_by"`
}

func summarizeCLICredentials(creds store.SecureCLIContextCredentials) contextCLICredentialSummary {
	keys := store.SecureCLIEnvKeys(creds.EncryptedEnv)
	return contextCLICredentialSummary{
		ChannelInstanceID: creds.ChannelInstanceID,
		ScopeType:         creds.ScopeType,
		ScopeKey:          creds.ScopeKey,
		BinaryID:          creds.BinaryID,
		EnvSet:            len(keys) > 0,
		EnvKeys:           keys,
		Metadata:          creds.Metadata,
		CredentialType:    creds.CredentialType,
		HostScope:         creds.HostScope,
		CreatedBy:         creds.CreatedBy,
	}
}

func (h *ChannelInstancesHandler) handleListContextCLICredentials(w http.ResponseWriter, r *http.Request) {
	if h.cliContextStore == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "Secure CLI context store is not available"})
		return
	}
	if !h.requireContextAdmin(w, r) {
		return
	}
	path, ok := h.resolveContextPath(w, r)
	if !ok {
		return
	}
	creds, err := h.cliContextStore.ListContextCredentials(r.Context(), path.Instance.ID, path.ScopeType, path.ScopeKey)
	if err != nil {
		slog.Error("channel_context.cli_credentials.list", "error", err)
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "CLI context credentials"))
		return
	}
	out := make([]contextCLICredentialSummary, 0, len(creds))
	for _, c := range creds {
		out = append(out, summarizeCLICredentials(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
}

func (h *ChannelInstancesHandler) handleSetContextCLICredentials(w http.ResponseWriter, r *http.Request) {
	if h.cliContextStore == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "Secure CLI context store is not available"})
		return
	}
	if !h.requireContextAdmin(w, r) {
		return
	}
	path, ok := h.resolveContextPath(w, r)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	binaryID, err := uuid.Parse(r.PathValue("binaryID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "credential"))
		return
	}
	var req contextCLICredentialRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON))
		return
	}
	if len(req.EnvVars) == 0 {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "env_vars"))
		return
	}
	envJSON, ok := validateAndSerializeEnvVars(w, locale, req.EnvVars)
	if !ok {
		return
	}
	creds := &store.SecureCLIContextCredentials{
		ChannelInstanceID: path.Instance.ID,
		ScopeType:         path.ScopeType,
		ScopeKey:          path.ScopeKey,
		BinaryID:          binaryID,
		EncryptedEnv:      envJSON,
		Metadata:          req.Metadata,
		CredentialType:    req.CredentialType,
		HostScope:         req.HostScope,
		CreatedBy:         store.UserIDFromContext(r.Context()),
	}
	if err := h.cliContextStore.SetContextCredentials(r.Context(), creds); err != nil {
		slog.Error("channel_context.cli_credentials.set", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, err.Error())
		return
	}
	emitAudit(h.msgBus, r, "channel_context.cli_credentials_set", "secure_cli", binaryID.String())
	writeJSON(w, http.StatusOK, map[string]any{"status": "updated"})
}

func (h *ChannelInstancesHandler) handleDeleteContextCLICredentials(w http.ResponseWriter, r *http.Request) {
	if h.cliContextStore == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "Secure CLI context store is not available"})
		return
	}
	if !h.requireContextAdmin(w, r) {
		return
	}
	path, ok := h.resolveContextPath(w, r)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	binaryID, err := uuid.Parse(r.PathValue("binaryID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "credential"))
		return
	}
	if err := h.cliContextStore.DeleteContextCredentials(r.Context(), path.Instance.ID, path.ScopeType, path.ScopeKey, binaryID); err != nil {
		slog.Error("channel_context.cli_credentials.delete", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, err.Error())
		return
	}
	emitAudit(h.msgBus, r, "channel_context.cli_credentials_deleted", "secure_cli", binaryID.String())
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted"})
}
