package http

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (h *SecureCLIHandler) handleListUserCredentials(w http.ResponseWriter, r *http.Request) {
	binaryID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		locale := store.LocaleFromContext(r.Context())
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID)})
		return
	}
	creds, err := h.store.ListUserCredentials(r.Context(), binaryID)
	if err != nil {
		locale := store.LocaleFromContext(r.Context())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}
	// Return without env values for listing (names only + timestamps).
	// credential_type + host_scope surface so the table can show a Type badge
	// (PAT / SSH / Env) without a second round-trip per row.
	type entry struct {
		ID             uuid.UUID                                  `json:"id"`
		BinaryID       uuid.UUID                                  `json:"binary_id"`
		UserID         string                                     `json:"user_id"`
		HasEnv         bool                                       `json:"has_env"`
		EnvKeys        []string                                   `json:"env_keys,omitempty"`
		Env            map[string]store.SecureCLIEnvResponseEntry `json:"env,omitempty"`
		CredentialType *string                                    `json:"credential_type,omitempty"`
		HostScope      *string                                    `json:"host_scope,omitempty"`
		CreatedAt      string                                     `json:"created_at"`
		UpdatedAt      string                                     `json:"updated_at"`
	}
	entries := make([]entry, 0, len(creds))
	for _, c := range creds {
		envKeys := envKeysFromDecryptedJSON(c.EncryptedEnv)
		// Typed credentials (pat / ssh_key) hold a single secret in the blob.
		// Suppress env / env_keys for them — those keys would leak the wire
		// shape (`token` / `key`) to the listing endpoint.
		isTyped := c.CredentialType != nil && *c.CredentialType != "" && *c.CredentialType != "env"
		e := entry{
			ID:             c.ID,
			BinaryID:       c.BinaryID,
			UserID:         c.UserID,
			HasEnv:         len(c.EncryptedEnv) > 0,
			CredentialType: c.CredentialType,
			HostScope:      c.HostScope,
			CreatedAt:      c.CreatedAt,
			UpdatedAt:      c.UpdatedAt,
		}
		if !isTyped {
			e.EnvKeys = envKeys
			e.Env = store.SanitizeSecureCLIEnvJSON(c.EncryptedEnv)
		}
		entries = append(entries, e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"user_credentials": entries})
}

func (h *SecureCLIHandler) handleGetUserCredentials(w http.ResponseWriter, r *http.Request) {
	binaryID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		locale := store.LocaleFromContext(r.Context())
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID)})
		return
	}
	userID := r.PathValue("userId")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id required"})
		return
	}

	cred, err := h.store.GetUserCredentials(r.Context(), binaryID, userID)
	if err != nil {
		locale := store.LocaleFromContext(r.Context())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}
	if cred == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	// Typed creds suppress env to avoid leaking the blob shape (`token`/`key`).
	isTyped := cred.CredentialType != nil && *cred.CredentialType != "" && *cred.CredentialType != "env"
	resp := map[string]any{
		"user_id":         cred.UserID,
		"credential_type": cred.CredentialType,
		"host_scope":      cred.HostScope,
		"has_secret":      len(cred.EncryptedEnv) > 0,
	}
	if !isTyped {
		resp["env"] = store.SanitizeSecureCLIEnvJSON(cred.EncryptedEnv)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *SecureCLIHandler) handleSetUserCredentials(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	binaryID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID)})
		return
	}
	userID := r.PathValue("userId")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id required"})
		return
	}

	var body struct {
		Env json.RawMessage `json:"env"`
		typedCredentialBody
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}

	// Typed branch (pat / ssh_key): validate + encrypt blob, store with
	// credential_type + host_scope. Audit emits credential_type only — never
	// the secret or host (host is operator-visible config but still scoped to
	// the audit channel).
	envBytes, credType, hostScope, terr := prepareTypedCredentialEnv(locale, body.typedCredentialBody)
	if terr != nil {
		writeTypedCredentialError(w, terr)
		return
	}
	if envBytes != nil {
		if err := h.store.SetUserCredentialsTyped(r.Context(), binaryID, userID, envBytes, credType, hostScope); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
			return
		}
		emitAudit(h.msgBus, r, "secure_cli.user_credentials.updated", "secure_cli_user_credentials", binaryID.String()+"/"+userID+"#"+*credType)
		h.emitCacheInvalidate("")
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// Legacy env-paste branch (no credential_type or credential_type="env").
	if len(body.Env) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "env is required"})
		return
	}

	existing, err := h.store.GetUserCredentials(r.Context(), binaryID, userID)
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

	if err := h.store.SetUserCredentials(r.Context(), binaryID, userID, envJSON); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}

	emitAudit(h.msgBus, r, "secure_cli.user_credentials.updated", "secure_cli_user_credentials", binaryID.String()+"/"+userID)
	h.emitCacheInvalidate("")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *SecureCLIHandler) handleDeleteUserCredentials(w http.ResponseWriter, r *http.Request) {
	binaryID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		locale := store.LocaleFromContext(r.Context())
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID)})
		return
	}
	userID := r.PathValue("userId")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_id required"})
		return
	}

	if err := h.store.DeleteUserCredentials(r.Context(), binaryID, userID); err != nil {
		locale := store.LocaleFromContext(r.Context())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}

	emitAudit(h.msgBus, r, "secure_cli.user_credentials.deleted", "secure_cli_user_credentials", binaryID.String()+"/"+userID)
	h.emitCacheInvalidate("")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
