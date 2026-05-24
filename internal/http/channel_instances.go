package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// OrphanChannelCleaner runs channel-type-specific cleanup when a delete
// arrives for a channel that's NOT loaded in the runtime Manager (e.g. it
// was disabled so InstanceLoader removed it). Closure injected from
// cmd/gateway.go captures the per-channel dependencies (portal store,
// encryption key) so the handler doesn't need to import per-channel
// packages directly.
//
// Returns nil for no-op cases (nothing to clean) so callers can ignore the
// error in those situations; real failures (store read, decode) propagate.
type OrphanChannelCleaner func(ctx context.Context, tenantID uuid.UUID, configJSON []byte) error

// ChannelInstancesHandler handles channel instance CRUD endpoints.
type ChannelInstancesHandler struct {
	store           store.ChannelInstanceStore
	agentStore      store.AgentStore
	configPermStore store.ConfigPermissionStore
	contactStore    store.ContactStore
	tenantStore     store.TenantStore
	msgBus          *bus.MessageBus
	memberResolver  channels.MemberResolver // optional — enriches file_writer metadata on addwriter
	channelMgr      *channels.Manager       // optional — enables ChannelDestroyer hook on delete
	// orphanCleaners is keyed by channel_type; called when channelMgr.GetChannel
	// returns false. Keeps handler agnostic of per-channel packages.
	orphanCleaners map[string]OrphanChannelCleaner
}

// NewChannelInstancesHandler creates a handler for channel instance management endpoints.
func NewChannelInstancesHandler(s store.ChannelInstanceStore, agentStore store.AgentStore, configPermStore store.ConfigPermissionStore, contactStore store.ContactStore, tenantStore store.TenantStore, msgBus *bus.MessageBus) *ChannelInstancesHandler {
	return &ChannelInstancesHandler{store: s, agentStore: agentStore, configPermStore: configPermStore, contactStore: contactStore, tenantStore: tenantStore, msgBus: msgBus}
}

// SetMemberResolver wires a channel member resolver so addwriter can auto-fill
// metadata when the caller supplies neither DisplayName nor Username.
func (h *ChannelInstancesHandler) SetMemberResolver(r channels.MemberResolver) {
	h.memberResolver = r
}

// SetChannelManager wires the channel Manager so handleDelete can invoke
// ChannelDestroyer.Destroy() before removing the DB row — required for
// Bitrix24 to imbot.unregister its bot on portal-side. Setter-pattern (vs
// constructor param) because the Manager is created AFTER this handler
// in cmd/gateway.go's startup ordering.
func (h *ChannelInstancesHandler) SetChannelManager(mgr *channels.Manager) {
	h.channelMgr = mgr
}

// RegisterOrphanCleaner registers a per-channel-type cleanup function that
// fires during handleDelete when the channel is no longer loaded in the
// Manager (typically because admin disabled it). Without this, deleting a
// disabled Bitrix24 channel leaves the bot as a zombie on the portal.
func (h *ChannelInstancesHandler) RegisterOrphanCleaner(channelType string, fn OrphanChannelCleaner) {
	if h.orphanCleaners == nil {
		h.orphanCleaners = make(map[string]OrphanChannelCleaner)
	}
	h.orphanCleaners[channelType] = fn
}

