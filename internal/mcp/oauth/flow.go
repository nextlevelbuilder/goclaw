package mcpoauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/security"

	"github.com/google/uuid"
)

const (
	flowTTL          = 10 * time.Minute
	flowCleanupEvery = time.Minute
)

// OAuthTokens is the token response from the Authorization Server.
type OAuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// PendingFlow holds state for an in-flight authorization code flow.
type PendingFlow struct {
	ServerID         uuid.UUID
	TenantID         uuid.UUID
	UserID           string // empty = global/tenant-level, non-empty = per-user
	InitiatingUserID string // admin who called /start — used for WS event routing
	CodeVerifier     string
	UsePKCE          bool   // true → send code_verifier on token exchange
	RedirectURI      string
	TokenEndpoint    string
	ClientID         string
	ClientSecret     string // empty for public clients (PKCE)
	ResourceURI      string // RFC 8707
	Issuer           string // AS issuer — persisted with the token for status display
	CreatedAt        time.Time
}

// StartFlowParams holds the parameters required to begin an OAuth flow.
type StartFlowParams struct {
	ServerID         uuid.UUID
	TenantID         uuid.UUID
	UserID           string
	InitiatingUserID string // admin who called /start — stored for WS event routing on callback
	DiscoveryResult  *DiscoveryResult
	ClientID         string
	ClientSecret     string
	RedirectURI      string
	Scopes           string
	// GrantType controls the authorization flow variant.
	// "pkce" (default/empty) — Authorization Code + PKCE S256, public client (no ClientSecret).
	// "authorization_code"   — Authorization Code + PKCE S256, confidential client (sends ClientSecret).
	// "client_credentials"   — not handled by StartFlow; use ClientCredentials() instead.
	GrantType string
}

// FlowManager manages in-progress OAuth authorization code flows.
type FlowManager struct {
	mu      sync.Mutex
	pending map[string]*PendingFlow // state → flow
	client  *http.Client
}

func NewFlowManager(client *http.Client) *FlowManager {
	fm := &FlowManager{
		pending: make(map[string]*PendingFlow),
		client:  client,
	}
	go fm.cleanupLoop()
	return fm
}

// StartFlow initiates an authorization code flow.
// GrantType "pkce" — PKCE S256, public client (no client_secret).
// GrantType "authorization_code" — PKCE S256, confidential client (sends client_secret too).
// OAuth 2.1 mandates PKCE for ALL authorization code flows; the grant type only controls
// whether a client_secret is included, not whether PKCE is used.
// Returns the authorization URL and CSRF state token.
func (fm *FlowManager) StartFlow(ctx context.Context, p StartFlowParams) (authURL, state string, err error) {
	_ = ctx

	// OAuth 2.1 §4.1.1: PKCE is mandatory for all authorization code flows.
	// "authorization_code" differs from "pkce" only in that it sends client_secret too.
	usePKCE := p.GrantType != "client_credentials"

	var verifier, challenge string
	if usePKCE {
		verifier, challenge, err = generatePKCE()
		if err != nil {
			return "", "", fmt.Errorf("mcpoauth: generate PKCE: %w", err)
		}
	}

	state, err = generateState()
	if err != nil {
		return "", "", fmt.Errorf("mcpoauth: generate state: %w", err)
	}

	fm.mu.Lock()
	fm.pending[state] = &PendingFlow{
		ServerID:         p.ServerID,
		TenantID:         p.TenantID,
		UserID:           p.UserID,
		InitiatingUserID: p.InitiatingUserID,
		CodeVerifier:     verifier,
		UsePKCE:          usePKCE,
		RedirectURI:      p.RedirectURI,
		TokenEndpoint:    p.DiscoveryResult.TokenEndpoint,
		ClientID:         p.ClientID,
		ClientSecret:     p.ClientSecret,
		ResourceURI:      p.DiscoveryResult.ResourceURI,
		Issuer:           p.DiscoveryResult.Issuer,
		CreatedAt:        time.Now(),
	}
	fm.mu.Unlock()

	q := url.Values{
		"response_type": {"code"},
		"client_id":     {p.ClientID},
		"redirect_uri":  {p.RedirectURI},
		"state":         {state},
	}
	if usePKCE {
		q.Set("code_challenge", challenge)
		q.Set("code_challenge_method", "S256")
	}
	if p.Scopes != "" {
		q.Set("scope", p.Scopes)
	}
	if p.DiscoveryResult.ResourceURI != "" {
		q.Set("resource", p.DiscoveryResult.ResourceURI) // RFC 8707
	}

	authURL = p.DiscoveryResult.AuthorizationEndpoint + "?" + q.Encode()
	return authURL, state, nil
}

