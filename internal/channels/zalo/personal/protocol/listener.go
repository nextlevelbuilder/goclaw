package protocol

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	CloseCodeDuplicate = 3000 // another Zalo session opened — never reconnect
	msgBufferSize      = 64
	minEncDataLen      = 48
)

// Listener connects to Zalo's WebSocket and dispatches messages.
type Listener struct {
	mu   sync.RWMutex
	sess *Session

	wsURLs      []string
	wsURL       string
	rotateCount int

	client      *WSClient
	cipherKey   string
	connectedAt time.Time
	stopped     bool         // prevents reconnect after Stop()
	reconnTimer *time.Timer  // pending reconnect timer, cancelled on Stop()

	retryStates map[string]*retryState

	uploadCallbacks sync.Map // fileID (string) → chan string (fileURL)

	messageCh      chan Message
	disconnectedCh chan CloseInfo
	closedCh       chan CloseInfo
	errorCh        chan error

	pingCancel context.CancelFunc
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// RegisterUploadCallback registers a callback channel for a file upload completion.
// Returns a channel that will receive the fileURL when the upload is done.
func (ln *Listener) RegisterUploadCallback(fileID string) <-chan string {
	ch := make(chan string, 1)
	ln.uploadCallbacks.Store(fileID, ch)
	return ch
}

// CancelUploadCallback removes a pending upload callback.
func (ln *Listener) CancelUploadCallback(fileID string) {
	ln.uploadCallbacks.Delete(fileID)
}

// CloseInfo carries the WebSocket close code and reason.
type CloseInfo struct {
	Code   int
	Reason string
}

type retryState struct {
	count int
	max   int
	times []int // delay in ms per retry attempt
}

// NewListener creates a listener from an authenticated session.
func NewListener(sess *Session) (*Listener, error) {
	if sess.LoginInfo == nil || len(sess.LoginInfo.ZpwWebsocket) == 0 {
		return nil, fmt.Errorf("zalo_personal: no websocket URLs in session")
	}

	wsURL := buildWSURL(sess, sess.LoginInfo.ZpwWebsocket[0])
	return &Listener{
		sess:           sess,
		wsURLs:         sess.LoginInfo.ZpwWebsocket,
		wsURL:          wsURL,
		retryStates:    buildListenerRetryStates(sess.Settings),
		messageCh:      make(chan Message, msgBufferSize),
		disconnectedCh: make(chan CloseInfo, 4),
		closedCh:       make(chan CloseInfo, 1),
		errorCh:        make(chan error, 16),
	}, nil
}

// Channel accessors.
func (ln *Listener) Messages() <-chan Message      { return ln.messageCh }
func (ln *Listener) Disconnected() <-chan CloseInfo { return ln.disconnectedCh }
func (ln *Listener) Closed() <-chan CloseInfo       { return ln.closedCh }
func (ln *Listener) Errors() <-chan error           { return ln.errorCh }

// Start connects to WebSocket and begins reading messages.
func (ln *Listener) Start(ctx context.Context) error {
	ln.mu.Lock()
	defer ln.mu.Unlock()

	if ln.client != nil {
		return fmt.Errorf("zalo_personal: listener already started")
	}

	ln.stopped = false
	lctx, cancel := context.WithCancel(ctx)
	ln.cancel = cancel

	u, _ := url.Parse(ln.wsURL)
	h := http.Header{}
	h.Set("Accept-Language", "en-US,en;q=0.9")
	h.Set("Cache-Control", "no-cache")
	h.Set("Host", u.Host)
	h.Set("Origin", DefaultBaseURL.String())
	h.Set("Pragma", "no-cache")
	h.Set("User-Agent", ln.sess.UserAgent)

	client, err := DialWS(lctx, ln.wsURL, h, ln.sess.CookieJar)
	if err != nil {
		cancel()
		return err
	}

	slog.Debug("zalo websocket connected", "url", ln.wsURL)

	// Set initial read deadline for the cipher key handshake message.
	// Without this, ReadMessage blocks indefinitely if the server never responds.
	// The deadline is cleared once the ping loop starts (cipher key received).
	client.conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	ln.client = client
	ln.connectedAt = time.Now()
	ln.wg.Add(1)
	go ln.run(lctx)
	return nil
}

// Stop gracefully closes the WebSocket connection and cancels any pending reconnect.
func (ln *Listener) Stop() {
	ln.mu.Lock()
	ln.stopped = true
	client := ln.client
	cancel := ln.cancel
	timer := ln.reconnTimer
	ln.reconnTimer = nil
	ln.mu.Unlock()

	if timer != nil {
		timer.Stop()
	}
	// Always cancel context to prevent pending reconnect timers from firing.
	if cancel != nil {
		cancel()
	}
	if client != nil {
		client.Close(1000, "")
	}
	ln.wg.Wait()
}

func (ln *Listener) run(ctx context.Context) {
	defer ln.wg.Done()

	for {
		if ctx.Err() != nil {
			return
		}

		var data []byte
		var err error

		// Apply read deadline if ping loop is active (cipher key received).
		// Detects silent disconnects where Zalo stops sending without closing.
		ln.mu.RLock()
		hasPing := ln.pingCancel != nil
		ln.mu.RUnlock()

		if hasPing {
			readCtx, rcancel := context.WithTimeout(ctx, ln.readDeadline())
			data, err = ln.client.ReadMessage(readCtx)
			rcancel()
		} else {
			data, err = ln.client.ReadMessage(ctx)
		}

		if err != nil {
			ci := parseWSCloseInfo(err)
			// Distinguish read timeout (silent disconnect) from real close.
			// If the parent ctx is still alive but we got a deadline/cancel error,
			// it was the per-read timeout that expired.
			if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
				ci = CloseInfo{Code: 1006, Reason: "read timeout (silent disconnect)"}
				slog.Warn("zalo_personal silent disconnect detected")
			}
			ln.handleDisconnect(ctx, ci)
			return
		}
		ln.handleFrame(ctx, data)
	}
}

