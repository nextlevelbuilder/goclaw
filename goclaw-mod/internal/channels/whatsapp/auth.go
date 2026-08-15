package whatsapp

import (
	"context"
	"fmt"
	"log/slog"

	"go.mau.fi/whatsmeow"
	wastore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

// StartQRFlow initiates the QR authentication flow.
// Returns a channel that emits QR code strings and auth events.
// Lazily initializes the whatsmeow client if Start() hasn't been called yet
// (handles timing race between async instance reload and wizard auto-start).
// Serialized with Reauth via reauthMu to prevent races on rapid double-clicks.
func (c *Channel) StartQRFlow(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
	c.reauthMu.Lock()
	defer c.reauthMu.Unlock()

	c.mu.Lock()
	if err := c.ensureQRClientLocked(ctx); err != nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("whatsapp get device: %w", err)
	}
	client := c.client
	c.mu.Unlock()

	if c.IsAuthenticated() {
		return nil, nil // caller checks this
	}

	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		return nil, fmt.Errorf("whatsapp get QR channel: %w", err)
	}

	if !client.IsConnected() {
		if err := client.Connect(); err != nil {
			return nil, fmt.Errorf("whatsapp connect for QR: %w", err)
		}
	}

	return qrChan, nil
}

// Reauth clears the current session and prepares for a fresh QR scan.
// Serialized with StartQRFlow via reauthMu to prevent races on rapid double-clicks.
func (c *Channel) Reauth() error {
	c.reauthMu.Lock()
	defer c.reauthMu.Unlock()

	slog.Info("whatsapp: reauth requested", "channel", c.Name())

	c.lastQRMu.Lock()
	c.waAuthenticated = false
	c.lastQRB64 = ""
	c.lastQRMu.Unlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		c.client.Disconnect()
	}

	// Delete device from store to force fresh QR on next connect.
	if c.client != nil && c.client.Store.ID != nil {
		if err := c.client.Store.Delete(context.Background()); err != nil {
			slog.Warn("whatsapp: failed to delete device store", "error", err)
		}
	}

	// Reset context so the new client gets a fresh lifecycle.
	if c.cancel != nil {
		c.cancel()
	}
	// Use parentCtx if available so the new lifecycle is still bound to the gateway.
	parent := c.parentCtx
	if parent == nil {
		parent = context.Background()
	}
	c.ctx, c.cancel = context.WithCancel(parent)

	if err := c.resetClientLocked(context.Background()); err != nil {
		return fmt.Errorf("whatsapp: get fresh device: %w", err)
	}

	return nil
}

// ensureQRClientLocked lazily creates or refreshes the client before QR login.
// The caller must hold c.mu and c.reauthMu.
func (c *Channel) ensureQRClientLocked(ctx context.Context) error {
	if c.client == nil {
		return c.resetClientLocked(ctx)
	}
	if !c.client.Store.Deleted {
		return nil
	}
	c.lastQRMu.Lock()
	c.waAuthenticated = false
	c.lastQRB64 = ""
	c.lastQRMu.Unlock()
	return c.resetClientLocked(ctx)
}

// resetClientLocked replaces the whatsmeow client while preserving the channel lifecycle.
// The caller must hold c.mu.
func (c *Channel) resetClientLocked(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.ctx == nil {
		parent := c.parentCtx
		if parent == nil {
			parent = context.Background()
		}
		c.ctx, c.cancel = context.WithCancel(parent)
	}
	deviceStore, err := c.resolveDeviceStoreLocked(ctx)
	if err != nil {
		return err
	}
	c.client = whatsmeow.NewClient(deviceStore, nil)
	c.client.AddEventHandler(c.handleEvent)
	return nil
}

func (c *Channel) resolveDeviceStoreLocked(ctx context.Context) (*wastore.Device, error) {
	if !c.deviceJID.IsEmpty() {
		deviceStore, err := c.container.GetDevice(ctx, c.deviceJID)
		if err != nil {
			return nil, err
		}
		if deviceStore != nil && !deviceStore.Deleted {
			return deviceStore, nil
		}
		slog.Info("whatsapp scoped device missing; creating fresh QR device",
			"channel", c.Name(), "device_hash", hashWhatsAppIdentifier(c.deviceJID.String()))
		return c.container.NewDevice(), nil
	}

	if c.legacyFirstDeviceFallback {
		devices, err := c.container.GetAllDevices(ctx)
		if err != nil {
			return nil, err
		}
		if len(devices) > 0 {
			return devices[0], nil
		}
	}

	return c.container.NewDevice(), nil
}

func (c *Channel) currentDeviceJID() types.JID {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil || c.client.Store == nil {
		return types.EmptyJID
	}
	return c.client.Store.GetJID()
}

func (c *Channel) setDeviceJID(jid types.JID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deviceJID = jid
}
