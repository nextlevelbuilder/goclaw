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
	// Return without env values for listing (names only + timestamps)
	type entry struct {
		ID        uuid.UUID `json:"id"`
		BinaryID  uuid.UUID `json:"binary_id"`
		UserID    string    `json:"user_id"`
		HasEnv    bool      `json:"has_env"`
		EnvKeys   []string  `json:"env_keys,omitempty"`
		CreatedAt string    `json:"created_at"`
		UpdatedAt string    `json:"updated_at"`
	}
	entries := make([]entry, 0, len(creds))
	for _, c := range creds {
		envKeys := envKeysFromDecryptedJSON(c.EncryptedEnv)
		entries = append(entries, entry{
			ID:        c.ID,
			BinaryID:  c.BinaryID,
			UserID:    c.UserID,
			HasEnv:    len(c.EncryptedEnv) > 0,
			EnvKeys:   envKeys,
			CreatedAt: c.CreatedAt,
			UpdatedAt: c.UpdatedAt,
		})
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

	// Return decrypted env as JSON object (admin-only endpoint)
	var envObj any
	if len(cred.EncryptedEnv) > 0 {
		_ = json.Unmarshal(cred.EncryptedEnv, &envObj)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id": cred.UserID,
		"env":     envObj,
	})
}

func (h *SecureCLIHandler) handleSetUserCredentials(w http.ResponseWriter, r *http.Request) {
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

	var body struct {
		Env json.RawMessage `json:"env"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if len(body.Env) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "env is required"})
		return
	}

	// Validate env is a JSON object
	var envCheck map[string]string
	if err := json.Unmarshal(body.Env, &envCheck); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "env must be a JSON object with string values"})
		return
	}

	if err := h.store.SetUserCredentials(r.Context(), binaryID, userID, body.Env); err != nil {
		locale := store.LocaleFromContext(r.Context())
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
