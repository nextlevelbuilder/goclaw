package oa

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ErrPartialSend signals that an attachment was delivered but the trailing
// caption/text message failed. Callers may use errors.Is to special-case retry.
var ErrPartialSend = errors.New("zalo_oa: attachment delivered but trailing text failed")

const (
	defaultClientTimeout        = 15 * time.Second
	defaultSafetyTickerInterval = 30 * time.Minute
	// reauthWarningWindow: surface "re-consent due soon" once the refresh
	// token's remaining lifetime drops to or below this window.
	reauthWarningWindow = 14 * 24 * time.Hour
)

// Channel is the Zalo OA channel. Upload caps enforced by Zalo (error -210):
// image 1MB, file 5MB, gif 5MB.
type Channel struct {
	*channels.BaseChannel

	client  *Client
	ciStore store.ChannelInstanceStore
	cfg     config.ZaloOAConfig

	instanceID uuid.UUID

	tokens *tokenSource

	cursor       *pollCursor
	seenIDs      *seenMessageIDs // dedup fallback for messages with time == 0
	pollInterval time.Duration
	pollWG       sync.WaitGroup

	safetyTickerInterval time.Duration

	stopOnce  sync.Once
	stopCh    chan struct{}
	tickerWG  sync.WaitGroup
	catchUpWG sync.WaitGroup

	webhookRouter *common.Router
	resolvedSlug  string // resolved slug stored at Start; surfaced to RPC

	bootstrapDroppedCount atomic.Int64

	reactions       sync.Map // key: "<userID>:<sourceMessageID>" → *zaloReactionController
	reactionWG      sync.WaitGroup
	reactionCtx     context.Context
	reactionCancel  context.CancelFunc
	lastReplyChars  sync.Map // key: chatID → int (latest reply char count, used to scale terminal-reaction delay)

	// downloadMediaFn lets tests inject a fixture writer that bypasses SSRF
	// on httptest loopback URLs. nil → downloadOAMedia.
	downloadMediaFn func(ctx context.Context, fileURL string) (string, error)
}

// creds returns a read-only snapshot. Refresh swaps the pointer atomically;
// callers must not mutate the returned struct.
func (c *Channel) creds() *ChannelCreds {
	return c.tokens.Snapshot()
}

// inBootstrap: webhook + signature-enforcing + no secret yet. Acks Zalo's
// URL-save ping so the operator can register the URL and retrieve the OA
// Secret Key from the dev console.
func (c *Channel) inBootstrap() bool {
	return c.creds().WebhookSecretKey == "" &&
		normalizeMode(c.cfg.WebhookSignatureMode) != SignatureModeDisabled
}

// New constructs the channel. InstanceLoader calls SetInstanceID after.
func New(name string, cfg config.ZaloOAConfig, creds *ChannelCreds,
	ciStore store.ChannelInstanceStore, msgBus *bus.MessageBus, _ store.PairingStore) (*Channel, error) {

	if creds == nil {
		return nil, errors.New("zalo_oa: nil creds")
	}
	if creds.AppID == "" || creds.SecretKey == "" {
		return nil, errors.New("zalo_oa: app_id and secret_key are required")
	}

	if cfg.Transport == "" {
		cfg.Transport = "webhook"
	}

	c := &Channel{
		BaseChannel:          channels.NewBaseChannel(name, msgBus, []string(cfg.AllowFrom)),
		client:               NewClient(defaultClientTimeout),
		ciStore:              ciStore,
		cfg:                  cfg,
		cursor:               newPollCursor(defaultCursorMaxEntries),
		seenIDs:              newSeenMessageIDs(0),
		pollInterval:         pollIntervalFromCfg(cfg.PollIntervalSeconds),
		safetyTickerInterval: tickerInterval(cfg.SafetyTickerMinutes),
		stopCh:               make(chan struct{}),
		webhookRouter:        common.SharedRouter(),
	}
	c.tokens = &tokenSource{
		client: c.client,
		store:  c.ciStore,
	}
	c.tokens.creds.Store(creds)
	c.reactionCtx, c.reactionCancel = context.WithCancel(context.Background())
	return c, nil
}

func (c *Channel) SetInstanceID(id uuid.UUID) {
	c.instanceID = id
	c.tokens.instanceID = id
}

// SetTestEndpointsForTest overrides the OAuth + API hosts for integration tests.
func (c *Channel) SetTestEndpointsForTest(oauthBase, apiBase string) {
	if oauthBase != "" {
		c.client.oauthBase = oauthBase
	}
	if apiBase != "" {
		c.client.apiBase = apiBase
	}
}

