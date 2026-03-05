package browser

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- resolveToIPv4 ---

func TestResolveToIPv4_IPLiteral(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"127.0.0.1", "127.0.0.1"},
		{"192.168.1.1", "192.168.1.1"},
		{"::1", "::1"},
	}
	for _, tt := range tests {
		got, err := resolveToIPv4(tt.input)
		if err != nil {
			t.Errorf("resolveToIPv4(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("resolveToIPv4(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestResolveToIPv4_Localhost(t *testing.T) {
	ip, err := resolveToIPv4("localhost")
	if err != nil {
		t.Fatalf("resolveToIPv4(localhost) error: %v", err)
	}
	// Should resolve to 127.0.0.1 (IPv4 preferred)
	parsed := net.ParseIP(ip)
	if parsed == nil {
		t.Fatalf("resolveToIPv4(localhost) returned non-IP: %q", ip)
	}
	if parsed.To4() == nil {
		t.Logf("resolveToIPv4(localhost) returned IPv6 %q (no IPv4 available)", ip)
	}
}

func TestResolveToIPv4_UnknownHost(t *testing.T) {
	_, err := resolveToIPv4("this-host-definitely-does-not-exist.invalid")
	if err == nil {
		t.Fatal("expected error for unknown host, got nil")
	}
}

// --- resolveRemoteCDP ---

func TestResolveRemoteCDP_Success(t *testing.T) {
	// Start a fake Chrome /json/version endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{
			"webSocketDebuggerUrl": "ws://127.0.0.1:9222/devtools/browser/abc-123",
		})
	}))
	defer srv.Close()

	// Extract host:port from test server URL.
	wsURL := "ws://" + srv.Listener.Addr().String()

	got, err := resolveRemoteCDP(wsURL)
	if err != nil {
		t.Fatalf("resolveRemoteCDP(%q) error: %v", wsURL, err)
	}

	// Should contain the devtools path.
	if !strings.Contains(got, "/devtools/browser/abc-123") {
		t.Errorf("resolveRemoteCDP result missing devtools path: %q", got)
	}
	// Should be a ws:// URL.
	if !strings.HasPrefix(got, "ws://") {
		t.Errorf("resolveRemoteCDP result should start with ws://: %q", got)
	}
}

func TestResolveRemoteCDP_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Chrome is not ready"))
	}))
	defer srv.Close()

	wsURL := "ws://" + srv.Listener.Addr().String()
	_, err := resolveRemoteCDP(wsURL)
	if err == nil {
		t.Fatal("expected error for 500 status, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("error should mention HTTP 500: %v", err)
	}
}

func TestResolveRemoteCDP_EmptyWebSocketURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"Browser": "HeadlessChrome/124.0",
			// webSocketDebuggerUrl intentionally missing
		})
	}))
	defer srv.Close()

	wsURL := "ws://" + srv.Listener.Addr().String()
	_, err := resolveRemoteCDP(wsURL)
	if err == nil {
		t.Fatal("expected error for empty webSocketDebuggerUrl, got nil")
	}
	if !strings.Contains(err.Error(), "empty webSocketDebuggerUrl") {
		t.Errorf("error should mention empty URL: %v", err)
	}
}

func TestResolveRemoteCDP_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	wsURL := "ws://" + srv.Listener.Addr().String()
	_, err := resolveRemoteCDP(wsURL)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestResolveRemoteCDP_ConnectionRefused(t *testing.T) {
	// Use a port that's definitely not listening.
	_, err := resolveRemoteCDP("ws://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error for connection refused, got nil")
	}
}

func TestResolveRemoteCDP_InvalidURL(t *testing.T) {
	_, err := resolveRemoteCDP("://invalid")
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

func TestResolveRemoteCDP_DefaultPort(t *testing.T) {
	// Verify that when port is omitted, 9222 is used.
	// This will fail to connect but the error should reference port 9222.
	_, err := resolveRemoteCDP("ws://127.0.0.1")
	if err == nil {
		t.Fatal("expected error (nothing on 9222), got nil")
	}
	if !strings.Contains(err.Error(), "9222") {
		t.Errorf("error should reference default port 9222: %v", err)
	}
}

func TestResolveRemoteCDP_HostReplacement(t *testing.T) {
	// Chrome returns ws://127.0.0.1/... but we need the server's actual IP:port.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		// Simulate Chrome returning localhost in the WS URL.
		json.NewEncoder(w).Encode(map[string]string{
			"webSocketDebuggerUrl": "ws://localhost:9999/devtools/browser/xyz",
		})
	}))
	defer srv.Close()

	wsURL := "ws://" + srv.Listener.Addr().String()
	got, err := resolveRemoteCDP(wsURL)
	if err != nil {
		t.Fatalf("resolveRemoteCDP(%q) error: %v", wsURL, err)
	}

	// The host in the result should be the test server's address, NOT localhost:9999.
	if strings.Contains(got, "localhost:9999") {
		t.Errorf("host should be replaced but still has localhost:9999: %q", got)
	}
	if !strings.Contains(got, "/devtools/browser/xyz") {
		t.Errorf("path should be preserved: %q", got)
	}
}

// --- Manager options ---

func TestManagerOptions(t *testing.T) {
	m := New(
		WithHeadless(true),
		WithRemoteURL("ws://chrome:9222"),
	)
	if !m.headless {
		t.Error("WithHeadless(true) not applied")
	}
	if m.remoteURL != "ws://chrome:9222" {
		t.Errorf("WithRemoteURL not applied: %q", m.remoteURL)
	}
}

func TestManagerStopWhenNil(t *testing.T) {
	m := New()
	// Stop on a fresh manager should be a no-op.
	if err := m.Close(); err != nil {
		t.Errorf("Close() on nil browser should be nil, got: %v", err)
	}
}

func TestManagerStatusWhenStopped(t *testing.T) {
	m := New()
	status := m.Status()
	if status.Running {
		t.Error("Status.Running should be false when browser is nil")
	}
}
