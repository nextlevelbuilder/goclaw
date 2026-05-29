// Package bitrix24 implements a native goclaw channel for the Bitrix24 portal.
//
// This file is the low-level REST client. Phase 01 only exposes the OAuth2
// endpoints (token exchange + refresh) so the Portal runtime can bootstrap
// and keep its session alive. Phase 03 layers authenticated Call() on top
// of this so bot methods (imbot.message.add, im.message.update, …) can reuse
// the same client instance.
//
// Endpoint layout (reference: https://apidocs.bitrix24.com/):
//
//	Token exchange / refresh → POST https://oauth.bitrix.info/oauth/token/
//	Authenticated REST calls → POST https://<domain>/rest/<method>.json
//
// Everything is JSON in, JSON out. We deliberately avoid a third-party SDK:
// the surface we need is small and the upstream quirks (alternate error
// shapes, 24h edit window on im.message.update, etc.) are easier to handle
// when the transport is explicit.
package bitrix24

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// oauthTokenURL is the Bitrix24 global OAuth endpoint.
//
// Every portal exchanges / refreshes against the same host — the `domain`
// comes back in the response body and tells the client which portal the
// token belongs to.
const oauthTokenURL = "https://oauth.bitrix.info/oauth/token/"

// TokenResponse models the Bitrix24 OAuth2 response.
//
// On error Bitrix sets `error` + `error_description` and omits the token
// fields. We still decode the whole envelope in one pass so callers see
// both the error summary and any partial fields (e.g. domain) for logging.
type TokenResponse struct {
	AccessToken      string `json:"access_token,omitempty"`
	RefreshToken     string `json:"refresh_token,omitempty"`
	ExpiresIn        int64  `json:"expires_in,omitempty"` // seconds
	Domain           string `json:"domain,omitempty"`
	MemberID         string `json:"member_id,omitempty"`
	Scope            string `json:"scope,omitempty"`
	ClientEndpoint   string `json:"client_endpoint,omitempty"`
	ServerEndpoint   string `json:"server_endpoint,omitempty"`
	UserID           int64  `json:"user_id,omitempty"`
	Status           string `json:"status,omitempty"`
	ApplicationToken string `json:"application_token,omitempty"`

	// Error fields populated when Bitrix rejects the request.
	Error            string `json:"error,omitempty"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// RawResult is the envelope returned by every authenticated REST call.
//
// `Result` is kept as raw JSON so callers can decode into method-specific
// shapes (int for imbot.register, array for im.dialog.get, etc). `Total`
// + `Next` surface pagination on list endpoints.
type RawResult struct {
	Result           json.RawMessage `json:"result,omitempty"`
	Total            int             `json:"total,omitempty"`
	Next             int             `json:"next,omitempty"`
	Time             any             `json:"time,omitempty"`
	Error            string          `json:"error,omitempty"`
	ErrorDescription string          `json:"error_description,omitempty"`
}

// APIError wraps a non-2xx or `error`-bearing Bitrix24 response.
//
// Status is the HTTP code (may be 200 for application-level errors).
// Code/Description map to Bitrix's own fields so the channel layer can pattern
// match on expired_token / NO_AUTH_FOUND / QUERY_LIMIT_EXCEEDED etc.
type APIError struct {
	Status      int
	Code        string
	Description string
	Method      string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Description != "" {
		return fmt.Sprintf("bitrix24 %s: %s (%s, http=%d)", e.Method, e.Description, e.Code, e.Status)
	}
	return fmt.Sprintf("bitrix24 %s: %s (http=%d)", e.Method, e.Code, e.Status)
}

// Client is the REST client for a single portal. Safe for concurrent use.
//
// `domain` is set at construction; `portal` is filled in by Portal.bindClient
// so Call() can fetch a fresh access token without a tight circular dep.
type Client struct {
	http   *http.Client
	domain string

	portalMu sync.RWMutex
	portal   *Portal
}

// NewClient returns a client pointed at the given portal domain.
//
// A nil http.Client yields a sensible default (15s timeout). Pass a custom
// client for tests that need to stub transport.
func NewClient(domain string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		http:   httpClient,
		domain: strings.TrimSpace(domain),
	}
}

// SetPortal wires the portal runtime into the client so Call() can fetch
// tokens without a back-reference at construction time.
// Used by Portal.NewPortal right after the client is built.
func (c *Client) SetPortal(p *Portal) {
	c.portalMu.Lock()
	defer c.portalMu.Unlock()
	c.portal = p
}

// Domain returns the portal hostname this client targets.
func (c *Client) Domain() string {
	return c.domain
}

// ExchangeAuthCode trades an install-time authorization code for the
// initial access+refresh token pair. Runs on /bitrix24/install.
func (c *Client) ExchangeAuthCode(ctx context.Context, clientID, clientSecret, code string) (*TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
	}
	return c.postTokenForm(ctx, form)
}

// RefreshToken rotates the access+refresh pair using the stored refresh token.
// Bitrix24 returns a new refresh_token each time; callers must persist it.
func (c *Client) RefreshToken(ctx context.Context, clientID, clientSecret, refreshToken string) (*TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"refresh_token": {refreshToken},
	}
	return c.postTokenForm(ctx, form)
}

// postTokenForm handles the OAuth POST + response decoding for both
// exchange and refresh. Returned error is *APIError for application-level
// rejections (so classifiers can switch on Code) or a wrapped net error
// for transport failures.
func (c *Client) postTokenForm(ctx context.Context, form url.Values) (*TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bitrix24 oauth http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB hard cap
	if err != nil {
		return nil, fmt.Errorf("bitrix24 oauth read: %w", err)
	}

	var tr TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("bitrix24 oauth decode (status=%d): %w: %s", resp.StatusCode, err, truncate(string(body), 200))
	}

	if resp.StatusCode >= 400 || tr.Error != "" {
		return &tr, &APIError{
			Status:      resp.StatusCode,
			Code:        tr.Error,
			Description: tr.ErrorDescription,
			Method:      "oauth/token",
		}
	}
	if tr.AccessToken == "" {
		return &tr, &APIError{
			Status:      resp.StatusCode,
			Code:        "empty_token",
			Description: "Bitrix24 returned no access_token",
			Method:      "oauth/token",
		}
	}
	return &tr, nil
}

// ValidateAccessToken checks that Bitrix24 accepts the access token on this
// portal domain before GoClaw persists it as install state.
func (c *Client) ValidateAccessToken(ctx context.Context, accessToken string) error {
	if c.domain == "" {
		return errors.New("bitrix24 client: domain not set")
	}
	if strings.TrimSpace(accessToken) == "" {
		return errors.New("bitrix24 client: access token required")
	}

	form := url.Values{"auth": {accessToken}}
	endpoint := "https://" + c.domain + "/rest/profile.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("bitrix24 profile http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("bitrix24 profile read: %w", err)
	}

	var rr RawResult
	if err := json.Unmarshal(body, &rr); err != nil {
		return fmt.Errorf("bitrix24 profile decode (status=%d): %w: %s", resp.StatusCode, err, truncate(string(body), 200))
	}
	if resp.StatusCode >= 400 || rr.Error != "" {
		return &APIError{
			Status:      resp.StatusCode,
			Code:        rr.Error,
			Description: rr.ErrorDescription,
			Method:      "profile",
		}
	}
	return nil
}

// Call performs an authenticated REST call against this client's portal.
//
// Phase 01 ships Call for completeness — higher phases (03+) use it for
// every im./imbot./disk./user. method. The method takes a plain map[string]any
// so callers can pass whatever shape Bitrix expects without a generic type.
// The client will:
//  1. Pull a fresh access token via the bound Portal.
//  2. POST form-encoded params to https://<domain>/rest/<method>.json.
//  3. Decode the envelope and surface error + description via *APIError.
//
// If the Portal is not yet bound (e.g. Phase 01 unit test), Call returns
// an error instead of silently no-op'ing.
func (c *Client) Call(ctx context.Context, method string, params map[string]any) (*RawResult, error) {
	if c.domain == "" {
		return nil, errors.New("bitrix24 client: domain not set")
	}
	if method == "" {
		return nil, errors.New("bitrix24 client: method required")
	}
	c.portalMu.RLock()
	portal := c.portal
	c.portalMu.RUnlock()
	if portal == nil {
		return nil, errors.New("bitrix24 client: portal not bound")
	}

	token, err := portal.AccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("bitrix24 %s: get token: %w", method, err)
	}

	form, err := encodeParams(params)
	if err != nil {
		return nil, fmt.Errorf("bitrix24 %s: encode params: %w", method, err)
	}
	form.Set("auth", token)

	endpoint := "https://" + c.domain + "/rest/" + method + ".json"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bitrix24 %s http: %w", method, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20)) // 16 MiB — disk file metadata can be chunky
	if err != nil {
		return nil, fmt.Errorf("bitrix24 %s read: %w", method, err)
	}

	var rr RawResult
	if err := json.Unmarshal(body, &rr); err != nil {
		return nil, fmt.Errorf("bitrix24 %s decode (status=%d): %w: %s", method, resp.StatusCode, err, truncate(string(body), 200))
	}
	if resp.StatusCode >= 400 || rr.Error != "" {
		return &rr, &APIError{
			Status:      resp.StatusCode,
			Code:        rr.Error,
			Description: rr.ErrorDescription,
			Method:      method,
		}
	}
	return &rr, nil
}

// encodeParams converts a map[string]any into url.Values following Bitrix24's
// convention for nested params (PHP-style a[b][c]=v keys). Implementation is
// recursive and tolerant: map, []any, primitives, and json-serialisable
// structs are all supported.
func encodeParams(params map[string]any) (url.Values, error) {
	out := url.Values{}
	for k, v := range params {
		if err := encodeParamValue(out, k, v); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func encodeParamValue(dst url.Values, key string, val any) error {
	switch v := val.(type) {
	case nil:
		return nil
	case string:
		dst.Set(key, v)
	case bool:
		if v {
			dst.Set(key, "Y")
		} else {
			dst.Set(key, "N")
		}
	case int:
		dst.Set(key, fmt.Sprintf("%d", v))
	case int64:
		dst.Set(key, fmt.Sprintf("%d", v))
	case float64:
		dst.Set(key, trimFloat(v))
	case json.Number:
		dst.Set(key, v.String())
	case []string:
		for i, s := range v {
			dst.Set(fmt.Sprintf("%s[%d]", key, i), s)
		}
	case []any:
		for i, item := range v {
			if err := encodeParamValue(dst, fmt.Sprintf("%s[%d]", key, i), item); err != nil {
				return err
			}
		}
	case map[string]any:
		for mk, mv := range v {
			if err := encodeParamValue(dst, fmt.Sprintf("%s[%s]", key, mk), mv); err != nil {
				return err
			}
		}
	default:
		// Fall back to JSON for unsupported types (structs, etc).
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("encode param %q: %w", key, err)
		}
		dst.Set(key, string(b))
	}
	return nil
}

func trimFloat(f float64) string {
	s := fmt.Sprintf("%g", f)
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
