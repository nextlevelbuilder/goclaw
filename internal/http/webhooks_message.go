package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// webhookContentMaxBytes is the maximum allowed content field length (16 KB).
const webhookContentMaxBytes = 16 * 1024

// channelDispatcher is the subset of *channels.Manager used by WebhookMessageHandler.
// Declared as an interface so tests can substitute a stub without spinning up a full Manager.
type channelDispatcher interface {
	ChannelTenantID(channelName string) (uuid.UUID, bool)
	ChannelTypeForName(channelName string) string
	SendToChannel(ctx context.Context, channelName, chatID, content string) error
	SendMediaToChannel(ctx context.Context, channelName, chatID, content string, media []bus.MediaAttachment) error
}

// WebhookMessageHandler handles POST /v1/webhooks/message.
// Standard edition only — mount via edition.Current().AllowsChannels() gate.
// Auth is enforced by WebhookAuthMiddleware (phase 03) with kind="message".
type WebhookMessageHandler struct {
	channelMgr       channelDispatcher
	channelInstances store.ChannelInstanceStore
	callStore        store.WebhookCallStore
	webhooks         store.WebhookStore
	limiter          *webhookLimiter
	encKey           string // AES-256-GCM key for decrypting encrypted_secret at HMAC verify time
}

// NewWebhookMessageHandler constructs a WebhookMessageHandler.
// mgr must be *channels.Manager (satisfies channelDispatcher).
func NewWebhookMessageHandler(
	mgr *channels.Manager,
	channelInstances store.ChannelInstanceStore,
	callStore store.WebhookCallStore,
	webhooks store.WebhookStore,
	limiter *webhookLimiter,
) *WebhookMessageHandler {
	return &WebhookMessageHandler{
		channelMgr:       mgr,
		channelInstances: channelInstances,
		callStore:        callStore,
		webhooks:         webhooks,
		limiter:          limiter,
	}
}

// SetEncKey sets the AES-256-GCM encryption key for decrypting webhook secrets at HMAC verify time.
func (h *WebhookMessageHandler) SetEncKey(encKey string) {
	h.encKey = encKey
}

// RegisterRoutes mounts POST /v1/webhooks/message wrapped in the auth middleware.
// Only call when edition.Current().AllowsChannels() — callers enforce the gate.
func (h *WebhookMessageHandler) RegisterRoutes(mux *http.ServeMux) {
	authMW := WebhookAuthMiddleware(
		h.webhooks,
		h.callStore,
		h.limiter,
		h.encKey,
		"message",
		WebhookMaxBodyMessage,
	)
	mux.Handle("POST /v1/webhooks/message", authMW(http.HandlerFunc(h.handle)))
}

// webhookMessageReq is the JSON request body for POST /v1/webhooks/message.
type webhookMessageReq struct {
	// ChannelName is the channel instance name to deliver through.
	// Required when the webhook row has no bound channel_id.
	ChannelName string `json:"channel_name"`

	// ChatID is the channel-specific recipient identifier (required).
	ChatID string `json:"chat_id"`

	// Content is the text body (required unless media_url is set; max 16 KB).
	Content string `json:"content"`

	// MediaURL is an optional HTTPS URL to a media file.
	MediaURL string `json:"media_url,omitempty"`

	// MediaCaption is an optional caption attached to the media.
	MediaCaption string `json:"media_caption,omitempty"`

	// FallbackToText controls media-unsupported channel behavior:
	//   true  → drop media, send text only, 200 + warning
	//   false → 501 (default)
	FallbackToText bool `json:"fallback_to_text,omitempty"`
}

// webhookMessageResp is the success response envelope.
type webhookMessageResp struct {
	CallID      string `json:"call_id"`
	Status      string `json:"status"`            // always "sent"
	ChannelName string `json:"channel_name"`
	ChatID      string `json:"chat_id"`
	Warning     string `json:"warning,omitempty"` // set when media was dropped on fallback
}

