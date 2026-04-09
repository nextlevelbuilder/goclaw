package googlechat

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Compile-time interface assertions.
var _ channels.Channel = (*Channel)(nil)
var _ channels.StreamingChannel = (*Channel)(nil)

// Channel implements channels.Channel, channels.BlockReplyChannel, and
// channels.PendingCompactable for Google Chat via Pub/Sub pull (phase 1).
type Channel struct {
	*channels.BaseChannel

	// Auth
	auth *ServiceAccountAuth

	// Pub/Sub config
	projectID      string
	subscriptionID string
	pullInterval   time.Duration

	// Identity
	botUser string // bot's own user ID to filter self-messages

	// Policies
	dmPolicy       string
	groupPolicy    string
	requireMention bool // require @bot mention in groups

	// Outbound
	apiBase           string // overridable Chat API base (for testing)
	longFormThreshold int
	longFormFormat    string // "md" or "txt"
	drivePermission   string // "domain" or "anyone"
	driveDomain       string // domain for "domain" permission
	blockReply        *bool

	// Media
	mediaMaxBytes     int64
	fileRetentionDays int

	// HTTP client (shared for all API calls)
	httpClient *http.Client

	// State
	dedup        *dedupCache
	threadIDs    sync.Map // spaceID:senderID → threadName
	placeholders sync.Map // chatID → messageName (placeholder for edit)
	groupHistory *channels.PendingHistory
	historyLimit int
	driveFiles   []driveFileRecord
	driveFilesMu sync.Mutex

	// Streaming
	dmStream    bool
	groupStream bool
	streams     sync.Map // chatID → *chatStream

	// Lifecycle
	pullCancel    context.CancelFunc
	pullDone      chan struct{}
	cleanupCancel context.CancelFunc
}

// New creates a new Google Chat channel from config.
func New(cfg config.GoogleChatConfig, msgBus *bus.MessageBus, pendingStore store.PendingMessageStore) (*Channel, error) {
	auth, err := NewServiceAccountAuth(cfg.ServiceAccountFile, []string{scopeChat, scopePubSub, scopeDrive})
	if err != nil {
		return nil, err
	}

	pullInterval := defaultPullInterval
	if cfg.PullIntervalMs > 0 {
		pullInterval = time.Duration(cfg.PullIntervalMs) * time.Millisecond
	}

	longFormThreshold := longFormThresholdDefault
	if cfg.LongFormThreshold > 0 {
		longFormThreshold = cfg.LongFormThreshold
	} else if cfg.LongFormThreshold < 0 {
		longFormThreshold = 0 // disabled
	}

	longFormFormat := "md"
	if cfg.LongFormFormat == "txt" {
		longFormFormat = "txt"
	}

	mediaMaxBytes := int64(defaultMediaMaxMB) * 1024 * 1024
	if cfg.MediaMaxMB > 0 {
		mediaMaxBytes = int64(cfg.MediaMaxMB) * 1024 * 1024
	}

	drivePermission := "domain"
	if cfg.DrivePermission == "anyone" {
		drivePermission = "anyone"
	}
	driveDomain := "vnpay.vn"
	if cfg.DriveDomain != "" {
		driveDomain = cfg.DriveDomain
	}

	dmPolicy := cfg.DMPolicy
	if dmPolicy == "" {
		dmPolicy = "open"
	}
	groupPolicy := cfg.GroupPolicy
	if groupPolicy == "" {
		groupPolicy = "open"
	}

	requireMention := true
	if cfg.RequireMention != nil {
		requireMention = *cfg.RequireMention
	}

	historyLimit := 50
	if cfg.HistoryLimit > 0 {
		historyLimit = cfg.HistoryLimit
	}

	dmStream := true // default: enable streaming for DMs
	if cfg.DMStream != nil {
		dmStream = *cfg.DMStream
	}
	groupStream := false // default: disable streaming for groups
	if cfg.GroupStream != nil {
		groupStream = *cfg.GroupStream
	}

	ch := &Channel{
		BaseChannel:       channels.NewBaseChannel(channels.TypeGoogleChat, msgBus, cfg.AllowFrom),
		auth:              auth,
		projectID:         cfg.ProjectID,
		subscriptionID:    cfg.SubscriptionID,
		pullInterval:      pullInterval,
		botUser:           cfg.BotUser,
		dmPolicy:          dmPolicy,
		groupPolicy:       groupPolicy,
		requireMention:    requireMention,
		apiBase:           chatAPIBase,
		longFormThreshold: longFormThreshold,
		longFormFormat:    longFormFormat,
		drivePermission:   drivePermission,
		driveDomain:       driveDomain,
		blockReply:        cfg.BlockReply,
		mediaMaxBytes:     mediaMaxBytes,
		fileRetentionDays: cfg.FileRetentionDays,
		httpClient:        &http.Client{Timeout: 30 * time.Second},
		dedup:             newDedupCache(dedupTTL),
		historyLimit:      historyLimit,
		dmStream:          dmStream,
		groupStream:       groupStream,
		groupHistory:      channels.MakeHistory("google_chat", pendingStore),
	}

	ch.BaseChannel.SetType(typeGoogleChat)
	ch.BaseChannel.ValidatePolicy(dmPolicy, groupPolicy)

	return ch, nil
}

// Start begins the Pub/Sub pull loop and optional Drive cleanup goroutine.
func (c *Channel) Start(ctx context.Context) error {
	if c.IsRunning() {
		return nil
	}

	pullCtx, cancel := context.WithCancel(ctx)
	c.pullCancel = cancel
	c.pullDone = make(chan struct{})

	go func() {
		defer close(c.pullDone)
		c.startPullLoop(pullCtx)
	}()

	// Start Drive file cleanup goroutine if retention is configured.
	if c.fileRetentionDays > 0 {
		cleanupCtx, cleanupCancel := context.WithCancel(ctx)
		c.cleanupCancel = cleanupCancel
		go c.startDriveCleanupLoop(cleanupCtx)
	}

	c.SetRunning(true)
	slog.Info("googlechat: channel started",
		"name", c.Name(),
		"project", c.projectID,
		"subscription", c.subscriptionID)
	return nil
}

// Stop gracefully shuts down the channel.
func (c *Channel) Stop(ctx context.Context) error {
	if !c.IsRunning() {
		return nil
	}

	c.SetRunning(false)

	// Drain active streams to cancel flush timers and avoid goroutine leaks.
	c.streams.Range(func(key, value any) bool {
		cs := value.(*chatStream)
		cs.stop(ctx)
		c.streams.Delete(key)
		return true
	})

	if c.cleanupCancel != nil {
		c.cleanupCancel()
	}
	if c.pullCancel != nil {
		c.pullCancel()
	}

	// Wait for pull loop to drain (with timeout).
	if c.pullDone != nil {
		select {
		case <-c.pullDone:
		case <-time.After(shutdownDrainTimeout):
			slog.Warn("googlechat: shutdown drain timeout exceeded")
		}
	}

	slog.Info("googlechat: channel stopped", "name", c.Name())
	return nil
}

// SetPendingCompaction implements channels.PendingCompactable.
func (c *Channel) SetPendingCompaction(cfg *channels.CompactionConfig) {
	if c.groupHistory != nil {
		c.groupHistory.SetCompactionConfig(cfg)
	}
}

// BlockReplyEnabled implements channels.BlockReplyChannel.
func (c *Channel) BlockReplyEnabled() *bool {
	return c.blockReply
}
