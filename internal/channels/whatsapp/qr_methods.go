package whatsapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	goclawprotocol "github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const qrSessionTimeout = 3 * time.Minute

// cancelEntry wraps a CancelFunc so it can be stored in sync.Map.CompareAndDelete.
type cancelEntry struct {
	cancel context.CancelFunc
}

// QRMethods handles whatsapp.qr.start — delivers QR codes to the UI wizard.
type QRMethods struct {
	instanceStore  store.ChannelInstanceStore
	manager        *channels.Manager
	activeSessions sync.Map // instanceID (string) -> *cancelEntry
}

func NewQRMethods(instanceStore store.ChannelInstanceStore, manager *channels.Manager) *QRMethods {
	return &QRMethods{instanceStore: instanceStore, manager: manager}
}

func (m *QRMethods) Register(router *gateway.MethodRouter) {
	router.Register(goclawprotocol.MethodWhatsAppQRStart, m.handleQRStart)
}

func (m *QRMethods) handleQRStart(ctx context.Context, client *gateway.Client, req *goclawprotocol.RequestFrame) {
	var params struct {
		InstanceID  string `json:"instance_id"`
		ForceReauth bool   `json:"force_reauth"`
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
	if err != nil || inst.ChannelType != channels.TypeWhatsApp {
		client.SendResponse(goclawprotocol.NewErrorResponse(req.ID, goclawprotocol.ErrNotFound, "whatsapp instance not found"))
		return
	}

	qrCtx, cancel := context.WithTimeout(ctx, qrSessionTimeout)
	entry := &cancelEntry{cancel: cancel}

	// Cancel any previous QR session for this instance.
	if prev, loaded := m.activeSessions.Swap(params.InstanceID, entry); loaded {
		if prevEntry, ok := prev.(*cancelEntry); ok {
			prevEntry.cancel()
		}
	}

	// ACK immediately — QR/done events arrive asynchronously.
	client.SendResponse(goclawprotocol.NewOKResponse(req.ID, map[string]any{"status": "started"}))

	go m.runQRSession(qrCtx, entry, client, params.InstanceID, inst.Name, params.ForceReauth)
}

func (m *QRMethods) runQRSession(ctx context.Context, entry *cancelEntry,
	client *gateway.Client, instanceIDStr, channelName string, forceReauth bool) {

	defer entry.cancel()
	defer m.activeSessions.CompareAndDelete(instanceIDStr, entry)

	// Wait for channel to appear in manager — instance creation triggers an async
	// reload, so the channel may not be registered yet when the wizard fires QR start.
	var wa *Channel
	for attempt := 0; attempt < 10; attempt++ {
		if ch, ok := m.manager.GetChannel(channelName); ok {
			if w, ok := ch.(*Channel); ok {
				wa = w
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
	if wa == nil {
		client.SendEvent(goclawprotocol.EventFrame{
			Type:  goclawprotocol.FrameTypeEvent,
			Event: goclawprotocol.EventWhatsAppQRDone,
			Payload: map[string]any{
				"instance_id": instanceIDStr,
				"success":     false,
				"error":       "channel not found",
			},
		})
		return
	}

	// Already authenticated and no force-reauth → signal connected.
	if wa.IsAuthenticated() && !forceReauth {
		client.SendEvent(goclawprotocol.EventFrame{
			Type:  goclawprotocol.FrameTypeEvent,
			Event: goclawprotocol.EventWhatsAppQRDone,
			Payload: map[string]any{
				"instance_id":       instanceIDStr,
				"success":           true,
				"already_connected": true,
			},
		})
		return
	}

	// Force reauth: clear session and prepare for fresh QR.
	if forceReauth {
		if err := wa.Reauth(); err != nil {
			slog.Warn("whatsapp QR: reauth failed", "error", err)
		}
	}

	// Deliver cached QR if available.
	if cached := wa.GetLastQRB64(); cached != "" {
		client.SendEvent(goclawprotocol.EventFrame{
			Type:  goclawprotocol.FrameTypeEvent,
			Event: goclawprotocol.EventWhatsAppQRCode,
			Payload: map[string]any{
				"instance_id": instanceIDStr,
				"png_b64":     cached,
			},
		})
	}

	// Start QR flow — get QR channel from whatsmeow.
	qrChan, err := wa.StartQRFlow(ctx)
	if err != nil {
		slog.Warn("whatsapp QR: start flow failed", "error", err)
		client.SendEvent(goclawprotocol.EventFrame{
			Type:  goclawprotocol.FrameTypeEvent,
			Event: goclawprotocol.EventWhatsAppQRDone,
			Payload: map[string]any{
				"instance_id": instanceIDStr,
				"success":     false,
				"error":       err.Error(),
			},
		})
		return
	}

	if qrChan == nil {
		// Already authenticated (StartQRFlow returned nil).
		client.SendEvent(goclawprotocol.EventFrame{
			Type:  goclawprotocol.FrameTypeEvent,
			Event: goclawprotocol.EventWhatsAppQRDone,
			Payload: map[string]any{
				"instance_id":       instanceIDStr,
				"success":           true,
				"already_connected": true,
			},
		})
		return
	}

	// Process QR events from whatsmeow.
	for {
		select {
		case <-ctx.Done():
			client.SendEvent(goclawprotocol.EventFrame{
				Type:  goclawprotocol.FrameTypeEvent,
				Event: goclawprotocol.EventWhatsAppQRDone,
				Payload: map[string]any{
					"instance_id": instanceIDStr,
					"success":     false,
					"error":       "QR session timed out — restart to try again",
				},
			})
			return

		case evt, ok := <-qrChan:
			if !ok {
				return // channel closed
			}

			switch evt.Event {
			case "code":
				png, qrErr := qrcode.Encode(evt.Code, qrcode.Medium, 256)
				if qrErr != nil {
					slog.Warn("whatsapp: QR PNG encode failed", "error", qrErr)
					continue
				}
				pngB64 := base64.StdEncoding.EncodeToString(png)

				wa.cacheQR(pngB64)

				client.SendEvent(goclawprotocol.EventFrame{
					Type:  goclawprotocol.FrameTypeEvent,
					Event: goclawprotocol.EventWhatsAppQRCode,
					Payload: map[string]any{
						"instance_id": instanceIDStr,
						"png_b64":     pngB64,
					},
				})

			case "success":
				client.SendEvent(goclawprotocol.EventFrame{
					Type:  goclawprotocol.FrameTypeEvent,
					Event: goclawprotocol.EventWhatsAppQRDone,
					Payload: map[string]any{
						"instance_id": instanceIDStr,
						"success":     true,
					},
				})
				slog.Info("whatsapp QR session completed", "instance", instanceIDStr)
				return

			case "timeout":
				client.SendEvent(goclawprotocol.EventFrame{
					Type:  goclawprotocol.FrameTypeEvent,
					Event: goclawprotocol.EventWhatsAppQRDone,
					Payload: map[string]any{
						"instance_id": instanceIDStr,
						"success":     false,
						"error":       "QR code expired — restart to try again",
					},
				})
				return
			}
		}
	}
}
