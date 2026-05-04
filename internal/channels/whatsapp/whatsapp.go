package whatsapp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.mau.fi/whatsmeow"
	wastore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	pairingDebounceTime = 60 * time.Second
	maxMessageLen       = 4096 // WhatsApp practical message length limit
)

func init() {
	// Set device name shown in WhatsApp's "Linked Devices" screen (once at package init).
	wastore.DeviceProps.Os = new("GoClaw")
}

// Channel connects directly to WhatsApp via go.mau.fi/whatsmeow.
// Auth state is stored in PostgreSQL (standard) or SQLite (desktop).
type Channel struct {
	*channels.BaseChannel
	client    *whatsmeow.Client
	container *sqlstore.Container
	config    config.WhatsAppConfig
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	parentCtx        context.Context       // stored from Start() for Reauth() context chain
	audioMgr         *audio.Manager        // unified STT via audio.Manager (nil = no STT)
	builtinToolStore store.BuiltinToolStore // reads stt settings (whatsapp_enabled) per voice message; nil = opt-out

	// QR state
	lastQRMu        sync.RWMutex
	lastQRB64       string    // base64-encoded PNG, empty when authenticated
	waAuthenticated bool      // true once WhatsApp account is connected
	myJID           types.JID // linked account's phone JID for mention detection
	myLID           types.JID // linked account's LID — WhatsApp's newer identifier

	// typingCancel tracks active typing-refresh loops per chatID.
	typingCancel sync.Map // chatID string → context.CancelFunc

	// reauthMu serializes Reauth() and StartQRFlow() to prevent race when user clicks reauth rapidly.
	reauthMu sync.Mutex
	// pairingService, pairingDebounce, approvedGroups, groupHistory are inherited from channels.BaseChannel.

	// instanceID + instanceStore scope this channel to a specific channel_instances row,
	// so multiple WhatsApp channels in one deploy each bind to their own whatsmeow_device row.
	// Without this, every channel reused the first device returned by GetFirstDevice and ended
	// up logged in as the same WhatsApp account regardless of name.
	instanceID    uuid.UUID
	instanceStore store.ChannelInstanceStore
	// configJID is the device JID this channel adopted on a previous run, mirrored from the
	// instance's config jsonb (key "jid"). Empty when the channel has never paired.
	configJID string
}

// GetLastQRB64 returns the most recent QR PNG (base64).
func (c *Channel) GetLastQRB64() string {
	c.lastQRMu.RLock()
	defer c.lastQRMu.RUnlock()
	return c.lastQRB64
}

// IsAuthenticated reports whether the WhatsApp account is currently authenticated.
func (c *Channel) IsAuthenticated() bool {
	c.lastQRMu.RLock()
	defer c.lastQRMu.RUnlock()
	return c.waAuthenticated
}

// cacheQR stores the latest QR PNG (base64) for late-joining wizard clients.
func (c *Channel) cacheQR(pngB64 string) {
	c.lastQRMu.Lock()
	c.lastQRB64 = pngB64
	c.lastQRMu.Unlock()
}

// New creates a new WhatsApp channel backed by whatsmeow.
// dialect must be "pgx" (PostgreSQL) or "sqlite3" (SQLite/desktop).
// audioMgr is optional (nil = STT disabled).
// builtinToolStore is optional (nil = STT permanently opt-out regardless of admin toggle).
// instanceStore is optional but required for multi-instance device scoping; without it,
// the channel falls back to GetFirstDevice (legacy single-instance behavior).
// configJID is the JID adopted on a prior run (from instance config "jid"); empty for
// fresh instances that should NewDevice + QR.
func New(cfg config.WhatsAppConfig, msgBus *bus.MessageBus,
	pairingSvc store.PairingStore, db *sql.DB,
	pendingStore store.PendingMessageStore, dialect string, audioMgr *audio.Manager,
	builtinToolStore store.BuiltinToolStore,
	instanceStore store.ChannelInstanceStore, configJID string) (*Channel, error) {

	base := channels.NewBaseChannel(channels.TypeWhatsApp, msgBus, cfg.AllowFrom)
	base.ValidatePolicy(cfg.DMPolicy, cfg.GroupPolicy)

	container := sqlstore.NewWithDB(db, dialect, nil)
	if err := container.Upgrade(context.Background()); err != nil {
		return nil, fmt.Errorf("whatsapp sqlstore upgrade: %w", err)
	}

	ch := &Channel{
		BaseChannel:      base,
		config:           cfg,
		container:        container,
		audioMgr:         audioMgr,
		builtinToolStore: builtinToolStore,
		instanceStore:    instanceStore,
		configJID:        configJID,
	}
	ch.SetPairingService(pairingSvc)
	ch.SetGroupHistory(channels.MakeHistory("whatsapp", pendingStore, base.TenantID()))
	return ch, nil
}

// SetInstanceID associates this channel with its channel_instances row.
// Called by InstanceLoader after construction so we can persist the paired JID
// back to the row's config jsonb on PairSuccess.
func (c *Channel) SetInstanceID(id uuid.UUID) { c.instanceID = id }

