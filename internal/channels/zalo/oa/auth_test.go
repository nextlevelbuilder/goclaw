package oa

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// newAuthServer mounts a handler that asserts header + form shape and
// returns the supplied response body.
func newAuthServer(t *testing.T, wantHeader, wantGrantType string, body string, status int) (*httptest.Server, *http.Request) {
	t.Helper()
	var captured *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		r.Body = io.NopCloser(strings.NewReader(string(buf)))
		captured = r

		if got := r.Header.Get("secret_key"); got != wantHeader {
			t.Errorf("secret_key header = %q, want %q", got, wantHeader)
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/x-www-form-urlencoded") {
			t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", got)
		}
		form, err := url.ParseQuery(string(buf))
		if err != nil {
			t.Errorf("parse form: %v", err)
		}
		if got := form.Get("grant_type"); got != wantGrantType {
			t.Errorf("grant_type = %q, want %q", got, wantGrantType)
		}
		if form.Get("app_id") == "" {
			t.Errorf("app_id missing")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

func TestExchangeCode_HappyPath(t *testing.T) {
	t.Parallel()

	body := `{"access_token":"AT-NEW","refresh_token":"RT-NEW","expires_in":3600}`
	srv, _ := newAuthServer(t, "the-secret", "authorization_code", body, http.StatusOK)

	c := NewClient(5 * time.Second)
	c.oauthBase = srv.URL // override for test

	before := time.Now()
	tok, err := c.ExchangeCode(context.Background(), "app-1", "the-secret", "the-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "AT-NEW" || tok.RefreshToken != "RT-NEW" {
		t.Errorf("tokens = %+v", tok)
	}
	// expires_in=3600 → ExpiresAt ≈ now+1h. Allow ±5s slack for test wall-clock.
	wantExp := before.Add(time.Hour)
	if tok.ExpiresAt.Before(wantExp.Add(-5*time.Second)) || tok.ExpiresAt.After(time.Now().Add(time.Hour+time.Second)) {
		t.Errorf("ExpiresAt out of range: %v", tok.ExpiresAt)
	}
}

func TestRefreshToken_HappyPath(t *testing.T) {
	t.Parallel()

	body := `{"access_token":"AT-2","refresh_token":"RT-2","expires_in":3600}`
	srv, _ := newAuthServer(t, "the-secret", "refresh_token", body, http.StatusOK)

	c := NewClient(5 * time.Second)
	c.oauthBase = srv.URL

	tok, err := c.RefreshToken(context.Background(), "app-1", "the-secret", "old-rt")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if tok.AccessToken != "AT-2" || tok.RefreshToken != "RT-2" {
		t.Errorf("tokens = %+v", tok)
	}
}

func TestExchangeCode_ErrorEnvelope(t *testing.T) {
	t.Parallel()

	// Zalo returns HTTP 200 with non-zero error code in body.
	body := `{"error":-123,"message":"invalid_code","data":null}`
	srv, _ := newAuthServer(t, "the-secret", "authorization_code", body, http.StatusOK)

	c := NewClient(5 * time.Second)
	c.oauthBase = srv.URL

	_, err := c.ExchangeCode(context.Background(), "app-1", "the-secret", "bad")
	if err == nil {
		t.Fatal("expected error from non-zero envelope code")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != -123 {
		t.Errorf("APIError.Code = %d, want -123", apiErr.Code)
	}
	if apiErr.Message != "invalid_code" {
		t.Errorf("APIError.Message = %q", apiErr.Message)
	}
}

func TestExchangeCode_ContextCancel(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Bound the handler so srv.Close() during cleanup never deadlocks
		// if the client-side context cancel doesn't propagate to the server.
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)

	c := NewClient(5 * time.Second)
	c.oauthBase = srv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.ExchangeCode(ctx, "app", "key", "code")
	if err == nil {
		t.Fatal("expected error on context cancel")
	}
}

func TestExchangeCode_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":500,"message":"boom"}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(5 * time.Second)
	c.oauthBase = srv.URL

	_, err := c.ExchangeCode(context.Background(), "app", "key", "code")
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}

// Sanity: response decoder must tolerate extra unknown fields.
func TestExchangeCode_UnknownFieldsTolerated(t *testing.T) {
	t.Parallel()

	body := `{"access_token":"AT","refresh_token":"RT","expires_in":3600,"future_field":"x"}`
	srv, _ := newAuthServer(t, "k", "authorization_code", body, http.StatusOK)

	c := NewClient(5 * time.Second)
	c.oauthBase = srv.URL

	tok, err := c.ExchangeCode(context.Background(), "app", "k", "code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "AT" {
		t.Errorf("AccessToken = %q", tok.AccessToken)
	}
}

// Compile-time guard: make sure JSON tags on response structs don't drift.
func TestTokenResponseShape_GuardsTagDrift(t *testing.T) {
	t.Parallel()
	// Numeric form (ChickenAI SDK's documented shape).
	var resp tokenResponse
	if err := json.Unmarshal([]byte(`{"access_token":"a","refresh_token":"b","expires_in":1}`), &resp); err != nil {
		t.Fatalf("unmarshal numeric: %v", err)
	}
	if resp.AccessToken != "a" || resp.RefreshToken != "b" || resp.ExpiresIn != 1 {
		t.Errorf("tag drift (numeric): %+v", resp)
	}

	// String form (what Zalo's live OA endpoint actually returns as of 2026).
	var resp2 tokenResponse
	if err := json.Unmarshal([]byte(`{"access_token":"a","refresh_token":"b","expires_in":"3600"}`), &resp2); err != nil {
		t.Fatalf("unmarshal string form: %v", err)
	}
	if resp2.ExpiresIn != 3600 {
		t.Errorf("string form: ExpiresIn = %d, want 3600", resp2.ExpiresIn)
	}
}

// Captures Zalo's refresh_token_expires_in across the response shapes we have
// seen in the wild (string + numeric) and the omitted case (legacy / shape drift).
func TestTokenCall_CapturesRefreshExpiry(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		body      string
		wantSet   bool
		wantSecs  int64
	}{
		{
			name:     "string_form",
			body:     `{"access_token":"AT","refresh_token":"RT","expires_in":"3600","refresh_token_expires_in":"7776000"}`,
			wantSet:  true,
			wantSecs: 7776000,
		},
		{
			name:     "numeric_form",
			body:     `{"access_token":"AT","refresh_token":"RT","expires_in":3600,"refresh_token_expires_in":2592000}`,
			wantSet:  true,
			wantSecs: 2592000,
		},
		{
			name:    "omitted",
			body:    `{"access_token":"AT","refresh_token":"RT","expires_in":3600}`,
			wantSet: false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := newAuthServer(t, "k", "authorization_code", tc.body, http.StatusOK)
			c := NewClient(5 * time.Second)
			c.oauthBase = srv.URL

			before := time.Now()
			tok, err := c.ExchangeCode(context.Background(), "app", "k", "code")
			if err != nil {
				t.Fatalf("ExchangeCode: %v", err)
			}
			if tc.wantSet {
				if tok.RefreshTokenExpiresAt.IsZero() {
					t.Fatalf("RefreshTokenExpiresAt should be set")
				}
				wantExp := before.Add(time.Duration(tc.wantSecs) * time.Second)
				delta := tok.RefreshTokenExpiresAt.Sub(wantExp)
				if delta < -2*time.Second || delta > 2*time.Second {
					t.Errorf("RefreshTokenExpiresAt = %v, want ≈ %v (delta %v)", tok.RefreshTokenExpiresAt, wantExp, delta)
				}
			} else {
				if !tok.RefreshTokenExpiresAt.IsZero() {
					t.Errorf("RefreshTokenExpiresAt should be zero, got %v", tok.RefreshTokenExpiresAt)
				}
			}
		})
	}
}

