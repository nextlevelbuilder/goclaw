package methods

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
	zalooa "github.com/nextlevelbuilder/goclaw/internal/channels/zalo/oa"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ZaloWebhookMethods serves the WS RPC returning the webhook path fragment
// the operator pastes into the Zalo developer console (path-only; operator
// prepends their own externally-reachable host).
type ZaloWebhookMethods struct {
	store store.ChannelInstanceStore
}

func NewZaloWebhookMethods(s store.ChannelInstanceStore) *ZaloWebhookMethods {
	return &ZaloWebhookMethods{store: s}
}

func (m *ZaloWebhookMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodChannelInstancesZaloWebhookURL, m.handleWebhookURL)
}

// handleWebhookURL validates instance ownership + channel type and returns
// {path, instance_id, hint}. Cross-tenant lookups return ErrNotFound to
// avoid leaking instance existence across tenants.
func (m *ZaloWebhookMethods) handleWebhookURL(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		InstanceID string `json:"instance_id"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}
	instID, err := uuid.Parse(params.InstanceID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "instance")))
		return
	}

	inst, err := m.store.Get(ctx, instID)
	if err != nil {
		slog.Warn("zalo.webhook_url.lookup_failed", "instance_id", instID, "tenant_id", client.TenantID(), "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgInstanceNotFound)))
		return
	}
	if inst.TenantID != client.TenantID() {
		slog.Warn("security.cross_tenant_access_attempt",
			"method", "zalo.webhook_url",
			"instance_id", instID,
			"instance_tenant_id", inst.TenantID,
			"client_tenant_id", client.TenantID())
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgInstanceNotFound)))
		return
	}
	if inst.ChannelType != channels.TypeZaloBot && inst.ChannelType != channels.TypeZaloOA {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgZaloWebhookWrongChannelType)))
		return
	}

	slug := resolveWebhookSlug(inst)
	path := common.WebhookPathPrefix + slug
	resp := map[string]any{
		"path":        path,
		"slug":        slug,
		"instance_id": instID.String(),
		"hint":        i18n.T(locale, i18n.MsgZaloWebhookPathHint),
	}
	// For zalo_oa, surface the auto-discovered OA ID read-only so operators
	// can confirm the connect handshake landed without re-checking creds.
	if inst.ChannelType == channels.TypeZaloOA {
		if creds, err := zalooa.LoadCreds(inst.Credentials); err == nil && creds.OAID != "" {
			resp["oa_id"] = creds.OAID
		}
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, resp))
}

// resolveWebhookSlug reads the webhook_path config field; if absent, derives
// from instance name so the RPC matches what the channel registers at Start.
func resolveWebhookSlug(inst *store.ChannelInstanceData) string {
	var cfg struct {
		WebhookPath string `json:"webhook_path,omitempty"`
	}
	if len(inst.Config) > 0 {
		_ = json.Unmarshal(inst.Config, &cfg)
	}
	if cfg.WebhookPath != "" {
		return cfg.WebhookPath
	}
	return common.DeriveSlugFromName(inst.Name)
}
