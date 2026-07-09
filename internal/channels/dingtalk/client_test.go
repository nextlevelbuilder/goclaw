package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient points a Client at a stub server for both hosts. The two base
// URLs are separate fields precisely so this is possible.
func newTestClient(apiBase, oapiBase string) *Client {
	c := NewClient("key", "secret")
	if apiBase != "" {
		c.apiBase = apiBase
	}
	if oapiBase != "" {
		c.oapiBase = oapiBase
	}
	return c
}

func TestAPIToken_FetchesAndCaches(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1.0/oauth2/accessToken" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["appKey"] != "key" || body["appSecret"] != "secret" {
			t.Errorf("credentials not sent: %v", body)
		}
		fmt.Fprint(w, `{"accessToken":"tok-1","expireIn":7200}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "")
	ctx := context.Background()

	for i := range 3 {
		tok, err := c.apiToken(ctx)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if tok != "tok-1" {
			t.Fatalf("token = %q", tok)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("token endpoint hit %d times, want 1 (cache miss)", got)
	}
}

// A token whose remaining life is inside the refresh margin must be refetched,
// or an in-flight request races the expiry.
func TestAPIToken_RefreshesInsideMargin(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		// expireIn shorter than tokenRefreshMargin: always "about to expire".
		fmt.Fprintf(w, `{"accessToken":"tok-%d","expireIn":30}`, n)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "")
	ctx := context.Background()

	first, err := c.apiToken(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.apiToken(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Errorf("token %q reused despite being inside the %v refresh margin", first, tokenRefreshMargin)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("token endpoint hit %d times, want 2", got)
	}
}

// tokenCache takes `now` as a parameter so the margin boundary can be tested
// exactly, without sleeping. A token issued at T with a 1h life is reused until
// T+1h-60s and refetched from T+1h-60s onward.
func TestTokenCache_ExpiryBoundary(t *testing.T) {
	base := time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC)
	const life = time.Hour

	tests := []struct {
		name        string
		secondCall  time.Time
		wantFetches int
	}{
		{"one second before the margin opens", base.Add(life - tokenRefreshMargin - time.Second), 1},
		{"exactly at the margin", base.Add(life - tokenRefreshMargin), 2},
		{"inside the margin", base.Add(life - time.Second), 2},
		{"after expiry", base.Add(life + time.Minute), 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var cache tokenCache
			fetches := 0
			fetch := func() (string, time.Duration, error) {
				fetches++
				return fmt.Sprintf("tok-%d", fetches), life, nil
			}

			if _, err := cache.get(base, fetch); err != nil {
				t.Fatal(err)
			}
			if _, err := cache.get(tc.secondCall, fetch); err != nil {
				t.Fatal(err)
			}
			if fetches != tc.wantFetches {
				t.Errorf("fetches = %d, want %d", fetches, tc.wantFetches)
			}
		})
	}
}

// A failed fetch must not poison the cache with an empty token.
func TestTokenCache_FailedFetchCachesNothing(t *testing.T) {
	var cache tokenCache
	now := time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC)

	_, err := cache.get(now, func() (string, time.Duration, error) {
		return "", 0, fmt.Errorf("boom")
	})
	if err == nil {
		t.Fatal("want error")
	}
	if cache.token != "" || !cache.expiresAt.IsZero() {
		t.Errorf("failed fetch mutated cache: token=%q expiresAt=%v", cache.token, cache.expiresAt)
	}
}

// A hundred goroutines finding an expired token must produce one request.
func TestAPIToken_ConcurrentSingleFetch(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		time.Sleep(10 * time.Millisecond) // widen the race window
		fmt.Fprint(w, `{"accessToken":"tok","expireIn":7200}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "")
	ctx := context.Background()

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if _, err := c.apiToken(ctx); err != nil {
				t.Errorf("apiToken: %v", err)
			}
		})
	}
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Errorf("token endpoint hit %d times under concurrency, want 1", got)
	}
}

func TestAPIToken_ErrorPaths(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantSub string
	}{
		{"http error", http.StatusUnauthorized, `{"code":"Forbidden"}`, "http 401"},
		{"no token in body", http.StatusOK, `{"expireIn":7200}`, "no accessToken"},
		{"malformed json", http.StatusOK, `{nope`, "decode token response"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()

			c := newTestClient(srv.URL, "")
			_, err := c.apiToken(context.Background())
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSub)
			}
		})
	}
}