// ExchangeCode validates the CSRF state, exchanges the authorization code for tokens,
// and returns the tokens along with the original flow parameters.
func (fm *FlowManager) ExchangeCode(ctx context.Context, state, code string) (*OAuthTokens, *PendingFlow, error) {
	fm.mu.Lock()
	flow, ok := fm.pending[state]
	if ok {
		delete(fm.pending, state)
	}
	fm.mu.Unlock()

	if !ok || time.Since(flow.CreatedAt) > flowTTL {
		return nil, nil, fmt.Errorf("mcpoauth: invalid or expired OAuth state")
	}

	tokens, err := fm.exchangeCodeHTTP(ctx, flow, code)
	if err != nil {
		return nil, nil, err
	}
	return tokens, flow, nil
}

func (fm *FlowManager) exchangeCodeHTTP(ctx context.Context, flow *PendingFlow, code string) (*OAuthTokens, error) {
	params := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {flow.RedirectURI},
		"client_id":    {flow.ClientID},
	}
	if flow.UsePKCE {
		params.Set("code_verifier", flow.CodeVerifier)
	}
	if flow.ClientSecret != "" {
		params.Set("client_secret", flow.ClientSecret)
	}
	if flow.ResourceURI != "" {
		params.Set("resource", flow.ResourceURI) // RFC 8707
	}

	return postTokenRequest(ctx, fm.client, flow.TokenEndpoint, params)
}

// ClientCredentials performs a client credentials grant (no user interaction).
func (fm *FlowManager) ClientCredentials(ctx context.Context, tokenEndpoint, clientID, clientSecret, scopes, resourceURI string) (*OAuthTokens, error) {
	params := url.Values{
		"grant_type": {"client_credentials"},
		"client_id":  {clientID},
	}
	if clientSecret != "" {
		params.Set("client_secret", clientSecret)
	}
	if scopes != "" {
		params.Set("scope", scopes)
	}
	if resourceURI != "" {
		params.Set("resource", resourceURI)
	}
	return postTokenRequest(ctx, fm.client, tokenEndpoint, params)
}

func (fm *FlowManager) cleanupLoop() {
	ticker := time.NewTicker(flowCleanupEvery)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-flowTTL)
		fm.mu.Lock()
		for state, flow := range fm.pending {
			if flow.CreatedAt.Before(cutoff) {
				delete(fm.pending, state)
			}
		}
		fm.mu.Unlock()
	}
}

// postTokenRequest sends a POST to the token endpoint with form-encoded params.
func postTokenRequest(ctx context.Context, client *http.Client, tokenEndpoint string, params url.Values) (*OAuthTokens, error) {
	_, ip, err := security.Validate(tokenEndpoint)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: SSRF validation failed for token endpoint: %w", err)
	}
	ctx = security.WithPinnedIP(ctx, ip)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, bytes.NewBufferString(params.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: token request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mcpoauth: token endpoint returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	var tokens OAuthTokens
	if err := json.Unmarshal(body, &tokens); err != nil {
		return nil, fmt.Errorf("mcpoauth: token response parse error: %w", err)
	}
	if tokens.AccessToken == "" {
		return nil, fmt.Errorf("mcpoauth: token response missing access_token")
	}
	if tokens.TokenType == "" {
		tokens.TokenType = "Bearer"
	}
	return &tokens, nil
}

// generatePKCE returns (code_verifier, code_challenge_S256).
func generatePKCE() (verifier, challenge string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

// generateState returns a cryptographically random CSRF state token.
func generateState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
