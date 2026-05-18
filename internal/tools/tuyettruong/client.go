// Package tuyettruong wires goclaw tools to the tuyettruong Next.js HTTP API.
// Auth model: the agent has its own Supabase user account (role=admin). The
// shared Client logs in once at process start, caches the access_token, and
// auto-refreshes a few minutes before expiry. Every API call sends the JWT
// as `Authorization: Bearer ...` — same path a logged-in admin would take
// from a browser. No bot-specific headers; no separate machine API key.
package tuyettruong

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	defaultTimeout = 15 * time.Second

	envAPIBase       = "TUYETTRUONG_API_BASE"
	envSupabaseURL   = "TUYETTRUONG_SUPABASE_URL"
	envSupabaseAnon  = "TUYETTRUONG_SUPABASE_ANON_KEY"
	envAgentEmail    = "TUYETTRUONG_AGENT_EMAIL"
	envAgentPassword = "TUYETTRUONG_AGENT_PASSWORD"

	// Refresh the token N seconds before it expires to avoid mid-call expiry.
	refreshLeadSeconds = 300
)

// BotRole is retained for tool signatures but is currently a no-op — every
// request uses the single agent JWT. Kept so we can wire per-role tokens in
// the future without re-touching every tool file.
type BotRole int

const (
	RoleAdmin BotRole = iota
	RoleSales
)

type tokenBundle struct {
	accessToken  string
	refreshToken string
	expiresAt    time.Time
}

// Client is a small typed wrapper around net/http for tuyettruong endpoints.
// Shared across all tools — construct once at registration. Holds the cached
// JWT and refreshes it on demand.
type Client struct {
	baseURL     string
	supabaseURL string
	anonKey     string
	email       string
	password    string
	http        *http.Client

	mu    sync.Mutex
	token *tokenBundle
}

// NewClient resolves config from env. Returns nil if any required var is
// missing — RegisterAll will then skip tool registration and log a friendly
// reason so goclaw still boots cleanly.
func NewClient() *Client {
	base := strings.TrimRight(os.Getenv(envAPIBase), "/")
	sbURL := strings.TrimRight(os.Getenv(envSupabaseURL), "/")
	anon := os.Getenv(envSupabaseAnon)
	email := os.Getenv(envAgentEmail)
	pwd := os.Getenv(envAgentPassword)
	if base == "" || sbURL == "" || anon == "" || email == "" || pwd == "" {
		return nil
	}
	return &Client{
		baseURL:     base,
		supabaseURL: sbURL,
		anonKey:     anon,
		email:       email,
		password:    pwd,
		http:        &http.Client{Timeout: defaultTimeout},
	}
}

// MissingEnv returns the names of any required env vars that are unset.
// Used by RegisterAll for a helpful log message.
func MissingEnv() []string {
	want := []string{envAPIBase, envSupabaseURL, envSupabaseAnon, envAgentEmail, envAgentPassword}
	missing := []string{}
	for _, k := range want {
		if os.Getenv(k) == "" {
			missing = append(missing, k)
		}
	}
	return missing
}

// ensureToken returns a valid access token, performing a login or refresh as
// needed. Safe for concurrent callers (mutex-guarded).
func (c *Client) ensureToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != nil && time.Now().Before(c.token.expiresAt) {
		return c.token.accessToken, nil
	}
	if c.token != nil && c.token.refreshToken != "" {
		if err := c.refresh(ctx); err == nil {
			return c.token.accessToken, nil
		}
		// fall through to fresh password login
	}
	if err := c.login(ctx); err != nil {
		return "", err
	}
	return c.token.accessToken, nil
}

func (c *Client) login(ctx context.Context) error {
	url := c.supabaseURL + "/auth/v1/token?grant_type=password"
	body, _ := json.Marshal(map[string]string{
		"email":    c.email,
		"password": c.password,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build login: %w", err)
	}
	req.Header.Set("apikey", c.anonKey)
	req.Header.Set("Content-Type", "application/json")
	return c.consumeTokenResponse(req)
}

func (c *Client) refresh(ctx context.Context) error {
	url := c.supabaseURL + "/auth/v1/token?grant_type=refresh_token"
	body, _ := json.Marshal(map[string]string{"refresh_token": c.token.refreshToken})
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build refresh: %w", err)
	}
	req.Header.Set("apikey", c.anonKey)
	req.Header.Set("Content-Type", "application/json")
	return c.consumeTokenResponse(req)
}

func (c *Client) consumeTokenResponse(req *http.Request) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("supabase token: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("supabase token → %d: %s", resp.StatusCode, truncate(string(respBody), 300))
	}
	var parsed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return fmt.Errorf("decode token: %w", err)
	}
	if parsed.AccessToken == "" {
		return fmt.Errorf("supabase returned empty access_token")
	}
	if parsed.ExpiresIn <= 0 {
		parsed.ExpiresIn = 3600
	}
	c.token = &tokenBundle{
		accessToken:  parsed.AccessToken,
		refreshToken: parsed.RefreshToken,
		expiresAt:    time.Now().Add(time.Duration(parsed.ExpiresIn-refreshLeadSeconds) * time.Second),
	}
	return nil
}

// Do executes a JSON HTTP call against the tuyettruong API and decodes the
// response into out (if non-nil). Sends the agent JWT for any path that
// needs auth; public store endpoints accept it too without complaint, so we
// always attach it.
func (c *Client) Do(
	ctx context.Context,
	_ BotRole,
	method, path string,
	body any,
	out any,
) error {
	if c == nil {
		return fmt.Errorf("tuyettruong client not configured")
	}
	token, err := c.ensureToken(ctx)
	if err != nil {
		return err
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	// One retry on 401 — token may have rotated since the cached check.
	if resp.StatusCode == 401 {
		_ = resp.Body.Close()
		c.mu.Lock()
		c.token = nil
		c.mu.Unlock()
		token, err = c.ensureToken(ctx)
		if err != nil {
			return err
		}
		req2, _ := http.NewRequestWithContext(ctx, method, url, bytesReaderFromAny(body))
		req2.Header.Set("Authorization", "Bearer "+token)
		req2.Header.Set("Content-Type", "application/json")
		resp, err = c.http.Do(req2)
		if err != nil {
			return fmt.Errorf("http (retry): %w", err)
		}
		defer resp.Body.Close()
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("tuyettruong %s %s → %d: %s", method, path, resp.StatusCode, truncate(string(respBody), 500))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func bytesReaderFromAny(body any) io.Reader {
	if body == nil {
		return nil
	}
	b, _ := json.Marshal(body)
	return bytes.NewReader(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// jsonResult marshals an arbitrary value and returns it wrapped in a Tool
// Result with both ForLLM and ForUser populated.
func jsonResult(v any) *tools.Result {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errorResult(fmt.Errorf("marshal result: %w", err))
	}
	s := string(b)
	return &tools.Result{ForLLM: s, ForUser: s}
}

func errorResult(err error) *tools.Result {
	msg := err.Error()
	return &tools.Result{ForLLM: msg, ForUser: msg, IsError: true, Err: err}
}