// ForceRefreshForTest exposes tokenSource.ForceRefresh for integration tests.
func (c *Channel) ForceRefreshForTest() {
	c.tokens.ForceRefresh()
}

func (c *Channel) Type() string { return channels.TypeZaloOA }

// QuoteInboundOnDM gates auto-stamping of metadata["reply_to_message_id"]
// upstream. Default off. Explicit metadata from callers (e.g. agent tools)
// is still honored in Send regardless.
func (c *Channel) QuoteInboundOnDM() bool {
	if c.cfg.QuoteUserMessage == nil {
		return false
	}
	return *c.cfg.QuoteUserMessage
}

var _ channels.WebhookChannel = (*Channel)(nil)
var _ channels.DMQuoteChannel = (*Channel)(nil)
var _ channels.ReactionChannel = (*Channel)(nil)

// WebhookHandler returns (path, handler) on the first caller across the
// shared router; subsequent calls return ("", nil). Per-instance dispatch
// uses the slug suffix of the path: /channels/zalo/webhook/<slug>.
func (c *Channel) WebhookHandler() (string, http.Handler) {
	return common.SharedRouter().MountRoute()
}

// ResolvedWebhookSlug returns the slug the channel registered with the shared
// router (empty if not yet started or polling mode).
func (c *Channel) ResolvedWebhookSlug() string { return c.resolvedSlug }

// Start brings the channel up. Safety ticker always runs. Transport
// "webhook" (default) registers with the shared router and optionally fires
// a catch-up sweep; "polling" starts the listrecentchat poll loop.
func (c *Channel) Start(_ context.Context) error {
	c.SetRunning(true)
	if c.creds().OAID == "" {
		slog.Info("zalo_oa.started", "state", "unauthorized", "name", c.Name())
		c.MarkDegraded("awaiting consent", "no oa_id yet — paste consent code to authorize",
			channels.ChannelFailureKindAuth, true)
		// Pre-consent: only run safety ticker; nothing to poll or receive.
		c.tickerWG.Add(1)
		go c.runSafetyTicker()
		return nil
	}

	c.tickerWG.Add(1)
	go c.runSafetyTicker()

	transport := c.cfg.Transport
	switch transport {
	case "webhook":
		return c.startWebhookTransport()
	case "polling":
		c.pollWG.Add(1)
		// Background ctx so the loop survives the caller's ctx cancel; Stop()
		// is the canonical exit signal. Each cycle uses its own per-tick ctx.
		go c.runPollLoop(context.Background())
		slog.Info("zalo_oa.started", "state", "connected", "oa_id", c.creds().OAID, "transport", "polling", "name", c.Name())
		c.MarkHealthy("connected")
	default:
		c.MarkFailed("unknown transport",
			fmt.Sprintf("unknown transport %q (expected polling|webhook)", transport),
			channels.ChannelFailureKindConfig, false)
		return fmt.Errorf("zalo_oa: unknown transport %q", transport)
	}
	return nil
}

// Stop signals ticker, poll loop, and any in-flight catch-up sweep to
// exit and waits. Webhook teardown unregisters from the shared router.
// Idempotent.
func (c *Channel) Stop(_ context.Context) error {
	c.stopOnce.Do(func() { close(c.stopCh) })
	if c.cfg.Transport == "webhook" && c.webhookRouter != nil {
		c.webhookRouter.UnregisterInstance(c.instanceID)
	}
	// Cancel reaction debounce timers + any in-flight HTTP call before Wait.
	c.reactions.Range(func(_, v any) bool {
		if rc, ok := v.(*zaloReactionController); ok {
			rc.Stop()
		}
		return true
	})
	if c.reactionCancel != nil {
		c.reactionCancel()
	}
	c.reactionWG.Wait()
	c.catchUpWG.Wait()
	c.tickerWG.Wait()
	c.pollWG.Wait()
	c.SetRunning(false)
	slog.Info("zalo_oa.stopped", "name", c.Name())
	return nil
}