const defaultReadDeadline = 3 * time.Minute

// readDeadline returns 2.5× the ping interval, or 3 minutes as fallback.
func (ln *Listener) readDeadline() time.Duration {
	if ln.sess.Settings != nil {
		interval := ln.sess.Settings.Features.Socket.PingInterval
		if interval > 0 {
			return time.Duration(interval) * time.Millisecond * 5 / 2
		}
	}
	return defaultReadDeadline
}

func (ln *Listener) handleFrame(ctx context.Context, data []byte) {
	if len(data) < 4 {
		return
	}

	version := data[0]
	cmd := binary.LittleEndian.Uint16(data[1:3])
	subCmd := data[3]
	body := data[4:]

	var envelope struct {
		Key     *string `json:"key"`
		Encrypt uint    `json:"encrypt"`
		Data    string  `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		emit(ctx, ln.errorCh, fmt.Errorf("zalo_personal: parse ws frame: %w", err))
		return
	}

	key := fmt.Sprintf("%d_%d_%d", version, cmd, subCmd)
	switch key {
	case "1_1_1":
		ln.handleCipherKey(ctx, envelope.Key)
	case "1_501_0":
		ln.handleUserMessages(ctx, envelope.Data, envelope.Encrypt)
	case "1_521_0":
		ln.handleGroupMessages(ctx, envelope.Data, envelope.Encrypt)
	case "1_601_0":
		ln.handleControlEvents(ctx, envelope.Data, envelope.Encrypt)
	case "1_3000_0":
		slog.Warn("zalo_personal: duplicate connection detected, closing")
		ln.mu.RLock()
		client := ln.client
		ln.mu.RUnlock()
		if client != nil {
			client.Close(CloseCodeDuplicate, "duplicate")
		}
	}
}

func (ln *Listener) handleCipherKey(ctx context.Context, key *string) {
	if key == nil || *key == "" {
		return
	}
	ln.mu.Lock()
	ln.cipherKey = *key
	// Clear initial handshake deadline; ping loop manages deadlines from here.
	if ln.client != nil {
		ln.client.conn.SetReadDeadline(time.Time{})
	}
	ln.mu.Unlock()

	// Start ping loop
	if ln.sess.Settings != nil {
		interval := ln.sess.Settings.Features.Socket.PingInterval
		if interval > 0 {
			pctx, pcancel := context.WithCancel(ctx)
			ln.mu.Lock()
			ln.pingCancel = pcancel
			ln.mu.Unlock()
			go ln.pingLoop(pctx, time.Duration(interval)*time.Millisecond)
		}
	}
}

