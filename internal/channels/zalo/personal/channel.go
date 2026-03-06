package personal

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/typing"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	maxTextLength         = 2000
	maxChannelRestarts    = 10
	maxChannelBackoff     = 60 * time.Second
	code3000InitialDelay  = 60 * time.Second
)

// Channel connects to Zalo Personal Chat via the internal protocol port (from zcago, MIT).
// WARNING: Zalo Personal is an unofficial, reverse-engineered integration. Account may be locked/banned.
type Channel struct {
	*channels.BaseChannel
	config          config.ZaloPersonalConfig
	pairingService  store.PairingStore
	pairingDebounce sync.Map // senderID -> time.Time
	approvedGroups  sync.Map // groupID → true (in-memory cache for paired groups)
	typingCtrls     sync.Map // threadID → *typing.Controller

	mu       sync.RWMutex // protects sess and listener
	sess     *protocol.Session
	listener *protocol.Listener

	// Pre-loaded credentials (from DB or from file/QR as fallback).
	preloadedCreds *protocol.Credentials

	requireMention bool
	stopCh         chan struct{}
	stopOnce       sync.Once
}

// New creates a new Zalo Personal channel from config.
func New(cfg config.ZaloPersonalConfig, msgBus *bus.MessageBus, pairingSvc store.PairingStore) (*Channel, error) {
	base := channels.NewBaseChannel("zalo_personal", msgBus, cfg.AllowFrom)

	if cfg.DMPolicy == "" {
		cfg.DMPolicy = "allowlist"
	}
	if cfg.GroupPolicy == "" {
		cfg.GroupPolicy = "allowlist"
	}
	base.ValidatePolicy(cfg.DMPolicy, cfg.GroupPolicy)

	requireMention := true
	if cfg.RequireMention != nil {
		requireMention = *cfg.RequireMention
	}

	return &Channel{
		BaseChannel:    base,
		config:         cfg,
		pairingService: pairingSvc,
		requireMention: requireMention,
		stopCh:         make(chan struct{}),
	}, nil
}

// session returns the current session snapshot (thread-safe).
func (c *Channel) session() *protocol.Session {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sess
}

// getListener returns the current listener snapshot (thread-safe).
func (c *Channel) getListener() *protocol.Listener {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.listener
}

// Start authenticates and begins listening for Zalo messages.
func (c *Channel) Start(ctx context.Context) error {
	slog.Warn("security.unofficial_api",
		"channel", "zalo_personal",
		"msg", "Zalo Personal is unofficial and reverse-engineered. Account may be locked/banned. Use at own risk.",
	)

	sess, err := c.authenticate(ctx)
	if err != nil {
		return fmt.Errorf("zalo_personal auth: %w", err)
	}

	ln, err := protocol.NewListener(sess)
	if err != nil {
		return fmt.Errorf("zalo_personal listener: %w", err)
	}
	if err := ln.Start(ctx); err != nil {
		return fmt.Errorf("zalo_personal listener start: %w", err)
	}

	c.mu.Lock()
	c.sess = sess
	c.listener = ln
	c.mu.Unlock()

	slog.Info("zalo_personal connected", "uid", sess.UID)

	c.SetRunning(true)
	go c.listenLoop(ctx)

	slog.Info("zalo_personal listener loop started")
	return nil
}

// Stop gracefully shuts down the Zalo Personal channel.
func (c *Channel) Stop(_ context.Context) error {
	slog.Info("stopping zalo_personal channel")
	c.stopOnce.Do(func() { close(c.stopCh) })
	c.typingCtrls.Range(func(key, val any) bool {
		val.(*typing.Controller).Stop()
		c.typingCtrls.Delete(key)
		return true
	})
	if ln := c.getListener(); ln != nil {
		ln.Stop()
	}
	c.SetRunning(false)
	return nil
}

// Send delivers an outbound message to a Zalo chat.
func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	sess := c.session()
	if !c.IsRunning() || sess == nil {
		return fmt.Errorf("zalo_personal channel not running")
	}

	// Stop typing indicator before sending response
	if ctrl, ok := c.typingCtrls.LoadAndDelete(msg.ChatID); ok {
		ctrl.(*typing.Controller).Stop()
	}

	threadType := protocol.ThreadTypeUser
	if msg.Metadata != nil {
		if _, ok := msg.Metadata["group_id"]; ok {
			threadType = protocol.ThreadTypeGroup
		}
	}

	return c.sendChunkedText(ctx, sess, msg.ChatID, threadType, msg.Content)
}