// RegisterRoutes registers all channel instance routes on the given mux.
func (h *ChannelInstancesHandler) RegisterRoutes(mux *http.ServeMux) {
	// Channel instance CRUD (reads: viewer+, writes: admin+)
	mux.HandleFunc("GET /v1/channels/instances", h.auth(h.handleList))
	mux.HandleFunc("POST /v1/channels/instances", h.adminAuth(h.handleCreate))
	mux.HandleFunc("GET /v1/channels/instances/{id}", h.auth(h.handleGet))
	mux.HandleFunc("PUT /v1/channels/instances/{id}", h.adminAuth(h.handleUpdate))
	mux.HandleFunc("DELETE /v1/channels/instances/{id}", h.adminAuth(h.handleDelete))

	// Channel contacts (global, not per-agent)
	if h.contactStore != nil {
		mux.HandleFunc("GET /v1/contacts", h.auth(h.handleListContacts))
		mux.HandleFunc("GET /v1/contacts/resolve", h.auth(h.handleResolveContacts))
		mux.HandleFunc("POST /v1/contacts/merge", h.adminAuth(h.handleMergeContacts))
		mux.HandleFunc("POST /v1/contacts/unmerge", h.adminAuth(h.handleUnmergeContacts))
		mux.HandleFunc("GET /v1/contacts/merged/{tenantUserId}", h.auth(h.handleListMergedContacts))
	}
	if h.tenantStore != nil {
		mux.HandleFunc("GET /v1/tenant-users", h.auth(h.handleListTenantUsers))
	}

	// Unified user search (contacts + tenant_users)
	if h.contactStore != nil {
		mux.HandleFunc("GET /v1/users/search", h.auth(h.handleSearchUsers))
	}

	// Group file writers (nested under channel instances)
	if h.configPermStore != nil {
		mux.HandleFunc("GET /v1/channels/instances/{id}/writers/groups", h.auth(h.handleWriterGroups))
		mux.HandleFunc("GET /v1/channels/instances/{id}/writers", h.auth(h.handleListWriters))
		mux.HandleFunc("POST /v1/channels/instances/{id}/writers/test", h.auth(h.handleTestWriter))
		mux.HandleFunc("POST /v1/channels/instances/{id}/writers", h.adminAuth(h.handleAddWriter))
		mux.HandleFunc("DELETE /v1/channels/instances/{id}/writers/{userId}", h.adminAuth(h.handleRemoveWriter))
	}
}

func (h *ChannelInstancesHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

func (h *ChannelInstancesHandler) adminAuth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(permissions.RoleAdmin, next)
}

func (h *ChannelInstancesHandler) emitCacheInvalidate() {
	if h.msgBus == nil {
		return
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindChannelInstances},
	})
}

func (h *ChannelInstancesHandler) handleList(w http.ResponseWriter, r *http.Request) {
	opts := store.ChannelInstanceListOpts{
		Limit:  50,
		Offset: 0,
	}

	if v := r.URL.Query().Get("search"); v != "" {
		opts.Search = v
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			opts.Limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			opts.Offset = n
		}
	}

	instances, err := h.store.ListPaged(r.Context(), opts)
	if err != nil {
		slog.Error("channel_instances.list", "error", err)
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "instances"))
		return
	}

	total, _ := h.store.CountInstances(r.Context(), opts)

	result := make([]map[string]any, 0, len(instances))
	for _, inst := range instances {
		result = append(result, maskInstanceHTTP(inst))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"instances": result,
		"total":     total,
		"limit":     opts.Limit,
		"offset":    opts.Offset,
	})
}

