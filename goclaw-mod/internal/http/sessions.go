package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// SessionsHandler exposes read-only HTTP session discovery for automation.
type SessionsHandler struct {
	sessions store.SessionStore
	ownerIDs []string
}

func NewSessionsHandler(s store.SessionStore, ownerIDs []string) *SessionsHandler {
	return &SessionsHandler{sessions: s, ownerIDs: ownerIDs}
}

func (h *SessionsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/sessions", requireAuth("", h.handleList))
	mux.HandleFunc("POST /v1/chat/sessions/{key}/branch", requireAuth("", h.handleBranch))
	mux.HandleFunc("GET /v1/chat/sessions/{key}/history/follow", requireAuth("", h.handleHistoryFollow))
}

func (h *SessionsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	userID := store.UserIDFromContext(r.Context())
	role := permissions.Role(store.RoleFromContext(r.Context()))

	limit := parsePositiveInt(r.URL.Query().Get("limit"), 20)
	offset := parseNonNegativeInt(r.URL.Query().Get("offset"), 0)
	opts := store.SessionListOpts{
		AgentID:  firstSessionQueryValue(r.URL.Query().Get("agent_id"), r.URL.Query().Get("agentId")),
		Channel:  r.URL.Query().Get("channel"),
		Limit:    limit,
		Offset:   offset,
		TenantID: store.TenantIDFromContext(r.Context()),
	}

	if !canSeeAllHTTP(role, h.ownerIDs, userID) {
		if userID == "" {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgUserIDHeader))
			return
		}
		opts.UserID = userID
	}

	result := h.sessions.ListPagedRich(r.Context(), opts)
	writeJSON(w, http.StatusOK, map[string]any{
		"sessions": result.Sessions,
		"total":    result.Total,
		"limit":    limit,
		"offset":   offset,
	})
}

func (h *SessionsHandler) handleBranch(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	sourceKey := r.PathValue("key")
	sourceAgent, _ := sessions.ParseSessionKey(sourceKey)
	if sourceAgent == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "invalid session key"))
		return
	}
	source := h.sessions.Get(r.Context(), sourceKey)
	if source == nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "session", sourceKey))
		return
	}
	if !h.canAccessSession(r, source) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "session"))
		return
	}

	var body struct {
		NewSessionKey string            `json:"new_session_key"`
		UpToIndex     *int              `json:"up_to_index"`
		Label         string            `json:"label"`
		Metadata      map[string]string `json:"metadata"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON))
		return
	}
	if body.UpToIndex == nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "up_to_index"))
		return
	}
	newKey := body.NewSessionKey
	if newKey == "" {
		newKey = "agent:" + sourceAgent + ":branch:direct:" + uuid.NewString()
	} else if targetAgent, _ := sessions.ParseSessionKey(newKey); targetAgent == "" || targetAgent != sourceAgent {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "new_session_key must use the same agent key"))
		return
	}

	brancher, ok := h.sessions.(store.SessionBranchStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, protocol.ErrInternal, i18n.T(locale, i18n.MsgInvalidRequest, "session branching unavailable"))
		return
	}
	branch, copied, err := brancher.BranchSession(r.Context(), sourceKey, store.SessionBranchOpts{
		NewKey:    newKey,
		UpToIndex: *body.UpToIndex,
		Label:     body.Label,
		Metadata:  body.Metadata,
	})
	if err != nil {
		switch {
		case errors.Is(err, store.ErrSessionAlreadyExists):
			writeError(w, http.StatusConflict, protocol.ErrFailedPrecondition, i18n.T(locale, i18n.MsgAlreadyExists, "session", newKey))
		case errors.Is(err, store.ErrInvalidSessionBranch):
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "up_to_index out of range"))
		case errors.Is(err, store.ErrSessionNotFound):
			writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "session", sourceKey))
		default:
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"ok":              true,
		"source_key":      sourceKey,
		"session_key":     branch.Key,
		"copied_messages": copied,
		"total_messages":  len(source.Messages),
		"label":           branch.Label,
	})
}

func (h *SessionsHandler) handleHistoryFollow(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	sessionKey := r.PathValue("key")
	sess := h.sessions.Get(r.Context(), sessionKey)
	if sess == nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "session", sessionKey))
		return
	}
	if !h.canAccessSession(r, sess) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "session"))
		return
	}

	cursor, ok := parseStrictNonNegativeInt(r.URL.Query().Get("cursor"), 0)
	if !ok {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "cursor must be a non-negative integer"))
		return
	}
	limit, ok := parseStrictPositiveInt(r.URL.Query().Get("limit"), 50, 200)
	if !ok {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "limit must be a positive integer"))
		return
	}
	history := h.sessions.GetHistory(r.Context(), sessionKey)
	total := len(history)
	if cursor > total {
		writeJSON(w, http.StatusOK, map[string]any{
			"session_key": sessionKey,
			"cursor":      cursor,
			"next_cursor": total,
			"total":       total,
			"messages":    []any{},
			"reset":       true,
			"updated":     sess.Updated.UTC().Format(time.RFC3339Nano),
		})
		return
	}

	end := cursor + limit
	if end > total {
		end = total
	}
	messages := append([]providers.Message(nil), history[cursor:end]...)
	secret := FileSigningKey()
	for i := range messages {
		messages[i].Content = SignFileURLs(messages[i].Content, secret)
		for j := range messages[i].MediaRefs {
			messages[i].MediaRefs[j].Path = SignMediaPath(messages[i].MediaRefs[j].Path, secret)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_key": sessionKey,
		"cursor":      cursor,
		"next_cursor": end,
		"total":       total,
		"messages":    messages,
		"reset":       false,
		"updated":     sess.Updated.UTC().Format(time.RFC3339Nano),
	})
}

func (h *SessionsHandler) canAccessSession(r *http.Request, sess *store.SessionData) bool {
	userID := store.UserIDFromContext(r.Context())
	role := permissions.Role(store.RoleFromContext(r.Context()))
	return canSeeAllHTTP(role, h.ownerIDs, userID) || (userID != "" && sess.UserID == userID)
}

func canSeeAllHTTP(role permissions.Role, ownerIDs []string, userID string) bool {
	if permissions.HasMinRole(role, permissions.RoleAdmin) {
		return true
	}
	return userID != "" && slices.Contains(ownerIDs, userID)
}

func parsePositiveInt(raw string, fallback int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func parseStrictPositiveInt(raw string, fallback, maxValue int) (int, bool) {
	if raw == "" {
		return fallback, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, false
	}
	if n > maxValue {
		n = maxValue
	}
	return n, true
}

func parseStrictNonNegativeInt(raw string, fallback int) (int, bool) {
	if raw == "" {
		return fallback, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func parseNonNegativeInt(raw string, fallback int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

func firstSessionQueryValue(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
