package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// --- test helpers ---

// testServer creates a minimal gateway server wired for integration tests.
// Returns the server, a cancel func to shut it down, and the ws:// URL.
func testServer(t *testing.T, cfg *config.Config) (*Server, string, context.CancelFunc) {
	t.Helper()
	mb := bus.New()
	agents := agent.NewRouter()
	s := NewServer(cfg, mb, agents, nil)
	s.SetVersion("test-0.0.1")
	s.SetPolicyEngine(permissions.NewPolicyEngine(cfg.Gateway.OwnerIDs))

	// Register no-op handlers for admin/write methods so permission checks are exercised.
	noop := func(_ context.Context, c *Client, req *protocol.RequestFrame) {
		c.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))
	}
	s.Router().Register(protocol.MethodAgentsCreate, noop)
	s.Router().Register(protocol.MethodChatSend, noop)

	ctx, cancel := context.WithCancel(context.Background())
	addr, start := StartTestServer(s, ctx)
	go start()

	// Poll until server is accepting connections instead of sleeping.
	wsURL := fmt.Sprintf("ws://%s/ws", addr)
	httpURL := fmt.Sprintf("http://%s/health", addr)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(httpURL)
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return s, wsURL, cancel
}

// dial opens a WebSocket connection to the test server.
func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// sendReq sends a JSON-RPC request frame over WebSocket.
func sendReq(t *testing.T, conn *websocket.Conn, id, method string, params any) {
	t.Helper()
	raw, _ := json.Marshal(params)
	frame := map[string]any{
		"type":   "req",
		"id":     id,
		"method": method,
		"params": json.RawMessage(raw),
	}
	if err := conn.WriteJSON(frame); err != nil {
		t.Fatalf("sendReq %s: %v", method, err)
	}
}

// readResp reads the next response frame from WebSocket with a timeout.
func readResp(t *testing.T, conn *websocket.Conn) protocol.ResponseFrame {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var resp protocol.ResponseFrame
	if err := conn.ReadJSON(&resp); err != nil {
		t.Fatalf("readResp: %v", err)
	}
	return resp
}

// --- integration tests ---

func TestIntegration_Connect_WithToken_Admin(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.Token = "test-secret-token"
	cfg.Gateway.OwnerIDs = []string{"system"}

	_, wsURL, cancel := testServer(t, cfg)
	defer cancel()

	conn := dial(t, wsURL)
	sendReq(t, conn, "1", "connect", map[string]any{
		"token":   "test-secret-token",
		"user_id": "alice",
	})

	resp := readResp(t, conn)
	if !resp.OK {
		t.Fatalf("connect failed: %v", resp.Error)
	}

	payload := resp.Payload.(map[string]any)
	if payload["role"] != "admin" {
		t.Errorf("role = %v, want admin", payload["role"])
	}
	if payload["user_id"] != "alice" {
		t.Errorf("user_id = %v, want alice", payload["user_id"])
	}
	if payload["protocol"] != float64(protocol.ProtocolVersion) {
		t.Errorf("protocol = %v, want %d", payload["protocol"], protocol.ProtocolVersion)
	}
}

func TestIntegration_Connect_WithToken_Owner(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.Token = "test-secret-token"
	cfg.Gateway.OwnerIDs = []string{"owner-user"}

	_, wsURL, cancel := testServer(t, cfg)
	defer cancel()

	conn := dial(t, wsURL)
	sendReq(t, conn, "1", "connect", map[string]any{
		"token":   "test-secret-token",
		"user_id": "owner-user",
	})

	resp := readResp(t, conn)
	if !resp.OK {
		t.Fatalf("connect failed: %v", resp.Error)
	}
	payload := resp.Payload.(map[string]any)
	if payload["role"] != "owner" {
		t.Errorf("role = %v, want owner", payload["role"])
	}
	if payload["is_owner"] != true {
		t.Errorf("is_owner = %v, want true", payload["is_owner"])
	}
}

