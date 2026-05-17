// Package tuyettruong wires goclaw tools to the tuyettruong Next.js HTTP API.
// Each tool calls the shared Client; the Client knows how to attach the bot
// API key + actor header derived from goclaw session context.
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
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	defaultTimeout = 15 * time.Second
	envAPIBase     = "TUYETTRUONG_API_BASE"
	envAdminKey    = "TUYETTRUONG_ADMIN_BOT_API_KEY"
	envSalesKey    = "TUYETTRUONG_SALES_BOT_API_KEY"
)

// BotRole identifies which API key the tool should send. Each tool declares
// its required role at construction time.
type BotRole int

const (
	RoleAdmin BotRole = iota
	RoleSales
)

// Client is a small typed wrapper around net/http for tuyettruong endpoints.
// Shared across all tools — construct once at registration, never holds per-call state.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient resolves base URL from env. Returns nil if not configured (tools
// will short-circuit with a clear error). One client per goclaw process is fine.
func NewClient() *Client {
	base := strings.TrimRight(os.Getenv(envAPIBase), "/")
	if base == "" {
		return nil
	}
	return &Client{
		baseURL: base,
		http:    &http.Client{Timeout: defaultTimeout},
	}
}

func apiKeyFor(role BotRole) string {
	switch role {
	case RoleSales:
		return os.Getenv(envSalesKey)
	default:
		return os.Getenv(envAdminKey)
	}
}

// actorFromCtx builds the X-Bot-Actor-Id header value from goclaw context.
// Format: "<platform>:<platformUserId>". Returns "" if we can't determine —
// the API will reject; this is preferred over silently impersonating someone.
func actorFromCtx(ctx context.Context) string {
	channel := tools.ToolChannelFromCtx(ctx)
	chatID := tools.ToolChatIDFromCtx(ctx)
	if channel == "" || chatID == "" {
		return ""
	}
	// Telegram chatID for DMs is the user_id; for groups it's negative
	// "-12345:topic:99" form. For now we expect admin/sales DM only.
	if idx := strings.IndexByte(chatID, ':'); idx > 0 {
		chatID = chatID[:idx]
	}
	switch channel {
	case "telegram":
		return "tg:" + chatID
	case "zalo_personal":
		return "zalo_personal:" + chatID
	case "zalo_oa":
		return "zalo_oa:" + chatID
	default:
		return ""
	}
}

// Do executes a JSON HTTP call against the tuyettruong API and decodes the
// response into out (if non-nil). Returns a Result-shaped error on transport
// or non-2xx responses so callers can return it directly.
func (c *Client) Do(
	ctx context.Context,
	role BotRole,
	method, path string,
	body any,
	out any,
) error {
	if c == nil {
		return fmt.Errorf("tuyettruong client not configured (set %s)", envAPIBase)
	}
	key := apiKeyFor(role)
	if key == "" {
		switch role {
		case RoleSales:
			return fmt.Errorf("missing %s env", envSalesKey)
		default:
			return fmt.Errorf("missing %s env", envAdminKey)
		}
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
	req.Header.Set("x-api-key", key)
	req.Header.Set("Content-Type", "application/json")
	if actor := actorFromCtx(ctx); actor != "" {
		req.Header.Set("X-Bot-Actor-Id", actor)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

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
