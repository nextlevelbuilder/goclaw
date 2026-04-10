package whatsapp

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	wastore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

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
	wastore.DeviceProps.Os = proto.String("GoClaw")
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
	parentCtx context.Context // stored from Start() for Reauth() context chain

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
func New(cfg config.WhatsAppConfig, msgBus *bus.MessageBus,
	pairingSvc store.PairingStore, db *sql.DB,
	pendingStore store.PendingMessageStore, dialect string) (*Channel, error) {

	base := channels.NewBaseChannel(channels.TypeWhatsApp, msgBus, cfg.AllowFrom)
	base.ValidatePolicy(cfg.DMPolicy, cfg.GroupPolicy)

	container := sqlstore.NewWithDB(db, dialect, nil)
	if err := container.Upgrade(context.Background()); err != nil {
		return nil, fmt.Errorf("whatsapp sqlstore upgrade: %w", err)
	}

	ch := &Channel{
		BaseChannel: base,
		config:      cfg,
		container:   container,
	}
	ch.SetPairingService(pairingSvc)
	ch.SetGroupHistory(channels.MakeHistory("whatsapp", pendingStore, base.TenantID()))
	return ch, nil
}

// Start initializes the whatsmeow client and connects to WhatsApp.
func (c *Channel) Start(ctx context.Context) error {
	slog.Info("starting whatsapp channel (whatsmeow)")
	c.MarkStarting("Initializing WhatsApp connection")

	c.parentCtx = ctx
	c.ctx, c.cancel = context.WithCancel(ctx)

	deviceStore, err := c.container.GetFirstDevice(ctx)
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
		slog.Info("whatsapp: pair success", "channel", c.Name())
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
