package bitrix24

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
)

// rewriteRT redirects requests to oauth.bitrix.info or any
// real bitrix24.com domain to our httptest.Server.
type rewriteRT struct {
	target string
	base   http.RoundTripper
}

func (r *rewriteRT) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(r.target)
	if err != nil {
		return nil, err
	}
	// Preserve path so /oauth/token/ and /rest/<method>.json land correctly.
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	return r.base.RoundTrip(req)
}

func newTestClient(t *testing.T, target string) *Client {
	t.Helper()
	httpClient := &http.Client{Transport: &rewriteRT{target: target, base: http.DefaultTransport}}
	return NewClient("portal.bitrix24.com", httpClient)
}

func TestClient_ExchangeAuthCode_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token/" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %q", r.Method)
		}
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "authorization_code" {
			t.Errorf("grant_type want=authorization_code got=%q", r.Form.Get("grant_type"))
		}
		if r.Form.Get("code") != "abc123" {
			t.Errorf("code want=abc123 got=%q", r.Form.Get("code"))
		}
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("missing form content-type")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"access_token":"AT",
			"refresh_token":"RT",
			"expires_in":3600,
			"domain":"portal.bitrix24.com",
			"member_id":"mem1",
			"client_endpoint":"https://portal.bitrix24.com/rest/",
			"application_token":"APP"
		}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	tr, err := c.ExchangeAuthCode(context.Background(), "cid", "secret", "abc123")
	if err != nil {
		t.Fatalf("ExchangeAuthCode: %v", err)
	}
	if tr.AccessToken != "AT" || tr.RefreshToken != "RT" || tr.ExpiresIn != 3600 {
		t.Fatalf("token mismatch: %+v", tr)
	}
	if tr.MemberID != "mem1" || tr.ApplicationToken != "APP" {
		t.Fatalf("metadata mismatch: %+v", tr)
	}
}

func TestClient_ExchangeAuthCode_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"bad code"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.ExchangeAuthCode(context.Background(), "cid", "secret", "wrong")
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.Code != "invalid_grant" || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("unexpected APIError: %+v", apiErr)
	}
	if apiErr.Method != "oauth/token" {
		t.Fatalf("APIError.Method = %q", apiErr.Method)
	}
}

