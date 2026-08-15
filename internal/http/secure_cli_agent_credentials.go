package http

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type agentCredentialEntry struct {
	ID             uuid.UUID                                  `json:"id"`
	BinaryID       uuid.UUID                                  `json:"binary_id"`
	AgentID        uuid.UUID                                  `json:"agent_id"`
	AgentKey       string                                     `json:"agent_key,omitempty"`
	Name           string                                     `json:"name,omitempty"`
	HasSecret      bool                                       `json:"has_secret"`
	EnvKeys        []string                                   `json:"env_keys,omitempty"`
	Env            map[string]store.SecureCLIEnvResponseEntry `json:"env,omitempty"`
	CredentialType *string                                    `json:"credential_type,omitempty"`
	HostScope      *string                                    `json:"host_scope,omitempty"`
	CreatedBy      string                                     `json:"created_by,omitempty"`
	CreatedAt      string                                     `json:"created_at"`
	UpdatedAt      string                                     `json:"updated_at"`
}

func (h *SecureCLIHandler) requireAgentCredentialStore(w http.ResponseWriter, r *http.Request) (store.SecureCLIAgentCredentialStore, bool) {
	if h.agentCreds == nil {
		locale := store.LocaleFromContext(r.Context())
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "agent credentials store unavailable")})
		return nil, false
	}
	return h.agentCreds, true
}

func (h *SecureCLIHandler) requireAgentCredentialAdmin(w http.ResponseWriter, r *http.Request) bool {
	return requireTenantAdmin(w, r, h.tenants)
}

func agentCredentialResponse(c store.SecureCLIAgentCredential) agentCredentialEntry {
	isTyped := c.CredentialType != nil && *c.CredentialType != "" && *c.CredentialType != "env"
	e := agentCredentialEntry{
		ID:             c.ID,
		BinaryID:       c.BinaryID,
		AgentID:        c.AgentID,
		AgentKey:       c.AgentKey,
		Name:           c.Name,
		HasSecret:      len(c.EncryptedEnv) > 0,
		CredentialType: c.CredentialType,
		HostScope:      c.HostScope,
		CreatedBy:      c.CreatedBy,
		CreatedAt:      c.CreatedAt,
		UpdatedAt:      c.UpdatedAt,
	}
	if !isTyped {
		e.EnvKeys = envKeysFromDecryptedJSON(c.EncryptedEnv)
		e.Env = store.SanitizeSecureCLIEnvJSON(c.EncryptedEnv)
	}
	return e
}

func parseAgentCredentialPath(w http.ResponseWriter, r *http.Request, locale string) (uuid.UUID, uuid.UUID, bool) {
	binaryID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "credential")})
		return uuid.Nil, uuid.Nil, false
	}
	agentID, err := uuid.Parse(r.PathValue("agentId"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return uuid.Nil, uuid.Nil, false
	}
	return binaryID, agentID, true
}

func (h *SecureCLIHandler) handleListAgentCredentials(w http.ResponseWriter, r *http.Request) {
	if !h.requireAgentCredentialAdmin(w, r) {
		return
	}
	agentCreds, ok := h.requireAgentCredentialStore(w, r)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	binaryID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "credential")})
		return
	}
	creds, err := agentCreds.ListAgentCredentials(r.Context(), binaryID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}
	entries := make([]agentCredentialEntry, 0, len(creds))
	for _, c := range creds {
		entries = append(entries, agentCredentialResponse(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent_credentials": entries})
}

func (h *SecureCLIHandler) handleGetAgentCredentials(w http.ResponseWriter, r *http.Request) {
	if !h.requireAgentCredentialAdmin(w, r) {
		return
	}
	agentCreds, ok := h.requireAgentCredentialStore(w, r)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	binaryID, agentID, ok := parseAgentCredentialPath(w, r, locale)
	if !ok {
		return
	}
	cred, err := agentCreds.GetAgentCredentials(r.Context(), binaryID, agentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}
	if cred == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "agent credential", agentID.String())})
		return
	}
	writeJSON(w, http.StatusOK, agentCredentialResponse(*cred))
}

func (h *SecureCLIHandler) handleSetAgentCredentials(w http.ResponseWriter, r *http.Request) {
	if !h.requireAgentCredentialAdmin(w, r) {
		return
	}
	agentCreds, ok := h.requireAgentCredentialStore(w, r)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	binaryID, agentID, ok := parseAgentCredentialPath(w, r, locale)
	if !ok {
		return
	}
	if exists, err := agentCreds.BinaryExists(r.Context(), binaryID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "validate credential")})
		return
	} else if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "credential", binaryID.String())})
		return
	}
	if exists, err := agentCreds.AgentExists(r.Context(), agentID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, "validate agent")})
		return
	} else if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "agent", agentID.String())})
		return
	}

	var body struct {
		Env json.RawMessage `json:"env"`
		typedCredentialBody
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}
	envBytes, credType, hostScope, terr := prepareTypedCredentialEnv(r.Context(), locale, body.typedCredentialBody)
	if terr != nil {
		writeTypedCredentialError(w, terr)
		return
	}
	createdBy := store.UserIDFromContext(r.Context())
	if envBytes != nil {
		if err := agentCreds.SetAgentCredentialsTyped(r.Context(), binaryID, agentID, envBytes, credType, hostScope, createdBy); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
			return
		}
		emitAudit(h.msgBus, r, "secure_cli.agent_credentials.updated", "secure_cli_agent_credentials", binaryID.String()+"/"+agentID.String()+"#"+*credType)
		h.emitCacheInvalidate("")
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if len(body.Env) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "env")})
		return
	}
	existing, err := agentCreds.GetAgentCredentials(r.Context(), binaryID, agentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}
	var existingEnv []byte
	if existing != nil {
		existingEnv = existing.EncryptedEnv
	}
	envJSON, err := store.MergeSecureCLIEnv(existingEnv, body.Env)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgGrantEnvValueInvalid, err.Error())})
		return
	}
	envJSON, ok = validateAndSerializeEnvVars(w, locale, json.RawMessage(envJSON))
	if !ok {
		return
	}
	if err := agentCreds.SetAgentCredentials(r.Context(), binaryID, agentID, envJSON, createdBy); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}
	emitAudit(h.msgBus, r, "secure_cli.agent_credentials.updated", "secure_cli_agent_credentials", binaryID.String()+"/"+agentID.String())
	h.emitCacheInvalidate("")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *SecureCLIHandler) handleDeleteAgentCredentials(w http.ResponseWriter, r *http.Request) {
	if !h.requireAgentCredentialAdmin(w, r) {
		return
	}
	agentCreds, ok := h.requireAgentCredentialStore(w, r)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	binaryID, agentID, ok := parseAgentCredentialPath(w, r, locale)
	if !ok {
		return
	}
	if err := agentCreds.DeleteAgentCredentials(r.Context(), binaryID, agentID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}
	emitAudit(h.msgBus, r, "secure_cli.agent_credentials.deleted", "secure_cli_agent_credentials", binaryID.String()+"/"+agentID.String())
	h.emitCacheInvalidate("")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
