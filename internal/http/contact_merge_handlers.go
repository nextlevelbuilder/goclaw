package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// handleMergeContacts links selected contacts to a tenant_user identity.
// POST /v1/contacts/merge
func (h *ChannelInstancesHandler) handleMergeContacts(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	tid := store.TenantIDFromContext(r.Context())
	if tid == uuid.Nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgTenantScopeRequired)})
		return
	}

	var body struct {
		ContactIDs   []uuid.UUID `json:"contact_ids"`
		TenantUserID *uuid.UUID  `json:"tenant_user_id"`
		CreateUser   *struct {
			UserID      string `json:"user_id"`
			DisplayName string `json:"display_name"`
		} `json:"create_user"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if len(body.ContactIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgContactIDsRequired)})
		return
	}
	if len(body.ContactIDs) > 500 {
		body.ContactIDs = body.ContactIDs[:500]
	}
	hasTU := body.TenantUserID != nil
	hasCU := body.CreateUser != nil
	if hasTU == hasCU { // must have exactly one
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgMergeTargetRequired)})
		return
	}

	var targetID uuid.UUID
	var targetUserID string // tenant_user.user_id string for context file migration

	if hasTU {
		// Link to existing tenant_user — verify same tenant.
		tu, err := h.tenantStore.GetTenantUser(r.Context(), *body.TenantUserID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgTenantUserNotFound)})
			return
		}
		if tu.TenantID != tid {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgTenantMismatch)})
			return
		}
		targetID = tu.ID
		targetUserID = tu.UserID
	} else {
		// Create new tenant_user.
		userID := body.CreateUser.UserID
		displayName := body.CreateUser.DisplayName
		if userID == "" {
			// Fallback: derive from first contact's username.
			userID = h.deriveUserIDFromContacts(r.Context(), body.ContactIDs)
		}
		if userID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "user_id")})
			return
		}
		tu, err := h.tenantStore.CreateTenantUserReturning(r.Context(), tid, userID, displayName, store.TenantRoleMember)
		if err != nil {
			slog.Error("contacts.merge.create_user", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToCreate, "tenant user", err.Error())})
			return
		}
		targetID = tu.ID
		targetUserID = tu.UserID
	}

	if err := h.contactStore.MergeContacts(r.Context(), body.ContactIDs, targetID); err != nil {
		slog.Error("contacts.merge", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToUpdate, "contacts", err.Error())})
		return
	}

	// Migrate user_context_files from old sender_ids to new tenant_user_id.
	h.migrateContextFilesOnMerge(r.Context(), body.ContactIDs, targetUserID)

	emitAudit(h.msgBus, r, "contacts.merged", "tenant_user", targetID.String())
	writeJSON(w, http.StatusOK, map[string]any{
		"merged_id":    targetID,
		"merged_count": len(body.ContactIDs),
	})
}

// handleUnmergeContacts removes merged_id from selected contacts.
// POST /v1/contacts/unmerge
func (h *ChannelInstancesHandler) handleUnmergeContacts(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	tid := store.TenantIDFromContext(r.Context())
	if tid == uuid.Nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgTenantScopeRequired)})
		return
	}

	var body struct {
		ContactIDs []uuid.UUID `json:"contact_ids"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	if len(body.ContactIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgContactIDsRequired)})
		return
	}
	if len(body.ContactIDs) > 500 {
		body.ContactIDs = body.ContactIDs[:500]
	}

	if err := h.contactStore.UnmergeContacts(r.Context(), body.ContactIDs); err != nil {
		slog.Error("contacts.unmerge", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToUpdate, "contacts", err.Error())})
		return
	}

	emitAudit(h.msgBus, r, "contacts.unmerged", "contacts", "")
	writeJSON(w, http.StatusOK, map[string]any{"unmerged_count": len(body.ContactIDs)})
}

// handleListMergedContacts returns contacts linked to a tenant_user.
// GET /v1/contacts/merged/{tenantUserId}
func (h *ChannelInstancesHandler) handleListMergedContacts(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	tid := store.TenantIDFromContext(r.Context())
	if tid == uuid.Nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgTenantScopeRequired)})
		return
	}

	mergedID, err := uuid.Parse(r.PathValue("tenantUserId"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "tenantUserId")})
		return
	}

	contacts, err := h.contactStore.GetContactsByMergedID(r.Context(), mergedID)
	if err != nil {
		slog.Error("contacts.merged.list", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "contacts")})
		return
	}
	if contacts == nil {
		contacts = []store.ChannelContact{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"contacts": contacts})
}

// handleListTenantUsers returns users for the current tenant (for merge dialog dropdown).
// GET /v1/tenant-users
func (h *ChannelInstancesHandler) handleListTenantUsers(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	tid := store.TenantIDFromContext(r.Context())
	if tid == uuid.Nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgTenantScopeRequired)})
		return
	}

	users, err := h.tenantStore.ListUsers(r.Context(), tid)
	if err != nil {
		slog.Error("tenant_users.list", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "tenant users")})
		return
	}
	if users == nil {
		users = []store.TenantUserData{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

// migrateContextFilesOnMerge moves user_context_files from old sender_ids to the new tenant_user_id.
// Best-effort: log errors but don't fail the merge.
func (h *ChannelInstancesHandler) migrateContextFilesOnMerge(ctx context.Context, contactIDs []uuid.UUID, newUserID string) {
	// Batch-fetch sender_ids from merged contacts in one query.
	oldUserIDs, err := h.contactStore.GetSenderIDsByContactIDs(ctx, contactIDs)
	if err != nil {
		slog.Warn("contacts.merge.get_sender_ids", "error", err)
		return
	}
	// Filter out the target user_id itself (no self-migration needed).
	filtered := oldUserIDs[:0]
	for _, id := range oldUserIDs {
		if id != newUserID {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) == 0 {
		return
	}
	if err := h.agentStore.MigrateUserDataOnMerge(ctx, filtered, newUserID); err != nil {
		slog.Warn("contacts.merge.migrate_context_files", "error", err, "old_ids", filtered, "new_id", newUserID)
	}
}

// deriveUserIDFromContacts returns the first contact's username or sender_id as fallback user_id.
func (h *ChannelInstancesHandler) deriveUserIDFromContacts(ctx context.Context, contactIDs []uuid.UUID) string {
	if len(contactIDs) == 0 {
		return ""
	}
	c, err := h.contactStore.GetContactByID(ctx, contactIDs[0])
	if err != nil {
		return ""
	}
	if c.Username != nil && *c.Username != "" {
		return *c.Username
	}
	return c.SenderID
}