// Send dispatches text / image / file based on the Media slice. Zalo OA
// sends one attachment per message; extra Media entries are skipped.
// Caption + Content ride as a separate trailing text message (Zalo OA's
// attachment payload has no caption field). Returns ErrPartialSend if
// the attachment succeeded but the trailing text failed.
func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if msg.ChatID == "" {
		return errors.New("zalo_oa: empty user_id")
	}

	msg.Content = common.StripMarkdown(msg.Content)
	if len(msg.Media) > 0 {
		msg.Media = slices.Clone(msg.Media)
		for i := range msg.Media {
			msg.Media[i].Caption = common.StripMarkdown(msg.Media[i].Caption)
		}
	}

	quoteID := msg.Metadata["reply_to_message_id"]
	if len(msg.Media) == 0 {
		_, err := c.SendText(ctx, msg.ChatID, msg.Content, quoteID)
		if err == nil {
			c.recordReplyLen(msg.ChatID, len(msg.Content))
		}
		return err
	}
	if len(msg.Media) > 1 {
		slog.Info("zalo_oa.send.extra_media_skipped",
			"oa_id", c.creds().OAID, "extra", len(msg.Media)-1)
	}

	m := msg.Media[0]
	// 50MB stat-first guard prevents OOM; per-type caps enforced below.
	data, mt, err := c.readMedia(m, 50*1024*1024)
	if err != nil {
		return err
	}

	var attachMID string
	if mt == "image/gif" {
		// Dedicated /upload/gif endpoint (5MB cap) preserves animation.
		const zaloGIFCapBytes = 5 * 1024 * 1024
		if len(data) > zaloGIFCapBytes {
			return fmt.Errorf("zalo_oa: gif too large: %d bytes (Zalo cap is 5MB)", len(data))
		}
		attachMID, err = c.SendGIF(ctx, msg.ChatID, data)
	} else if strings.HasPrefix(mt, "image/") {
		// /upload/image caps at 1MB, jpg/png only. Auto-compress to JPEG.
		const zaloImageCapBytes = 1 * 1024 * 1024
		compressed, newMT, cerr := common.CompressImage(data, mt, zaloImageCapBytes)
		if cerr != nil {
			return cerr
		}
		data, mt = compressed, newMT
		attachMID, err = c.SendImage(ctx, msg.ChatID, data, mt)
	} else {
		// /upload/file accepts PDF/DOC/DOCX up to 5MB.
		const zaloFileCapBytes = 5 * 1024 * 1024
		if !isZaloSupportedFileMIME(mt) {
			// Drop unsupported attachment, deliver trailing text + note.
			// Avoids surfacing a hard error to the dispatcher.
			slog.Warn("zalo_oa.send.unsupported_attachment_dropped",
				"oa_id", c.creds().OAID, "mime", mt, "filename", filepath.Base(m.URL))
			fallback := mergeTrailingText(m.Caption, msg.Content)
			heads := i18n.T(store.LocaleFromContext(ctx), i18n.MsgZaloOAUnsupportedAttachment,
				filepath.Base(m.URL), mt)
			if fallback == "" {
				fallback = heads
			} else {
				fallback = fallback + "\n\n" + heads
			}
			_, terr := c.SendText(ctx, msg.ChatID, fallback, "")
			return terr
		}
		if len(data) > zaloFileCapBytes {
			return fmt.Errorf("zalo_oa: file too large: %d bytes (Zalo cap is 5MB)", len(data))
		}
		attachMID, err = c.SendFile(ctx, msg.ChatID, data, filepath.Base(m.URL))
	}
	if err != nil {
		return err
	}

	trailing := mergeTrailingText(m.Caption, msg.Content)
	if trailing == "" {
		return nil
	}
	if _, terr := c.SendText(ctx, msg.ChatID, trailing, ""); terr != nil {
		slog.Error("zalo_oa.send.text_after_attachment_failed",
			"oa_id", c.creds().OAID, "user_id", msg.ChatID,
			"attachment_message_id", attachMID, "error", terr)
		return fmt.Errorf("%w: %v", ErrPartialSend, terr)
	}
	c.recordReplyLen(msg.ChatID, len(trailing))
	return nil
}

// mergeTrailingText joins caption + content for the post-attachment text.
// Both present → joined with a blank line.
func mergeTrailingText(caption, content string) string {
	caption = strings.TrimSpace(caption)
	content = strings.TrimSpace(content)
	switch {
	case caption == "" && content == "":
		return ""
	case caption == "":
		return content
	case content == "":
		return caption
	default:
		return caption + "\n\n" + content
	}
}

// readMedia stat-checks before allocating to bound memory on large paths.
func (c *Channel) readMedia(m bus.MediaAttachment, maxBytes int64) ([]byte, string, error) {
	if m.URL == "" {
		return nil, "", errors.New("zalo_oa: media URL empty")
	}
	if maxBytes > 0 {
		info, statErr := os.Stat(m.URL)
		if statErr == nil && info.Size() > maxBytes {
			return nil, "", fmt.Errorf("zalo_oa: media too large: %d bytes (local cap %d; Zalo OA hard-caps uploads at 1MB via error -210)", info.Size(), maxBytes)
		}
	}
	data, err := os.ReadFile(m.URL)
	if err != nil {
		return nil, "", fmt.Errorf("zalo_oa: read media %s: %w", m.URL, err)
	}
	mt := m.ContentType
	if mt == "" {
		mt = mime.TypeByExtension(strings.ToLower(filepath.Ext(m.URL)))
		if mt == "" {
			mt = "application/octet-stream"
		}
	}
	return data, mt, nil
}