func (h *ChannelInstancesHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	var body struct {
		Name        string          `json:"name"`
		DisplayName string          `json:"display_name"`
		ChannelType string          `json:"channel_type"`
		AgentID     string          `json:"agent_id"`
		Credentials json.RawMessage `json:"credentials"`
		Config      json.RawMessage `json:"config"`
		Enabled     *bool           `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON))
		return
	}

	if body.Name == "" || body.ChannelType == "" || body.AgentID == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name, channel_type, and agent_id"))
		return
	}

	if !isValidChannelType(body.ChannelType) {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidChannelType))
		return
	}

	agentID, err := uuid.Parse(body.AgentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "agent"))
		return
	}

	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}

	userID := store.UserIDFromContext(r.Context())

	inst := &store.ChannelInstanceData{
		Name:        body.Name,
		DisplayName: body.DisplayName,
		ChannelType: body.ChannelType,
		AgentID:     agentID,
		Credentials: body.Credentials,
		Config:      body.Config,
		Enabled:     enabled,
		CreatedBy:   userID,
	}

	if err := h.store.Create(r.Context(), inst); err != nil {
		slog.Error("channel_instances.create", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "channel instance", "internal error"))
		return
	}

	h.emitCacheInvalidate()
	emitAudit(h.msgBus, r, "channel_instance.created", "channel_instance", inst.ID.String())
	writeJSON(w, http.StatusCreated, maskInstanceHTTP(*inst))
}

func (h *ChannelInstancesHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "instance"))
		return
	}

	inst, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgInstanceNotFound))
		return
	}

	writeJSON(w, http.StatusOK, maskInstanceHTTP(*inst))
}

func (h *ChannelInstancesHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "instance"))
		return
	}

	var updates map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON))
		return
	}

	// Allowlist: only permit known channel instance columns.
	updates = filterAllowedKeys(updates, channelInstanceAllowedFields)

	if err := h.store.Update(r.Context(), id, updates); err != nil {
		slog.Error("channel_instances.update", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "channel instance", "internal error"))
		return
	}

	h.emitCacheInvalidate()
	emitAudit(h.msgBus, r, "channel_instance.updated", "channel_instance", id.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *ChannelInstancesHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "instance"))
		return
	}

	// Look up instance to check if it's a default (seeded) instance.
	inst, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgInstanceNotFound))
		return
	}
	if store.IsDefaultChannelInstance(inst.Name) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgCannotDeleteDefaultInst))
		return
	}

	// Best-effort: notify the channel impl so external resources (e.g. the
	// Bitrix24 imbot.register'd bot) get cleaned up BEFORE the DB row is
	// removed. Order matters: deleting the row first triggers a cache
	// invalidate → InstanceLoader Reload Stop's the channel and clears
	// in-memory botID, leaving the upstream bot orphaned.
	//
	// Two paths:
	//   1. Channel still loaded in Manager → ChannelDestroyer.Destroy() —
	//      uses cached botID, calls imbot.unregister directly via the live
	//      Client. This is the normal path.
	//   2. Channel NOT in Manager (e.g. admin disabled it earlier, so
	//      InstanceLoader.Reload removed it) → fall back to a registered
	//      orphan cleaner for this channel type. Reads bot_id from
	//      persisted portal state. Without this branch, deleting a disabled
	//      Bitrix24 channel orphans the bot on the portal.
	//
	// Channels without external state (Telegram, Discord, Slack, …) don't
	// implement ChannelDestroyer AND don't register an orphan cleaner —
	// both branches no-op for them.
	if h.channelMgr != nil {
		if ch, ok := h.channelMgr.GetChannel(inst.Name); ok {
			if destroyer, ok := ch.(channels.ChannelDestroyer); ok {
				if err := destroyer.Destroy(r.Context()); err != nil {
					slog.Warn("channel_instances.delete: destroyer failed — proceeding with DB delete",
						"name", inst.Name, "tenant_id", inst.TenantID, "type", inst.ChannelType, "err", err)
				}
			}
		} else if cleaner, ok := h.orphanCleaners[inst.ChannelType]; ok && cleaner != nil {
			if err := cleaner(r.Context(), inst.TenantID, inst.Config); err != nil {
				slog.Warn("channel_instances.delete: orphan cleaner failed — proceeding with DB delete",
					"name", inst.Name, "tenant_id", inst.TenantID, "type", inst.ChannelType, "err", err)
			}
		}
	}

	if err := h.store.Delete(r.Context(), id); err != nil {
		slog.Error("channel_instances.delete", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "channel instance", "internal error"))
		return
	}

	h.emitCacheInvalidate()
	emitAudit(h.msgBus, r, "channel_instance.deleted", "channel_instance", id.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// maskInstanceHTTP returns a map with credentials masked for HTTP responses.
func maskInstanceHTTP(inst store.ChannelInstanceData) map[string]any {
	result := map[string]any{
		"id":              inst.ID,
		"name":            inst.Name,
		"display_name":    inst.DisplayName,
		"channel_type":    inst.ChannelType,
		"agent_id":        inst.AgentID,
		"config":          inst.Config,
		"enabled":         inst.Enabled,
		"is_default":      store.IsDefaultChannelInstance(inst.Name),
		"has_credentials": len(inst.Credentials) > 0,
		"created_by":      inst.CreatedBy,
		"created_at":      inst.CreatedAt,
		"updated_at":      inst.UpdatedAt,
	}

	if len(inst.Credentials) > 0 {
		var raw map[string]any
		if json.Unmarshal(inst.Credentials, &raw) == nil {
			masked := make(map[string]any, len(raw))
			for k := range raw {
				masked[k] = "***"
			}
			result["credentials"] = masked
		} else {
			result["credentials"] = map[string]string{}
		}
	} else {
		result["credentials"] = map[string]string{}
	}

	return result
}

// --- Group file writers ---

// resolveAgentID looks up the channel instance and returns its agent_id.
func (h *ChannelInstancesHandler) resolveAgentID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	inst, ok := h.resolveInstance(w, r)
	if !ok {
		return uuid.Nil, false
	}
	return inst.AgentID, true
}

func (h *ChannelInstancesHandler) resolveInstance(w http.ResponseWriter, r *http.Request) (*store.ChannelInstanceData, bool) {
	locale := store.LocaleFromContext(r.Context())
	instID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "instance"))
		return nil, false
	}
	inst, err := h.store.Get(r.Context(), instID)
	if err != nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgInstanceNotFound))
		return nil, false
	}
	return inst, true
}

func (h *ChannelInstancesHandler) handleWriterGroups(w http.ResponseWriter, r *http.Request) {
	agentID, ok := h.resolveAgentID(w, r)
	if !ok {
		return
	}
	perms, err := h.configPermStore.List(r.Context(), agentID, store.ConfigTypeFileWriter, "")
	if err != nil {
		slog.Error("channel_instances.writer_groups", "error", err)
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "writer groups"))
		return
	}
	// Group by scope
	counts := make(map[string]int)
	for _, p := range perms {
		if p.Permission == "allow" {
			counts[p.Scope]++
		}
	}
	type groupInfo struct {
		GroupID     string `json:"group_id"`
		WriterCount int    `json:"writer_count"`
	}
	groups := make([]groupInfo, 0, len(counts))
	for scope, count := range counts {
		groups = append(groups, groupInfo{GroupID: scope, WriterCount: count})
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (h *ChannelInstancesHandler) handleListWriters(w http.ResponseWriter, r *http.Request) {
	agentID, ok := h.resolveAgentID(w, r)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	groupID := r.URL.Query().Get("group_id")
	if groupID == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "group_id"))
		return
	}
	perms, err := h.configPermStore.List(r.Context(), agentID, store.ConfigTypeFileWriter, groupID)
	if err != nil {
		slog.Error("channel_instances.list_writers", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "writers"))
		return
	}
	type writerData struct {
		UserID      string  `json:"user_id"`
		DisplayName *string `json:"display_name,omitempty"`
		Username    *string `json:"username,omitempty"`
	}
	writers := make([]writerData, 0, len(perms))
	for _, p := range perms {
		if p.Permission != "allow" {
			continue
		}
		wd := writerData{UserID: p.UserID}
		var meta struct {
			DisplayName string `json:"displayName"`
			Username    string `json:"username"`
		}
		if json.Unmarshal(p.Metadata, &meta) == nil {
			if meta.DisplayName != "" {
				wd.DisplayName = &meta.DisplayName
			}
			if meta.Username != "" {
				wd.Username = &meta.Username
			}
		}
		writers = append(writers, wd)
	}
	writeJSON(w, http.StatusOK, map[string]any{"writers": writers})
}

func (h *ChannelInstancesHandler) handleTestWriter(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.resolveInstance(w, r)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	var body struct {
		GroupID string `json:"group_id"`
		UserID  string `json:"user_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON))
		return
	}
	if body.GroupID == "" || body.UserID == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "group_id and user_id"))
		return
	}
	channelName, _, groupOK := channels.ParseGroupScope(body.GroupID)
	if !groupOK || channelName != inst.Name {
		writeJSON(w, http.StatusOK, map[string]any{
			"allowed":      false,
			"reason":       "invalid_group",
			"instance_id":  inst.ID.String(),
			"agent_id":     inst.AgentID.String(),
			"group_id":     body.GroupID,
			"user_id":      body.UserID,
			"writer_count": 0,
		})
		return
	}

	writers, err := h.configPermStore.ListFileWriters(r.Context(), inst.AgentID, body.GroupID)
	if err != nil {
		slog.Error("channel_instances.test_writer", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "writers"))
		return
	}
	allowed := false
	for _, p := range writers {
		if p.Permission == "allow" && p.UserID == body.UserID {
			allowed = true
			break
		}
	}
	reason := "not_writer"
	if allowed {
		reason = "writer"
	} else if len(writers) == 0 {
		reason = "no_writers_configured"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"allowed":      allowed,
		"reason":       reason,
		"instance_id":  inst.ID.String(),
		"agent_id":     inst.AgentID.String(),
		"group_id":     body.GroupID,
		"user_id":      body.UserID,
		"writer_count": len(writers),
	})
}

