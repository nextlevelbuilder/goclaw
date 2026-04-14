package oauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
)

const (
	AnthropicAuthURL           = "https://claude.com/cai/oauth/authorize"
	AnthropicTokenURL          = "https://platform.claude.com/v1/oauth/token"
	AnthropicProfileURL        = "https://api.anthropic.com/api/oauth/profile"
	AnthropicManualRedirectURL = "https://platform.claude.com/oauth/code/callback"
	AnthropicScopes            = "user:profile user:inference"
	OAuthBetaHeader            = "oauth-2025-04-20"

	// devFallbackClientID is Claude Code's public OAuth client ID.
	// Used ONLY when GOCLAW_DEV=true and no explicit client ID is configured.
	devFallbackClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
)

// AnthropicClientID returns the configured OAuth client ID.
// Requires GOCLAW_ANTHROPIC_OAUTH_CLIENT_ID in production; falls back to
// Claude Code's public ID only when GOCLAW_DEV=true. Fails fast otherwise.
func AnthropicClientID() (string, error) {
	if id := os.Getenv("GOCLAW_ANTHROPIC_OAUTH_CLIENT_ID"); id != "" {
		return id, nil
	}
	if os.Getenv("GOCLAW_DEV") == "true" {
		return devFallbackClientID, nil
	}
	return "", fmt.Errorf("GOCLAW_ANTHROPIC_OAUTH_CLIENT_ID is required for Anthropic OAuth")
}

// AnthropicTokenResponse is the response from Anthropic's token endpoint.
// Embeds shared token fields and adds Anthropic-specific account/organization fields.
type AnthropicTokenResponse struct {
	AccessToken  string                      `json:"access_token"`
	RefreshToken string                      `json:"refresh_token"`
	ExpiresIn    int                         `json:"expires_in"`
	TokenType    string                      `json:"token_type"`
	Scope        string                      `json:"scope"`
	Account      *AnthropicTokenAccount      `json:"account,omitempty"`
	Organization *AnthropicTokenOrganization `json:"organization,omitempty"`
}

// AnthropicTokenAccount holds the account metadata returned in the token response.
type AnthropicTokenAccount struct {
	UUID         string `json:"uuid"`
	EmailAddress string `json:"email_address"`
}

// AnthropicTokenOrganization holds the organization metadata returned in the token response.
type AnthropicTokenOrganization struct {
	UUID string `json:"uuid"`
}

// ToTokenResponse converts an AnthropicTokenResponse to the generic TokenResponse
// used by DBTokenSource for refresh.
func (r *AnthropicTokenResponse) ToTokenResponse() *TokenResponse {
	return &TokenResponse{
		AccessToken:  r.AccessToken,
		RefreshToken: r.RefreshToken,
		ExpiresIn:    r.ExpiresIn,
		TokenType:    r.TokenType,
		Scope:        r.Scope,
	}
}

// AnthropicProfile is the response from /api/oauth/profile.
type AnthropicProfile struct {
	Account struct {
		UUID        string `json:"uuid"`
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		CreatedAt   string `json:"created_at"`
	} `json:"account"`
	Organization struct {
		UUID                  string `json:"uuid"`
		OrganizationType      string `json:"organization_type"`
		RateLimitTier         string `json:"rate_limit_tier"`
		BillingType           string `json:"billing_type"`
		HasExtraUsageEnabled  bool   `json:"has_extra_usage_enabled"`
		SubscriptionCreatedAt string `json:"subscription_created_at"`
	} `json:"organization"`
}

// SubscriptionType maps organization_type to a short subscription label.
// Returns empty string for unknown types.
func (p *AnthropicProfile) SubscriptionType() string {
	switch p.Organization.OrganizationType {
	case "claude_max":
		return "max"
	case "claude_pro":
		return "pro"
	case "claude_enterprise":
		return "enterprise"
	case "claude_team":
		return "team"
	default:
		return ""
	}
}

// AnthropicPendingLogin represents an in-progress Anthropic OAuth flow.
type AnthropicPendingLogin struct {
	AuthURL  string
	codeCh   chan string
	errCh    chan error
	verifier string
	state    string
	port     int
	srv      *http.Server
}

// Wait blocks until the OAuth callback is received or ctx is cancelled.
// Shuts down the callback server when done.
func (p *AnthropicPendingLogin) Wait(ctx context.Context) (*AnthropicTokenResponse, error) {
	defer p.srv.Shutdown(context.Background()) //nolint:errcheck

	select {
	case code := <-p.codeCh:
		return exchangeAnthropicCode(code, p.verifier, p.port, false)
	case err := <-p.errCh:
		return nil, err
	case <-ctx.Done():
		return nil, fmt.Errorf("authentication timed out: %w", ctx.Err())
	}
}

// Shutdown stops the callback server without waiting for a callback.
func (p *AnthropicPendingLogin) Shutdown() {
	p.srv.Shutdown(context.Background()) //nolint:errcheck
}