// runSafetyTicker calls Access() periodically so idle channels don't
// let the refresh-token rotation window lapse silently.
func (c *Channel) runSafetyTicker() {
	defer c.tickerWG.Done()

	t := time.NewTicker(c.safetyTickerInterval)
	defer t.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-t.C:
			if c.skipTickIfAuthFailed() {
				continue
			}
			// TenantID propagated so downstream listeners scoped by tenant
			// see the right scope.
			ctx, cancel := context.WithTimeout(store.WithTenantID(context.Background(), c.TenantID()), 30*time.Second)
			if _, err := c.tokens.Access(ctx); err != nil && !errors.Is(err, ErrNotAuthorized) {
				c.markAuthFailedIfNeeded(err)
				slog.Warn("zalo_oa.safety_tick_refresh_failed", "instance_id", c.instanceID, "error", err)
			} else {
				c.evaluateReauthWarning()
			}
			cancel()
		}
	}
}

func (c *Channel) skipTickIfAuthFailed() bool {
	snap := c.HealthSnapshot()
	return snap.State == channels.ChannelHealthStateFailed && snap.FailureKind == channels.ChannelFailureKindAuth
}

// markAuthFailedIfNeeded transitions health to Failed/Auth on:
//   - ErrAuthExpired: refresh token rejected (refresh-token dead).
//   - *APIError isAuth(): access_token rejected after the retry-once
//     ForceRefresh attempt (OA-app association broken; operator must re-consent).
//
// ErrNotAuthorized (pre-consent) is NOT escalated.
func (c *Channel) markAuthFailedIfNeeded(err error) {
	if err == nil {
		return
	}
	if errors.Is(err, ErrAuthExpired) {
		var apiErr *APIError
		var code int
		var msg string
		if errors.As(err, &apiErr) {
			code, msg = apiErr.Code, apiErr.Message
		}
		c.MarkFailed("Re-auth required",
			i18n.T(i18n.DefaultLocale, i18n.MsgZaloOAErrRefreshExpired, code, msg),
			channels.ChannelFailureKindAuth,
			false,
		)
		return
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.isAuth() {
		c.MarkFailed("Re-auth required",
			i18n.T(i18n.DefaultLocale, i18n.MsgZaloOAErrAuth, apiErr.Code, apiErr.Message),
			channels.ChannelFailureKindAuth,
			false,
		)
	}
}

// evaluateReauthWarning transitions Healthy <-> Degraded(warn) based on how
// close RefreshTokenExpiresAt is. Called after each successful safety-tick
// refresh. Failed states are left alone (Failed wins over warning); legacy
// channels with zero RefreshTokenExpiresAt stay silent. Logs only on
// transitions to avoid 30-minute log spam inside the warning window.
func (c *Channel) evaluateReauthWarning() {
	exp := c.creds().RefreshTokenExpiresAt
	if exp.IsZero() {
		return
	}
	remaining := time.Until(exp)
	if remaining <= 0 {
		return // imminent failure — let the Auth path surface it
	}
	snap := c.HealthSnapshot()
	if snap.State == channels.ChannelHealthStateFailed {
		return
	}

	inWindow := remaining <= reauthWarningWindow
	isWarning := snap.State == channels.ChannelHealthStateDegraded &&
		snap.FailureKind == channels.ChannelFailureKindAuth &&
		snap.Retryable

	switch {
	case inWindow && snap.State == channels.ChannelHealthStateHealthy:
		days := int(remaining.Hours()/24) + 1 // round up; 0.5d → "1 day"
		c.MarkDegraded(
			"Re-consent due soon",
			i18n.T(i18n.DefaultLocale, i18n.MsgZaloOAReauthDueSoon, days),
			channels.ChannelFailureKindAuth,
			true,
		)
		slog.Info("zalo_oa.reauth_warning",
			"instance_id", c.instanceID,
			"days_remaining", days,
			"expires_at", exp,
		)
	case !inWindow && isWarning:
		c.MarkHealthy("connected")
		slog.Info("zalo_oa.reauth_warning_cleared",
			"instance_id", c.instanceID,
			"expires_at", exp,
		)
	}
}

func tickerInterval(cfgMinutes int) time.Duration {
	switch {
	case cfgMinutes < 5:
		return defaultSafetyTickerInterval
	case cfgMinutes > 120:
		return 120 * time.Minute
	default:
		return time.Duration(cfgMinutes) * time.Minute
	}
}