func (c *Channel) listenLoop(ctx context.Context) {
	defer c.SetRunning(false)
	for {
		if !c.runListenerLoop(ctx) {
			return
		}
	}
}

// runListenerLoop reads from the current listener until it closes.
// Returns true if the channel restarted and the outer loop should continue,
// false if the channel should stop permanently.
func (c *Channel) runListenerLoop(ctx context.Context) bool {
	ln := c.getListener()
	if ln == nil {
		return false
	}
	for {
		select {
		case <-ctx.Done():
			slog.Info("zalo_personal listener loop stopped (context)")
			return false
		case <-c.stopCh:
			slog.Info("zalo_personal listener loop stopped")
			return false

		case msg, ok := <-ln.Messages():
			if !ok {
				return false
			}
			c.handleMessage(msg)

		case ci := <-ln.Disconnected():
			slog.Warn("zalo_personal disconnected", "code", ci.Code, "reason", ci.Reason)

		case ci := <-ln.Closed():
			slog.Warn("zalo_personal connection closed", "code", ci.Code, "reason", ci.Reason)

			// Code 3000: wait 60s before retry (duplicate session may be transient)
			if ci.Code == protocol.CloseCodeDuplicate {
				slog.Warn("zalo_personal duplicate session (code 3000), waiting before retry", "channel", c.Name())
				select {
				case <-ctx.Done():
					return false
				case <-c.stopCh:
					return false
				case <-time.After(code3000InitialDelay):
				}
			}

			return c.restartWithBackoff(ctx)

		case err := <-ln.Errors():
			slog.Warn("zalo_personal listener error", "error", err)
		}
	}
}

// restartWithBackoff attempts to restart the channel with exponential backoff.
// This is the channel-level retry layer for auth/session failures. The listener
// has its own internal retry for transient WS disconnects (endpoint rotation).
// When the listener exhausts its retries, it emits to closedCh which triggers this.
// Returns true if restart succeeded and the listen loop should continue.
func (c *Channel) restartWithBackoff(ctx context.Context) bool {
	for attempt := range maxChannelRestarts {
		delay := min(time.Duration(1<<uint(attempt+1))*time.Second, maxChannelBackoff)
		slog.Info("zalo_personal restarting channel", "attempt", attempt+1, "delay", delay, "channel", c.Name())

		select {
		case <-ctx.Done():
			return false
		case <-c.stopCh:
			return false
		case <-time.After(delay):
		}

		if err := c.restart(ctx); err != nil {
			slog.Warn("zalo_personal restart failed", "attempt", attempt+1, "error", err)
			continue
		}
		return true
	}
	slog.Error("zalo_personal channel gave up after max restart attempts", "channel", c.Name())
	return false
}

// restart performs a full re-authentication and listener restart.
// Called from the listenLoop goroutine when the listener exhausts retries.
func (c *Channel) restart(ctx context.Context) error {
	if ln := c.getListener(); ln != nil {
		ln.Stop()
	}

	sess, err := c.authenticate(ctx)
	if err != nil {
		return fmt.Errorf("re-auth: %w", err)
	}

	ln, err := protocol.NewListener(sess)
	if err != nil {
		return fmt.Errorf("new listener: %w", err)
	}
	if err := ln.Start(ctx); err != nil {
		return fmt.Errorf("start listener: %w", err)
	}

	c.mu.Lock()
	c.sess = sess
	c.listener = ln
	c.mu.Unlock()
	return nil
}

func (c *Channel) handleMessage(msg protocol.Message) {
	if msg.IsSelf() {
		return
	}

	switch m := msg.(type) {
	case protocol.UserMessage:
		c.handleDM(m)
	case protocol.GroupMessage:
		c.handleGroupMessage(m)
	}
}

