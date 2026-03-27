package protocol

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"
)

// --- Message handlers ---

func (ln *Listener) handleUserMessages(ctx context.Context, data string, encType uint) {
	ln.mu.RLock()
	ck := ln.cipherKey
	ln.mu.RUnlock()

	payload, err := ln.decryptEventData(data, encType, ck)
	if err != nil {
		emit(ctx, ln.errorCh, fmt.Errorf("zalo_personal: decrypt user msg: %w", err))
		return
	}

	var envelope struct {
		Data struct {
			Msgs []json.RawMessage `json:"msgs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		emit(ctx, ln.errorCh, fmt.Errorf("zalo_personal: parse user msgs: %w", err))
		return
	}

	for _, raw := range envelope.Data.Msgs {
		var msg TMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		um := NewUserMessage(ln.sess.UID, msg)
		if um.IsSelf() {
			continue // skip self-sent messages
		}
		emit(ctx, ln.messageCh, Message(um))
	}
}

func (ln *Listener) handleGroupMessages(ctx context.Context, data string, encType uint) {
	ln.mu.RLock()
	ck := ln.cipherKey
	ln.mu.RUnlock()

	payload, err := ln.decryptEventData(data, encType, ck)
	if err != nil {
		emit(ctx, ln.errorCh, fmt.Errorf("zalo_personal: decrypt group msg: %w", err))
		return
	}

	var envelope struct {
		Data struct {
			GroupMsgs []json.RawMessage `json:"groupMsgs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		emit(ctx, ln.errorCh, fmt.Errorf("zalo_personal: parse group msgs: %w", err))
		return
	}

	for _, raw := range envelope.Data.GroupMsgs {
		var msg TGroupMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		gm := NewGroupMessage(ln.sess.UID, msg)
		if gm.IsSelf() {
			continue
		}
		emit(ctx, ln.messageCh, Message(gm))
	}
}

// --- Control events (cmd=601) ---

func (ln *Listener) handleControlEvents(ctx context.Context, data string, encType uint) {
	ln.mu.RLock()
	ck := ln.cipherKey
	ln.mu.RUnlock()

	payload, err := ln.decryptEventData(data, encType, ck)
	if err != nil {
		emit(ctx, ln.errorCh, fmt.Errorf("zalo_personal: decrypt control event: %w", err))
		return
	}

	var envelope struct {
		Data struct {
			Controls []struct {
				Content struct {
					ActType string          `json:"act_type"`
					FileID  any             `json:"fileId"` // can be string or number
					Data    json.RawMessage `json:"data"`   // can be object {"url":"..."} or string
				} `json:"content"`
			} `json:"controls"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		emit(ctx, ln.errorCh, fmt.Errorf("zalo_personal: parse control event: %w", err))
		return
	}

	for _, ctrl := range envelope.Data.Controls {
		if ctrl.Content.ActType != "file_done" {
			continue
		}
		if ctrl.Content.FileID == nil {
			continue
		}
		// FileID can be string or number (float64 from JSON).
		// fmt.Sprint on large float64 produces scientific notation (3.15e+11),
		// but callbacks are registered with the plain decimal string.
		fileID := anyToDecimalString(ctrl.Content.FileID)
		// Data can be an object {"url":"..."} or a plain string; extract URL from either.
		var fileURL string
		if len(ctrl.Content.Data) > 0 {
			if ctrl.Content.Data[0] == '{' {
				var dataObj struct {
					URL string `json:"url"`
				}
				if json.Unmarshal(ctrl.Content.Data, &dataObj) == nil {
					fileURL = dataObj.URL
				}
			} else {
				json.Unmarshal(ctrl.Content.Data, &fileURL)
			}
		}

		slog.Debug("zalo_personal file upload completed", "file_id", fileID, "url_len", len(fileURL))

		if val, ok := ln.uploadCallbacks.LoadAndDelete(fileID); ok {
			ch := val.(chan string)
			select {
			case ch <- fileURL:
			default:
			}
		}
	}
}

// --- Decryption ---

func (ln *Listener) decryptEventData(data string, encType uint, cipherKey string) ([]byte, error) {
	var result []byte
	var err error

	switch encType {
	case 0: // plaintext
		result = []byte(data)
	case 1: // base64 + gzip
		raw, e := base64.StdEncoding.DecodeString(data)
		if e != nil {
			return nil, e
		}
		result, err = decompressGzip(raw)
	case 2: // AES-GCM + gzip
		raw, e := ln.decryptAESGCMPayload(data, cipherKey)
		if e != nil {
			return nil, e
		}
		result, err = decompressGzip(raw)
	case 3: // AES-GCM raw (no gzip)
		result, err = ln.decryptAESGCMPayload(data, cipherKey)
	default:
		return nil, fmt.Errorf("unknown encryption type %d", encType)
	}

	if err != nil {
		return nil, err
	}
	if !utf8.Valid(result) {
		return nil, fmt.Errorf("decrypted payload is not valid UTF-8")
	}
	return result, nil
}

func (ln *Listener) decryptAESGCMPayload(data, cipherKey string) ([]byte, error) {
	if cipherKey == "" {
		return nil, fmt.Errorf("cipher key required for encrypted data")
	}

	unescaped, err := url.PathUnescape(data)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(unescaped)
	if err != nil {
		return nil, err
	}
	if len(decoded) < minEncDataLen {
		return nil, fmt.Errorf("encrypted data too short (%d bytes)", len(decoded))
	}

	key, err := base64.StdEncoding.DecodeString(cipherKey)
	if err != nil {
		return nil, fmt.Errorf("decode cipher key: %w", err)
	}

	iv := decoded[0:16]
	aad := decoded[16:32]
	ct := decoded[32:]

	return DecodeAESGCM(key, iv, aad, ct)
}

func decompressGzip(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer r.Close()
	return io.ReadAll(r)
}

// --- Ping loop ---

func (ln *Listener) pingLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	ln.sendPing(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ln.sendPing(ctx)
		}
	}
}

func (ln *Listener) sendPing(ctx context.Context) {
	data := map[string]any{"eventId": time.Now().UnixMilli()}
	body, _ := json.Marshal(data)

	buf := make([]byte, 4+len(body))
	buf[0] = 1 // version
	binary.LittleEndian.PutUint16(buf[1:3], 2) // cmd=2
	buf[3] = 1 // subCmd=1
	copy(buf[4:], body)

	ln.mu.RLock()
	client := ln.client
	ln.mu.RUnlock()

	if client != nil {
		_ = client.WriteMessage(ctx, buf)
	}
}

// --- Reconnect ---
// Two reconnect layers:
// 1. Listener-level (here): retries transient WS disconnects with endpoint rotation.
//    Controlled by server-provided retry config per close code.
// 2. Channel-level (channel.go restartWithBackoff): full re-auth when listener gives up.
//    Triggers when listener emits to closedCh after exhausting retries.
// The stopped flag prevents zombie reconnects after Stop() is called.

const stableThreshold = 60 * time.Second

func (ln *Listener) handleDisconnect(ctx context.Context, ci CloseInfo) {
	// Reset retry counters if connection was stable (>60s uptime)
	if !ln.connectedAt.IsZero() && time.Since(ln.connectedAt) > stableThreshold {
		ln.resetRetryCounters()
	}

	ln.reset()

	select {
	case ln.disconnectedCh <- ci:
	default:
	}

	// Code 3000 = duplicate — let channel-level handle it
	if ci.Code == CloseCodeDuplicate {
		ln.emitClosed(ci)
		return
	}

	delay, ok := ln.canRetry(ci.Code)
	if !ok {
		ln.emitClosed(ci)
		return
	}

	// Store timer so Stop() can cancel it to prevent zombie reconnects.
	ln.mu.Lock()
	ln.reconnTimer = time.AfterFunc(time.Duration(delay)*time.Millisecond, func() {
		ln.mu.RLock()
		stopped := ln.stopped
		ln.mu.RUnlock()
		if stopped || ctx.Err() != nil {
			return
		}
		// Rotate endpoint if applicable
		ln.tryRotateEndpoint(ci.Code)
		if err := ln.Start(ctx); err != nil {
			ln.emitClosed(ci)
		}
	})
	ln.mu.Unlock()
}

func (ln *Listener) reset() {
	ln.mu.Lock()
	defer ln.mu.Unlock()
	if ln.pingCancel != nil {
		ln.pingCancel()
		ln.pingCancel = nil
	}
	ln.client = nil
	ln.cipherKey = ""
	ln.connectedAt = time.Time{}

	// Drain pending upload callbacks — old connection callbacks won't arrive.
	ln.uploadCallbacks.Range(func(key, val any) bool {
		if ch, ok := val.(chan string); ok {
			select {
			case ch <- "":
			default:
			}
		}
		ln.uploadCallbacks.Delete(key)
		return true
	})
}

func (ln *Listener) resetRetryCounters() {
	ln.mu.Lock()
	defer ln.mu.Unlock()
	for _, st := range ln.retryStates {
		st.count = 0
	}
	ln.rotateCount = 0
	slog.Debug("zalo_personal retry counters reset after stable connection")
}

func (ln *Listener) canRetry(code int) (int, bool) {
	ln.mu.Lock()
	defer ln.mu.Unlock()

	st, ok := ln.retryStates[fmt.Sprint(code)]
	if !ok || st == nil || st.max == 0 || len(st.times) == 0 {
		return 0, false
	}
	if st.count >= st.max {
		return 0, false
	}

	idx := st.count
	st.count++
	delay := st.times[len(st.times)-1]
	if idx < len(st.times) {
		delay = st.times[idx]
	}
	return delay, true
}

// tryRotateEndpoint atomically checks and rotates the WS endpoint.
func (ln *Listener) tryRotateEndpoint(code int) {
	ln.mu.Lock()
	defer ln.mu.Unlock()
	if ln.sess.Settings == nil {
		return
	}
	codes := ln.sess.Settings.Features.Socket.RotateErrorCodes
	if slices.Contains(codes, code) && ln.rotateCount < len(ln.wsURLs)-1 {
		ln.rotateCount++
		ln.wsURL = buildWSURL(ln.sess, ln.wsURLs[ln.rotateCount])
	}
}

func (ln *Listener) emitClosed(ci CloseInfo) {
	select {
	case ln.closedCh <- ci:
	default:
	}
}

// anyToDecimalString converts a value (string, float64, json.Number, etc.) to a
// plain decimal string. Needed because JSON numbers unmarshalled into `any`
// become float64, and fmt.Sprint produces scientific notation for large values.
func anyToDecimalString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case json.Number:
		return val.String()
	default:
		return fmt.Sprint(v)
	}
}

// --- Helpers ---

func buildWSURL(sess *Session, base string) string {
	return makeURL(sess, base, map[string]any{"t": time.Now().UnixMilli()}, true)
}

func buildListenerRetryStates(settings *Settings) map[string]*retryState {
	states := make(map[string]*retryState, 8)
	if settings == nil {
		return states
	}
	for reason, cfg := range settings.Features.Socket.Retries {
		maxRetries := 0
		if cfg.Max != nil {
			maxRetries = *cfg.Max
		}
		if len(cfg.Times) == 0 {
			continue
		}
		states[reason] = &retryState{count: 0, max: maxRetries, times: cfg.Times}
	}
	return states
}

// emit sends to a buffered channel; drops oldest if full.
func emit[T any](ctx context.Context, ch chan T, val T) {
	select {
	case <-ctx.Done():
		return
	case ch <- val:
	default:
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- val:
		default:
		}
	}
}
