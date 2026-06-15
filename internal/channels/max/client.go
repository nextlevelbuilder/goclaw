package max

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/http2"
)

// Default base URL for the Max Bot API. Overridable for tests.
const defaultBaseURL = "https://platform-api.max.ru"

// Default API version recommended by Max docs (sent as query param ?v=...).
// Optional in current API but documented as forward-compatible practice.
const defaultAPIVersion = "0.0.0"

// =====================================================================
// HTTP transport configuration
//
// The Max API uses long-polling on GET /updates (typically with a
// timeout=30s server-hold). The default net/http transport has two
// well-known weaknesses for this workload that we observed in production:
//
//   1. TCP-level keepalives use the OS default (Linux: tcp_keepalive_time
//      = 7200s). For long-polls that hold a TCP connection idle for 30s
//      while waiting for events, this is irrelevant — but it also means
//      NAT devices / stateful firewalls in front of the egress can drop
//      the flow state silently, leaving Go thinking the connection is
//      alive while the path is dead.
//
//   2. HTTP/2 is enabled by default but PING-based health checks are
//      NOT configured. Once a half-broken connection enters the
//      multiplex pool, every subsequent long-poll sent over it hangs
//      for the full Client.Timeout (we observed 120s+).
//
// The transport below fixes both — see newDefaultHTTPClient.
const (
	defaultDialTimeout         = 10 * time.Second
	defaultDialKeepAlive       = 15 * time.Second
	defaultTLSHandshakeTimeout = 10 * time.Second
	defaultResponseHeader      = 45 * time.Second
	defaultIdleConnTimeout     = 90 * time.Second
	defaultClientTimeout       = 45 * time.Second
	defaultH2ReadIdleTimeout   = 15 * time.Second
	defaultH2PingTimeout       = 10 * time.Second
	defaultMaxIdleConnsPerHost = 4
)

// newDefaultHTTPClient builds the production HTTP client used when the
// caller does not pass WithHTTPClient. It enables HTTP/2 PING-based
// health checks (detect half-broken connections within ~25s instead of
// waiting for Client.Timeout) and overrides the kernel's TCP keepalive
// defaults so NAT/firewall flow state stays warm during long-polls.
func newDefaultHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   defaultDialTimeout,
			KeepAlive: defaultDialKeepAlive,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   defaultMaxIdleConnsPerHost,
		IdleConnTimeout:       defaultIdleConnTimeout,
		TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: defaultResponseHeader,
	}

	// Configure HTTP/2 PING-based health checks. ConfigureTransports
	// upgrades the transport in-place. If this fails we still want a
	// working client (HTTP/1.1 fallback is acceptable degraded mode).
	if h2t, err := http2.ConfigureTransports(transport); err == nil {
		h2t.ReadIdleTimeout = defaultH2ReadIdleTimeout
		h2t.PingTimeout = defaultH2PingTimeout
	}

	return &http.Client{
		Transport: transport,
		Timeout:   defaultClientTimeout,
	}
}

// Client is a thin HTTP client for the Max Messenger Bot API.
//
// Authorization: Max requires the raw token in the Authorization header,
// WITHOUT the "Bearer " prefix. This is unusual and intentional per docs:
// https://dev.max.ru/docs-api
//
// All methods accept a context and respect its deadline/cancellation.
// On 429 Too Many Requests, the client performs bounded exponential
// backoff up to maxRetries attempts.
type Client struct {
	token      string
	baseURL    string
	apiVersion string
	httpClient *http.Client
	maxRetries int
}

// ClientOption configures the Client at construction time.
type ClientOption func(*Client)

