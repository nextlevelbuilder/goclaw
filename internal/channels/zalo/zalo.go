// Package zalo implements the Zalo OA Bot channel.
// Ported from OpenClaw TS extensions/zalo/.
//
// Zalo Bot API: https://bot-api.zaloplatforms.com
// DM only (no groups), text limit 2000 chars, polling + webhook modes.
package zalo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	apiBase            = "https://bot-api.zaloplatforms.com"
	defaultPollTimeout = 30
	maxTextLength      = 2000
	defaultMediaMaxMB  = 5
	pollErrorBackoff   = 5 * time.Second
	pairingDebounce    = 60 * time.Second
)

// Channel connects to the Zalo OA Bot API.
type Channel struct {
	*channels.BaseChannel
	token          string
	dmPolicy       string
	mediaMaxMB     int
	blockReply     *bool
	pairingService store.PairingStore
	pairingDebounce sync.Map // senderID → time.Time
	stopCh         chan struct{}
	client         *http.Client
}

// New creates a new Zalo channel.
func New(cfg config.ZaloConfig, msgBus *bus.MessageBus, pairingSvc store.PairingStore) (*Channel, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("zalo token is required")
	}

	base := channels.NewBaseChannel("zalo", msgBus, cfg.AllowFrom)
	base.ValidatePolicy(cfg.DMPolicy, "")

	dmPolicy := cfg.DMPolicy
	if dmPolicy == "" {
		dmPolicy = "pairing" // TS default
	}

	mediaMax := cfg.MediaMaxMB
	if mediaMax <= 0 {
		mediaMax = defaultMediaMaxMB
	}

	return &Channel{
		BaseChannel:    base,
		token:          cfg.Token,
		dmPolicy:       dmPolicy,
		mediaMaxMB:     mediaMax,
		blockReply:     cfg.BlockReply,
		pairingService: pairingSvc,
		stopCh:         make(chan struct{}),
		client:         &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// BlockReplyEnabled returns the per-channel block_reply override (nil = inherit gateway default).
func (c *Channel) BlockReplyEnabled() *bool { return c.blockReply }

// Start begins polling for Zalo updates.
func (c *Channel) Start(ctx context.Context) error {
	slog.Info("starting zalo bot (polling mode)")

	// Validate token
	info, err := c.getMe()
	if err != nil {
		return fmt.Errorf("zalo getMe failed: %w", err)
	}
	slog.Info("zalo bot connected", "bot_id", info.ID, "bot_name", info.Name)

	c.SetRunning(true)

	go c.pollLoop(ctx)

	return nil
}

// Stop shuts down the Zalo bot.
func (c *Channel) Stop(_ context.Context) error {
	slog.Info("stopping zalo bot")
	close(c.stopCh)
	c.SetRunning(false)
	return nil
}

// Send delivers an outbound message to a Zalo chat.
func (c *Channel) Send(_ context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("zalo bot not running")
	}

	// Check for media in content (URL-based photo sending)
	if strings.Contains(msg.Content, "[photo:") {
		// Extract photo URL from "[photo:URL]" pattern
		if start := strings.Index(msg.Content, "[photo:"); start >= 0 {
			end := strings.Index(msg.Content[start:], "]")
			if end > 0 {
				photoURL := msg.Content[start+7 : start+end]
				caption := strings.TrimSpace(msg.Content[:start] + msg.Content[start+end+1:])
				return c.sendPhoto(msg.ChatID, photoURL, caption)
			}
		}
	}

	// Send as text, chunking if over 2000 chars
	return c.sendChunkedText(msg.ChatID, msg.Content)
}

// --- Polling ---

func (c *Channel) pollLoop(ctx context.Context) {
	slog.Info("zalo polling loop started")

	for {
		select {
		case <-ctx.Done():
			slog.Info("zalo polling loop stopped (context)")
			return
		case <-c.stopCh:
			slog.Info("zalo polling loop stopped")
			return
		default:
		}

		updates, err := c.getUpdates(defaultPollTimeout)
		if err != nil {
			// 408 = no updates (timeout), not an error
			if !strings.Contains(err.Error(), "408") {
				slog.Warn("zalo getUpdates error", "error", err)
				select {
				case <-ctx.Done():
					return
				case <-c.stopCh:
					return
				case <-time.After(pollErrorBackoff):
				}
			}
			continue
		}

		for _, update := range updates {
			c.processUpdate(update)
		}
	}
}

func (c *Channel) processUpdate(update zaloUpdate) {
	switch update.EventName {
	case "message.text.received":
		if update.Message != nil {
			c.handleTextMessage(update.Message)
		}
	case "message.image.received":
		if update.Message != nil {
			c.handleImageMessage(update.Message)
		}
	default:
		slog.Debug("zalo unsupported event", "event", update.EventName)
	}
}

func (c *Channel) handleTextMessage(msg *zaloMessage) {
	senderID := msg.From.ID
	chatID := msg.Chat.ID
	if chatID == "" {
		chatID = senderID
	}

	// DM policy enforcement (Zalo is DM-only)
	if !c.checkDMPolicy(senderID, chatID) {
		return
	}

	content := msg.Text
	if content == "" {
		content = "[empty message]"
	}

	slog.Debug("zalo text message received",
		"sender_id", senderID,
		"chat_id", chatID,
		"preview", channels.Truncate(content, 50),
	)

	metadata := map[string]string{
		"message_id": msg.MessageID,
		"platform":   "zalo",
	}

	c.HandleMessage(senderID, chatID, content, nil, metadata, "direct")
}

