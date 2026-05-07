package http

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

type inboundPublisher interface {
	TryPublishInbound(bus.InboundMessage) bool
}

// SyntheticInboundHandler handles trusted internal requests that should enter
// the normal channel pipeline without relying on provider webhooks. It is for
// system-owned follow-up work such as cron failure triage threads: normal
// Discord bot-message loop guards stay intact, while authenticated internal
// callers can still start a real thread-scoped agent run.
type SyntheticInboundHandler struct {
	msgBus inboundPublisher
}

func NewSyntheticInboundHandler(msgBus inboundPublisher) *SyntheticInboundHandler {
	return &SyntheticInboundHandler{msgBus: msgBus}
}

func (h *SyntheticInboundHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/agents/{id}/synthetic-inbound", h.handle)
}

type syntheticInboundRequest struct {
	Message     string            `json:"message"`
	Channel     string            `json:"channel"`
	ChatID      string            `json:"chat_id,omitempty"`
	ThreadID    string            `json:"thread_id,omitempty"`
	PeerKind    string            `json:"peer_kind,omitempty"`
	SenderID    string            `json:"sender_id,omitempty"`
	DisplayName string            `json:"display_name,omitempty"`
	Username    string            `json:"username,omitempty"`
	GuildID     string            `json:"guild_id,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

func (h *SyntheticInboundHandler) handle(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	auth := resolveAuth(r)
	if !auth.Authenticated {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": i18n.T(locale, i18n.MsgUnauthorized)})
		return
	}
	if !permissions.HasMinRole(auth.Role, permissions.RoleOperator) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgPermissionDenied, r.URL.Path)})
		return
	}
	if h.msgBus == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "inbound bus unavailable"})
		return
	}

	r = r.WithContext(enrichContext(r.Context(), r, auth))
	agentID := strings.TrimSpace(r.PathValue("id"))
	if agentID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return
	}

	const maxBodySize = 1 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	var req syntheticInboundRequest
	if !bindJSON(w, r, locale, &req) {
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	req.Channel = strings.TrimSpace(req.Channel)
	chatID := syntheticFirstNonEmpty(strings.TrimSpace(req.ThreadID), strings.TrimSpace(req.ChatID))
	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}
	if req.Channel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel is required"})
		return
	}
	if chatID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "chat_id or thread_id is required"})
		return
	}

	peerKind := strings.TrimSpace(req.PeerKind)
	if peerKind == "" {
		peerKind = string(sessions.PeerGroup)
	}
	if peerKind != string(sessions.PeerGroup) && peerKind != string(sessions.PeerDirect) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "peer_kind must be direct or group"})
		return
	}

	userID := store.UserIDFromContext(r.Context())
	senderID := strings.TrimSpace(req.SenderID)
	if senderID == "" {
		suffix := userID
		if suffix == "" {
			suffix = "wake"
		}
		senderID = "system:" + suffix
	}
	if !bus.IsInternalSender(senderID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sender_id must be an internal sender"})
		return
	}

	displayName := syntheticFirstNonEmpty(strings.TrimSpace(req.DisplayName), "system")
	username := strings.TrimSpace(req.Username)
	meta := map[string]string{}
	for k, v := range req.Metadata {
		if strings.TrimSpace(k) != "" {
			meta[k] = v
		}
	}
	if meta["message_id"] == "" {
		meta["message_id"] = "synthetic-" + uuid.NewString()
	}
	meta["synthetic_inbound"] = "true"
	meta["display_name"] = displayName
	meta["username"] = username
	meta["channel_id"] = chatID
	meta["is_dm"] = fmt.Sprintf("%t", peerKind == string(sessions.PeerDirect))
	if req.ThreadID != "" {
		meta["is_thread"] = "true"
	}
	if req.GuildID != "" {
		meta["guild_id"] = req.GuildID
	}
	if userID != "" {
		meta[tools.MetaOriginSenderID] = userID
		meta[tools.MetaOriginUserID] = userID
	}
	if role := store.RoleFromContext(r.Context()); role != "" {
		meta[tools.MetaOriginRole] = role
	}

	content := req.Message
	if peerKind == string(sessions.PeerGroup) {
		content = fmt.Sprintf("[From: %s]\n%s", displayName, req.Message)
	}

	msg := bus.InboundMessage{
		Channel:  req.Channel,
		SenderID: senderID,
		ChatID:   chatID,
		Content:  content,
		PeerKind: peerKind,
		TenantID: store.TenantIDFromContext(r.Context()),
		AgentID:  agentID,
		UserID:   userID,
		Metadata: meta,
	}
	if !h.msgBus.TryPublishInbound(msg) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "inbound queue full"})
		return
	}

	slog.Info("synthetic inbound queued",
		"agent", agentID,
		"channel", req.Channel,
		"chat_id", chatID,
		"peer_kind", peerKind,
		"sender_id", senderID,
		"user_id", userID,
	)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func syntheticFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