// ExchangeRedirectURL extracts the code from a pasted redirect URL and exchanges it for tokens.
// Used for remote/VPS environments where the localhost callback can't be reached.
func (p *AnthropicPendingLogin) ExchangeRedirectURL(redirectURL string) (*AnthropicTokenResponse, error) {
	u, err := url.Parse(redirectURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redirect URL: %w", err)
	}

	state := u.Query().Get("state")
	code := u.Query().Get("code")

	if code == "" {
		errMsg := u.Query().Get("error")
		if errMsg != "" {
			return nil, fmt.Errorf("OAuth error: %s", errMsg)
		}
		return nil, fmt.Errorf("no authorization code in redirect URL")
	}

	if state == "" || state != p.state {
		return nil, fmt.Errorf("invalid state parameter (possible CSRF)")
	}

	// Manual redirect flow — use AnthropicManualRedirectURL as redirect_uri
	return exchangeAnthropicCode(code, p.verifier, 0, true)
}

// StartLoginAnthropic begins the OAuth PKCE flow for Anthropic subscription auth.
// Starts a callback server on a random OS-assigned port. Does NOT open a browser.
func StartLoginAnthropic() (*AnthropicPendingLogin, error) {
	clientID, err := AnthropicClientID()
	if err != nil {
		return nil, err
	}

	verifier, challenge, err := generatePKCE()
	if err != nil {
		return nil, err
	}

	stateBuf := make([]byte, 16)
	if _, err := rand.Read(stateBuf); err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}
	state := base64.RawURLEncoding.EncodeToString(stateBuf)

	// Random OS-assigned port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start callback server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	params := url.Values{
		"code":                  {"true"}, // triggers Claude Max upsell page
		"client_id":             {clientID},
		"response_type":         {"code"},
		"redirect_uri":          {redirectURI},
		"scope":                 {AnthropicScopes},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	authURL := AnthropicAuthURL + "?" + params.Encode()

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	var callbackOnce sync.Once

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		callbackOnce.Do(func() {
			if r.URL.Query().Get("state") != state {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<html><body><h2>Authentication Failed</h2><p>Invalid state parameter.</p></body></html>`)
				errCh <- fmt.Errorf("oauth callback: state mismatch (possible CSRF)")
				return
			}
			code := r.URL.Query().Get("code")
			if code == "" {
				errMsg := r.URL.Query().Get("error")
				if errMsg == "" {
					errMsg = "no authorization code received"
				}
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprintf(w, `<html><body><h2>Authentication Failed</h2><p>%s</p></body></html>`, html.EscapeString(errMsg))
				errCh <- fmt.Errorf("oauth callback: %s", errMsg)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body><h2>Authentication Successful!</h2><p>You can close this window.</p></body></html>`)
			codeCh <- code
		})
	})

	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("callback server: %w", err)
		}
	}()

	return &AnthropicPendingLogin{
		AuthURL:  authURL,
		codeCh:   codeCh,
		errCh:    errCh,
		verifier: verifier,
		state:    state,
		port:     port,
		srv:      srv,
	}, nil
}

// LoginAnthropic runs the interactive OAuth PKCE flow for Anthropic.
// Opens the user's browser, waits for callback, returns token response.
func LoginAnthropic(ctx context.Context) (*AnthropicTokenResponse, error) {
	pending, err := StartLoginAnthropic()
	if err != nil {
		return nil, err
	}

	fmt.Println("Opening browser for Anthropic authentication...")
	fmt.Printf("If the browser doesn't open, visit:\n%s\n\n", pending.AuthURL)
	openBrowser(pending.AuthURL)

	fmt.Println("Waiting for authentication callback...")
	return pending.Wait(ctx)
}

// exchangeAnthropicCode exchanges an authorization code for tokens.
// Anthropic's token endpoint requires JSON body (unlike OpenAI which uses form-encoded).
func exchangeAnthropicCode(code, verifier string, port int, useManualRedirect bool) (*AnthropicTokenResponse, error) {
	clientID, err := AnthropicClientID()
	if err != nil {
		return nil, err
	}

	var redirectURI string
	if useManualRedirect {
		redirectURI = AnthropicManualRedirectURL
	} else {
		redirectURI = fmt.Sprintf("http://localhost:%d/callback", port)
	}

	body := map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"redirect_uri":  redirectURI,
		"client_id":     clientID,
		"code_verifier": verifier,
	}

	return doAnthropicTokenRequest(body)
}

// RefreshAnthropicToken refreshes an expired Anthropic access token.
// Returns the generic TokenResponse so it fits DBTokenSource.refreshFunc signature.
func RefreshAnthropicToken(refreshToken string) (*TokenResponse, error) {
	clientID, err := AnthropicClientID()
	if err != nil {
		return nil, err
	}

	body := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     clientID,
		"scope":         AnthropicScopes,
	}

	resp, err := doAnthropicTokenRequest(body)
	if err != nil {
		return nil, err
	}
	return resp.ToTokenResponse(), nil
}

// doAnthropicTokenRequest POSTs a JSON body to the Anthropic token endpoint
// and parses the response as AnthropicTokenResponse.
func doAnthropicTokenRequest(body map[string]string) (*AnthropicTokenResponse, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", AnthropicTokenURL, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic token exchange failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var tokenResp AnthropicTokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	return &tokenResp, nil
}

// FetchAnthropicProfile fetches the user's profile from Anthropic's OAuth profile endpoint.
// Used to determine subscription type (max/pro/enterprise/team) and account metadata.
func FetchAnthropicProfile(accessToken string) (*AnthropicProfile, error) {
	req, err := http.NewRequest("GET", AnthropicProfileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build profile request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("profile request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic profile fetch failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var profile AnthropicProfile
	if err := json.Unmarshal(respBody, &profile); err != nil {
		return nil, fmt.Errorf("parse profile response: %w", err)
	}
	return &profile, nil
}
