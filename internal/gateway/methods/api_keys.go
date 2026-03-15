package methods

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// APIKeysMethods handles api_keys.list, api_keys.create, api_keys.revoke.
type APIKeysMethods struct {
	apiKeys store.APIKeyStore
}

// NewAPIKeysMethods creates a new API keys method handler.
func NewAPIKeysMethods(apiKeys store.APIKeyStore) *APIKeysMethods {
	return &APIKeysMethods{apiKeys: apiKeys}
}

// Register registers API key management RPC methods.
func (m *APIKeysMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodAPIKeysList, m.handleList)
	router.Register(protocol.MethodAPIKeysCreate, m.handleCreate)
	router.Register(protocol.MethodAPIKeysRevoke, m.handleRevoke)
}

func (m *APIKeysMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	keys, err := m.apiKeys.List(ctx)
	if err != nil {
		slog.Error("api_keys.list failed", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "API keys")))
		return
	}
	if keys == nil {
		keys = []store.APIKeyData{}
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, keys))
}

func (m *APIKeysMethods) handleCreate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)

	var params struct {
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
		ExpiresIn *int     `json:"expires_in"` // seconds; nil = never
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return
		}
	}

	if params.Name == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name")))
		return
	}
	if len(params.Scopes) == 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "scopes")))
		return
	}

	// Validate scopes
	for _, s := range params.Scopes {
		if !permissions.ValidScope(s) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "invalid scope: "+s)))
			return
		}
	}

	raw, hash, prefix, err := crypto.GenerateAPIKey()
	if err != nil {
		slog.Error("api_keys.generate failed", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "key generation")))
		return
	}

	now := time.Now()
	key := &store.APIKeyData{
		ID:        store.GenNewID(),
		Name:      params.Name,
		Prefix:    prefix,
		KeyHash:   hash,
		Scopes:    params.Scopes,
		CreatedBy: store.UserIDFromContext(ctx),
		CreatedAt: now,
		UpdatedAt: now,
	}

	if params.ExpiresIn != nil && *params.ExpiresIn > 0 {
		exp := now.Add(time.Duration(*params.ExpiresIn) * time.Second)
		key.ExpiresAt = &exp
	}

	if err := m.apiKeys.Create(ctx, key); err != nil {
		slog.Error("api_keys.create failed", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "API key", "internal error")))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"id":         key.ID,
		"name":       key.Name,
		"prefix":     key.Prefix,
		"key":        raw,
		"scopes":     key.Scopes,
		"expires_at": key.ExpiresAt,
		"created_at": key.CreatedAt,
	}))
}

func (m *APIKeysMethods) handleRevoke(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)

	var params struct {
		ID string `json:"id"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return
		}
	}

	id, err := uuid.Parse(params.ID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "API key")))
		return
	}

	if err := m.apiKeys.Revoke(ctx, id); err != nil {
		slog.Error("api_keys.revoke failed", "error", err, "id", params.ID)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "API key", params.ID)))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]string{"status": "revoked"}))
}
