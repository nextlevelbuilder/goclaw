package http

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/channelmemory"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func (h *ChannelInstancesHandler) handleMemoryExtractionStatus(w http.ResponseWriter, r *http.Request) {
	inst := h.memoryInstance(w, r)
	if inst == nil {
		return
	}
	status, err := h.memoryService.Status(r.Context(), inst)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "failed to load memory extraction status")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *ChannelInstancesHandler) handleMemoryExtractionItems(w http.ResponseWriter, r *http.Request) {
	inst := h.memoryInstance(w, r)
	if inst == nil {
		return
	}
	items, err := h.memoryService.Extractions.ListItems(r.Context(), store.ChannelMemoryItemListOptions{
		ChannelInstanceID: inst.ID,
		Status:            r.URL.Query().Get("status"),
		Limit:             100,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "failed to list memory extraction items")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *ChannelInstancesHandler) handleMemoryExtractionSettings(w http.ResponseWriter, r *http.Request) {
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	inst := h.memoryInstance(w, r)
	if inst == nil {
		return
	}
	var cfg channelmemory.Config
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&cfg); err != nil {
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON))
		return
	}
	normalized := channelmemory.ParseConfig(channelmemory.MergeIntoInstanceConfig(nil, cfg))
	configJSON := channelmemory.MergeIntoInstanceConfig(inst.Config, normalized)
	if err := h.store.Update(r.Context(), inst.ID, map[string]any{"config": configJSON}); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "failed to update memory extraction settings")
		return
	}
	h.emitCacheInvalidate(inst.ID.String())
	emitAudit(h.msgBus, r, "channel_memory.settings_updated", "channel_instance", inst.ID.String())
	writeJSON(w, http.StatusOK, map[string]any{"config": normalized})
}

func (h *ChannelInstancesHandler) handleMemoryExtractionRun(w http.ResponseWriter, r *http.Request) {
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	inst := h.memoryInstance(w, r)
	if inst == nil {
		return
	}
	run, err := h.memoryService.RunNow(r.Context(), inst, "manual")
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, err.Error())
		return
	}
	emitAudit(h.msgBus, r, "channel_memory.run_triggered", "channel_instance", inst.ID.String())
	writeJSON(w, http.StatusAccepted, map[string]any{"run": run})
}

func (h *ChannelInstancesHandler) handleMemoryExtractionApprove(w http.ResponseWriter, r *http.Request) {
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	inst := h.memoryInstance(w, r)
	if inst == nil {
		return
	}
	itemID, ok := parsePathUUID(w, r, "itemID")
	if !ok {
		return
	}
	if !h.memoryItemBelongsToInstance(w, r, itemID, inst.ID) {
		return
	}
	item, err := h.memoryService.Approve(r.Context(), itemID, store.UserIDFromContext(r.Context()))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, err.Error())
		return
	}
	emitAudit(h.msgBus, r, "channel_memory.item_approved", "channel_memory_item", itemID.String())
	writeJSON(w, http.StatusOK, item)
}

func (h *ChannelInstancesHandler) handleMemoryExtractionReject(w http.ResponseWriter, r *http.Request) {
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	inst := h.memoryInstance(w, r)
	if inst == nil {
		return
	}
	itemID, ok := parsePathUUID(w, r, "itemID")
	if !ok {
		return
	}
	if !h.memoryItemBelongsToInstance(w, r, itemID, inst.ID) {
		return
	}
	if err := h.memoryService.Reject(r.Context(), itemID, store.UserIDFromContext(r.Context())); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, err.Error())
		return
	}
	emitAudit(h.msgBus, r, "channel_memory.item_rejected", "channel_memory_item", itemID.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

func (h *ChannelInstancesHandler) handleMemoryExtractionDelete(w http.ResponseWriter, r *http.Request) {
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	inst := h.memoryInstance(w, r)
	if inst == nil {
		return
	}
	itemID, ok := parsePathUUID(w, r, "itemID")
	if !ok {
		return
	}
	if !h.memoryItemBelongsToInstance(w, r, itemID, inst.ID) {
		return
	}
	if err := h.memoryService.Delete(r.Context(), itemID); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, err.Error())
		return
	}
	emitAudit(h.msgBus, r, "channel_memory.item_deleted", "channel_memory_item", itemID.String())
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *ChannelInstancesHandler) memoryInstance(w http.ResponseWriter, r *http.Request) *store.ChannelInstanceData {
	id, ok := parsePathUUID(w, r, "id")
	if !ok {
		return nil
	}
	inst, err := h.store.Get(r.Context(), id)
	if err != nil {
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgInstanceNotFound))
		return nil
	}
	return inst
}

func parsePathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, name))
		return uuid.Nil, false
	}
	return id, true
}

func (h *ChannelInstancesHandler) memoryItemBelongsToInstance(w http.ResponseWriter, r *http.Request, itemID, channelID uuid.UUID) bool {
	item, err := h.memoryService.Extractions.GetItem(r.Context(), itemID)
	if err != nil || item.ChannelInstanceID != channelID {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, "memory extraction item not found")
		return false
	}
	return true
}
