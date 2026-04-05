package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// Azure AD token endpoint for multi-tenant bots
	multiTenantTokenURL = "https://login.microsoftonline.com/botframework.com/oauth2/v2.0/token"
	tokenScope          = "https://api.botframework.com/.default"
	tokenRefreshMargin  = 5 * time.Minute // refresh before actual expiry
)

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

// SendReply sends a text reply to a Teams conversation.
func (c *botClient) SendReply(ctx context.Context, serviceURL, conversationID, text string) error {
	activity := Activity{
		Type: "message",
		Text: text,
	}
	return c.SendActivity(ctx, serviceURL, conversationID, activity)
}

// SendActivity posts an Activity to a conversation via Bot Framework REST API.
func (c *botClient) SendActivity(ctx context.Context, serviceURL, conversationID string, activity Activity) error {
	token, err := c.ensureToken(ctx)
	if err != nil {
		return fmt.Errorf("acquire token: %w", err)
	}

	// Build URL: {serviceUrl}/v3/conversations/{conversationId}/activities
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
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("bot framework API %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
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