// WithBaseURL overrides the API base URL (for tests or alternate environments).
func WithBaseURL(u string) ClientOption {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient injects a custom *http.Client (for tests or custom transport).
func WithHTTPClient(h *http.Client) ClientOption {
	return func(c *Client) { c.httpClient = h }
}

// WithMaxRetries sets the maximum retry count for 429 responses. Default 3.
func WithMaxRetries(n int) ClientOption {
	return func(c *Client) { c.maxRetries = n }
}

// WithAPIVersion overrides the version query parameter sent with requests.
func WithAPIVersion(v string) ClientOption {
	return func(c *Client) { c.apiVersion = v }
}

// NewClient constructs a Max API client. Token must be non-empty.
func NewClient(token string, opts ...ClientOption) *Client {
	c := &Client{
		token:      token,
		baseURL:    defaultBaseURL,
		apiVersion: defaultAPIVersion,
		httpClient: newDefaultHTTPClient(),
		maxRetries: 3,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// CloseIdleConnections releases any pooled connections that are not
// currently in use. Called by pollLoop after a transient network error
// to ensure the next retry establishes a fresh TCP/TLS session,
// avoiding the "half-broken connection returned from pool" failure mode.
//
// Safe to call concurrently with in-flight requests: only IDLE
// connections are closed; active long-polls continue undisturbed.
func (c *Client) CloseIdleConnections() {
	if c.httpClient == nil {
		return
	}
	type idleCloser interface{ CloseIdleConnections() }
	if ic, ok := c.httpClient.Transport.(idleCloser); ok {
		ic.CloseIdleConnections()
	}
}

// GetMe calls GET /me and returns the bot's profile info.
// Used at Start() to validate the token and capture bot user_id/username.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	var resp User
	if err := c.do(ctx, http.MethodGet, "/me", nil, nil, &resp); err != nil {
		return nil, fmt.Errorf("get me: %w", err)
	}
	return &resp, nil
}

// GetUpdatesParams parameterises GET /updates per Max docs.
type GetUpdatesParams struct {
	// Limit is the max updates per response (1-1000, default 100).
	Limit int

	// Timeout is the long-poll timeout in seconds (0-90, default 30).
	Timeout int

	// Marker is the sequence cursor; nil = newest unread.
	Marker *int64

	// Types optionally filters update types (e.g. "message_created").
	Types []string
}

// GetUpdates calls GET /updates and returns the next batch + new marker.
// Long-polls up to params.Timeout seconds (default 30).
func (c *Client) GetUpdates(ctx context.Context, p GetUpdatesParams) (*UpdatesResponse, error) {
	q := url.Values{}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Timeout > 0 {
		q.Set("timeout", strconv.Itoa(p.Timeout))
	}
	if p.Marker != nil {
		q.Set("marker", strconv.FormatInt(*p.Marker, 10))
	}
	if len(p.Types) > 0 {
		q.Set("types", strings.Join(p.Types, ","))
	}

	var resp UpdatesResponse
	if err := c.do(ctx, http.MethodGet, "/updates", q, nil, &resp); err != nil {
		return nil, fmt.Errorf("get updates: %w", err)
	}
	return &resp, nil
}

// SendMessageParams identifies recipient (user OR chat) and the body to send.
type SendMessageParams struct {
	// Exactly one of UserID / ChatID must be non-zero.
	UserID int64
	ChatID int64

	// DisableLinkPreview suppresses URL preview generation.
	DisableLinkPreview bool

	// Body is the message content.
	Body SendMessageRequest
}

// SendMessage calls POST /messages with appropriate user_id/chat_id query params.
// Returns the full response including top-level message_id (needed for edits).
func (c *Client) SendMessage(ctx context.Context, p SendMessageParams) (*SendMessageResponse, error) {
	if (p.UserID == 0 && p.ChatID == 0) || (p.UserID != 0 && p.ChatID != 0) {
		return nil, errors.New("max client: exactly one of UserID/ChatID must be set")
	}

	q := url.Values{}
	if p.UserID != 0 {
		q.Set("user_id", strconv.FormatInt(p.UserID, 10))
	}
	if p.ChatID != 0 {
		q.Set("chat_id", strconv.FormatInt(p.ChatID, 10))
	}
	if p.DisableLinkPreview {
		q.Set("disable_link_preview", "true")
	}

	var resp SendMessageResponse
	if err := c.do(ctx, http.MethodPost, "/messages", q, p.Body, &resp); err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}
	return &resp, nil
}

// EditMessageParams identifies a message to edit by message_id.
type EditMessageParams struct {
	MessageID string
	Body      EditMessageRequest
}

// EditMessage calls PUT /messages — used by streaming preview to update text in-place.
func (c *Client) EditMessage(ctx context.Context, p EditMessageParams) (*SendMessageResponse, error) {
	if p.MessageID == "" {
		return nil, errors.New("max client: MessageID is required for EditMessage")
	}

	q := url.Values{}
	q.Set("message_id", p.MessageID)

	var resp SendMessageResponse
	if err := c.do(ctx, http.MethodPut, "/messages", q, p.Body, &resp); err != nil {
		return nil, fmt.Errorf("edit message: %w", err)
	}
	return &resp, nil
}

// DeleteMessage calls DELETE /messages.
func (c *Client) DeleteMessage(ctx context.Context, messageID string) error {
	if messageID == "" {
		return errors.New("max client: messageID is required")
	}
	q := url.Values{}
	q.Set("message_id", messageID)
	return c.do(ctx, http.MethodDelete, "/messages", q, nil, nil)
}

// PostAction sends a typing/sending indicator into a chat.
// action: "typing_on" | "sending_photo" | "sending_video" | "sending_audio" | "sending_file" | "mark_seen"
func (c *Client) PostAction(ctx context.Context, chatID int64, action string) error {
	path := fmt.Sprintf("/chats/%d/actions", chatID)
	body := map[string]string{"action": action}
	return c.do(ctx, http.MethodPost, path, nil, body, nil)
}

// AnswerCallback responds to a button-click callback.
// Either notification (toast text) or a new SendMessageRequest can be returned.
func (c *Client) AnswerCallback(ctx context.Context, callbackID, notification string, msg *SendMessageRequest) error {
	q := url.Values{}
	q.Set("callback_id", callbackID)

	body := map[string]any{}
	if notification != "" {
		body["notification"] = notification
	}
	if msg != nil {
		body["message"] = msg
	}
	return c.do(ctx, http.MethodPost, "/answers", q, body, nil)
}

// SubscribeWebhook registers a webhook URL to receive updates.
// The URL must be HTTPS (self-signed certificates are accepted).
func (c *Client) SubscribeWebhook(ctx context.Context, hookURL string, updateTypes []string) error {
	body := SubscriptionRequest{
		URL:         hookURL,
		UpdateTypes: updateTypes,
	}
	return c.do(ctx, http.MethodPost, "/subscriptions", nil, body, nil)
}

// UnsubscribeWebhook deregisters a previously subscribed webhook URL.
func (c *Client) UnsubscribeWebhook(ctx context.Context, hookURL string) error {
	q := url.Values{}
	q.Set("url", hookURL)
	return c.do(ctx, http.MethodDelete, "/subscriptions", q, nil, nil)
}

// =====================================================================
// Raw HTTP — used for asset downloads where the URL is provided by Max
// API responses (e.g. attachment URLs, upload service URLs).
// =====================================================================

// DownloadFile fetches a URL through the same HTTP transport used for API
// calls. The URL is treated as opaque: no auth header is added (Max
// attachment URLs are pre-signed; upload service URLs use their own
// query-string tokens).
//
// Caller is responsible for:
//   - closing resp.Body
//   - validating Content-Length / status code
//   - applying any size cap appropriate to the call site
//
// This method exists so package-private code in other files (media_download)
// doesn't reach into c.httpClient directly. If we add transport middleware
// (e.g. otelhttp) in NewClient, downloads automatically pick it up.
func (c *Client) DownloadFile(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build download request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	return resp, nil
}

// =====================================================================
// Internal: do — single source of truth for HTTP I/O.
// =====================================================================

// do performs an HTTP request, handles auth, JSON encoding/decoding, and
// retries on 429 Too Many Requests.
//
// query is appended to URL; body is JSON-encoded if non-nil; out is JSON-decoded
// from the response body if non-nil and the response is 2xx.
func (c *Client) do(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	body any,
	out any,
) error {
	endpoint, err := c.buildURL(path, query)
	if err != nil {
		return err
	}

	var bodyBytes []byte
	if body != nil {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
	}

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s, capped at 30s.
			delay := time.Duration(1<<min(attempt-1, 5)) * time.Second
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		var reqBody io.Reader
		if bodyBytes != nil {
			reqBody = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, reqBody)
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		// Max requires raw token, no "Bearer " prefix.
		req.Header.Set("Authorization", c.token)
		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			// Network/transport errors — return without retry; pollLoop handles.
			return fmt.Errorf("http: %w", err)
		}

		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read body: %w", readErr)
		}

		// Successful: 2xx
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out != nil && len(respBody) > 0 {
				if err := json.Unmarshal(respBody, out); err != nil {
					return fmt.Errorf("decode response: %w (body: %s)",
						err, truncateForLog(respBody, 500))
				}
			}
			return nil
		}

		// Rate limited — retry if budget remains.
		if resp.StatusCode == http.StatusTooManyRequests && attempt < c.maxRetries {
			continue
		}

		// Other error — parse APIError if possible.
		apiErr := &APIError{Code: strconv.Itoa(resp.StatusCode)}
		if len(respBody) > 0 {
			_ = json.Unmarshal(respBody, apiErr)
		}
		return apiErr
	}

	return fmt.Errorf("exhausted %d retries", c.maxRetries)
}

// buildURL composes the full endpoint URL with query parameters.
func (c *Client) buildURL(path string, query url.Values) (string, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return "", fmt.Errorf("invalid url %s%s: %w", c.baseURL, path, err)
	}
	if query == nil {
		query = url.Values{}
	}
	if c.apiVersion != "" && query.Get("v") == "" {
		query.Set("v", c.apiVersion)
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

// truncateForLog returns up to n bytes of b for inclusion in error strings.
func truncateForLog(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "...(truncated)"
}
