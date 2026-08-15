package http

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/channelmemory"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
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
	limit := parseMemoryExtractionLimit(r.URL.Query().Get("limit"))
	offset := parseMemoryExtractionOffset(r.URL.Query().Get("offset"))
	opts := store.ChannelMemoryItemListOptions{
		ChannelInstanceID: inst.ID,
		Status:            r.URL.Query().Get("status"),
		Limit:             limit,
		Offset:            offset,
	}
	items, err := h.memoryService.Extractions.ListItems(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "failed to list memory extraction items")
		return
	}
	total, err := h.memoryService.Extractions.CountItems(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "failed to count memory extraction items")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *ChannelInstancesHandler) handleMemoryExtractionSettings(w http.ResponseWriter, r *http.Request) {
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	inst := h.memoryInstance(w, r)
	if inst == nil {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON))
		return
	}
	configJSON, normalized, err := channelmemory.ApplyInstanceConfigPatch(inst.Config, body)
	if err != nil {
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON))
		return
	}
	if err := h.store.Update(r.Context(), inst.ID, map[string]any{"config": configJSON}); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "failed to update memory extraction settings")
		return
	}
	h.emitCacheInvalidate(inst.ID.String())
	emitAudit(h.msgBus, r, "channel_memory.settings_updated", "channel_instance", inst.ID.String())
	writeJSON(w, http.StatusOK, map[string]any{"config": normalized})
}

func (h *ChannelInstancesHandler) handleMemoryExtractionGroups(w http.ResponseWriter, r *http.Request) {
	inst := h.memoryInstance(w, r)
	if inst == nil {
		return
	}
	if inst.ChannelType != channels.TypeDiscord {
		writeJSON(w, http.StatusOK, map[string]any{"groups": []channelmemory.GroupOption{}})
		return
	}
	groups, err := h.memoryService.GroupOptions(r.Context(), inst)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "failed to list memory extraction groups")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
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

func (h *ChannelInstancesHandler) handleMemoryExtractionRunAll(w http.ResponseWriter, r *http.Request) {
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	inst := h.memoryInstance(w, r)
	if inst == nil {
		return
	}
	if acceptsNDJSON(r) {
		h.streamMemoryExtractionRunAll(w, r, inst)
		return
	}
	result, err := h.memoryService.RunAll(r.Context(), inst, "manual_all")
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, err.Error())
		return
	}
	emitAudit(h.msgBus, r, "channel_memory.run_all_triggered", "channel_instance", inst.ID.String())
	writeJSON(w, http.StatusAccepted, result)
}

func (h *ChannelInstancesHandler) streamMemoryExtractionRunAll(w http.ResponseWriter, r *http.Request, inst *store.ChannelInstanceData) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "streaming is not supported")
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusAccepted)
	_, err := h.memoryService.RunAllWithProgress(r.Context(), inst, "manual_all", func(event channelmemory.ProcessAllEvent) error {
		if err := json.NewEncoder(w).Encode(event); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	})
	if err != nil {
		_ = json.NewEncoder(w).Encode(channelmemory.ProcessAllEvent{Type: "error", Error: err.Error()})
		flusher.Flush()
		return
	}
	emitAudit(h.msgBus, r, "channel_memory.run_all_triggered", "channel_instance", inst.ID.String())
}

func acceptsNDJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/x-ndjson")
}

func parseMemoryExtractionLimit(raw string) int {
	if raw == "" {
		return 20
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 20
	}
	if n > 200 {
		return 200
	}
	return n
}

func parseMemoryExtractionOffset(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
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
