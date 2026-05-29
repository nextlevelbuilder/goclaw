package bitrix24

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// validReq returns a baseline rev4 autoOnboardRequest with all required
// fields populated so tests only need to override the field under test.
func validReq() autoOnboardRequest {
	return autoOnboardRequest{
		Domain:       "acme.bitrix24.com",
		BitrixUserID: "7",
		AccessToken:  "at-tok",
		RefreshToken: "rt-tok",
		ExpiresIn:    3600,
	}
}

func TestMCPClient_AutoOnboard_Success(t *testing.T) {
	var gotPath, gotAuth, gotCT string
	var gotBody autoOnboardRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api_key":"k-secret","user_id":"mcp-u-42","tenant_id":"mcp-t-1","created":true}`))
	}))
	defer srv.Close()

	c := newMCPClient(srv.URL, 2*time.Second)
	req := validReq()
	req.DisplayName = "Alice"
	resp, err := c.autoOnboard(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.APIKey != "k-secret" || resp.UserID != "mcp-u-42" || resp.TenantID != "mcp-t-1" || !resp.Created {
		t.Fatalf("unexpected resp: %+v", resp)
	}
	if gotPath != "/api/auto-onboard" {
		t.Fatalf("wrong path: %q", gotPath)
	}
	// Path B: no Authorization header — MCP server authenticates via the
	// caller-supplied Bitrix access_token in the body, not a bearer token.
	if gotAuth != "" {
		t.Fatalf("expected no Authorization header under Path B, got: %q", gotAuth)
	}
	if gotCT != "application/json" {
		t.Fatalf("wrong content-type: %q", gotCT)
	}
	if gotBody.Domain != "acme.bitrix24.com" || gotBody.BitrixUserID != "7" ||
		gotBody.AccessToken != "at-tok" || gotBody.RefreshToken != "rt-tok" ||
		gotBody.ExpiresIn != 3600 || gotBody.DisplayName != "Alice" {
		t.Fatalf("unexpected body: %+v", gotBody)
	}
}

func TestMCPClient_AutoOnboard_4xxNoRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid_bitrix_user"))
	}))
	defer srv.Close()

	c := newMCPClient(srv.URL, time.Second)
	_, err := c.autoOnboard(context.Background(), validReq())
	if err == nil {
		t.Fatalf("expected error on 401")
	}
	if calls != 1 {
		t.Fatalf("expected no retry on 4xx, saw %d calls", calls)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error should mention status: %v", err)
	}
}

func TestMCPClient_AutoOnboard_5xxRetriesOnce(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api_key":"k","user_id":"u","tenant_id":"t","created":false}`))
	}))
	defer srv.Close()

	c := newMCPClient(srv.URL, time.Second)
	resp, err := c.autoOnboard(context.Background(), validReq())
	if err != nil {
		t.Fatalf("expected success after retry: %v", err)
	}
	if resp.APIKey != "k" {
		t.Fatalf("unexpected resp: %+v", resp)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls (initial + retry), got %d", calls)
	}
}

func TestMCPClient_AutoOnboard_RejectsEmptyAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"api_key":"","user_id":"u","tenant_id":"t"}`))
	}))
	defer srv.Close()

	c := newMCPClient(srv.URL, time.Second)
	_, err := c.autoOnboard(context.Background(), validReq())
	if err == nil {
		t.Fatalf("expected error on incomplete response")
	}
	if !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMCPClient_AutoOnboard_TenantNotInstalled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"tenant_not_installed","domain":"acme.bitrix24.com"}`))
	}))
	defer srv.Close()

	c := newMCPClient(srv.URL, time.Second)
	_, err := c.autoOnboard(context.Background(), validReq())
	if err == nil {
		t.Fatalf("expected error on 404 tenant_not_installed")
	}
	if !errors.Is(err, ErrTenantNotInstalled) {
		t.Fatalf("expected ErrTenantNotInstalled sentinel, got: %v", err)
	}
}

func TestMCPClient_AutoOnboard_404OtherBodyFallsThroughAs4xx(t *testing.T) {
	// 404 with a different body shape should surface as a generic 4xx,
	// not the ErrTenantNotInstalled sentinel.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	c := newMCPClient(srv.URL, time.Second)
	_, err := c.autoOnboard(context.Background(), validReq())
	if err == nil {
		t.Fatalf("expected generic 4xx error")
	}
	if errors.Is(err, ErrTenantNotInstalled) {
		t.Fatalf("generic 404 must not map to ErrTenantNotInstalled: %v", err)
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 in error: %v", err)
	}
}

func TestMCPClient_AutoOnboard_RejectsMissingConfig(t *testing.T) {
	// Empty base URL
	c := newMCPClient("", time.Second)
	if _, err := c.autoOnboard(context.Background(), validReq()); err == nil {
		t.Fatalf("expected error on missing base URL")
	}
	// Missing request fields (empty)
	c = newMCPClient("http://x", time.Second)
	if _, err := c.autoOnboard(context.Background(), autoOnboardRequest{}); err == nil {
		t.Fatalf("expected error on empty request")
	}
	// Missing access token
	r := validReq()
	r.AccessToken = ""
	if _, err := c.autoOnboard(context.Background(), r); err == nil {
		t.Fatalf("expected error on missing access token")
	}
	// Missing refresh token
	r = validReq()
	r.RefreshToken = ""
	if _, err := c.autoOnboard(context.Background(), r); err == nil {
		t.Fatalf("expected error on missing refresh token")
	}
	// Missing domain
	r = validReq()
	r.Domain = ""
	if _, err := c.autoOnboard(context.Background(), r); err == nil {
		t.Fatalf("expected error on missing domain")
	}
	// Missing bitrix user id
	r = validReq()
	r.BitrixUserID = ""
	if _, err := c.autoOnboard(context.Background(), r); err == nil {
		t.Fatalf("expected error on missing bitrix_user_id")
	}
}

func TestIsGroupMessageType(t *testing.T) {
	cases := map[string]bool{
		"P":       false,
		"private": false,
		"":        false,
		" p ":     false,
		"C":       true,
		"c":       true,
		"chat":    true,
		"CHAT":    true,
		"O":       true,
		"open":    true,
		// "X" = entity-bound group chat (Tasks, Workgroups). Observed on
		// real ONIMBOTMESSAGEADD payloads where CHAT_ENTITY_TYPE=TASKS_TASK
		// and CHAT_USER_COUNT>1.
		"X":       true,
		"x":       true,
		" X ":     true,
		"unknown": false,
	}
	for input, want := range cases {
		if got := isGroupMessageType(input); got != want {
			t.Errorf("isGroupMessageType(%q) = %v, want %v", input, got, want)
		}
	}
}
