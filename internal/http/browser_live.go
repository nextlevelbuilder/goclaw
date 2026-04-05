package http

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/browser"
)

// BrowserLiveHandler provides HTTP endpoints for browser live view (screencast + input relay).
type BrowserLiveHandler struct {
	sessions store.ScreencastSessionStore
	manager  *browser.Manager
	logger   *slog.Logger
	upgrader websocket.Upgrader
}

// NewBrowserLiveHandler creates a BrowserLiveHandler.
func NewBrowserLiveHandler(ss store.ScreencastSessionStore, mgr *browser.Manager, l *slog.Logger) *BrowserLiveHandler {
	if l == nil {
		l = slog.Default()
	}
	return &BrowserLiveHandler{
		sessions: ss,
		manager:  mgr,
		logger:   l,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 65536,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
	}
}

// SetManager replaces the underlying browser Manager (used for config hot-reload).
func (h *BrowserLiveHandler) SetManager(mgr *browser.Manager) { h.manager = mgr }

// RegisterRoutes registers the live view HTTP routes.
func (h *BrowserLiveHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /browser/status", requireAuth("", h.handleStatus))
	mux.HandleFunc("GET /browser/tabs", requireAuth("", h.handleTabs))
	mux.HandleFunc("POST /browser/start", requireAuth("", h.handleStartBrowser))
	mux.HandleFunc("POST /browser/stop", requireAuth("", h.handleStopBrowser))
	// Authenticated screencast — direct WS for chat panel.
	// Auth via ?token= query param (WebSocket API cannot send custom headers).
	// The token is the gateway bearer token, same as used by all HTTP endpoints.
	mux.HandleFunc("GET /browser/screencast/{targetId}", h.handleScreencastWS)
	// Token-based endpoints — for sharing with unauthenticated viewers
	mux.HandleFunc("POST /browser/live", requireAuth("", h.handleCreate))
	mux.HandleFunc("GET /browser/live/{token}", h.handleView)
	mux.HandleFunc("GET /browser/live/{token}/info", h.handleInfo)
	mux.HandleFunc("GET /browser/live/{token}/ws", h.handleWS)
}

// browserCtx bridges the store tenant ID into the browser package's own context key.
func browserCtx(r *http.Request) context.Context {
	ctx := r.Context()
	if tid := store.TenantIDFromContext(ctx); tid.String() != "00000000-0000-0000-0000-000000000000" {
		ctx = browser.WithTenantID(ctx, tid.String())
	}
	return ctx
}

// handleStatus returns the current browser engine status.
func (h *BrowserLiveHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.manager.Status())
}

// handleTabs returns a list of open browser tabs, optionally filtered by agentKey query param.
func (h *BrowserLiveHandler) handleTabs(w http.ResponseWriter, r *http.Request) {
	tabs, err := h.manager.ListTabs(browserCtx(r))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"tabs": []any{}, "error": err.Error()})
		return
	}
	// Filter by session key (strict) or agent key.
	sessionKey := r.URL.Query().Get("sessionKey")
	agentKey := r.URL.Query().Get("agentKey")
	if sessionKey != "" {
		var bySession []browser.TabInfo
		for _, t := range tabs {
			if t.SessionKey == sessionKey {
				bySession = append(bySession, t)
			}
		}
		tabs = bySession
	} else if agentKey != "" {
		filtered := make([]browser.TabInfo, 0, len(tabs))
		for _, t := range tabs {
			if t.AgentKey == agentKey {
				filtered = append(filtered, t)
			}
		}
		tabs = filtered
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"tabs": tabs})
}