// handle is the HTTP handler for POST /v1/webhooks/message.
func (h *WebhookMessageHandler) handle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)

	// Webhook row injected by WebhookAuthMiddleware — always present here.
	webhook := WebhookDataFromContext(ctx)
	if webhook == nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, "webhook context missing"))
		return
	}

	// Decode and validate request body.
	var req webhookMessageReq
	if !bindJSON(w, r, locale, &req) {
		return
	}

	// Resolve channel name: webhook-bound channel_id takes precedence.
	channelName, ok := h.resolveChannelName(ctx, w, webhook, req.ChannelName, locale)
	if !ok {
		return
	}

	// P0: Cross-tenant isolation — channel must belong to webhook's tenant.
	if !h.validateChannelTenant(ctx, w, webhook, channelName, locale) {
		return
	}

	// Field validation (after channel resolution so tenant check runs first).
	if req.ChatID == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgRequired, "chat_id"))
		return
	}
	if req.Content == "" && req.MediaURL == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgRequired, "content"))
		return
	}
	if len(req.Content) > webhookContentMaxBytes {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, "content exceeds 16 KB limit"))
		return
	}

	// Build the audit call record (written on success or failure below).
	callID := store.GenNewID()
	deliveryID := store.GenNewID()
	now := time.Now()
	callRecord := h.newCallRecord(r, webhook, callID, deliveryID, now, channelName, req)

	// Dispatch — media or text-only path.
	warning, sendErr := h.dispatch(ctx, w, r, webhook, req, channelName, callRecord, locale)
	if sendErr != nil {
		return // error response already written by dispatch
	}

	// Record successful delivery.
	completedAt := time.Now()
	callRecord.Status = "done"
	callRecord.CompletedAt = &completedAt
	callRecord.Attempts = 1

	respBody := webhookMessageResp{
		CallID:      callID.String(),
		Status:      "sent",
		ChannelName: channelName,
		ChatID:      req.ChatID,
		Warning:     warning,
	}
	respBytes, _ := json.Marshal(respBody)
	callRecord.Response = respBytes

	if err := h.callStore.Create(ctx, callRecord); err != nil {
		// Non-fatal: audit failure must not fail a delivered message.
		slog.Warn("webhook.message.audit_write_failed",
			"error", err,
			"call_id", callID,
		)
	}

	slog.Info("webhook.message.delivered",
		"tenant_id", webhook.TenantID,
		"webhook_id", webhook.ID,
		"channel", channelName,
		"chat_id", req.ChatID,
		"has_media", req.MediaURL != "",
	)

	writeJSON(w, http.StatusOK, respBody)
}

// dispatch sends the message (media or text) to the channel.
// Returns (warning string, error). On non-nil error the response was already written.
func (h *WebhookMessageHandler) dispatch(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	webhook *store.WebhookData,
	req webhookMessageReq,
	channelName string,
	callRecord *store.WebhookCallData,
	locale string,
) (warning string, _ error) {
	if req.MediaURL == "" {
		// Text-only path.
		if err := h.channelMgr.SendToChannel(ctx, channelName, req.ChatID, req.Content); err != nil {
			h.failCall(ctx, callRecord, err.Error())
			slog.Error("webhook.message.dispatch_failed",
				"error", err,
				"channel_name", channelName,
				"webhook_id", webhook.ID,
			)
			writeError(w, http.StatusBadGateway, protocol.ErrInternal,
				i18n.T(locale, i18n.MsgInternalError, "channel send failed"))
			return "", err
		}
		return "", nil
	}

	// Media path: SSRF validation + HEAD probe.
	probe, probeErr := probeMediaURL(req.MediaURL)
	if probeErr != nil {
		var mve *mediaValidateError
		if errors.As(probeErr, &mve) {
			h.failCall(ctx, callRecord, mve.message)
			switch mve.code {
			case "ssrf":
				slog.Warn("security.webhook.ssrf_blocked",
					"host", redactedHost(req.MediaURL),
					"webhook_id", webhook.ID,
				)
				writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
					i18n.T(locale, i18n.MsgWebhookMediaSSRFBlocked))
			case "too_large":
				writeError(w, http.StatusRequestEntityTooLarge, protocol.ErrInvalidRequest,
					i18n.T(locale, i18n.MsgWebhookMediaTooLarge))
			case "mime_denied":
				writeError(w, http.StatusUnsupportedMediaType, protocol.ErrInvalidRequest,
					i18n.T(locale, i18n.MsgWebhookMediaMIMEDenied))
			default:
				writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
					i18n.T(locale, i18n.MsgWebhookMediaSSRFBlocked))
			}
		} else {
			h.failCall(ctx, callRecord, probeErr.Error())
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
				i18n.T(locale, i18n.MsgWebhookMediaSSRFBlocked))
		}
		return "", probeErr
	}

	// Channel media capability gate.
	channelType := h.channelMgr.ChannelTypeForName(channelName)
	if channels.IsMediaCapable(channelType) {
		media := []bus.MediaAttachment{{
			URL:         req.MediaURL,
			ContentType: probe.ContentType,
			Caption:     req.MediaCaption,
		}}
		if err := h.channelMgr.SendMediaToChannel(ctx, channelName, req.ChatID, req.Content, media); err != nil {
			h.failCall(ctx, callRecord, err.Error())
			slog.Error("webhook.message.dispatch_failed",
				"error", err,
				"channel_name", channelName,
				"webhook_id", webhook.ID,
			)
			writeError(w, http.StatusBadGateway, protocol.ErrInternal,
				i18n.T(locale, i18n.MsgInternalError, "channel send failed"))
			return "", err
		}
		return "", nil
	}

	if req.FallbackToText {
		// Degrade to text-only send.
		slog.Warn("webhook.media_unsupported_fallback",
			"channel_name", channelName,
			"channel_type", channelType,
			"webhook_id", webhook.ID,
		)
		if err := h.channelMgr.SendToChannel(ctx, channelName, req.ChatID, req.Content); err != nil {
			h.failCall(ctx, callRecord, err.Error())
			slog.Error("webhook.message.dispatch_failed",
				"error", err,
				"channel_name", channelName,
				"webhook_id", webhook.ID,
			)
			writeError(w, http.StatusBadGateway, protocol.ErrInternal,
				i18n.T(locale, i18n.MsgInternalError, "channel send failed"))
			return "", err
		}
		return "media_not_supported_fallback_text", nil
	}

	// Media unsupported + no fallback → 501.
	const reason = "channel does not support media and fallback_to_text is false"
	h.failCall(ctx, callRecord, reason)
	writeError(w, http.StatusNotImplemented, protocol.ErrInvalidRequest,
		i18n.T(locale, i18n.MsgWebhookMediaChannelUnsupported))
	return "", errors.New(reason)
}