// resolveDevice returns the *store.Device this channel should use, scoped to the
// channel_instances row identified by configJID/instanceID. Three paths:
//  1. configJID set + device exists in whatsmeow_device → reuse it.
//  2. configJID empty + adoption succeeds → adopt an unclaimed orphan device
//     (covers single-channel deploys upgrading to multi-channel without re-pair).
//  3. Otherwise → NewDevice() returns a fresh in-memory device that whatsmeow
//     will persist via Connect → QR pairing flow.
func (c *Channel) resolveDevice(ctx context.Context) (*wastore.Device, error) {
	if c.configJID != "" {
		jid, err := types.ParseJID(c.configJID)
		if err == nil {
			dev, err := c.container.GetDevice(ctx, jid)
			if err != nil {
				return nil, fmt.Errorf("whatsapp get device by jid %s: %w", jid, err)
			}
			if dev != nil {
				return dev, nil
			}
			slog.Warn("whatsapp: stored JID not found in device store, falling back to fresh pairing",
				"channel", c.Name(), "jid", c.configJID)
		} else {
			slog.Warn("whatsapp: stored JID is malformed, falling back to fresh pairing",
				"channel", c.Name(), "jid", c.configJID, "error", err)
		}
	}
	if dev, ok := c.adoptOrphanDevice(ctx); ok {
		slog.Info("whatsapp: adopted existing device for instance",
			"channel", c.Name(), "jid", dev.ID)
		// Persist the adopted JID so subsequent boots take the configJID path
		// directly and don't risk re-adopting a device already claimed by another
		// channel that just happened to start later.
		if dev.ID != nil {
			c.persistJID(ctx, *dev.ID)
		}
		return dev, nil
	}
	return c.container.NewDevice(), nil
}

// adoptOrphanDevice handles the upgrade case where a deploy with a single
// pre-existing whatsmeow_device row gains a second WhatsApp channel_instance.
// To avoid stealing the legacy device from the wrong instance, we only adopt
// when ALL of the following hold:
//   - exactly one whatsmeow_device row exists in the store (so there is no
//     ambiguity about which device is "the legacy one"), AND
//   - exactly one WhatsApp channel_instance exists in the database (so the
//     legacy device unambiguously belongs to that instance), AND
//   - this channel IS that single instance.
//
// In every other configuration (multi-instance deploys, fresh installs, etc.)
// adoption is skipped and the channel goes through QR pairing.
func (c *Channel) adoptOrphanDevice(ctx context.Context) (*wastore.Device, bool) {
	if c.instanceStore == nil || c.instanceID == uuid.Nil {
		return nil, false
	}
	devs, err := c.container.GetAllDevices(ctx)
	if err != nil || len(devs) != 1 {
		return nil, false
	}
	dev := devs[0]
	if dev == nil || dev.ID == nil {
		return nil, false
	}
	listCtx := store.WithCrossTenant(ctx)
	instances, err := c.instanceStore.ListAllInstances(listCtx)
	if err != nil {
		slog.Warn("whatsapp: list instances for adoption failed", "error", err)
		return nil, false
	}
	var (
		whatsappCount int
		soleID        uuid.UUID
		soleJID       string
	)
	for _, inst := range instances {
		if inst.ChannelType != channels.TypeWhatsApp {
			continue
		}
		whatsappCount++
		if whatsappCount > 1 {
			return nil, false
		}
		soleID = inst.ID
		var ic struct {
			JID string `json:"jid"`
		}
		if len(inst.Config) > 0 {
			_ = json.Unmarshal(inst.Config, &ic)
		}
		soleJID = ic.JID
	}
	if whatsappCount != 1 || soleID != c.instanceID {
		return nil, false
	}
	if soleJID != "" && soleJID != dev.ID.String() {
		// Sole instance already claims a different JID — refuse to adopt.
		return nil, false
	}
	return dev, true
}

// persistJID writes the device JID back to channel_instances.config so the next
// channel start binds to the same device without going through QR. Best-effort:
// failures are logged but don't fail the boot — the channel is already connected.
func (c *Channel) persistJID(ctx context.Context, jid types.JID) {
	if c.instanceStore == nil || c.instanceID == uuid.Nil {
		return
	}
	jidStr := jid.String()
	if jidStr == c.configJID {
		return
	}
	tenantID := c.TenantID()
	scopeCtx := ctx
	if tenantID != uuid.Nil {
		scopeCtx = store.WithTenantID(ctx, tenantID)
	} else {
		scopeCtx = store.WithCrossTenant(ctx)
	}
	inst, err := c.instanceStore.Get(scopeCtx, c.instanceID)
	if err != nil {
		slog.Warn("whatsapp: persist JID — instance lookup failed",
			"channel", c.Name(), "instance_id", c.instanceID, "error", err)
		return
	}
	cfgMap := map[string]any{}
	if len(inst.Config) > 0 {
		if err := json.Unmarshal(inst.Config, &cfgMap); err != nil {
			slog.Warn("whatsapp: persist JID — config unmarshal failed",
				"channel", c.Name(), "error", err)
			cfgMap = map[string]any{}
		}
	}
	cfgMap["jid"] = jidStr
	cfgBytes, err := json.Marshal(cfgMap)
	if err != nil {
		slog.Warn("whatsapp: persist JID — config marshal failed", "error", err)
		return
	}
	if err := c.instanceStore.Update(scopeCtx, c.instanceID,
		map[string]any{"config": cfgBytes}); err != nil {
		slog.Warn("whatsapp: persist JID — update failed",
			"channel", c.Name(), "instance_id", c.instanceID, "error", err)
		return
	}
	c.configJID = jidStr
	slog.Info("whatsapp: persisted device JID to channel instance",
		"channel", c.Name(), "jid", jidStr)
}

