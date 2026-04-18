package wechat

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	// DefaultBaseURL is the default iLink Bot API endpoint.
	DefaultBaseURL = "https://ilinkai.weixin.qq.com"
	// DefaultCDNBaseURL is the default CDN endpoint.
	DefaultCDNBaseURL = "https://novac2c.cdn.weixin.qq.com/c2c"

	maxConsecutiveFailures = 3
	backoffDelayMs         = 30000
	retryDelayMs           = 2000
)

// Channel implements channels.Channel for WeChat personal via iLink Bot API.
type Channel struct {
	*channels.BaseChannel
	api        *APIClient
	cfg        config.WechatConfig
	cdnBaseURL string
	tokens     *contextTokenStore
	guard      *sessionGuard
	botUserID  string // the bot's own WeChat ID (from ToUserID in inbound messages)

	pollCancel context.CancelFunc
	pollDone   chan struct{}
	handlerWg  sync.WaitGroup
}

// New creates a new WeChat channel.
func New(cfg config.WechatConfig, msgBus *bus.MessageBus, pairingSvc store.PairingStore) (*Channel, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("wechat token is required")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	cdnBaseURL := cfg.CDNBaseURL
	if cdnBaseURL == "" {
		cdnBaseURL = DefaultCDNBaseURL
	}

	base := channels.NewBaseChannel(channels.TypeWeChat, msgBus, cfg.AllowFrom)
	base.ValidatePolicy(cfg.DMPolicy, "")

	api := NewAPIClient(baseURL, cfg.Token)

	ch := &Channel{
		BaseChannel: base,
		api:         api,
		cfg:         cfg,
		cdnBaseURL:  cdnBaseURL,
		tokens:      newContextTokenStore(),
		guard:       newSessionGuard(),
	}
	ch.SetPairingService(pairingSvc)
	return ch, nil
}

// Start begins the long-poll loop for inbound messages.
func (ch *Channel) Start(ctx context.Context) error {
	pollCtx, cancel := context.WithCancel(ctx)
	ch.pollCancel = cancel
	ch.pollDone = make(chan struct{})

	go ch.monitorLoop(pollCtx)

	slog.Info("wechat channel started", "baseUrl", ch.api.BaseURL)
	ch.MarkHealthy(ch.connectedSummary())
	return nil
}

// Stop gracefully shuts down the channel.
func (ch *Channel) Stop(ctx context.Context) error {
	if ch.pollCancel != nil {
		ch.pollCancel()
	}
	if ch.pollDone != nil {
		select {
		case <-ch.pollDone:
		case <-ctx.Done():
		}
	}
	ch.handlerWg.Wait()
	slog.Info("wechat channel stopped")
	return nil
}

// Send delivers an outbound message to a WeChat user.
func (ch *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	to := msg.ChatID
	if to == "" {
		return fmt.Errorf("wechat send: chatID is required")
	}

	contextToken := ch.tokens.Get(ch.Name(), to)

	// Handle media attachments
	if len(msg.Media) > 0 {
		for _, media := range msg.Media {
			mediaURL := media.URL
			if mediaURL == "" {
				continue
			}

			var filePath string
			var isDownloaded bool

			if isLocalFilePath(mediaURL) {
				filePath = resolveLocalPath(mediaURL)
			} else if isRemoteURL(mediaURL) {
				var err error
				filePath, err = downloadRemoteToTemp(ctx, mediaURL)
				if err != nil {
					slog.Error("wechat send: download remote media failed", "url", mediaURL, "error", err)
					continue
				}
				isDownloaded = true
			} else {
				slog.Warn("wechat send: unrecognized media URL scheme", "url", mediaURL)
				continue
			}

			// Use a closure to ensure cleanup of downloaded files after each item
			func(path string, downloaded bool, m bus.MediaAttachment) {
				if downloaded {
					defer os.Remove(path)
				}

				caption := m.Caption
				if caption == "" {
					caption = msg.Content
					msg.Content = "" // Avoid sending text twice
				}

				_, err := sendMediaFile(ctx, ch.api, path, to, caption, contextToken, ch.cdnBaseURL)
				if err != nil {
					slog.Error("wechat send media failed", "to", to, "path", path, "error", err)
				}
			}(filePath, isDownloaded, media)
		}

		// If all media sent and text was consumed by caption, we're done
		if msg.Content == "" {
			return nil
		}
	}

	// Send text message
	if msg.Content != "" {
		_, err := sendTextMessage(ctx, ch.api, to, msg.Content, contextToken)
		if err != nil {
			return fmt.Errorf("wechat sendText: %w", err)
		}
	}

	return nil
}

