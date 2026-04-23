package bitrix24

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// mcpClient talks to an MCP server's /api/auto-onboard endpoint.
//
// When a Bitrix24 user sends their first message, the bitrix24 channel
// doesn't yet know which per-user MCP API key that user should use. It POSTs
// to the MCP server — which is the authoritative identity provider for this
// integration — with the triggering user's OAuth tokens (access_token +
// refresh_token + expires_in) harvested from the Bitrix event auth block.
// The MCP server verifies the access_token against Bitrix `profile` to
// confirm the caller actually owns bitrix_user_id (Path B — no shared admin
// secret required), then upserts its own tenants + bitrix_users tables keyed
// by (domain, bitrix_user_id) and returns the per-user api_key we persist
// via mcp_user_credentials.
//
// The client is deliberately thin:
//   - No retries on 4xx (auth config wrong → operator must fix).
//   - One auto-retry on 5xx / network errors (honours a tight timeout so a
//     slow MCP server can't stall the Bitrix webhook handler).
//   - 404 with body {"error":"tenant_not_installed"} surfaces as
//     ErrTenantNotInstalled so the handler can reply with a specific
//     "reinstall the portal" message instead of a generic error.
//   - Other errors surface verbatim so ensureMCPCredentials can debounce
//     them via the pairing-style replyError gate.
type mcpClient struct {
	httpClient *http.Client
	baseURL    string
}

// ErrTenantNotInstalled is returned when the MCP server reports 404
// tenant_not_installed for the supplied domain. The channel handler treats
// this as a distinct failure mode (operator must reinstall the Bitrix app
// against the MCP server) and surfaces a user-visible reinstall message
// instead of the generic "try again later" debounce.
var ErrTenantNotInstalled = errors.New("mcp auto-onboard: tenant_not_installed")

// newMCPClient builds a client pointed at baseURL. The MCP server
// authenticates each auto-onboard call via the caller-supplied Bitrix
// access_token (Path B) — no shared admin secret is required.
// baseURL MUST be the MCP server root (e.g. https://mcp.example.com) — we
// append /api/auto-onboard internally so channel config stays minimal.
func newMCPClient(baseURL string, timeout time.Duration) *mcpClient {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &mcpClient{
		httpClient: &http.Client{Timeout: timeout},
		baseURL:    strings.TrimRight(baseURL, "/"),
	}
}

// autoOnboardRequest is the rev4 payload we POST to the MCP server.
//
// Required: Domain (tenant key on the MCP side), BitrixUserID (senderID),
// AccessToken + RefreshToken (forwarded from the Bitrix event so MCP can
// call Bitrix REST as that user). ExpiresIn is forwarded so MCP can
// compute a token_expires_at without its own clock drift surprising it.
// DisplayName is optional and lets MCP seed a friendly profile on insert.
type autoOnboardRequest struct {
	Domain       string `json:"domain"`
	BitrixUserID string `json:"bitrix_user_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	DisplayName  string `json:"display_name,omitempty"`
}

// autoOnboardResponse is what we expect back from the MCP server (rev4).
//
// APIKey is the per-user MCP credential we cache in mcp_user_credentials
// (the goclaw_user_id key on our side is the Bitrix senderID — we do NOT
// use UserID from the response, that's the MCP internal user row id).
// UserID + TenantID are echoed for observability / debugging. Created
// distinguishes the fresh-insert path from the token-refresh path.
type autoOnboardResponse struct {
	APIKey   string `json:"api_key"`
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
	Created  bool   `json:"created"`
}

// autoOnboard POSTs to {baseURL}/api/auto-onboard and returns the resolved
// per-user api_key. Fails closed on: missing base URL, missing domain /
// bitrix_user_id / tokens, 4xx (config error), 5xx after one retry,
// malformed JSON, or empty api_key.
//
// On 404 with body {"error":"tenant_not_installed"} returns
// ErrTenantNotInstalled so the caller can render a specific "admin must
// reinstall the portal" reply instead of the generic failure debounce.
func (c *mcpClient) autoOnboard(ctx context.Context, req autoOnboardRequest) (*autoOnboardResponse, error) {
	if c.baseURL == "" {
		return nil, errors.New("mcp auto-onboard: base URL not configured")
	}
	if req.Domain == "" || req.BitrixUserID == "" || req.AccessToken == "" || req.RefreshToken == "" {
		return nil, errors.New("mcp auto-onboard: domain, bitrix_user_id, access_token, refresh_token all required")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("mcp auto-onboard: marshal request: %w", err)
	}

	url := c.baseURL + "/api/auto-onboard"

	// One retry on 5xx / transport error. The webhook handler is on a tight
	// path (Bitrix24 expects a response in <30s and will retry on 5xx itself)
	// so we cap total attempts at 2 with a short backoff.
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("mcp auto-onboard: new request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("mcp auto-onboard: http: %w", err)
			continue
		}
		out, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<16)) // 64KiB cap
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("mcp auto-onboard: read body: %w", readErr)
			continue
		}

		switch {
		case resp.StatusCode == http.StatusOK, resp.StatusCode == http.StatusCreated:
			var ob autoOnboardResponse
			if err := json.Unmarshal(out, &ob); err != nil {
				return nil, fmt.Errorf("mcp auto-onboard: decode response (status %d): %w", resp.StatusCode, err)
			}
			if ob.APIKey == "" {
				return nil, fmt.Errorf("mcp auto-onboard: incomplete response (status %d): missing api_key", resp.StatusCode)
			}
			return &ob, nil
		case resp.StatusCode == http.StatusNotFound && isTenantNotInstalledBody(out):
			// MCP server doesn't know this portal → operator must run the
			// install flow on the MCP side before users can onboard.
			return nil, ErrTenantNotInstalled
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			// Auth / config errors are non-retryable — surface the body so
			// operators can see the domain / access_token mismatch.
			return nil, fmt.Errorf("mcp auto-onboard: %d %s: %s", resp.StatusCode, http.StatusText(resp.StatusCode), truncateMCPBody(string(out), 500))
		default:
			lastErr = fmt.Errorf("mcp auto-onboard: %d %s: %s", resp.StatusCode, http.StatusText(resp.StatusCode), truncateMCPBody(string(out), 500))
			// fall through to retry
		}
	}

	if lastErr == nil {
		lastErr = errors.New("mcp auto-onboard: unknown error")
	}
	return nil, lastErr
}

// isTenantNotInstalledBody reports whether a 404 body matches the MCP
// contract {"error":"tenant_not_installed", ...}. Parse failures return
// false so a generic 404 with a different body falls through to the
// normal 4xx error path (operator still sees the body in the log).
func isTenantNotInstalledBody(body []byte) bool {
	var env struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return false
	}
	return env.Error == "tenant_not_installed"
}

// truncateMCPBody keeps error messages a sensible length so we don't log a
// multi-MB MCP 5xx body on every failed onboard. Named distinctly from the
// package-local `truncate` in client.go (imbot payload truncation) so the
// two don't collide on a rename/refactor.
func truncateMCPBody(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