func TestClient_RefreshToken_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Errorf("grant_type want=refresh_token got=%q", r.Form.Get("grant_type"))
		}
		if r.Form.Get("refresh_token") != "OLDREFRESH" {
			t.Errorf("refresh_token mismatch: %q", r.Form.Get("refresh_token"))
		}
		_, _ = w.Write([]byte(`{
			"access_token":"NEW_AT",
			"refresh_token":"NEW_RT",
			"expires_in":3600,
			"domain":"portal.bitrix24.com"
		}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	tr, err := c.RefreshToken(context.Background(), "cid", "secret", "OLDREFRESH")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if tr.AccessToken != "NEW_AT" || tr.RefreshToken != "NEW_RT" {
		t.Fatalf("token rotation failed: %+v", tr)
	}
}

func TestClient_PostTokenForm_EmptyAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"refresh_token":"RT","expires_in":3600,"domain":"portal.bitrix24.com"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := c.ExchangeAuthCode(context.Background(), "cid", "secret", "code")
	if err == nil {
		t.Fatal("expected error on empty access_token")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "empty_token" {
		t.Fatalf("expected APIError empty_token, got %v", err)
	}
}

func TestClient_Call_RequiresPortal(t *testing.T) {
	c := NewClient("portal.bitrix24.com", nil)
	_, err := c.Call(context.Background(), "any.method", nil)
	if err == nil || !strings.Contains(err.Error(), "portal not bound") {
		t.Fatalf("expected portal-not-bound error, got %v", err)
	}
}

func TestClient_Call_RequiresDomain(t *testing.T) {
	c := NewClient("", nil)
	_, err := c.Call(context.Background(), "any.method", nil)
	if err == nil || !strings.Contains(err.Error(), "domain not set") {
		t.Fatalf("expected domain-not-set error, got %v", err)
	}
}

func TestClient_Call_RequiresMethod(t *testing.T) {
	c := NewClient("portal.bitrix24.com", nil)
	_, err := c.Call(context.Background(), "", nil)
	if err == nil || !strings.Contains(err.Error(), "method required") {
		t.Fatalf("expected method-required error, got %v", err)
	}
}

func TestClient_Domain(t *testing.T) {
	c := NewClient("  trim.me.bitrix24.com  ", nil)
	if got := c.Domain(); got != "trim.me.bitrix24.com" {
		t.Fatalf("Domain not trimmed: %q", got)
	}
}

func TestEncodeParams_FlatTypes(t *testing.T) {
	got, err := encodeParams(map[string]any{
		"s": "hello",
		"i": 42,
		"l": int64(99),
		"b": true,
		"x": false,
		"n": json.Number("1.5"),
	})
	if err != nil {
		t.Fatalf("encodeParams: %v", err)
	}
	if got.Get("s") != "hello" {
		t.Errorf("s: %q", got.Get("s"))
	}
	if got.Get("i") != "42" {
		t.Errorf("i: %q", got.Get("i"))
	}
	if got.Get("l") != "99" {
		t.Errorf("l: %q", got.Get("l"))
	}
	if got.Get("b") != "Y" {
		t.Errorf("b: %q (want Y)", got.Get("b"))
	}
	if got.Get("x") != "N" {
		t.Errorf("x: %q (want N)", got.Get("x"))
	}
	if got.Get("n") != "1.5" {
		t.Errorf("n: %q", got.Get("n"))
	}
}

func TestEncodeParams_NestedMap(t *testing.T) {
	got, err := encodeParams(map[string]any{
		"FIELDS": map[string]any{
			"DIALOG_ID": "chat42",
			"MESSAGE":   "hi",
		},
	})
	if err != nil {
		t.Fatalf("encodeParams: %v", err)
	}
	if got.Get("FIELDS[DIALOG_ID]") != "chat42" {
		t.Errorf("nested key missing: %v", got)
	}
	if got.Get("FIELDS[MESSAGE]") != "hi" {
		t.Errorf("nested key missing: %v", got)
	}
}

func TestEncodeParams_StringSlice(t *testing.T) {
	got, err := encodeParams(map[string]any{
		"USERS": []string{"u1", "u2", "u3"},
	})
	if err != nil {
		t.Fatalf("encodeParams: %v", err)
	}
	if got.Get("USERS[0]") != "u1" || got.Get("USERS[1]") != "u2" || got.Get("USERS[2]") != "u3" {
		t.Errorf("slice encoding wrong: %v", got)
	}
}

func TestEncodeParams_AnySliceOfMaps(t *testing.T) {
	got, err := encodeParams(map[string]any{
		"KEYBOARD": []any{
			map[string]any{"TEXT": "Yes", "ACTION": "yes"},
			map[string]any{"TEXT": "No", "ACTION": "no"},
		},
	})
	if err != nil {
		t.Fatalf("encodeParams: %v", err)
	}
	if got.Get("KEYBOARD[0][TEXT]") != "Yes" || got.Get("KEYBOARD[1][ACTION]") != "no" {
		t.Errorf("nested slice-of-maps encoding wrong: %v", got)
	}
}

func TestEncodeParams_NilDropped(t *testing.T) {
	got, err := encodeParams(map[string]any{
		"keep": "v",
		"drop": nil,
	})
	if err != nil {
		t.Fatalf("encodeParams: %v", err)
	}
	if _, has := got["drop"]; has {
		t.Errorf("nil value should be dropped, got %v", got)
	}
	if got.Get("keep") != "v" {
		t.Errorf("non-nil sibling missing: %v", got)
	}
}

func TestEncodeParams_StructFallback(t *testing.T) {
	type payload struct {
		A string `json:"a"`
		B int    `json:"b"`
	}
	got, err := encodeParams(map[string]any{
		"raw": payload{A: "x", B: 7},
	})
	if err != nil {
		t.Fatalf("encodeParams: %v", err)
	}
	if got.Get("raw") != `{"a":"x","b":7}` {
		t.Errorf("struct fallback wrong: %q", got.Get("raw"))
	}
}

func TestAPIError_FormatsBothShapes(t *testing.T) {
	withDesc := &APIError{Status: 401, Code: "expired_token", Description: "Token has expired", Method: "im.message.add"}
	if !strings.Contains(withDesc.Error(), "Token has expired") {
		t.Errorf("APIError missing description: %q", withDesc.Error())
	}
	noDesc := &APIError{Status: 503, Code: "QUERY_LIMIT_EXCEEDED", Method: "imbot.message.add"}
	if !strings.Contains(noDesc.Error(), "QUERY_LIMIT_EXCEEDED") {
		t.Errorf("APIError missing code: %q", noDesc.Error())
	}
	var nilErr *APIError
	if got := nilErr.Error(); got != "" {
		t.Errorf("nil APIError.Error() = %q", got)
	}
}

// readBody is a tiny helper to avoid leaking httptest body in case
// future tests need to inspect raw bytes.
func readBody(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("readBody: %v", err)
	}
	return string(b)
}