func (c *Channel) handleDM(msg protocol.UserMessage) {
	senderID := msg.Data.UIDFrom
	threadID := msg.ThreadID()

	content, media := extractContentAndMedia(msg.Data.Content)
	if content == "" {
		return
	}

	if !c.checkDMPolicy(senderID, threadID) {
		return
	}

	slog.Debug("zalo_personal DM received",
		"sender", senderID,
		"thread", threadID,
		"preview", channels.Truncate(content, 50),
	)

	c.startTyping(threadID, protocol.ThreadTypeUser)

	metadata := map[string]string{
		"message_id": msg.Data.MsgID,
		"platform":   "zalo_personal",
	}
	c.HandleMessage(senderID, threadID, content, media, metadata, "direct")
}

func (c *Channel) handleGroupMessage(msg protocol.GroupMessage) {
	senderID := msg.Data.UIDFrom
	threadID := msg.ThreadID()

	content, media := extractContentAndMedia(msg.Data.Content)
	if content == "" {
		return
	}

	if !c.checkGroupPolicy(senderID, threadID, msg.Data.Mentions) {
		return
	}

	slog.Debug("zalo_personal group message received",
		"sender", senderID,
		"group", threadID,
		"preview", channels.Truncate(content, 50),
	)

	c.startTyping(threadID, protocol.ThreadTypeGroup)

	metadata := map[string]string{
		"message_id": msg.Data.MsgID,
		"platform":   "zalo_personal",
		"group_id":   threadID,
	}
	c.HandleMessage(senderID, threadID, content, media, metadata, "group")
}

// startTyping starts a typing indicator with keepalive for the given thread.
// Zalo typing expires after ~5s, so keepalive fires every 3s.
func (c *Channel) startTyping(threadID string, threadType protocol.ThreadType) {
	sess := c.session()
	ctrl := typing.New(typing.Options{
		MaxDuration:       60 * time.Second,
		KeepaliveInterval: 4 * time.Second,
		StartFn: func() error {
			return protocol.SendTypingEvent(context.Background(), sess, threadID, threadType)
		},
	})
	if prev, ok := c.typingCtrls.Load(threadID); ok {
		prev.(*typing.Controller).Stop()
	}
	c.typingCtrls.Store(threadID, ctrl)
	ctrl.Start()
}

// extractContentAndMedia returns text content and optional local media paths from a message.
// For text messages, media is nil. For image attachments, the image is downloaded to a temp file.
func extractContentAndMedia(content protocol.Content) (string, []string) {
	if text := content.Text(); text != "" {
		return text, nil
	}
	att := content.ParseAttachment()
	if att == nil {
		return "", nil
	}
	text := content.AttachmentText()
	if text == "" {
		return "", nil
	}
	var media []string
	if att.IsImage() {
		if path, err := downloadFile(context.Background(), att.Href); err != nil {
			slog.Warn("zalo_personal: failed to download image", "url", att.Href, "error", err)
		} else {
			media = []string{path}
		}
	}
	return text, media
}

const maxImageBytes = 10 * 1024 * 1024 // 10MB

// downloadFile downloads an image URL to a temp file and returns the local path.
// Validates against SSRF and enforces HTTPS-only, timeout, and size limits.
func downloadFile(ctx context.Context, imageURL string) (string, error) {
	if err := tools.CheckSSRF(imageURL); err != nil {
		return "", fmt.Errorf("ssrf check: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download status %d", resp.StatusCode)
	}

	// Strip query params before extracting extension
	path := imageURL
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	ext := filepath.Ext(path)
	if ext == "" || len(ext) > 5 {
		ext = ".jpg"
	}

	tmpFile, err := os.CreateTemp("", "goclaw_zca_*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	defer tmpFile.Close()

	written, err := io.Copy(tmpFile, io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("save: %w", err)
	}
	if written > maxImageBytes {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("image too large: %d bytes", written)
	}

	return tmpFile.Name(), nil
}

func (c *Channel) sendChunkedText(ctx context.Context, sess *protocol.Session, chatID string, threadType protocol.ThreadType, text string) error {
	for len(text) > 0 {
		chunk := text
		if len(chunk) > maxTextLength {
			cutAt := maxTextLength
			if idx := strings.LastIndex(text[:maxTextLength], "\n"); idx > maxTextLength/2 {
				cutAt = idx + 1
			}
			chunk = text[:cutAt]
			text = text[cutAt:]
		} else {
			text = ""
		}

		if _, err := protocol.SendMessage(ctx, sess, chatID, threadType, chunk); err != nil {
			return err
		}
	}
	return nil
}
