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
	// Non-admin callers only see their own keys.
	ownerID := ""
	if !permissions.HasMinRole(client.Role(), permissions.RoleAdmin) {
		ownerID = client.UserID()
	}
	keys, err := m.apiKeys.List(ctx, ownerID)
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
		OwnerID   string   `json:"owner_id"`   // optional; non-admin callers always get their own user_id
		TenantID  string   `json:"tenant_id"`  // optional UUID; cross-tenant callers may specify or omit (NULL = system key)
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

	// Non-admin callers always bind the key to their own user_id.
	ownerID := params.OwnerID
	if !permissions.HasMinRole(client.Role(), permissions.RoleAdmin) {
		ownerID = client.UserID()
	}

	raw, hash, prefix, err := crypto.GenerateAPIKey()
	if err != nil {
		slog.Error("api_keys.generate failed", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "key generation")))
		return
	}

	// Resolve tenant_id based on caller type.
	var tenantID uuid.UUID // uuid.Nil = system-level (NULL in DB)
	if client.IsOwner() {
		if params.TenantID != "" {
			tid, err := uuid.Parse(params.TenantID)
			if err != nil {
				client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "tenant_id")))
				return
			}
			tenantID = tid
		}
		// else: uuid.Nil stays → system-level key
	} else if client.HasScope(permissions.ScopeProvision) {
		// Provision-scoped callers may create tenant-bound keys only (not system-level).
		if params.TenantID != "" {
			tid, err := uuid.Parse(params.TenantID)
			if err != nil {
				client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "tenant_id")))
				return
			}
			tenantID = tid
		} else {
			tenantID = client.TenantID()
		}
	} else {
		tenantID = client.TenantID()
	}

	now := time.Now()
	key := &store.APIKeyData{
		ID:        store.GenNewID(),
		Name:      params.Name,
		Prefix:    prefix,
		KeyHash:   hash,
		Scopes:    params.Scopes,
		OwnerID:   ownerID,
		TenantID:  tenantID,
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

// handleRevoke revokes an API key after verifying ownership.
//
// Phase 0b hotfix: store-layer Revoke SQL matches on
// `tenant_id = $N OR tenant_id IS NULL`, which previously allowed any
// tenant admin to revoke system-level (NULL-tenant) API keys. The fix
// pre-fetches the key and enforces strict tenant match for non-owner
// callers. Non-admin callers continue to use the ownerID filter path
// (unchanged behaviour for personal keys).
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

	// Non-admin callers can only revoke their own keys — personal-key path,
	// ownerID filter enforced by the store layer.
	ownerID := ""
	if !permissions.HasMinRole(client.Role(), permissions.RoleAdmin) {
		ownerID = client.UserID()
	}

	// Admin path: verify the target key belongs to the caller's tenant (or
	// caller is a system owner) before revoking. Personal-key path skips
	// this because the ownerID filter already scopes to the caller.
	//
	// NOTE: Use client.IsOwner() — NOT store.IsOwnerRole(ctx). The WS router
	// does not inject role into ctx (see router.go handleRequest), so the
	// ctx-based helper is dead here. Client carries the authoritative role
	// from connect.
	if ownerID == "" && !client.IsOwner() {
		key, gerr := m.apiKeys.Get(ctx, id)
		if gerr != nil || key == nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "API key", params.ID)))
			return
		}
		callerTID := store.TenantIDFromContext(ctx)
		if key.TenantID == uuid.Nil || key.TenantID != callerTID {
			slog.Warn("security.api_key_revoke_forbidden",
				"key_id", params.ID,
				"caller_tenant", callerTID,
				"key_tenant", key.TenantID,
				"user_id", client.UserID(),
			)
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "API key")))
			return
		}
	}

	if err := m.apiKeys.Revoke(ctx, id, ownerID); err != nil {
		slog.Error("api_keys.revoke failed", "error", err, "id", params.ID)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "API key", params.ID)))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]string{"status": "revoked"}))
}