// WithTokens must NOT zero a previously set RefreshTokenExpiresAt if the
// freshly returned Tokens omits the field — guards against transient Zalo
// shape drift wiping out the warning deadline.
func TestWithTokens_PreservesRefreshExpiryWhenOmitted(t *testing.T) {
	t.Parallel()

	prev := time.Now().UTC().Add(60 * 24 * time.Hour)
	c := &ChannelCreds{RefreshTokenExpiresAt: prev}
	c.WithTokens(&Tokens{
		AccessToken:  "AT",
		RefreshToken: "RT",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
		// RefreshTokenExpiresAt: zero
	})
	if !c.RefreshTokenExpiresAt.Equal(prev) {
		t.Errorf("RefreshTokenExpiresAt = %v, want preserved %v", c.RefreshTokenExpiresAt, prev)
	}

	// And it MUST overwrite when a fresh value is provided.
	next := time.Now().UTC().Add(90 * 24 * time.Hour)
	c.WithTokens(&Tokens{
		AccessToken:           "AT2",
		RefreshToken:          "RT2",
		ExpiresAt:             time.Now().UTC().Add(time.Hour),
		RefreshTokenExpiresAt: next,
	})
	if !c.RefreshTokenExpiresAt.Equal(next) {
		t.Errorf("RefreshTokenExpiresAt = %v, want overwritten to %v", c.RefreshTokenExpiresAt, next)
	}
}
