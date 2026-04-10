package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// Azure AD token endpoint for multi-tenant bots
	multiTenantTokenURL = "https://login.microsoftonline.com/botframework.com/oauth2/v2.0/token"
	tokenScope          = "https://api.botframework.com/.default"
	tokenRefreshMargin  = 5 * time.Minute // refresh before actual expiry

	// Retry constants for Bot Framework API (rate limit: 50 RPS, 1800/hr per thread)
	sendMaxRetries    = 3
	sendBaseDelay     = 500 * time.Millisecond
	sendMaxDelay      = 30 * time.Second
	sendMaxRetryAfter = 120 * time.Second // cap absurd Retry-After values
)

// teamsSendError wraps Bot Framework API errors with status code and Retry-After.
type teamsSendError struct {
	statusCode int
	retryAfter time.Duration
	body       string
}

func (e *teamsSendError) Error() string {
	return fmt.Sprintf("bot framework API %d: %s", e.statusCode, e.body)
}

// botClient acquires Azure AD tokens and sends replies via Bot Framework REST API.
type botClient struct {
	botID       string
	botPassword string
	tenantID    string // used for single-tenant token endpoint

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
	httpClient  *http.Client
}

func newBotClient(botID, botPassword, tenantID string) *botClient {
	return &botClient{
		botID:       botID,
		botPassword: botPassword,
		tenantID:    tenantID,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
	}
}

// SendReply sends a text reply to a Teams conversation with retry.
func (c *botClient) SendReply(ctx context.Context, serviceURL, conversationID, text string) error {
	activity := Activity{
		Type: "message",
		Text: text,
	}
	return c.retrySendActivity(ctx, serviceURL, conversationID, activity)
}

// SendActivity posts an Activity with retry logic for 429/5xx errors.
func (c *botClient) SendActivity(ctx context.Context, serviceURL, conversationID string, activity Activity) error {
	return c.retrySendActivity(ctx, serviceURL, conversationID, activity)
}

// retrySendActivity wraps doSendActivity with exponential backoff retry.
func (c *botClient) retrySendActivity(ctx context.Context, serviceURL, conversationID string, activity Activity) error {
	var lastErr error
	for attempt := range sendMaxRetries {
		err := c.doSendActivity(ctx, serviceURL, conversationID, activity)
		if err == nil {
			return nil
		}
		lastErr = err

		sendErr, ok := err.(*teamsSendError)
		if !ok || !isRetryableStatus(sendErr.statusCode) {
			return err // non-retryable error, fail immediately
		}

		delay := computeBackoff(attempt, sendErr.retryAfter)
		slog.Warn("teams: retrying send",
			"attempt", attempt+1,
			"status", sendErr.statusCode,
			"delay", delay,
			"error", err,
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return lastErr
}

// doSendActivity performs a single HTTP POST to the Bot Framework API.
func (c *botClient) doSendActivity(ctx context.Context, serviceURL, conversationID string, activity Activity) error {
	token, err := c.ensureToken(ctx)
	if err != nil {
		return fmt.Errorf("acquire token: %w", err)
	}

	u := strings.TrimRight(serviceURL, "/") + "/v3/conversations/" + url.PathEscape(conversationID) + "/activities"

	body, err := json.Marshal(activity)
	if err != nil {
		return fmt.Errorf("marshal activity: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send activity: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		retryAfter := parseRetryAfterHeader(resp.Header.Get("Retry-After"))
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return &teamsSendError{
			statusCode: resp.StatusCode,
			retryAfter: retryAfter,
			body:       string(respBody),
		}
	}

	return nil
}

// isRetryableStatus returns true for 429 (rate limit) and 5xx (server errors).
func isRetryableStatus(code int) bool {
	return code == 429 || code == 500 || code == 502 || code == 503 || code == 504
}

// computeBackoff returns the delay for a retry attempt.
// Honors Retry-After if present, otherwise uses exponential backoff with jitter.
func computeBackoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > sendMaxRetryAfter {
			retryAfter = sendMaxRetryAfter
		}
		return retryAfter
	}
	// Exponential backoff: base * 2^attempt + jitter
	delay := sendBaseDelay * time.Duration(math.Pow(2, float64(attempt)))
	if delay > sendMaxDelay {
		delay = sendMaxDelay
	}
	// Add 0-25% jitter
	if quarter := int64(delay / 4); quarter > 0 {
		return delay + time.Duration(rand.Int64N(quarter))
	}
	return delay
}

// parseRetryAfterHeader parses the Retry-After header value.
// Supports integer seconds (standard for Bot Framework). Returns 0 if missing/invalid.
func parseRetryAfterHeader(value string) time.Duration {
	if value == "" {
		return 0
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// ensureToken returns a valid Azure AD token, refreshing if expired.
func (c *botClient) ensureToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && time.Now().Before(c.tokenExpiry) {
		return c.token, nil
	}

	tokenURL := c.resolveTokenURL()
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.botID},
		"client_secret": {c.botPassword},
		"scope":         {tokenScope},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("token endpoint %d: %s", resp.StatusCode, string(respBody))
	}

	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}

	c.token = tokenResp.AccessToken
	// Floor expiry at 30s to prevent tight retry loop if ExpiresIn is 0 or tiny
	expiryDuration := time.Duration(tokenResp.ExpiresIn)*time.Second - tokenRefreshMargin
	if expiryDuration < 30*time.Second {
		expiryDuration = 30 * time.Second
	}
	c.tokenExpiry = time.Now().Add(expiryDuration)

	slog.Debug("teams: acquired Azure AD token", "expires_in", tokenResp.ExpiresIn)
	return c.token, nil
}

// resolveTokenURL returns the OAuth2 token endpoint based on tenant type.
func (c *botClient) resolveTokenURL() string {
	if c.tenantID != "" {
		return "https://login.microsoftonline.com/" + c.tenantID + "/oauth2/v2.0/token"
	}
	return multiTenantTokenURL
}
