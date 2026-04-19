package http

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// UserSearchResult is a unified result from contacts + tenant_users.
// ID is the user_id string (human-facing identifier). UUID is the tenant_user
// primary key (only populated when Source == "tenant_user"); callers that need
// to reference a tenant_user by foreign key (e.g. contact merge) must use UUID.
type UserSearchResult struct {
	ID                 string  `json:"id"`
	UUID               string  `json:"uuid,omitempty"`
	DisplayName        *string `json:"display_name,omitempty"`
	Username           *string `json:"username,omitempty"`
	Source             string  `json:"source"` // "contact" or "tenant_user"
	ChannelType        *string `json:"channel_type,omitempty"`
	PeerKind           *string `json:"peer_kind,omitempty"`
	MergedTenantUserID *string `json:"merged_tenant_user_id,omitempty"`
	Role               *string `json:"role,omitempty"`
}

// handleSearchUsers returns unified results from channel_contacts + tenant_users.
// GET /v1/users/search?q=&limit=30&peer_kind=
// Empty q → return most recent. With q → ILIKE search across both tables.
func (h *ChannelInstancesHandler) handleSearchUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	peerKind := r.URL.Query().Get("peer_kind")
	source := r.URL.Query().Get("source") // "contact", "tenant_user", or "" (both)
	limit := 30
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	ctx := r.Context()
	tid := store.TenantIDFromContext(ctx)
	var results []UserSearchResult
	mergedUserIDs := make(map[string]bool) // for deduplication between contacts and tenant_users

	// 1. Search channel_contacts (skip if source=tenant_user)
	if h.contactStore != nil && source != "tenant_user" {
		opts := store.ContactListOpts{
			Search:   q,
			PeerKind: peerKind,
			Limit:    limit,
		}
		contacts, err := h.contactStore.ListContacts(ctx, opts)
		if err != nil {
			slog.Warn("user_search.contacts", "error", err)
		}
		for _, c := range contacts {
			r := UserSearchResult{
				ID:          c.SenderID,
				DisplayName: c.DisplayName,
				Username:    c.Username,
				Source:      "contact",
				ChannelType: &c.ChannelType,
				PeerKind:    c.PeerKind,
			}
			if c.MergedID != nil {
				if resolved, err := h.contactStore.ResolveTenantUserID(ctx, c.ChannelType, c.SenderID); err == nil && resolved != "" {
					r.MergedTenantUserID = &resolved
					mergedUserIDs[resolved] = true
				}
			}
			results = append(results, r)
		}
	}

	// 2. Search tenant_users (skip if source=contact)
	if h.tenantStore != nil && tid != uuid.Nil && source != "contact" {
		users, err := h.tenantStore.ListUsers(ctx, tid)
		if err != nil {
			slog.Warn("user_search.tenant_users", "error", err)
		}
		for _, u := range users {
			if mergedUserIDs[u.UserID] {
				continue
			}
			if q != "" && !containsInsensitive(u.UserID, q) && !containsInsensitive(ptrStr(u.DisplayName), q) {
				continue
			}
			if len(results) >= limit {
				break
			}
			role := u.Role
			results = append(results, UserSearchResult{
				ID:          u.UserID,
				UUID:        u.ID.String(),
				DisplayName: u.DisplayName,
				Source:      "tenant_user",
				Role:        &role,
			})
		}
	}

	if results == nil {
		results = []UserSearchResult{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func containsInsensitive(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
