package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// DingTalk splits its surface across two hosts, and a caller needs a different
// token for each.
//
//   - apiBase  (api.dingtalk.com)  — the v1.0 OpenAPI: tokens, AI Cards, robot
//     messages, media download. Auth: header x-acs-dingtalk-access-token.
//   - oapiBase (oapi.dingtalk.com) — the legacy API. We touch it for exactly
//     one thing, media upload, which has no v1.0 equivalent.
//     Auth: access_token query parameter.
const (
	defaultAPIBase  = "https://api.dingtalk.com"
	defaultOAPIBase = "https://oapi.dingtalk.com"

	// tokenRefreshMargin refetches a token slightly before it expires so an
	// in-flight request never races the expiry.
	tokenRefreshMargin = 60 * time.Second

	// defaultOAPITokenTTL is used when gettoken omits expires_in.
	defaultOAPITokenTTL = 7200 * time.Second

	defaultHTTPTimeout = 30 * time.Second
)

// Client talks to both DingTalk HTTP surfaces on behalf of one app.
//
// apiBase and oapiBase are fields rather than constants so tests can point them
// at an httptest server — the same seam Feishu's LarkClient uses.
type Client struct {
	clientID     string
	clientSecret string

	apiBase  string
	oapiBase string
	http     *http.Client

	apiTok  tokenCache
	oapiTok tokenCache
}

// NewClient builds a Client for the given app credentials.
func NewClient(clientID, clientSecret string) *Client {
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		apiBase:      defaultAPIBase,
		oapiBase:     defaultOAPIBase,
		http:         &http.Client{Timeout: defaultHTTPTimeout},
	}
}

