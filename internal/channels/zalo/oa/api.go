package oa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// traceEnvVar=1 dumps raw Zalo response bodies via slog.Debug. Bodies
// contain PII (display names, IDs, message text) — do not enable in
// production without scrubbing review.
const traceEnvVar = "GOCLAW_ZALO_OA_TRACE"

var traceEnabled = os.Getenv(traceEnvVar) == "1"

const traceBodyMaxBytes = 256

func truncateForTrace(b []byte) string {
	if len(b) <= traceBodyMaxBytes {
		return string(b)
	}
	return string(b[:traceBodyMaxBytes]) + "…(truncated)"
}

// uploadTimeout accommodates multi-MB multipart uploads over slow mobile carriers.
const uploadTimeout = 60 * time.Second

// Client wraps Zalo's OAuth + OpenAPI hosts.
type Client struct {
	http      *http.Client
	oauthBase string
	apiBase   string
}

// NewClient returns a Client. Bounded idle-connection lifetime avoids
// stale connections that cause "awaiting headers" timeouts on Zalo's hosts.
func NewClient(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	return &Client{
		http:      &http.Client{Timeout: timeout, Transport: transport},
		oauthBase: defaultOAuthBase,
		apiBase:   defaultAPIBase,
	}
}

// ErrRateLimit signals HTTP 429; callers should back off.
var ErrRateLimit = errors.New("zalo_oa: rate limited")

// APIError is Zalo's non-zero error envelope.
type APIError struct {
	Code    int    `json:"error"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	if hint := Classify(e.Code).LLMHint; hint != "" {
		return fmt.Sprintf("zalo api error %d: %s — %s", e.Code, e.Message, hint)
	}
	return fmt.Sprintf("zalo api error %d: %s", e.Code, e.Message)
}

// Info returns the catalog classification for this error. Unknown codes
// return CodeInfo{Family: FamilyUnknown}.
func (e *APIError) Info() CodeInfo {
	if e == nil {
		return CodeInfo{}
	}
	return Classify(e.Code)
}

// isAuth reports whether the error is an invalid/expired access_token at
// the OpenAPI layer (refresh-token death is classifyRefreshError's job).
// Codes in errors.go; substring fallback for doc drift.
func (e *APIError) isAuth() bool {
	if e == nil {
		return false
	}
	if isAccessTokenInvalid(e.Code) {
		return true
	}
	msg := strings.ToLower(e.Message)
	return strings.Contains(msg, "access_token") && (strings.Contains(msg, "invalid") || strings.Contains(msg, "expired"))
}

// apiGet sends GET apiBase+path. access_token rides in the HEADER (the
// query-param form returns 404 on live OpenAPI endpoints). 429 → ErrRateLimit.
func (c *Client) apiGet(ctx context.Context, path string, query url.Values, accessToken string) (json.RawMessage, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("zalo_oa: empty access_token for %s", path)
	}
	u := c.apiBase + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request %s: %w", path, err)
	}
	req.Header.Set("access_token", accessToken)
	return c.do(req, path)
}

// apiPost POSTs application/json. access_token in HEADER. Errors expose
// path only — never full URL.
func (c *Client) apiPost(ctx context.Context, path string, body any, accessToken string) (json.RawMessage, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("zalo_oa: empty access_token for %s", path)
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase+path, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("build request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("access_token", accessToken)
	return c.do(req, path)
}

// apiPostMultipart uploads a single file as multipart/form-data.
func (c *Client) apiPostMultipart(ctx context.Context, path string, fileFieldName, fileName string, fileBytes []byte, fields map[string]string, accessToken string) (json.RawMessage, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("zalo_oa: empty access_token for %s", path)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			return nil, fmt.Errorf("write field %s: %w", k, err)
		}
	}
	part, err := mw.CreateFormFile(fileFieldName, fileName)
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(fileBytes); err != nil {
		return nil, fmt.Errorf("write file part: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("close multipart: %w", err)
	}

	// Per-request client: longer timeout for uploads, reuse shared Transport.
	uploadClient := &http.Client{Timeout: uploadTimeout, Transport: c.http.Transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase+path, &buf)
	if err != nil {
		return nil, fmt.Errorf("build upload request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("access_token", accessToken)
	return doRequest(uploadClient, req, path)
}

func (c *Client) do(req *http.Request, path string) (json.RawMessage, error) {
	return doRequest(c.http, req, path)
}

// doRequest runs the call and parses Zalo's envelope. Rewrites *url.Error.URL
// to path-only so any logged error never leaks tokens or full URLs.
func doRequest(client *http.Client, req *http.Request, path string) (json.RawMessage, error) {
	resp, err := client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			urlErr.URL = path
		}
		return nil, fmt.Errorf("zalo api %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if traceEnabled {
		slog.Debug("zalo_oa.raw_response", "path", path, "status", resp.StatusCode, "body", truncateForTrace(raw))
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("%w (path=%s)", ErrRateLimit, path)
	}
	if resp.StatusCode >= 400 {
		var env APIError
		if jerr := json.Unmarshal(raw, &env); jerr == nil && (env.Code != 0 || env.Message != "") {
			return nil, &env
		}
		return nil, fmt.Errorf("zalo api %s: http %d", path, resp.StatusCode)
	}
	var env APIError
	if jerr := json.Unmarshal(raw, &env); jerr == nil && env.Code != 0 {
		return nil, &env
	}
	return raw, nil
}

// postForm POSTs application/x-www-form-urlencoded with optional headers,
// returns the raw decoded JSON body. HTTP-status errors and Zalo's in-body
// error envelope (`error != 0`) are both surfaced as errors.
func (c *Client) postForm(ctx context.Context, fullURL string, headers map[string]string, body url.Values) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if traceEnabled {
		// Body omitted: OAuth responses carry plaintext access/refresh tokens.
		slog.Debug("zalo_oa.raw_response", "path", "oauth_token", "status", resp.StatusCode)
	}

	if resp.StatusCode >= 400 {
		var env APIError
		if jerr := json.Unmarshal(raw, &env); jerr == nil && (env.Code != 0 || env.Message != "") {
			return nil, &env
		}
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}

	// Zalo returns HTTP 200 with `{"error":N,"message":"..."}` for app errors.
	var env APIError
	if jerr := json.Unmarshal(raw, &env); jerr == nil && env.Code != 0 {
		return nil, &env
	}
	return raw, nil
}