func TestIntegration_Connect_NoTokenConfigured_Operator(t *testing.T) {
	cfg := &config.Config{} // no gateway token

	_, wsURL, cancel := testServer(t, cfg)
	defer cancel()

	conn := dial(t, wsURL)
	sendReq(t, conn, "1", "connect", map[string]any{
		"user_id": "anyone",
	})

	resp := readResp(t, conn)
	if !resp.OK {
		t.Fatalf("connect failed: %v", resp.Error)
	}
	payload := resp.Payload.(map[string]any)
	if payload["role"] != "operator" {
		t.Errorf("role = %v, want operator (no token configured)", payload["role"])
	}
}

func TestIntegration_Connect_WrongToken_Viewer(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.Token = "correct-token"

	_, wsURL, cancel := testServer(t, cfg)
	defer cancel()

	conn := dial(t, wsURL)
	sendReq(t, conn, "1", "connect", map[string]any{
		"token":   "wrong-token",
		"user_id": "intruder",
	})

	resp := readResp(t, conn)
	if !resp.OK {
		t.Fatalf("connect should succeed with viewer role, got error: %v", resp.Error)
	}
	payload := resp.Payload.(map[string]any)
	if payload["role"] != "viewer" {
		t.Errorf("role = %v, want viewer (wrong token)", payload["role"])
	}
}

