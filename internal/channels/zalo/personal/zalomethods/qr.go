package zalomethods

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	goclawprotocol "github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// cancelEntry wraps a CancelFunc so it can be stored and compared by pointer
// in sync.Map.CompareAndDelete (function types are not comparable).
type cancelEntry struct {
	cancel context.CancelFunc
}

// QRMethods handles QR login for zalo_personal channel instances.
type QRMethods struct {
	instanceStore  store.ChannelInstanceStore
	msgBus         *bus.MessageBus
	activeSessions sync.Map // instanceID (string) -> *cancelEntry
}

func NewQRMethods(s store.ChannelInstanceStore, msgBus *bus.MessageBus) *QRMethods {
	return &QRMethods{instanceStore: s, msgBus: msgBus}
}

func (m *QRMethods) Register(router *gateway.MethodRouter) {
	router.Register(goclawprotocol.MethodZaloPersonalQRStart, m.handleQRStart)
}

func (m *QRMethods) handleQRStart(ctx context.Context, client *gateway.Client, req *goclawprotocol.RequestFrame) {
	var params struct {
		InstanceID string `json:"instance_id"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}

	instID, err := uuid.Parse(params.InstanceID)
	if err != nil {
		client.SendResponse(goclawprotocol.NewErrorResponse(req.ID, goclawprotocol.ErrInvalidRequest, "invalid instance_id"))
		return
	}

	inst, err := m.instanceStore.Get(ctx, instID)
	if err != nil || inst.ChannelType != channels.TypeZaloPersonal {
		client.SendResponse(goclawprotocol.NewErrorResponse(req.ID, goclawprotocol.ErrNotFound, "zalo_personal instance not found"))
		return
	}

	qrCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	entry := &cancelEntry{cancel: cancel}

	// Atomically swap cancel entry; cancel any previous QR session so the user can retry.
	if prev, loaded := m.activeSessions.Swap(params.InstanceID, entry); loaded {
		if prevEntry, ok := prev.(*cancelEntry); ok {
			prevEntry.cancel()
		}
	}

	// ACK immediately — QR arrives via event.
	client.SendResponse(goclawprotocol.NewOKResponse(req.ID, map[string]any{"status": "started"}))

	go m.runQRFlow(qrCtx, entry, client, params.InstanceID, instID)
}

func (m *QRMethods) runQRFlow(ctx context.Context, entry *cancelEntry, client *gateway.Client, instanceIDStr string, instanceID uuid.UUID) {
	defer entry.cancel()
	defer m.activeSessions.CompareAndDelete(instanceIDStr, entry)

	sess := protocol.NewSession()

	cred, err := protocol.LoginQR(ctx, sess, func(qrPNG []byte) {
		client.SendEvent(goclawprotocol.EventFrame{
			Type:  goclawprotocol.FrameTypeEvent,
			Event: goclawprotocol.EventZaloPersonalQRCode,
			Payload: map[string]any{
				"instance_id": instanceIDStr,
				"png_b64":     base64.StdEncoding.EncodeToString(qrPNG),
			},
		})
	})

	if err != nil {
		slog.Warn("Zalo Personal QR login failed", "instance", instanceIDStr, "error", err)
		client.SendEvent(*goclawprotocol.NewEvent(goclawprotocol.EventZaloPersonalQRDone, map[string]any{
			"instance_id": instanceIDStr,
			"success":     false,
			"error":       err.Error(),
		}))
		return
	}

	credsJSON, err := json.Marshal(map[string]any{
		"imei":      cred.IMEI,
		"cookie":    cred.Cookie,
		"userAgent": cred.UserAgent,
		"language":  cred.Language,
	})
	if err != nil {
		slog.Error("Zalo Personal QR: marshal credentials failed", "error", err)
		client.SendEvent(*goclawprotocol.NewEvent(goclawprotocol.EventZaloPersonalQRDone, map[string]any{
			"instance_id": instanceIDStr,
			"success":     false,
			"error":       "internal error: credential serialization failed",
		}))
		return
	}

	if err := m.instanceStore.Update(ctx, instanceID, map[string]any{
		"credentials": string(credsJSON),
	}); err != nil {
		slog.Error("Zalo Personal QR: save credentials failed", "instance", instanceIDStr, "error", err)
		client.SendEvent(*goclawprotocol.NewEvent(goclawprotocol.EventZaloPersonalQRDone, map[string]any{
			"instance_id": instanceIDStr,
			"success":     false,
			"error":       "failed to save credentials",
		}))
		return
	}

	// Trigger instanceLoader reload via cache invalidation.
	if m.msgBus != nil {
		m.msgBus.Broadcast(bus.Event{
			Name:    goclawprotocol.EventCacheInvalidate,
			Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindChannelInstances},
		})
	}

	client.SendEvent(*goclawprotocol.NewEvent(goclawprotocol.EventZaloPersonalQRDone, map[string]any{
		"instance_id": instanceIDStr,
		"success":     true,
	}))

	slog.Info("Zalo Personal QR login completed, credentials saved", "instance", instanceIDStr)
}
