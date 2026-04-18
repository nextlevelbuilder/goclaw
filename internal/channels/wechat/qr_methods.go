package wechat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/skip2/go-qrcode"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// cancelEntry wraps a CancelFunc so it can be stored in sync.Map.CompareAndDelete.
type cancelEntry struct {
	cancel context.CancelFunc
}

// QRMethods handles wechat.qr.start — delivers QR codes to the UI wizard
// for WeChat personal login via the iLink Bot API.
type QRMethods struct {
	instanceStore  store.ChannelInstanceStore
	msgBus         *bus.MessageBus
	activeSessions sync.Map // instanceID (string) -> *cancelEntry
}

func NewQRMethods(s store.ChannelInstanceStore, msgBus *bus.MessageBus) *QRMethods {
	return &QRMethods{instanceStore: s, msgBus: msgBus}
}

func (m *QRMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodWeChatQRStart, m.handleQRStart)
}

func (m *QRMethods) handleQRStart(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		InstanceID string `json:"instance_id"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}

	instID, err := uuid.Parse(params.InstanceID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid instance_id"))
		return
	}

	inst, err := m.instanceStore.Get(ctx, instID)
	if err != nil || inst.ChannelType != channels.TypeWeChat {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, "wechat instance not found"))
		return
	}

	qrCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	entry := &cancelEntry{cancel: cancel}

	// Cancel any previous QR session for this instance.
	if prev, loaded := m.activeSessions.Swap(params.InstanceID, entry); loaded {
		if prevEntry, ok := prev.(*cancelEntry); ok {
			prevEntry.cancel()
		}
	}

	// ACK immediately — QR arrives via event.
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"status": "started"}))

	go m.runQRFlow(qrCtx, entry, client, params.InstanceID, instID)
}

func (m *QRMethods) runQRFlow(ctx context.Context, entry *cancelEntry,
	client *gateway.Client, instanceIDStr string, instanceID uuid.UUID) {

	defer entry.cancel()
	defer m.activeSessions.CompareAndDelete(instanceIDStr, entry)

	// Create temporary API client — QR endpoints don't require a bot token.
	api := NewAPIClient(DefaultBaseURL, "")

	// 1. Fetch QR code from iLink.
	qrResp, err := api.GetBotQRCode(ctx, "3")
	if err != nil {
		slog.Warn("WeChat QR: fetch QR failed", "instance", instanceIDStr, "error", err)
		client.SendEvent(*protocol.NewEvent(protocol.EventWeChatQRDone, map[string]any{
			"instance_id": instanceIDStr,
			"success":     false,
			"error":       err.Error(),
		}))
		return
	}

	// 2. Convert text URL to a QR code PNG, then base64 to avoid CSP issues.
	var imgContent string
	if pngBytes, err := qrcode.Encode(qrResp.QRCodeImgContent, qrcode.Medium, 256); err == nil {
		imgContent = base64.StdEncoding.EncodeToString(pngBytes)
	} else {
		// Fallback strictly for error bounds; frontend will fail on this.
		imgContent = qrResp.QRCodeImgContent
	}

	client.SendEvent(protocol.EventFrame{
		Type:  protocol.FrameTypeEvent,
		Event: protocol.EventWeChatQRCode,
		Payload: map[string]any{
			"instance_id": instanceIDStr,
			"img_content": imgContent, // now base64 encoded PNG
			"qrcode":      qrResp.QRCode,
		},
	})

	// 3. Long-poll QR code status until scanned or timeout.
	for {
		select {
		case <-ctx.Done():
			client.SendEvent(*protocol.NewEvent(protocol.EventWeChatQRDone, map[string]any{
				"instance_id": instanceIDStr,
				"success":     false,
				"error":       "QR session timed out — restart to try again",
			}))
			return
		default:
		}

		status, err := api.GetQRCodeStatus(ctx, qrResp.QRCode, 40000)
		if err != nil {
			if ctx.Err() != nil {
				// Context cancelled or timed out during poll — handled at top of loop.
				continue
			}
			slog.Warn("WeChat QR: poll status failed", "instance", instanceIDStr, "error", err)
			client.SendEvent(*protocol.NewEvent(protocol.EventWeChatQRDone, map[string]any{
				"instance_id": instanceIDStr,
				"success":     false,
				"error":       err.Error(),
			}))
			return
		}

		if status.Status == "confirmed" && status.BotToken != "" {
			// QR scanned — save credentials to the channel instance.
			credsJSON, err := json.Marshal(map[string]any{
				"token": status.BotToken,
			})
			if err != nil {
				slog.Error("WeChat QR: marshal credentials failed", "error", err)
				client.SendEvent(*protocol.NewEvent(protocol.EventWeChatQRDone, map[string]any{
					"instance_id": instanceIDStr,
					"success":     false,
					"error":       "internal error: credential serialization failed",
				}))
				return
			}

			// Build config updates with baseURL/redirectHost if returned.
			updates := map[string]any{
				"credentials": string(credsJSON),
			}
			if status.BaseURL != "" || status.RedirectHost != "" {
				// Fetch latest instance to avoid overwriting existing config like dm_policy.
				if latestInst, err := m.instanceStore.Get(ctx, instanceID); err == nil {
					var existingConfig map[string]any
					if len(latestInst.Config) > 0 {
						_ = json.Unmarshal(latestInst.Config, &existingConfig)
					}
					if existingConfig == nil {
						existingConfig = make(map[string]any)
					}
					if status.RedirectHost != "" {
						existingConfig["base_url"] = "https://" + status.RedirectHost
					} else {
						existingConfig["base_url"] = status.BaseURL
					}
					configJSON, _ := json.Marshal(existingConfig)
					updates["config"] = string(configJSON)
				}
			}

			if err := m.instanceStore.Update(ctx, instanceID, updates); err != nil {
				slog.Error("WeChat QR: save credentials failed", "instance", instanceIDStr, "error", err)
				client.SendEvent(*protocol.NewEvent(protocol.EventWeChatQRDone, map[string]any{
					"instance_id": instanceIDStr,
					"success":     false,
					"error":       "failed to save credentials",
				}))
				return
			}

			// Trigger instanceLoader reload via cache invalidation.
			if m.msgBus != nil {
				m.msgBus.Broadcast(bus.Event{
					Name:    protocol.EventCacheInvalidate,
					Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindChannelInstances},
				})
			}

			client.SendEvent(*protocol.NewEvent(protocol.EventWeChatQRDone, map[string]any{
				"instance_id": instanceIDStr,
				"success":     true,
			}))
			slog.Info("WeChat QR login completed, credentials saved", "instance", instanceIDStr)
			return
		}

		// Status not confirmed yet — continue polling.
	}
}