// monitorLoop is the long-poll getUpdates loop.
func (ch *Channel) monitorLoop(ctx context.Context) {
	defer close(ch.pollDone)

	slog.Info("wechat monitor started", "baseUrl", ch.api.BaseURL)

	var getUpdatesBuf string
	nextTimeoutMs := defaultLongPollTimeoutMs
	consecutiveFailures := 0

	for {
		select {
		case <-ctx.Done():
			slog.Info("wechat monitor stopped (context cancelled)")
			return
		default:
		}

		resp, err := ch.api.GetUpdates(ctx, getUpdatesBuf, nextTimeoutMs)
		if err != nil {
			if ctx.Err() != nil {
				slog.Info("wechat monitor stopped (aborted)")
				return
			}
			consecutiveFailures++
			slog.Error("wechat getUpdates error",
				"consecutive", consecutiveFailures,
				"max", maxConsecutiveFailures,
				"error", err,
			)
			if consecutiveFailures >= maxConsecutiveFailures {
				slog.Error("wechat getUpdates: max failures reached, backing off 30s")
				ch.MarkFailed("Network error", fmt.Sprintf("persistent getUpdates failure: %v", err), channels.ChannelFailureKindNetwork, true)
				consecutiveFailures = 0
				sleepCtx(ctx, backoffDelayMs)
			} else {
				sleepCtx(ctx, retryDelayMs)
			}
			continue
		}

		// Update long-poll timeout if server suggests one
		if resp.LongPollingTimeoutMs > 0 {
			nextTimeoutMs = resp.LongPollingTimeoutMs
		}

		// Check for API errors
		isAPIError := (resp.Ret != 0) || (resp.ErrCode != 0)
		if isAPIError {
			isSessionExpired := resp.ErrCode == SessionExpiredErrCode || resp.Ret == SessionExpiredErrCode
			if isSessionExpired {
				ch.guard.Pause(ch.Name())
				pauseMs := ch.guard.RemainingPauseMs(ch.Name())
				slog.Error("wechat getUpdates: session expired, pausing",
					"errcode", SessionExpiredErrCode,
					"pauseMin", (pauseMs+59999)/60000,
				)
				ch.MarkFailed("Authentication failed", "401 unauthorized: session expired or token invalid", channels.ChannelFailureKindAuth, false)
				consecutiveFailures = 0
				sleepCtx(ctx, int(pauseMs))
				continue
			}

			consecutiveFailures++
			slog.Error("wechat getUpdates failed",
				"ret", resp.Ret,
				"errcode", resp.ErrCode,
				"errmsg", resp.ErrMsg,
				"consecutive", consecutiveFailures,
			)
			if consecutiveFailures >= maxConsecutiveFailures {
				ch.MarkFailed("Network error", fmt.Sprintf("iLink API failure: code=%d msg=%s", resp.ErrCode, resp.ErrMsg), channels.ChannelFailureKindNetwork, true)
				consecutiveFailures = 0
				sleepCtx(ctx, backoffDelayMs)
			} else {
				sleepCtx(ctx, retryDelayMs)
			}
			continue
		}

		if consecutiveFailures > 0 {
			ch.MarkHealthy(ch.connectedSummary())
		}
		consecutiveFailures = 0

		// Update sync buf
		if resp.GetUpdatesBuf != "" {
			getUpdatesBuf = resp.GetUpdatesBuf
		}

		// Process inbound messages
		for _, full := range resp.Msgs {
			slog.Info("wechat inbound message",
				"from", full.FromUserID,
				"items", len(full.ItemList),
			)

			// Store context token for future replies
			if full.ContextToken != "" && full.FromUserID != "" {
				ch.tokens.Set(ch.Name(), full.FromUserID, full.ContextToken)
			}

			// Capture the bot's own WeChat ID from the first inbound message
			if ch.botUserID == "" && full.ToUserID != "" {
				ch.botUserID = full.ToUserID
				ch.MarkHealthy(ch.connectedSummary())
				slog.Info("wechat bot identity captured", "botUserID", ch.botUserID)
			}

			// Convert to bus message and publish
			inbound := weixinMessageToInbound(&full, ch.Name())
			inbound.AgentID = ch.AgentID()
			inbound.TenantID = ch.TenantID()

			// Download and attach media
			mediaFiles := ch.downloadMedia(ctx, &full)
			inbound.Media = mediaFiles
			var tmpPaths []string
			for _, m := range mediaFiles {
				tmpPaths = append(tmpPaths, m.Path)
			}
			if len(tmpPaths) > 0 {
				scheduleMediaCleanup(tmpPaths, 5*time.Minute)
			}

			// Check DM Policy (Pairing, Allowlist, or Open)
			if inbound.PeerKind == "direct" {
				if !ch.checkDMPolicy(ctx, inbound.SenderID, inbound.ChatID) {
					continue
				}
			}

			// Check allowlist (Secondary defense)
			if !ch.IsAllowed(inbound.SenderID) {
				slog.Debug("wechat: sender not in allowlist, skipping", "sender", inbound.SenderID)
				continue
			}

			ch.Bus().PublishInbound(inbound)
		}
	}
}

// connectedSummary returns a health summary string.
// Unlike Telegram (which has getMe for bot username), WeChat has no such API.
// We show the channel name + bot WeChat ID once captured from an inbound message.
func (ch *Channel) connectedSummary() string {
	if ch.botUserID != "" {
		return fmt.Sprintf("Connected as %s (%s)", ch.Name(), ch.botUserID)
	}
	name := ch.Name()
	if name != "" {
		return fmt.Sprintf("Connected as %s", name)
	}
	return "Connected"
}

func sleepCtx(ctx context.Context, ms int) {
	timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func isLocalFilePath(mediaURL string) bool {
	return !strings.Contains(mediaURL, "://")
}

func isRemoteURL(mediaURL string) bool {
	return strings.HasPrefix(mediaURL, "http://") || strings.HasPrefix(mediaURL, "https://")
}

func resolveLocalPath(mediaURL string) string {
	if strings.HasPrefix(mediaURL, "file://") {
		return strings.TrimPrefix(mediaURL, "file://")
	}
	if filepath.IsAbs(mediaURL) {
		return mediaURL
	}
	abs, err := filepath.Abs(mediaURL)
	if err != nil {
		return mediaURL
	}
	return abs
}
