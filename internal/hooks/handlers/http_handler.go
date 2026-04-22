package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/security"
)

// HTTPHandler posts event data to an HTTP endpoint and interprets the JSON
// response as a hook decision. Implements hooks.Handler.
//
// SSRF hardening: caller must supply a Client built with a DialContext that
// pins the resolved IP and blocks loopback/link-local/private ranges.
// NewSSRFSafeClient() returns a preconfigured client for production use.
type HTTPHandler struct {
	// EncryptKey is the AES-256-GCM key used to decrypt Authorization header
	// values stored encrypted in cfg.Config["headers"]. Empty = no decryption.
	EncryptKey string
	// Client is the HTTP client used for outbound requests. Must not be nil.
	// Use NewSSRFSafeClient() for production. Tests may supply a mock.
	Client *http.Client
}

// httpResponse is the expected JSON body from a webhook endpoint.
type httpResponse struct {
	Decision         *string        `json:"decision"`
	AdditionalCtx    string         `json:"additionalContext"`
	UpdatedInput     map[string]any `json:"updatedInput"`
	Continue         *bool          `json:"continue"`
}

// Execute implements hooks.Handler.
func (h *HTTPHandler) Execute(ctx context.Context, cfg hooks.HookConfig, ev hooks.Event) (hooks.Decision, error) {
	urlStr, _ := cfg.Config["url"].(string)
	if urlStr == "" {
		return hooks.DecisionError, fmt.Errorf("hook: http handler: missing 'url' in config")
	}

	// SSRF validation: resolve host once and pin the IP.
	// When a custom Client is set (e.g. in tests via httptest), the client's
	// own transport handles routing; Validate is still called so that
	// production-path URLs are always vetted. Tests that use httptest must
	// call security.SetAllowLoopbackForTest(true) to bypass the loopback block.
	parsedURL, pinnedIP, err := security.Validate(urlStr)
	if err != nil {
		return hooks.DecisionError, fmt.Errorf("hook: http handler: ssrf check: %w", err)
	}
	_ = parsedURL // URL string already validated; we keep urlStr for the request.

	// Stash pinned IP into context so NewSafeClient's DialContext can read it.
	ctx = security.WithPinnedIP(ctx, pinnedIP)

	body, err := json.Marshal(ev)
	if err != nil {
		return hooks.DecisionError, fmt.Errorf("hook: http handler: marshal event: %w", err)
	}

	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	decision, err := h.doRequest(ctx, client, urlStr, cfg.Config, body)
	if err != nil {
		// Retry once on 5xx / network error with 1s backoff.
		select {
		case <-ctx.Done():
			return hooks.DecisionError, ctx.Err()
		case <-time.After(1 * time.Second):
		}
		decision, err = h.doRequest(ctx, client, urlStr, cfg.Config, body)
		if err != nil {
			return hooks.DecisionError, fmt.Errorf("hook: http handler: %w", err)
		}
	}
	return decision, nil
}

func (h *HTTPHandler) doRequest(ctx context.Context, client *http.Client, urlStr string, cfgMap map[string]any, body []byte) (hooks.Decision, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, bytes.NewReader(body))
	if err != nil {
		return hooks.DecisionError, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Apply configured headers; decrypt Authorization values if key is set.
	if raw, ok := cfgMap["headers"]; ok {
		if headers, ok := raw.(map[string]any); ok {
			for k, v := range headers {
				val, _ := v.(string)
				if k == "Authorization" && h.EncryptKey != "" && val != "" {
					if dec, err := crypto.Decrypt(val, h.EncryptKey); err == nil {
						val = dec
					}
				}
				req.Header.Set(k, val)
			}
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return hooks.DecisionError, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return hooks.DecisionError, fmt.Errorf("server error %d", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return hooks.DecisionError, fmt.Errorf("client error %d (no retry)", resp.StatusCode)
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		return hooks.DecisionError, fmt.Errorf("read response: %w", err)
	}

	if len(respBody) == 0 {
		return hooks.DecisionAllow, nil
	}

	var hr httpResponse
	if err := json.Unmarshal(respBody, &hr); err != nil {
		// Non-JSON 2xx is treated as allow.
		return hooks.DecisionAllow, nil
	}

	// Explicit "decision" field takes priority.
	if hr.Decision != nil {
		switch hooks.Decision(*hr.Decision) {
		case hooks.DecisionBlock:
			return hooks.DecisionBlock, nil
		default:
			return hooks.DecisionAllow, nil
		}
	}
	// Fallback: "continue": false → block.
	if hr.Continue != nil && !*hr.Continue {
		return hooks.DecisionBlock, nil
	}
	return hooks.DecisionAllow, nil
}