func TestIntegration_FirstRequestMustBeConnect(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.Token = "test-token"

	_, wsURL, cancel := testServer(t, cfg)
	defer cancel()

	conn := dial(t, wsURL)
	// Send a non-connect request without authenticating first
	sendReq(t, conn, "1", "health", nil)

	resp := readResp(t, conn)
	if resp.OK {
		t.Fatal("expected error for non-connect first request")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("error code = %q, want %q", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestIntegration_UnknownMethod(t *testing.T) {
	cfg := &config.Config{} // no token → operator on connect

	_, wsURL, cancel := testServer(t, cfg)
	defer cancel()

	conn := dial(t, wsURL)
	// Connect first
	sendReq(t, conn, "1", "connect", map[string]any{"user_id": "alice"})
	readResp(t, conn) // consume connect response

	// Send unknown method
	sendReq(t, conn, "2", "totally.fake.method", nil)
	resp := readResp(t, conn)
	if resp.OK {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Errorf("error code = %q, want %q", resp.Error.Code, protocol.ErrInvalidRequest)
	}
}

func TestIntegration_PermissionDenied_ViewerCannotAccessAdmin(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.Token = "correct-token"

	_, wsURL, cancel := testServer(t, cfg)
	defer cancel()

	conn := dial(t, wsURL)
	// Connect with wrong token → viewer role
	sendReq(t, conn, "1", "connect", map[string]any{
		"token":   "wrong-token",
		"user_id": "viewer-user",
	})
	resp := readResp(t, conn)
	if !resp.OK {
		t.Fatalf("connect failed: %v", resp.Error)
	}

	// Try to call an admin method — should be denied
	sendReq(t, conn, "2", protocol.MethodAgentsCreate, map[string]any{"name": "test"})
	resp = readResp(t, conn)
	if resp.OK {
		t.Fatal("viewer should be denied access to admin method")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("error code = %q, want %q", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestIntegration_PermissionDenied_ViewerCannotWrite(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.Token = "correct-token"

	_, wsURL, cancel := testServer(t, cfg)
	defer cancel()

	conn := dial(t, wsURL)
	sendReq(t, conn, "1", "connect", map[string]any{
		"token":   "wrong-token",
		"user_id": "viewer-user",
	})
	readResp(t, conn)

	// Try to call a write method — should be denied
	sendReq(t, conn, "2", protocol.MethodChatSend, map[string]any{"message": "hello"})
	resp := readResp(t, conn)
	if resp.OK {
		t.Fatal("viewer should be denied access to write method")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("error code = %q, want %q", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestIntegration_AdminCanAccessAll(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.Token = "test-token"

	_, wsURL, cancel := testServer(t, cfg)
	defer cancel()

	conn := dial(t, wsURL)
	sendReq(t, conn, "1", "connect", map[string]any{
		"token":   "test-token",
		"user_id": "admin-user",
	})
	resp := readResp(t, conn)
	if !resp.OK {
		t.Fatalf("connect failed: %v", resp.Error)
	}

	// Health should work for admin
	sendReq(t, conn, "2", "health", nil)
	resp = readResp(t, conn)
	if !resp.OK {
		t.Fatalf("admin should access health: %v", resp.Error)
	}
}

func TestIntegration_Health_ReturnsServerInfo(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.Token = "test-token"

	_, wsURL, cancel := testServer(t, cfg)
	defer cancel()

	conn := dial(t, wsURL)
	sendReq(t, conn, "1", "connect", map[string]any{
		"token":   "test-token",
		"user_id": "admin",
	})
	readResp(t, conn)

	sendReq(t, conn, "2", "health", nil)
	resp := readResp(t, conn)
	if !resp.OK {
		t.Fatalf("health failed: %v", resp.Error)
	}
	payload := resp.Payload.(map[string]any)
	if payload["status"] != "ok" {
		t.Errorf("status = %v, want ok", payload["status"])
	}
	if payload["version"] != "test-0.0.1" {
		t.Errorf("version = %v, want test-0.0.1", payload["version"])
	}
}

func TestIntegration_Status_ReturnsAgentInfo(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.Token = "test-token"

	_, wsURL, cancel := testServer(t, cfg)
	defer cancel()

	conn := dial(t, wsURL)
	sendReq(t, conn, "1", "connect", map[string]any{
		"token":   "test-token",
		"user_id": "admin",
	})
	readResp(t, conn)

	sendReq(t, conn, "2", "status", nil)
	resp := readResp(t, conn)
	if !resp.OK {
		t.Fatalf("status failed: %v", resp.Error)
	}
	payload := resp.Payload.(map[string]any)
	if _, ok := payload["agents"]; !ok {
		t.Error("status should include agents field")
	}
	// At least 1 client connected (ourselves)
	clients := payload["clients"].(float64)
	if clients < 1 {
		t.Errorf("clients = %v, want >= 1", clients)
	}
}

func TestIntegration_InvalidFrame(t *testing.T) {
	cfg := &config.Config{}

	_, wsURL, cancel := testServer(t, cfg)
	defer cancel()

	conn := dial(t, wsURL)
	// Send invalid JSON
	conn.WriteMessage(websocket.TextMessage, []byte(`not-json`))

	resp := readResp(t, conn)
	if resp.OK {
		t.Fatal("expected error for invalid frame")
	}
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Errorf("error code = %q, want %q", resp.Error.Code, protocol.ErrInvalidRequest)
	}
}

func TestIntegration_UnexpectedFrameType(t *testing.T) {
	cfg := &config.Config{}

	_, wsURL, cancel := testServer(t, cfg)
	defer cancel()

	conn := dial(t, wsURL)
	// Send a response-type frame (server expects request)
	conn.WriteJSON(map[string]any{
		"type": "res",
		"id":   "1",
		"ok":   true,
	})

	resp := readResp(t, conn)
	if resp.OK {
		t.Fatal("expected error for unexpected frame type")
	}
}

func TestIntegration_MultipleClients(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.Token = "test-token"

	srv, wsURL, cancel := testServer(t, cfg)
	defer cancel()

	// Connect two clients
	conn1 := dial(t, wsURL)
	sendReq(t, conn1, "1", "connect", map[string]any{
		"token": "test-token", "user_id": "alice",
	})
	readResp(t, conn1)

	conn2 := dial(t, wsURL)
	sendReq(t, conn2, "1", "connect", map[string]any{
		"token": "test-token", "user_id": "bob",
	})
	readResp(t, conn2)

	// Poll until server tracks both clients.
	if !pollUntil(t, 2*time.Second, func() bool { return len(srv.ClientList()) == 2 }) {
		t.Errorf("expected 2 clients, got %d", len(srv.ClientList()))
	}
}

func TestIntegration_ClientDisconnect_Cleanup(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.Token = "test-token"

	srv, wsURL, cancel := testServer(t, cfg)
	defer cancel()

	conn := dial(t, wsURL)
	sendReq(t, conn, "1", "connect", map[string]any{
		"token": "test-token", "user_id": "temp",
	})
	readResp(t, conn)

	if !pollUntil(t, 2*time.Second, func() bool { return len(srv.ClientList()) == 1 }) {
		t.Fatal("expected 1 client before disconnect")
	}

	conn.Close()

	if !pollUntil(t, 2*time.Second, func() bool { return len(srv.ClientList()) == 0 }) {
		t.Error("expected 0 clients after disconnect")
	}
}

// --- helpers ---

// pollUntil retries check every 10ms until it returns true or timeout expires.
func pollUntil(t *testing.T, timeout time.Duration, check func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// --- Server checkOrigin tests ---

func TestCheckOrigin_NoConfig_AllowAll(t *testing.T) {
	cfg := &config.Config{}
	mb := bus.New()
	s := NewServer(cfg, mb, agent.NewRouter(), nil)

	req, _ := http.NewRequest("GET", "/ws", nil)
	req.Header.Set("Origin", "https://evil.com")
	if !s.checkOrigin(req) {
		t.Error("should allow all origins when no config")
	}
}

func TestCheckOrigin_NoBrowserOrigin_AlwaysAllow(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.AllowedOrigins = []string{"https://app.example.com"}
	mb := bus.New()
	s := NewServer(cfg, mb, agent.NewRouter(), nil)

	req, _ := http.NewRequest("GET", "/ws", nil)
	// No Origin header (CLI/SDK client)
	if !s.checkOrigin(req) {
		t.Error("should allow non-browser clients (no Origin header)")
	}
}

func TestCheckOrigin_AllowedOrigin(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.AllowedOrigins = []string{"https://app.example.com", "https://admin.example.com"}
	mb := bus.New()
	s := NewServer(cfg, mb, agent.NewRouter(), nil)

	req, _ := http.NewRequest("GET", "/ws", nil)
	req.Header.Set("Origin", "https://app.example.com")
	if !s.checkOrigin(req) {
		t.Error("should allow configured origin")
	}
}

func TestCheckOrigin_DeniedOrigin(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.AllowedOrigins = []string{"https://app.example.com"}
	mb := bus.New()
	s := NewServer(cfg, mb, agent.NewRouter(), nil)

	req, _ := http.NewRequest("GET", "/ws", nil)
	req.Header.Set("Origin", "https://evil.com")
	if s.checkOrigin(req) {
		t.Error("should deny non-configured origin")
	}
}

func TestCheckOrigin_Wildcard(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.AllowedOrigins = []string{"*"}
	mb := bus.New()
	s := NewServer(cfg, mb, agent.NewRouter(), nil)

	req, _ := http.NewRequest("GET", "/ws", nil)
	req.Header.Set("Origin", "https://anything.com")
	if !s.checkOrigin(req) {
		t.Error("wildcard should allow any origin")
	}
}

// --- clientIP tests ---

func TestClientIP_XRealIP(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-Real-IP", "10.0.0.1")
	req.RemoteAddr = "192.168.1.1:12345"
	if got := clientIP(req); got != "10.0.0.1" {
		t.Errorf("clientIP = %q, want %q", got, "10.0.0.1")
	}
}

func TestClientIP_XForwardedFor_Single(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.2")
	req.RemoteAddr = "192.168.1.1:12345"
	if got := clientIP(req); got != "10.0.0.2" {
		t.Errorf("clientIP = %q, want %q", got, "10.0.0.2")
	}
}

func TestClientIP_XForwardedFor_Chain(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.3, 10.0.0.4, 10.0.0.5")
	req.RemoteAddr = "192.168.1.1:12345"
	if got := clientIP(req); got != "10.0.0.3" {
		t.Errorf("clientIP = %q, want %q (first in chain)", got, "10.0.0.3")
	}
}

func TestClientIP_Fallback(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	if got := clientIP(req); got != "192.168.1.1" {
		t.Errorf("clientIP = %q, want %q", got, "192.168.1.1")
	}
}