// handleStartBrowser starts the browser engine.
func (h *BrowserLiveHandler) handleStartBrowser(w http.ResponseWriter, r *http.Request) {
	if err := h.manager.Start(browserCtx(r)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// handleStopBrowser stops the browser engine.
func (h *BrowserLiveHandler) handleStopBrowser(w http.ResponseWriter, r *http.Request) {
	if err := h.manager.Stop(browserCtx(r)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// handleCreate creates a new screencast session.
func (h *BrowserLiveHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TargetID string `json:"targetId"`
		Mode     string `json:"mode"` // "view" or "takeover"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.TargetID == "" {
		http.Error(w, "targetId is required", http.StatusBadRequest)
		return
	}
	if req.Mode == "" {
		req.Mode = "view"
	}

	// Verify target page exists before creating a session token.
	// After browser restart, old targetIDs are gone — fail fast with a clear error.
	if h.manager.PageByTargetID(req.TargetID) == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "target page not found"})
		return
	}

	// Generate crypto-random token (20 bytes = 40 hex chars).
	// Kept under 64 hex chars to avoid the tool output scrubber's long-hex-string rule.
	tokenBytes := make([]byte, 20)
	if _, err := rand.Read(tokenBytes); err != nil {
		http.Error(w, "failed to generate token", http.StatusInternalServerError)
		return
	}
	token := hex.EncodeToString(tokenBytes)

	// Extract tenant from context (set by auth middleware)
	tenantID := store.TenantIDFromContext(r.Context()).String()

	sess := &store.ScreencastSession{
		TenantID:  tenantID,
		Token:     token,
		TargetID:  req.TargetID,
		Mode:      req.Mode,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	if err := h.sessions.Create(r.Context(), sess); err != nil {
		h.logger.Warn("failed to create screencast session", "error", err)
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"token": token,
		"url":   "/browser/live/" + token,
	})
}

// handleView serves the self-contained HTML viewer for a live session.
func (h *BrowserLiveHandler) handleView(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		http.Error(w, "token required", http.StatusBadRequest)
		return
	}

	sess, err := h.sessions.GetByToken(r.Context(), token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "session not found or expired", http.StatusNotFound)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if time.Now().After(sess.ExpiresAt) {
		http.Error(w, "session expired", http.StatusGone)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, liveViewHTML, token, sess.Mode)
}

// handleInfo returns session metadata as JSON (public, token-based auth).
func (h *BrowserLiveHandler) handleInfo(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		http.Error(w, "token required", http.StatusBadRequest)
		return
	}

	sess, err := h.sessions.GetByToken(r.Context(), token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "session not found or expired", http.StatusNotFound)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if time.Now().After(sess.ExpiresAt) {
		http.Error(w, "session expired", http.StatusGone)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"mode":      sess.Mode,
		"targetId":  sess.TargetID,
		"expiresAt": sess.ExpiresAt.Format(time.RFC3339),
	})
}

