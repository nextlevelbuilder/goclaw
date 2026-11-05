package methods

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// SecretVaultMethods lets the user who owns a session answer the Secure Form
// and approval requests an agent raised. Every call is bound to the caller's
// tenant and user; params are never logged because they carry secret values.
type SecretVaultMethods struct {
	forms *secretvault.FormManager
}

func NewSecretVaultMethods(forms *secretvault.FormManager) *SecretVaultMethods {
	return &SecretVaultMethods{forms: forms}
}

func (m *SecretVaultMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodSecretVaultFormSubmit, m.handleSubmit)
	router.Register(protocol.MethodSecretVaultFormCancel, m.handleCancel)
	router.Register(protocol.MethodSecretVaultApprovalRespond, m.handleApproval)
}

func (m *SecretVaultMethods) handleSubmit(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		ID         string `json:"id"`
		SessionKey string `json:"sessionKey"`
		Scope      string `json:"scope"`
		Username   string `json:"username"`
		Secret     string `json:"secret"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}
	if params.ID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "id")))
		return
	}

	err := m.forms.Submit(ctx, client.TenantID(), client.UserID(), params.ID, params.SessionKey, params.Scope,
		store.SecretVaultPayload{Username: params.Username, Secret: params.Secret})
	if err != nil {
		client.SendResponse(m.errorResponse(req.ID, locale, err))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"submitted": true}))
}

func (m *SecretVaultMethods) handleCancel(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		ID string `json:"id"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}
	if params.ID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "id")))
		return
	}
	if err := m.forms.Cancel(client.TenantID(), client.UserID(), params.ID); err != nil {
		client.SendResponse(m.errorResponse(req.ID, locale, err))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"cancelled": true}))
}

func (m *SecretVaultMethods) handleApproval(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		ID      string `json:"id"`
		Approve bool   `json:"approve"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}
	if params.ID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "id")))
		return
	}
	if err := m.forms.Respond(client.TenantID(), client.UserID(), params.ID, params.Approve, client.Role()); err != nil {
		client.SendResponse(m.errorResponse(req.ID, locale, err))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"approved": params.Approve}))
}

// errorResponse maps manager errors to localized, value-free messages.
func (m *SecretVaultMethods) errorResponse(id, locale string, err error) *protocol.ResponseFrame {
	switch {
	case errors.Is(err, secretvault.ErrRequestNotFound):
		return protocol.NewErrorResponse(id, protocol.ErrNotFound, i18n.T(locale, i18n.MsgSecretFormNotFound))
	case errors.Is(err, secretvault.ErrScopeInvalid):
		return protocol.NewErrorResponse(id, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgSecretFormScopeInvalid))
	case errors.Is(err, store.ErrSecretVaultNoKey):
		return protocol.NewErrorResponse(id, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgSecretVaultNoKey))
	case errors.Is(err, store.ErrSecretVaultInvalid):
		return protocol.NewErrorResponse(id, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgSecretVaultPayloadInvalid))
	default:
		slog.Warn("secretvault.form.apply_failed")
		return protocol.NewErrorResponse(id, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "secret vault"))
	}
}