func (h *ChannelInstancesHandler) handleAddWriter(w http.ResponseWriter, r *http.Request) {
	agentID, ok := h.resolveAgentID(w, r)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	var body struct {
		GroupID     string `json:"group_id"`
		UserID      string `json:"user_id"`
		DisplayName string `json:"display_name"`
		Username    string `json:"username"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON))
		return
	}
	if body.GroupID == "" || body.UserID == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "group_id and user_id"))
		return
	}
	meta, _ := json.Marshal(map[string]string{"displayName": body.DisplayName, "username": body.Username})
	// Auto-enrich when caller supplied neither display name nor username.
	// Best-effort — any resolver error leaves the metadata as-is so the
	// store's fallback ({}) applies and the grant still succeeds.
	if body.DisplayName == "" && body.Username == "" {
		if enriched, ok := channels.EnrichFileWriterMetadata(r.Context(), h.memberResolver, body.GroupID, body.UserID); ok {
			meta = enriched
		}
	}
	if err := h.configPermStore.Grant(r.Context(), &store.ConfigPermission{
		AgentID:    agentID,
		Scope:      body.GroupID,
		ConfigType: store.ConfigTypeFileWriter,
		UserID:     body.UserID,
		Permission: "allow",
		Metadata:   meta,
	}); err != nil {
		slog.Error("channel_instances.add_writer", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "writer", "internal error"))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

func (h *ChannelInstancesHandler) handleRemoveWriter(w http.ResponseWriter, r *http.Request) {
	agentID, ok := h.resolveAgentID(w, r)
	if !ok {
		return
	}
	locale := store.LocaleFromContext(r.Context())
	userID := r.PathValue("userId")
	groupID := r.URL.Query().Get("group_id")
	if groupID == "" || userID == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "group_id and userId"))
		return
	}
	// Prevent removing the last writer (same guard as Telegram /removewriter)
	writers, _ := h.configPermStore.List(r.Context(), agentID, store.ConfigTypeFileWriter, groupID)
	allowCount := 0
	for _, p := range writers {
		if p.Permission == "allow" {
			allowCount++
		}
	}
	if allowCount <= 1 {
		writeError(w, http.StatusConflict, protocol.ErrFailedPrecondition, i18n.T(locale, i18n.MsgCannotRemoveLastWriter))
		return
	}
	if err := h.configPermStore.Revoke(r.Context(), agentID, groupID, store.ConfigTypeFileWriter, userID); err != nil {
		slog.Error("channel_instances.remove_writer", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "writer", "internal error"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// --- Channel contacts ---

func (h *ChannelInstancesHandler) handleListContacts(w http.ResponseWriter, r *http.Request) {
	opts := store.ContactListOpts{
		Limit:  50,
		Offset: 0,
	}

	if v := r.URL.Query().Get("search"); v != "" {
		opts.Search = v
	}
	if v := r.URL.Query().Get("channel_type"); v != "" {
		opts.ChannelType = v
	}
	if v := r.URL.Query().Get("peer_kind"); v != "" {
		opts.PeerKind = v
	}
	if v := r.URL.Query().Get("contact_type"); v != "" {
		opts.ContactType = v
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			opts.Limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			opts.Offset = n
		}
	}

	contacts, err := h.contactStore.ListContacts(r.Context(), opts)
	if err != nil {
		slog.Error("contacts.list", "error", err)
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "contacts"))
		return
	}
	if contacts == nil {
		contacts = []store.ChannelContact{}
	}

	total, countErr := h.contactStore.CountContacts(r.Context(), opts)
	if countErr != nil {
		slog.Warn("contacts.count", "error", countErr)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"contacts": contacts,
		"total":    total,
		"limit":    opts.Limit,
		"offset":   opts.Offset,
	})
}

func (h *ChannelInstancesHandler) handleResolveContacts(w http.ResponseWriter, r *http.Request) {
	idsParam := r.URL.Query().Get("ids")
	if idsParam == "" {
		writeJSON(w, http.StatusOK, map[string]any{"contacts": map[string]any{}})
		return
	}

	ids := strings.Split(idsParam, ",")
	if len(ids) > 100 {
		ids = ids[:100]
	}

	result, err := h.contactStore.GetContactsBySenderIDs(r.Context(), ids)
	if err != nil {
		slog.Error("contacts.resolve", "error", err)
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "contacts"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"contacts": result})
}

// isValidChannelType checks if the channel type is supported.
//
// Keep this list in sync with the WS twin in
// internal/gateway/methods/channel_instances.go and with CHANNEL_TYPES in
// ui/web/src/constants/channels.ts.
func isValidChannelType(ct string) bool {
	switch ct {
	case "telegram", "discord", "slack", "whatsapp", "zalo_oa", "zalo_personal", "feishu", "facebook", "pancake", "bitrix24":
		return true
	}
	return false
}