// The legacy host answers HTTP 200 even for failures; errcode carries the truth.
// A client that only checked the status would cache an empty token.
func TestOAPIToken_ErrcodeIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"errcode":40001,"errmsg":"invalid appsecret"}`)
	}))
	defer srv.Close()

	c := newTestClient("", srv.URL)
	_, err := c.oapiToken(context.Background())
	if err == nil {
		t.Fatal("want error on errcode!=0, got nil")
	}
	if !strings.Contains(err.Error(), "40001") || !strings.Contains(err.Error(), "invalid appsecret") {
		t.Fatalf("error should surface errcode and errmsg: %v", err)
	}
	// Nothing may be cached from a failed fetch.
	if c.oapiTok.token != "" {
		t.Errorf("failed fetch cached token %q", c.oapiTok.token)
	}
}

func TestOAPIToken_SendsCredentialsAsQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gettoken" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("appkey"); got != "key" {
			t.Errorf("appkey = %q", got)
		}
		if got := r.URL.Query().Get("appsecret"); got != "secret" {
			t.Errorf("appsecret = %q", got)
		}
		fmt.Fprint(w, `{"errcode":0,"access_token":"oapi-tok","expires_in":7200}`)
	}))
	defer srv.Close()

	c := newTestClient("", srv.URL)
	tok, err := c.oapiToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok != "oapi-tok" {
		t.Errorf("token = %q", tok)
	}
}

// gettoken may omit expires_in; falling back to 0 would refetch on every call.
func TestOAPIToken_DefaultsTTLWhenMissing(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"errcode":0,"access_token":"oapi-tok"}`)
	}))
	defer srv.Close()

	c := newTestClient("", srv.URL)
	ctx := context.Background()
	if _, err := c.oapiToken(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.oapiToken(ctx); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("gettoken hit %d times, want 1 (missing expires_in must default, not expire instantly)", got)
	}
}

// api.dingtalk.com authenticates by header; oapi.dingtalk.com by query param.
// Mixing them up yields opaque 401s, so both are pinned.
func TestDoAPI_AttachesBearerHeader(t *testing.T) {
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			fmt.Fprint(w, `{"accessToken":"tok-1","expireIn":7200}`)
			return
		}
		gotToken = r.Header.Get("x-acs-dingtalk-access-token")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "")
	var out struct {
		OK bool `json:"ok"`
	}
	if err := c.doAPI(context.Background(), http.MethodPost, "/v1.0/card/instances", map[string]string{"a": "b"}, &out); err != nil {
		t.Fatalf("doAPI: %v", err)
	}
	if gotToken != "tok-1" {
		t.Errorf("x-acs-dingtalk-access-token = %q, want tok-1", gotToken)
	}
	if !out.OK {
		t.Error("response body not decoded into out")
	}
}

func TestDoAPI_SurfacesErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			fmt.Fprint(w, `{"accessToken":"t","expireIn":7200}`)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"code":"QpsLimit","message":"too fast","requestid":"req-9"}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, "")
	err := c.doAPI(context.Background(), http.MethodPut, "/v1.0/card/streaming", map[string]string{}, nil)
	if err == nil {
		t.Fatal("want error, got nil")
	}

	// Phase 6's QPS backoff needs to recognize this shape, so it must survive
	// as a typed error, not be flattened into a string.
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *apiError: %T %v", err, err)
	}
	if apiErr.Status != http.StatusForbidden || apiErr.Code != "QpsLimit" {
		t.Errorf("apiError = %+v", apiErr)
	}
	if apiErr.Request != "req-9" {
		t.Errorf("requestid = %q", apiErr.Request)
	}
}

func TestDoOAPI_AttachesQueryTokenAndCatchesErrcode(t *testing.T) {
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gettoken" {
			fmt.Fprint(w, `{"errcode":0,"access_token":"oapi-tok","expires_in":7200}`)
			return
		}
		gotToken = r.URL.Query().Get("access_token")
		// 200 with an error body — the legacy envelope.
		fmt.Fprint(w, `{"errcode":40035,"errmsg":"invalid media"}`)
	}))
	defer srv.Close()

	c := newTestClient("", srv.URL)
	err := c.doOAPI(context.Background(), http.MethodPost, "/media/upload", nil, nil, "", nil)
	if err == nil {
		t.Fatal("want error from errcode!=0 despite HTTP 200")
	}
	if !strings.Contains(err.Error(), "40035") {
		t.Fatalf("error should carry errcode: %v", err)
	}
	if gotToken != "oapi-tok" {
		t.Errorf("access_token query param = %q", gotToken)
	}
}

func TestDoOAPI_DecodesSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gettoken" {
			fmt.Fprint(w, `{"errcode":0,"access_token":"t","expires_in":7200}`)
			return
		}
		fmt.Fprint(w, `{"errcode":0,"media_id":"@lAD123"}`)
	}))
	defer srv.Close()

	c := newTestClient("", srv.URL)
	var out struct {
		MediaID string `json:"media_id"`
	}
	if err := c.doOAPI(context.Background(), http.MethodPost, "/media/upload", nil, nil, "", &out); err != nil {
		t.Fatalf("doOAPI: %v", err)
	}
	if out.MediaID != "@lAD123" {
		t.Errorf("media_id = %q", out.MediaID)
	}
}
