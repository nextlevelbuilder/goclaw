package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveGatewayClientOverrides(t *testing.T) {
	t.Setenv("GOCLAW_GATEWAY_TOKEN", "env-token")
	t.Setenv("GOCLAW_SERVER", "")
	t.Setenv("GOCLAW_GATEWAY_URL", "")

	oldServer := gatewayServerOverride
	oldToken := gatewayTokenOverride
	oldCfg := cfgFile
	t.Cleanup(func() {
		gatewayServerOverride = oldServer
		gatewayTokenOverride = oldToken
		cfgFile = oldCfg
	})

	cfgFile = "/path/that/does/not/exist"
	gatewayServerOverride = "https://goclaw.example.com/"
	gatewayTokenOverride = "flag-token"

	if got := resolveGatewayBaseURL(); got != "https://goclaw.example.com" {
		t.Fatalf("resolveGatewayBaseURL() = %q, want trimmed override", got)
	}
	if got := resolveGatewayToken(); got != "flag-token" {
		t.Fatalf("resolveGatewayToken() = %q, want flag token", got)
	}

	gatewayTokenOverride = ""
	if got := resolveGatewayToken(); got != "env-token" {
		t.Fatalf("resolveGatewayToken() = %q, want env token", got)
	}

	gatewayServerOverride = ""
	t.Setenv("GOCLAW_SERVER", "remote.example.com:18790/")
	if got := resolveGatewayBaseURL(); got != "http://remote.example.com:18790" {
		t.Fatalf("resolveGatewayBaseURL() = %q, want normalized env URL", got)
	}

	t.Setenv("GOCLAW_SERVER", "http://127.0.0.1:19999/")
	if got := resolveGatewayBaseURL(); got != "http://127.0.0.1:19999" {
		t.Fatalf("resolveGatewayBaseURL() = %q, want trimmed GOCLAW_SERVER URL", got)
	}
}

func TestGatewayHTTPDoRawUsesServerAndTokenOverride(t *testing.T) {
	oldServer := gatewayServerOverride
	oldToken := gatewayTokenOverride
	oldClient := httpClient
	t.Cleanup(func() {
		gatewayServerOverride = oldServer
		gatewayTokenOverride = oldToken
		httpClient = oldClient
	})

	var sawRequest bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest = true
		if r.URL.Path != "/v1/traces" {
			t.Errorf("path = %q, want /v1/traces", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer flag-token" {
			t.Errorf("Authorization = %q, want Bearer flag-token", got)
		}
		if got := r.Header.Get("X-GoClaw-User-Id"); got != "system" {
			t.Errorf("X-GoClaw-User-Id = %q, want system", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	gatewayServerOverride = srv.URL
	gatewayTokenOverride = "flag-token"
	httpClient = srv.Client()

	raw, status, err := gatewayHTTPDoRaw(http.MethodGet, "/v1/traces", nil)
	if err != nil {
		t.Fatalf("gatewayHTTPDoRaw: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if string(raw) != `{"ok":true}` {
		t.Fatalf("raw = %s", raw)
	}
	if !sawRequest {
		t.Fatal("test server did not receive request")
	}
}

func TestResolveGatewayWebSocketURLUsesServerOverride(t *testing.T) {
	oldServer := gatewayServerOverride
	oldCfg := cfgFile
	t.Cleanup(func() {
		gatewayServerOverride = oldServer
		cfgFile = oldCfg
	})

	cfgFile = "/path/that/does/not/exist"
	gatewayServerOverride = "https://goclaw.example.com/base/"

	got, err := resolveGatewayWebSocketURL()
	if err != nil {
		t.Fatalf("resolveGatewayWebSocketURL: %v", err)
	}
	if got != "wss://goclaw.example.com/base/ws" {
		t.Fatalf("resolveGatewayWebSocketURL() = %q", got)
	}
}

func TestGatewayHTTPDoRawWithLimitRejectsOversizedResponse(t *testing.T) {
	oldServer := gatewayServerOverride
	oldClient := httpClient
	t.Cleanup(func() {
		gatewayServerOverride = oldServer
		httpClient = oldClient
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("12345"))
	}))
	defer srv.Close()

	gatewayServerOverride = srv.URL
	httpClient = srv.Client()

	if _, _, err := gatewayHTTPDoRawWithLimit(http.MethodGet, "/big", nil, 4); err == nil {
		t.Fatal("expected oversized response error")
	}
}