// resolveChannelName returns the channel instance name for dispatch.
// Preference: webhook-bound channel_id (resolved via ChannelInstanceStore) → req.ChannelName.
func (h *WebhookMessageHandler) resolveChannelName(
	ctx context.Context,
	w http.ResponseWriter,
	webhook *store.WebhookData,
	reqChannelName string,
	locale string,
) (string, bool) {
	if webhook.ChannelID != nil {
		inst, err := h.channelInstances.Get(ctx, *webhook.ChannelID)
		if err != nil || inst == nil {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWebhookChannelNotFound))
			return "", false
		}
		return inst.Name, true
	}

	if reqChannelName == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgRequired, "channel_name"))
		return "", false
	}
	return reqChannelName, true
}

// validateChannelTenant enforces the P0 cross-tenant isolation rule:
// the channel must belong to the same tenant as the webhook.
// Returns true if the check passes (caller may proceed).
func (h *WebhookMessageHandler) validateChannelTenant(
	ctx context.Context,
	w http.ResponseWriter,
	webhook *store.WebhookData,
	channelName string,
	locale string,
) bool {
	channelTenantID, exists := h.channelMgr.ChannelTenantID(channelName)
	if !exists {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound,
			i18n.T(locale, i18n.MsgWebhookChannelNotFound))
		return false
	}
	// uuid.Nil means legacy/config-based channel — allow from any tenant (backward compat).
	if channelTenantID != uuid.Nil && channelTenantID != webhook.TenantID {
		slog.Warn("security.webhook.tenant_leak_attempt",
			"webhook_id", webhook.ID,
			"webhook_tenant", webhook.TenantID,
			"channel_name", channelName,
			"channel_tenant", channelTenantID,
		)
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized,
			i18n.T(locale, i18n.MsgWebhookTenantMismatch))
		return false
	}
	return true
}

// newCallRecord builds the initial WebhookCallData for audit logging.
func (h *WebhookMessageHandler) newCallRecord(
	r *http.Request,
	webhook *store.WebhookData,
	callID, deliveryID uuid.UUID,
	now time.Time,
	channelName string,
	req webhookMessageReq,
) *store.WebhookCallData {
	// Encode canonical audit payload: {"body_hash": "<sha256>", "meta": {...}}.
	// PG jsonb rejects non-JSON bytes; this shape is valid JSON on both PG and SQLite.
	bodyBytes, _ := json.Marshal(req)
	requestPayload, _ := buildAuditPayload(bodyBytes, map[string]any{
		"channel_name": channelName,
		"chat_id":      req.ChatID,
		"has_media":    req.MediaURL != "",
	})

	call := &store.WebhookCallData{
		ID:             callID,
		TenantID:       webhook.TenantID,
		WebhookID:      webhook.ID,
		AgentID:        webhook.AgentID,
		DeliveryID:     deliveryID,
		Mode:           "sync",
		Status:         "running",
		StartedAt:      &now,
		RequestPayload: requestPayload,
		CreatedAt:      now,
	}

	if key := r.Header.Get("Idempotency-Key"); key != "" {
		call.IdempotencyKey = &key
	}

	return call
}

// failCall mutates call to status=failed and records it in the store. Best-effort.
func (h *WebhookMessageHandler) failCall(ctx context.Context, call *store.WebhookCallData, reason string) {
	now := time.Now()
	call.Status = "failed"
	call.CompletedAt = &now
	call.LastError = &reason
	call.Attempts = 1
	if err := h.callStore.Create(ctx, call); err != nil {
		slog.Warn("webhook.message.audit_write_failed", "error", err, "call_id", call.ID)
	}
}

// redactedHost extracts the hostname from a URL string for safe (no-path) log output.
func redactedHost(rawURL string) string {
	for _, prefix := range []string{"https://", "http://"} {
		if len(rawURL) > len(prefix) && rawURL[:len(prefix)] == prefix {
			rest := rawURL[len(prefix):]
			for i, c := range rest {
				if c == '/' || c == '?' || c == '#' {
					return rest[:i]
				}
			}
			return rest
		}
	}
	return "[unknown]"
}