// Start initializes the whatsmeow client and connects to WhatsApp.
func (c *Channel) Start(ctx context.Context) error {
	slog.Info("starting whatsapp channel (whatsmeow)")
	c.MarkStarting("Initializing WhatsApp connection")

	c.parentCtx = ctx
	c.ctx, c.cancel = context.WithCancel(ctx)

	deviceStore, err := c.resolveDevice(ctx)
	if err != nil {
		return fmt.Errorf("whatsapp get device: %w", err)
	}

	c.client = whatsmeow.NewClient(deviceStore, nil)
	c.client.AddEventHandler(c.handleEvent)

	if c.client.Store.ID == nil {
		// Not paired yet — QR flow will be triggered by qr_methods.go.
		slog.Info("whatsapp: not paired yet, waiting for QR scan", "channel", c.Name())
		c.MarkDegraded("Awaiting QR scan", "Scan QR code to authenticate",
			channels.ChannelFailureKindAuth, false)
	} else {
		if err := c.client.Connect(); err != nil {
			slog.Warn("whatsapp: initial connect failed", "error", err)
			c.MarkDegraded("Connection failed", err.Error(),
				channels.ChannelFailureKindNetwork, true)
		}
	}

	c.SetRunning(true)
	return nil
}

// BlockReplyEnabled returns the per-channel block_reply override (nil = inherit gateway default).
func (c *Channel) BlockReplyEnabled() *bool { return c.config.BlockReply }

// Stop gracefully shuts down the WhatsApp channel.
func (c *Channel) Stop(_ context.Context) error {
	slog.Info("stopping whatsapp channel")

	if c.cancel != nil {
		c.cancel()
	}
	if c.client != nil {
		c.client.Disconnect()
	}

	// Cancel all active typing goroutines.
	c.typingCancel.Range(func(key, value any) bool {
		if fn, ok := value.(context.CancelFunc); ok {
			fn()
		}
		c.typingCancel.Delete(key)
		return true
	})

	c.SetRunning(false)
	c.MarkStopped("Stopped")
	return nil
}

// handleEvent dispatches whatsmeow events.
func (c *Channel) handleEvent(evt any) {
	switch v := evt.(type) {
	case *events.Message:
		c.handleIncomingMessage(v)
	case *events.Connected:
		c.handleConnected()
	case *events.Disconnected:
		c.handleDisconnected()
	case *events.LoggedOut:
		c.handleLoggedOut(v)
	case *events.PairSuccess:
		slog.Info("whatsapp: pair success", "channel", c.Name(), "jid", v.ID.String())
		// Bind this freshly-paired device to our channel_instances row so the next
		// boot reuses the same device instead of going back through QR (or worse,
		// adopting a sibling channel's device).
		if c.parentCtx != nil {
			c.persistJID(c.parentCtx, v.ID)
		} else {
			c.persistJID(context.Background(), v.ID)
		}
	}
}

// handleConnected processes the Connected event.
func (c *Channel) handleConnected() {
	c.lastQRMu.Lock()
	c.waAuthenticated = true
	c.lastQRB64 = ""
	if c.client.Store.ID != nil {
		c.myJID = *c.client.Store.ID
		c.myLID = c.client.Store.GetLID()
		slog.Info("whatsapp: connected", "jid", c.myJID.String(),
			"lid", c.myLID.String(), "channel", c.Name())
	}
	c.lastQRMu.Unlock()

	c.MarkHealthy("WhatsApp authenticated and connected")
}

// handleDisconnected processes the Disconnected event.
func (c *Channel) handleDisconnected() {
	c.lastQRMu.Lock()
	c.waAuthenticated = false
	c.lastQRMu.Unlock()

	c.MarkDegraded("WhatsApp disconnected", "Waiting for reconnect",
		channels.ChannelFailureKindNetwork, true)
	// whatsmeow auto-reconnects — no manual reconnect loop needed.
}

// handleLoggedOut processes the LoggedOut event.
func (c *Channel) handleLoggedOut(evt *events.LoggedOut) {
	slog.Warn("whatsapp: logged out", "reason", evt.Reason, "channel", c.Name())
	c.lastQRMu.Lock()
	c.waAuthenticated = false
	c.lastQRMu.Unlock()

	c.MarkDegraded("WhatsApp logged out", "Re-scan QR to reconnect",
		channels.ChannelFailureKindAuth, false)
}
