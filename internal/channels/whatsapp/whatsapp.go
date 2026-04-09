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
	client          *whatsmeow.Client
	container       *sqlstore.Container
	config          config.WhatsAppConfig
	mu              sync.Mutex
	ctx             context.Context
	cancel          context.CancelFunc
	parentCtx       context.Context // stored from Start() for Reauth() context chain
	pairingService  store.PairingStore
	pairingDebounce sync.Map                 // senderID → time.Time
	approvedGroups  sync.Map                 // chatID → true (in-memory cache for paired groups)
	groupHistory    *channels.PendingHistory // tracks group messages for context

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

	// cachedGroups stores groups discovered via GetJoinedGroups().
	groupsMu     sync.RWMutex
	cachedGroups []types.GroupInfo
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

	return &Channel{
		BaseChannel:    base,
		config:         cfg,
		pairingService: pairingSvc,
		container:      container,
		groupHistory:   channels.MakeHistory("whatsapp", pendingStore, base.TenantID()),
	}, nil
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
	case *events.JoinedGroup:
		slog.Info("whatsapp: joined group, refreshing cache", "jid", v.JID.String(), "name", v.Name, "channel", c.Name())
		go c.refreshCachedGroups()
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

	// Cache joined groups for discovery API.
	go c.refreshCachedGroups()

	c.MarkHealthy("WhatsApp authenticated and connected")
}

// refreshCachedGroups fetches the list of groups the linked account has joined.
func (c *Channel) refreshCachedGroups() {
	if c.client == nil || !c.client.IsConnected() {
		return
	}
	groups, err := c.client.GetJoinedGroups(context.Background())
	if err != nil {
		slog.Warn("whatsapp: failed to fetch joined groups", "error", err, "channel", c.Name())
		return
	}
	c.groupsMu.Lock()
	// Convert []*types.GroupInfo to []types.GroupInfo for storage.
	c.cachedGroups = make([]types.GroupInfo, len(groups))
	for i, g := range groups {
		if g != nil {
			c.cachedGroups[i] = *g
		}
	}
	c.groupsMu.Unlock()
	slog.Info("whatsapp: cached joined groups", "count", len(groups), "channel", c.Name())
}

// GetCachedGroups returns the cached list of joined WhatsApp groups (thread-safe).
func (c *Channel) GetCachedGroups() []types.GroupInfo {
	c.groupsMu.RLock()
	defer c.groupsMu.RUnlock()
	return c.cachedGroups
}

// WAGroupDiscovery is a simplified group info struct for cross-package use.
type WAGroupDiscovery struct {
	JID         string
	Name        string
	MemberCount int
}

// GetCachedGroupsRaw returns cached groups as simplified structs (avoids whatsmeow type dependency).
func (c *Channel) GetCachedGroupsRaw() []WAGroupDiscovery {
	c.groupsMu.RLock()
	defer c.groupsMu.RUnlock()
	result := make([]WAGroupDiscovery, len(c.cachedGroups))
	for i, g := range c.cachedGroups {
		result[i] = WAGroupDiscovery{
			JID:         g.JID.String(),
			Name:        g.Name,
			MemberCount: g.ParticipantCount,
		}
	}
	return result
}

// RefreshGroups triggers an on-demand refresh of the cached groups list.
// This is called by the HTTP API when the user requests a group list refresh.
func (c *Channel) RefreshGroups() {
	go c.refreshCachedGroups()
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