// handleScreencastWS handles authenticated screencast WS for the chat panel.
// Auth via Sec-WebSocket-Protocol header: client sends bearer token as a subprotocol,
// server echoes it back to complete the handshake. Safe (not in URL, not logged).
func (h *BrowserLiveHandler) handleScreencastWS(w http.ResponseWriter, r *http.Request) {
	targetID := r.PathValue("targetId")
	if targetID == "" {
		http.Error(w, "targetId required", http.StatusBadRequest)
		return
	}

	// Extract bearer token from Sec-WebSocket-Protocol header (set by client as subprotocol).
	bearer := ""
	for _, p := range websocket.Subprotocols(r) {
		bearer = p
		break
	}

	// Validate auth before upgrading — reject with 401 if invalid.
	if !tokenMatch(bearer, pkgGatewayToken) {
		if _, role := ResolveAPIKey(r.Context(), bearer); role == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Upgrade with the token echoed back as selected subprotocol (required by spec).
	upgrader := h.upgrader
	upgrader.Subprotocols = []string{bearer}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Warn("screencast ws upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	page := h.manager.PageByTargetID(targetID)
	if page == nil {
		conn.WriteMessage(websocket.TextMessage, []byte(`{"error":"target page not found"}`))
		return
	}

	h.logger.Info("screencast connected", "target", targetID)
	h.runScreencastLoop(conn, page, "takeover", targetID)
	h.logger.Info("screencast disconnected", "target", targetID)
}

// handleWS handles the token-based WebSocket connection for streaming frames and input.
func (h *BrowserLiveHandler) handleWS(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		http.Error(w, "token required", http.StatusBadRequest)
		return
	}

	sess, err := h.sessions.GetByToken(r.Context(), token)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if time.Now().After(sess.ExpiresAt) {
		http.Error(w, "session expired", http.StatusGone)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Warn("websocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	page := h.manager.PageByTargetID(sess.TargetID)
	if page == nil {
		conn.WriteMessage(websocket.TextMessage, []byte(`{"error":"target page not found"}`))
		return
	}

	h.logger.Info("live view connected", "token", token[:8], "mode", sess.Mode, "target", sess.TargetID)
	h.runScreencastLoop(conn, page, sess.Mode, sess.TargetID)
	h.logger.Info("live view disconnected", "token", token[:8])
}

// runScreencastLoop is the shared screencast + input relay loop used by both
// authenticated (chat panel) and token-based (share) WS handlers.
func (h *BrowserLiveHandler) runScreencastLoop(conn *websocket.Conn, page browser.Page, mode string, logID string) {
	frameCh := make(chan browser.ScreencastFrame, 20) // 2 seconds buffer at 10fps

	// Activate (bring to front) so CDP screencast captures this page.
	// Background tabs don't generate screencast frames.
	_ = page.Activate()

	vpW, vpH := h.manager.ViewportSize()
	const screencastFPS = 10
	const screencastQuality = 60
	if err := page.StartScreencast(screencastFPS, screencastQuality, vpW, vpH, frameCh); err != nil {
		conn.WriteMessage(websocket.TextMessage, fmt.Appendf(nil, `{"error":"screencast start failed: %v"}`, err))
		return
	}

	// Coordinate mapping state
	var (
		viewportW float64 = float64(vpW)
		viewportH float64 = float64(vpH)
		dimsMu    sync.Mutex
	)

	// Frame sender goroutine
	done := make(chan struct{})
	stopFrames := make(chan struct{})
	go func() {
		defer close(done)
		viewportSent := false
		for {
			var frame browser.ScreencastFrame
			var ok bool
			select {
			case frame, ok = <-frameCh:
				if !ok {
					return
				}
			case <-stopFrames:
				return
			}
			if frame.Metadata.DeviceWidth > 0 {
				dimsMu.Lock()
				viewportW = frame.Metadata.DeviceWidth
				viewportH = frame.Metadata.DeviceHeight
				dimsMu.Unlock()
				if !viewportSent {
					msg, _ := json.Marshal(map[string]any{
						"viewport": map[string]float64{
							"w": frame.Metadata.DeviceWidth,
							"h": frame.Metadata.DeviceHeight,
						},
					})
					conn.WriteMessage(websocket.TextMessage, msg)
					viewportSent = true
				}
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, frame.Data); err != nil {
				h.logger.Warn("screencast frame write failed", "id", logID, "error", err)
				return
			}
		}
	}()

	maxW, maxH := float64(vpW), float64(vpH)
	mapCoords := func(imgX, imgY float64) (float64, float64) {
		dimsMu.Lock()
		vw, vh := viewportW, viewportH
		dimsMu.Unlock()
		scale := min(maxW/vw, maxH/vh)
		return imgX / scale, imgY / scale
	}

	cdpButton := func(btn int) string {
		switch btn {
		case 0:
			return "left"
		case 1:
			return "middle"
		case 2:
			return "right"
		default:
			return "none"
		}
	}

	// Input receiver
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		if mode != "takeover" {
			continue
		}

		var input struct {
			Type       string  `json:"type"`
			X          float64 `json:"x"`
			Y          float64 `json:"y"`
			Button     int     `json:"button"`
			ButtonName string  `json:"buttonName"`
			ClickCount int     `json:"clickCount"`
			DeltaX     float64 `json:"deltaX"`
			DeltaY     float64 `json:"deltaY"`
			Key        string  `json:"key"`
			Code       string  `json:"code"`
			Text       string  `json:"text"`
			Modifiers  int     `json:"modifiers"`
			VKCode     int     `json:"vkCode"`
			Shift      bool    `json:"shift"`
			Ctrl       bool    `json:"ctrl"`
			Alt        bool    `json:"alt"`
			Meta       bool    `json:"meta"`
		}
		if err := json.Unmarshal(msg, &input); err != nil {
			continue
		}
		cssX, cssY := mapCoords(input.X, input.Y)
		btn := cdpButton(input.Button)
		if input.ButtonName != "" {
			btn = input.ButtonName
		}

		mod := input.Modifiers
		if mod == 0 {
			if input.Alt {
				mod |= 1
			}
			if input.Ctrl {
				mod |= 2
			}
			if input.Meta {
				mod |= 4
			}
			if input.Shift {
				mod |= 8
			}
		}

		if input.VKCode == 0 {
			input.VKCode = keyToVKCode(input.Key)
		}

		ev := input
		cx, cy := cssX, cssY
		bt, md := btn, mod

		go func() {
			switch ev.Type {
			case "mousedown":
				page.DispatchMouseEvent("mousePressed", cx, cy, bt, 1)
			case "mouseup":
				page.DispatchMouseEvent("mouseReleased", cx, cy, bt, 1)
			case "click":
				page.DispatchMouseEvent("mousePressed", cx, cy, bt, 1)
				time.Sleep(30 * time.Millisecond)
				page.DispatchMouseEvent("mouseReleased", cx, cy, bt, 1)
			case "dblclick":
				page.DispatchMouseEvent("mousePressed", cx, cy, bt, 1)
				page.DispatchMouseEvent("mouseReleased", cx, cy, bt, 1)
				time.Sleep(10 * time.Millisecond)
				page.DispatchMouseEvent("mousePressed", cx, cy, bt, 2)
				page.DispatchMouseEvent("mouseReleased", cx, cy, bt, 2)
			case "mousemove":
				page.DispatchMouseEvent("mouseMoved", cx, cy, "none", 0)
			case "scroll":
				if _, err := page.Eval(fmt.Sprintf(`() => window.scrollBy(%f, %f)`, ev.DeltaX, ev.DeltaY)); err != nil {
					page.DispatchScrollEvent(cx, cy, ev.DeltaX, ev.DeltaY)
				}
			case "mousePressed", "mouseReleased", "mouseMoved":
				page.DispatchMouseEvent(ev.Type, cx, cy, bt, ev.ClickCount)
			case "mouseWheel":
				if _, err := page.Eval(fmt.Sprintf(`() => window.scrollBy(%f, %f)`, ev.DeltaX, ev.DeltaY)); err != nil {
					page.DispatchScrollEvent(cx, cy, ev.DeltaX, ev.DeltaY)
				}
			case "keydown", "keyDown":
				text := ""
				if len(ev.Key) == 1 {
					text = ev.Key
				}
				page.DispatchKeyEvent("keyDown", ev.Key, ev.Code, "", md, ev.VKCode)
				if text != "" {
					page.DispatchKeyEvent("char", ev.Key, ev.Code, text, md, ev.VKCode)
				}
			case "keyup", "keyUp":
				page.DispatchKeyEvent("keyUp", ev.Key, ev.Code, "", md, ev.VKCode)
			}
		}()
	}

	// Unsubscribe this viewer's channel. CDP screencast stops only when last viewer leaves.
	page.StopScreencastCh(frameCh)
	close(stopFrames)
	<-done
}

// keyToVKCode maps KeyboardEvent.key names to Windows virtual key codes.
// CDP requires WindowsVirtualKeyCode for special keys to work correctly.
func keyToVKCode(key string) int {
	switch key {
	case "Backspace":
		return 8
	case "Tab":
		return 9
	case "Enter":
		return 13
	case "Shift":
		return 16
	case "Control":
		return 17
	case "Alt":
		return 18
	case "Pause":
		return 19
	case "CapsLock":
		return 20
	case "Escape":
		return 27
	case " ":
		return 32
	case "PageUp":
		return 33
	case "PageDown":
		return 34
	case "End":
		return 35
	case "Home":
		return 36
	case "ArrowLeft":
		return 37
	case "ArrowUp":
		return 38
	case "ArrowRight":
		return 39
	case "ArrowDown":
		return 40
	case "Insert":
		return 45
	case "Delete":
		return 46
	case "Meta":
		return 91
	case "ContextMenu":
		return 93
	case "F1":
		return 112
	case "F2":
		return 113
	case "F3":
		return 114
	case "F4":
		return 115
	case "F5":
		return 116
	case "F6":
		return 117
	case "F7":
		return 118
	case "F8":
		return 119
	case "F9":
		return 120
	case "F10":
		return 121
	case "F11":
		return 122
	case "F12":
		return 123
	case "NumLock":
		return 144
	case "ScrollLock":
		return 145
	default:
		// Single printable character: derive from ASCII/Unicode
		if len(key) == 1 {
			r := rune(key[0])
			// a-z → VK 0x41-0x5A (uppercase)
			if r >= 'a' && r <= 'z' {
				return int(r - 'a' + 'A')
			}
			// A-Z
			if r >= 'A' && r <= 'Z' {
				return int(r)
			}
			// 0-9
			if r >= '0' && r <= '9' {
				return int(r)
			}
		}
		return 0
	}
}

// liveViewHTML is the self-contained HTML/JS viewer for browser live view.
// %s placeholders: (1) token, (2) mode
const liveViewHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Browser Live View</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { background: #1a1a2e; display: flex; justify-content: center; align-items: center; height: 100vh; }
  #canvas { max-width: 100vw; max-height: 100vh; cursor: crosshair; }
  #status { position: fixed; top: 10px; right: 10px; color: #fff; font: 12px monospace;
            background: rgba(0,0,0,0.7); padding: 4px 8px; border-radius: 4px; }
</style>
</head>
<body>
<canvas id="canvas" width="1280" height="720"></canvas>
<div id="status">Connecting...</div>
<script>
const token = %q;
const mode = %q;
const canvas = document.getElementById('canvas');
const ctx = canvas.getContext('2d');
const status = document.getElementById('status');

const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
const ws = new WebSocket(proto + '//' + location.host + '/browser/live/' + token + '/ws');
ws.binaryType = 'arraybuffer';

ws.onopen = () => { status.textContent = 'Connected (' + mode + ')'; };
ws.onclose = () => { status.textContent = 'Disconnected'; };

ws.onmessage = (e) => {
  if (e.data instanceof ArrayBuffer) {
    const blob = new Blob([e.data], {type: 'image/jpeg'});
    const img = new Image();
    img.onload = () => {
      canvas.width = img.width;
      canvas.height = img.height;
      ctx.drawImage(img, 0, 0);
      URL.revokeObjectURL(img.src);
    };
    img.src = URL.createObjectURL(blob);
  }
};

if (mode === 'takeover') {
  canvas.addEventListener('mousedown', (e) => {
    const r = canvas.getBoundingClientRect();
    ws.send(JSON.stringify({type:'mousePressed', x:e.clientX-r.left, y:e.clientY-r.top, button:'left', clickCount:1}));
  });
  canvas.addEventListener('mouseup', (e) => {
    const r = canvas.getBoundingClientRect();
    ws.send(JSON.stringify({type:'mouseReleased', x:e.clientX-r.left, y:e.clientY-r.top, button:'left', clickCount:1}));
  });
  canvas.addEventListener('mousemove', (e) => {
    if (e.buttons === 0) return;
    const r = canvas.getBoundingClientRect();
    ws.send(JSON.stringify({type:'mouseMoved', x:e.clientX-r.left, y:e.clientY-r.top}));
  });
  document.addEventListener('keydown', (e) => {
    ws.send(JSON.stringify({type:'keyDown', key:e.key, code:e.code, text:e.key.length===1?e.key:'', modifiers:0}));
  });
  document.addEventListener('keyup', (e) => {
    ws.send(JSON.stringify({type:'keyUp', key:e.key, code:e.code}));
  });
}
</script>
</body>
</html>`