func (c *Channel) handleImageMessage(msg *zaloMessage) {
	senderID := msg.From.ID
	chatID := msg.Chat.ID
	if chatID == "" {
		chatID = senderID
	}

	if !c.checkDMPolicy(senderID, chatID) {
		return
	}

	content := msg.Caption
	if content == "" {
		content = "[image]"
	}

	var media []string
	if msg.Photo != "" {
		media = []string{msg.Photo}
	}

	slog.Debug("zalo image message received",
		"sender_id", senderID,
		"chat_id", chatID,
	)

	metadata := map[string]string{
		"message_id": msg.MessageID,
		"platform":   "zalo",
	}

	c.HandleMessage(senderID, chatID, content, media, metadata, "direct")
}

// --- DM Policy ---

func (c *Channel) checkDMPolicy(senderID, chatID string) bool {
	switch c.dmPolicy {
	case "disabled":
		slog.Debug("zalo message rejected: DMs disabled", "sender_id", senderID)
		return false

	case "open":
		return true

	case "allowlist":
		if !c.IsAllowed(senderID) {
			slog.Debug("zalo message rejected by allowlist", "sender_id", senderID)
			return false
		}
		return true

	default: // "pairing"
		// Check if already paired or in allowlist
		paired := false
		if c.pairingService != nil {
			paired = c.pairingService.IsPaired(senderID, c.Name())
		}
		inAllowList := c.HasAllowList() && c.IsAllowed(senderID)

		if paired || inAllowList {
			return true
		}

		// Send pairing reply (debounced)
		c.sendPairingReply(senderID, chatID)
		return false
	}
}

func (c *Channel) sendPairingReply(senderID, chatID string) {
	if c.pairingService == nil {
		return
	}

	// Debounce
	if lastSent, ok := c.pairingDebounce.Load(senderID); ok {
		if time.Since(lastSent.(time.Time)) < pairingDebounce {
			return
		}
	}

	code, err := c.pairingService.RequestPairing(senderID, c.Name(), chatID, "default")
	if err != nil {
		slog.Debug("zalo pairing request failed", "sender_id", senderID, "error", err)
		return
	}

	replyText := fmt.Sprintf(
		"GoClaw: access not configured.\n\nYour Zalo user id: %s\n\nPairing code: %s\n\nAsk the bot owner to approve with:\n  goclaw pairing approve %s",
		senderID, code, code,
	)

	if err := c.sendMessage(chatID, replyText); err != nil {
		slog.Warn("failed to send zalo pairing reply", "error", err)
	} else {
		c.pairingDebounce.Store(senderID, time.Now())
		slog.Info("zalo pairing reply sent", "sender_id", senderID, "code", code)
	}
}

// --- Chunked text sending ---

func (c *Channel) sendChunkedText(chatID, text string) error {
	for len(text) > 0 {
		chunk := text
		if len(chunk) > maxTextLength {
			// Try to break at newline
			cutAt := maxTextLength
			if idx := strings.LastIndex(text[:maxTextLength], "\n"); idx > maxTextLength/2 {
				cutAt = idx + 1
			}
			chunk = text[:cutAt]
			text = text[cutAt:]
		} else {
			text = ""
		}

		if err := c.sendMessage(chatID, chunk); err != nil {
			return err
		}
	}
	return nil
}

// --- API methods ---

type zaloAPIResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result,omitempty"`
	ErrorCode   int             `json:"error_code,omitempty"`
	Description string          `json:"description,omitempty"`
}

type zaloBotInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type zaloMessage struct {
	MessageID string   `json:"message_id"`
	Text      string   `json:"text"`
	Photo     string   `json:"photo"`
	Caption   string   `json:"caption"`
	From      zaloFrom `json:"from"`
	Chat      zaloChat `json:"chat"`
	Date      int64    `json:"date"`
}

type zaloFrom struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type zaloChat struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type zaloUpdate struct {
	EventName string       `json:"event_name"`
	Message   *zaloMessage `json:"message,omitempty"`
}

func (c *Channel) callAPI(method string, body interface{}) (json.RawMessage, error) {
	url := fmt.Sprintf("%s/bot%s/%s", apiBase, c.token, method)

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest("POST", url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api call %s: %w", method, err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var apiResp zaloAPIResponse
	if err := json.Unmarshal(respData, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if !apiResp.OK {
		return nil, fmt.Errorf("zalo API error %d: %s", apiResp.ErrorCode, apiResp.Description)
	}

	return apiResp.Result, nil
}

func (c *Channel) getMe() (*zaloBotInfo, error) {
	result, err := c.callAPI("getMe", nil)
	if err != nil {
		return nil, err
	}

	var info zaloBotInfo
	if err := json.Unmarshal(result, &info); err != nil {
		return nil, fmt.Errorf("unmarshal bot info: %w", err)
	}
	return &info, nil
}

func (c *Channel) getUpdates(timeout int) ([]zaloUpdate, error) {
	params := map[string]interface{}{
		"timeout": timeout,
	}

	result, err := c.callAPI("getUpdates", params)
	if err != nil {
		return nil, err
	}

	var updates []zaloUpdate
	if err := json.Unmarshal(result, &updates); err != nil {
		return nil, fmt.Errorf("unmarshal updates: %w", err)
	}
	return updates, nil
}

func (c *Channel) sendMessage(chatID, text string) error {
	params := map[string]interface{}{
		"chat_id": chatID,
		"text":    text,
	}

	_, err := c.callAPI("sendMessage", params)
	return err
}

func (c *Channel) sendPhoto(chatID, photoURL, caption string) error {
	params := map[string]interface{}{
		"chat_id": chatID,
		"photo":   photoURL,
	}
	if caption != "" {
		params["caption"] = caption
	}

	_, err := c.callAPI("sendPhoto", params)
	return err
}
