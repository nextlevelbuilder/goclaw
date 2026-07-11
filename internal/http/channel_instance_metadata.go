package http

import (
	"context"
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type channelMetadataRefresher interface {
	RefreshContactCache(ctx context.Context) (channels.MetadataRefreshReport, error)
}

func (h *ChannelInstancesHandler) handleRefreshChannelMetadata(w http.ResponseWriter, r *http.Request) {
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	inst, ok := h.resolveInstance(w, r)
	if !ok {
		return
	}
	if inst.ChannelType != channels.TypeDiscord {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "metadata refresh is only supported for Discord channels")
		return
	}
	if h.channelMgr == nil {
		writeError(w, http.StatusConflict, protocol.ErrInvalidRequest, "channel manager is not available")
		return
	}
	ch, ok := h.channelMgr.GetChannel(inst.Name)
	if !ok {
		writeError(w, http.StatusConflict, protocol.ErrInvalidRequest, "Discord channel is not running")
		return
	}
	if !ch.IsRunning() {
		writeError(w, http.StatusConflict, protocol.ErrInvalidRequest, "Discord channel is not running")
		return
	}
	refresher, ok := ch.(channelMetadataRefresher)
	if !ok {
		writeError(w, http.StatusConflict, protocol.ErrInvalidRequest, "Discord channel metadata refresh is not available")
		return
	}
	report, err := refresher.RefreshContactCache(r.Context())
	if err != nil {
		report.OK = false
		report.Errors = append(report.Errors, err.Error())
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "report": report, "error": err.Error()})
		return
	}
	emitAudit(h.msgBus, r, "channel_metadata.refresh_triggered", "channel_instance", inst.ID.String())
	writeJSON(w, http.StatusOK, map[string]any{"ok": report.OK, "report": report})
}
