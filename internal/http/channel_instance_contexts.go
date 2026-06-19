package http

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type channelContextDTO struct {
	ChannelInstanceID string     `json:"channel_instance_id"`
	ChannelType       string     `json:"channel_type"`
	ScopeType         string     `json:"scope_type"`
	ScopeKey          string     `json:"scope_key"`
	DisplayName       string     `json:"display_name"`
	Source            string     `json:"source"`
	PeerKind          string     `json:"peer_kind,omitempty"`
	ContactType       string     `json:"contact_type,omitempty"`
	LastSeenAt        *time.Time `json:"last_seen_at,omitempty"`
	MemberCount       int        `json:"member_count,omitempty"`
	LiveMembers       bool       `json:"live_members_supported"`
}

type channelContextMemberDTO struct {
	UserID      string     `json:"user_id"`
	DisplayName string     `json:"display_name,omitempty"`
	Username    string     `json:"username,omitempty"`
	PlatformID  string     `json:"platform_id"`
	Source      string     `json:"source"`
	Status      string     `json:"membership_status"`
	LastSeenAt  *time.Time `json:"last_seen_at,omitempty"`
}

func (h *ChannelInstancesHandler) handleListContexts(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.resolveInstance(w, r)
	if !ok {
		return
	}
	if h.contactStore == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"contexts": []channelContextDTO{baseChannelContextDTO(*inst)},
		})
		return
	}

	opts := store.ContactListOpts{
		ChannelType:     inst.ChannelType,
		ChannelInstance: inst.Name,
		ContactType:     "group",
		Limit:           200,
	}
	contacts, err := h.contactStore.ListContacts(r.Context(), opts)
	if err != nil {
		slog.Error("channel_instances.contexts", "error", err)
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "channel contexts"))
		return
	}

	contexts := []channelContextDTO{baseChannelContextDTO(*inst)}
	for _, c := range contacts {
		ctxDTO := contextDTOFromContact(inst.ID, c)
		contexts = append(contexts, ctxDTO)
	}

	writeJSON(w, http.StatusOK, map[string]any{"contexts": contexts})
}

func (h *ChannelInstancesHandler) handleListContextMembers(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.resolveInstance(w, r)
	if !ok {
		return
	}

	scopeType := strings.TrimSpace(r.PathValue("scopeType"))
	scopeKey := strings.TrimSpace(r.PathValue("scopeKey"))
	if scopeType == "" || scopeKey == "" {
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "scopeType and scopeKey"))
		return
	}

	var liveErr string
	members := make([]channelContextMemberDTO, 0)
	if h.channelMgr != nil && scopeType == store.ChannelScopeTypeGroup {
		if live, err := h.channelMgr.ListGroupMembers(r.Context(), inst.Name, scopeKey); err == nil {
			for _, m := range live {
				members = append(members, channelContextMemberDTO{
					UserID:      m.MemberID,
					DisplayName: m.Name,
					PlatformID:  m.MemberID,
					Source:      "live_provider",
					Status:      "visible",
				})
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"members":                members,
				"live_members_supported": true,
				"source":                 "live_provider",
			})
			return
		} else {
			liveErr = err.Error()
		}
	}

	if h.contactStore != nil && scopeType != store.ChannelScopeTypeGroup && scopeType != "topic" {
		contacts, err := h.contactStore.ListContacts(r.Context(), store.ContactListOpts{
			ChannelType:     inst.ChannelType,
			ChannelInstance: inst.Name,
			ContactType:     "user",
			PeerKind:        peerKindForScope(scopeType),
			Limit:           200,
		})
		if err != nil {
			slog.Error("channel_instances.context_members", "error", err)
			locale := store.LocaleFromContext(r.Context())
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "channel members"))
			return
		}
		for _, c := range contacts {
			members = append(members, memberDTOFromContact(c))
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"members":                members,
		"live_members_supported": false,
		"source":                 "stored_contacts",
		"unsupported_reason":     liveErr,
	})
}

func baseChannelContextDTO(inst store.ChannelInstanceData) channelContextDTO {
	name := inst.DisplayName
	if name == "" {
		name = inst.Name
	}
	return channelContextDTO{
		ChannelInstanceID: inst.ID.String(),
		ChannelType:       inst.ChannelType,
		ScopeType:         store.ChannelScopeTypeChannel,
		ScopeKey:          inst.Name,
		DisplayName:       name,
		Source:            "channel_instance",
		PeerKind:          "direct",
		ContactType:       "channel",
		LiveMembers:       false,
	}
}

func contextDTOFromContact(instanceID uuid.UUID, c store.ChannelContact) channelContextDTO {
	scopeType := store.ChannelScopeTypeGroup
	if c.ContactType == "topic" {
		scopeType = "topic"
	}
	displayName := c.SenderID
	if c.DisplayName != nil && *c.DisplayName != "" {
		displayName = *c.DisplayName
	}
	peerKind := ""
	if c.PeerKind != nil {
		peerKind = *c.PeerKind
	}
	lastSeen := c.LastSeenAt
	return channelContextDTO{
		ChannelInstanceID: instanceID.String(),
		ChannelType:       c.ChannelType,
		ScopeType:         scopeType,
		ScopeKey:          c.SenderID,
		DisplayName:       displayName,
		Source:            "stored_contact",
		PeerKind:          peerKind,
		ContactType:       c.ContactType,
		LastSeenAt:        &lastSeen,
	}
}

func memberDTOFromContact(c store.ChannelContact) channelContextMemberDTO {
	displayName := ""
	if c.DisplayName != nil {
		displayName = *c.DisplayName
	}
	username := ""
	if c.Username != nil {
		username = *c.Username
	}
	userID := c.SenderID
	if c.UserID != nil && *c.UserID != "" {
		userID = *c.UserID
	}
	lastSeen := c.LastSeenAt
	return channelContextMemberDTO{
		UserID:      userID,
		DisplayName: displayName,
		Username:    username,
		PlatformID:  c.SenderID,
		Source:      "stored_contact",
		Status:      "visible",
		LastSeenAt:  &lastSeen,
	}
}

func peerKindForScope(scopeType string) string {
	if scopeType == store.ChannelScopeTypeGroup || scopeType == "topic" {
		return "group"
	}
	return "direct"
}