// tokenCache holds one access token and its expiry.
//
// The mutex is held across the network fetch. That serializes concurrent
// callers behind a single refresh, which is the point: a hundred goroutines
// finding an expired token must produce one request, not a hundred.
type tokenCache struct {
	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// get returns a cached token, or calls fetch and caches the result. The clock
// is a parameter so tests can drive expiry without sleeping.
func (tc *tokenCache) get(now time.Time, fetch func() (string, time.Duration, error)) (string, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if tc.token != "" && now.Add(tokenRefreshMargin).Before(tc.expiresAt) {
		return tc.token, nil
	}

	token, ttl, err := fetch()
	if err != nil {
		return "", err
	}
	tc.token = token
	tc.expiresAt = now.Add(ttl)
	return token, nil
}

// apiToken returns a token for api.dingtalk.com.
func (c *Client) apiToken(ctx context.Context) (string, error) {
	return c.apiTok.get(time.Now(), func() (string, time.Duration, error) {
		body, err := json.Marshal(map[string]string{
			"appKey":    c.clientID,
			"appSecret": c.clientSecret,
		})
		if err != nil {
			return "", 0, fmt.Errorf("dingtalk: encode token request: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.apiBase+"/v1.0/oauth2/accessToken", bytes.NewReader(body))
		if err != nil {
			return "", 0, fmt.Errorf("dingtalk: build token request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return "", 0, fmt.Errorf("dingtalk: fetch api token: %w", err)
		}
		defer resp.Body.Close()

		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", 0, fmt.Errorf("dingtalk: read token response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return "", 0, fmt.Errorf("dingtalk: api token http %d: %s", resp.StatusCode, truncate(raw))
		}

		var out struct {
			AccessToken string `json:"accessToken"`
			ExpireIn    int64  `json:"expireIn"` // seconds
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return "", 0, fmt.Errorf("dingtalk: decode token response: %w", err)
		}
		if out.AccessToken == "" {
			return "", 0, fmt.Errorf("dingtalk: api token response has no accessToken: %s", truncate(raw))
		}
		return out.AccessToken, time.Duration(out.ExpireIn) * time.Second, nil
	})
}

// oapiToken returns a token for oapi.dingtalk.com (media upload only).
func (c *Client) oapiToken(ctx context.Context) (string, error) {
	return c.oapiTok.get(time.Now(), func() (string, time.Duration, error) {
		u := fmt.Sprintf("%s/gettoken?appkey=%s&appsecret=%s",
			c.oapiBase, url.QueryEscape(c.clientID), url.QueryEscape(c.clientSecret))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return "", 0, fmt.Errorf("dingtalk: build oapi token request: %w", err)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			return "", 0, fmt.Errorf("dingtalk: fetch oapi token: %w", err)
		}
		defer resp.Body.Close()

		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", 0, fmt.Errorf("dingtalk: read oapi token response: %w", err)
		}

		// The legacy API answers 200 even for failures; errcode carries the truth.
		var out struct {
			ErrCode     int    `json:"errcode"`
			ErrMsg      string `json:"errmsg"`
			AccessToken string `json:"access_token"`
			ExpiresIn   int64  `json:"expires_in"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return "", 0, fmt.Errorf("dingtalk: decode oapi token response: %w", err)
		}
		if out.ErrCode != 0 {
			return "", 0, fmt.Errorf("dingtalk: oapi token errcode=%d errmsg=%s", out.ErrCode, out.ErrMsg)
		}
		if out.AccessToken == "" {
			return "", 0, fmt.Errorf("dingtalk: oapi token response has no access_token")
		}

		ttl := time.Duration(out.ExpiresIn) * time.Second
		if ttl <= 0 {
			ttl = defaultOAPITokenTTL
		}
		return out.AccessToken, ttl, nil
	})
}

// apiError is the v1.0 OpenAPI failure envelope.
type apiError struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
	Request string `json:"requestid"`
}

func (e *apiError) Error() string {
	return fmt.Sprintf("dingtalk api: http %d code=%s message=%s requestid=%s",
		e.Status, e.Code, e.Message, e.Request)
}

// doAPI performs a request against api.dingtalk.com with the bearer header set.
// out may be nil when the caller does not need the body.
func (c *Client) doAPI(ctx context.Context, method, path string, in, out any) error {
	token, err := c.apiToken(ctx)
	if err != nil {
		return err
	}

	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("dingtalk: encode %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.apiBase+path, body)
	if err != nil {
		return fmt.Errorf("dingtalk: build %s %s: %w", method, path, err)
	}
	req.Header.Set("x-acs-dingtalk-access-token", token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("dingtalk: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("dingtalk: read %s %s: %w", method, path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &apiError{Status: resp.StatusCode}
		_ = json.Unmarshal(raw, apiErr) // best effort; body may not be JSON
		if apiErr.Message == "" {
			apiErr.Message = truncate(raw)
		}
		return apiErr
	}

	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("dingtalk: decode %s %s: %w", method, path, err)
	}
	return nil
}

// doOAPI performs a request against oapi.dingtalk.com with access_token in the
// query string, and surfaces the legacy errcode envelope as an error.
//
// Callers pass the already-built body reader and content type because the one
// caller (media upload) sends multipart, not JSON.
func (c *Client) doOAPI(ctx context.Context, method, path string, query url.Values,
	body io.Reader, contentType string, out any) error {

	token, err := c.oapiToken(ctx)
	if err != nil {
		return err
	}

	if query == nil {
		query = url.Values{}
	}
	query.Set("access_token", token)

	req, err := http.NewRequestWithContext(ctx, method, c.oapiBase+path+"?"+query.Encode(), body)
	if err != nil {
		return fmt.Errorf("dingtalk: build oapi %s %s: %w", method, path, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("dingtalk: oapi %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("dingtalk: read oapi %s %s: %w", method, path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dingtalk: oapi %s %s: http %d: %s", method, path, resp.StatusCode, truncate(raw))
	}

	// Check errcode before decoding into out: the legacy API returns 200 with
	// an error body, and a caller that only reads its own fields would see an
	// empty struct and carry on.
	var envelope struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.ErrCode != 0 {
		return fmt.Errorf("dingtalk: oapi %s %s: errcode=%d errmsg=%s",
			method, path, envelope.ErrCode, envelope.ErrMsg)
	}

	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("dingtalk: decode oapi %s %s: %w", method, path, err)
	}
	return nil
}

// truncate keeps error messages from carrying an entire response body.
func truncate(b []byte) string {
	const max = 256
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "…"
}
