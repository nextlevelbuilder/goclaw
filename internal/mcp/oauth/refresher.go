package mcpoauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"golang.org/x/sync/singleflight"
)

const tokenRefreshBuffer = 60 * time.Second

// ErrTokenExpired is returned when the token is expired and cannot be refreshed.
var ErrTokenExpired = errors.New("mcpoauth: token expired and no refresh token available")

type cachedToken struct {
	accessToken string
	expiresAt   time.Time
}

// Refresher provides valid OAuth access tokens for MCP servers, handling
// in-memory caching and transparent refresh using stored refresh tokens.
type Refresher struct {
	store  store.MCPOAuthTokenStore
	client *http.Client
	mu     sync.Mutex
	cache  map[string]*cachedToken // key: serverID+":"+userID
	// sf coalesces concurrent DB-load + refresh for the same slot so a rotating
	// refresh token (RFC 9700) is never consumed twice by parallel agent turns.
	sf singleflight.Group
}

// NewRefresher builds a Refresher. Token decryption happens in the store layer,
// so the Refresher itself never touches the encryption key.
func NewRefresher(s store.MCPOAuthTokenStore, client *http.Client) *Refresher {
	return &Refresher{
		store:  s,
		client: client,
		cache:  make(map[string]*cachedToken),
	}
}

// GetValidToken returns a valid access token for the given server.
// userID="" uses the global (tenant-level) token; non-empty uses the per-user token.
func (r *Refresher) GetValidToken(ctx context.Context, serverID, tenantID uuid.UUID, userID string) (string, error) {
	cacheKey := serverID.String() + ":" + userID

	if tok, ok := r.cachedValid(cacheKey); ok {
		return tok, nil
	}

	// Coalesce concurrent DB-load + refresh for the same slot. Without this,
	// two parallel agent turns hitting an expired token both POST a refresh and
	// the second one replays an already-rotated refresh token, killing the slot.
	v, err, _ := r.sf.Do(cacheKey, func() (any, error) {
		// Double-check: a coalesced caller ahead of us may have just refreshed.
		if tok, ok := r.cachedValid(cacheKey); ok {
			return tok, nil
		}
		return r.loadAndRefresh(ctx, serverID, tenantID, userID, cacheKey)
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// cachedValid returns the cached access token when it is still inside the
// refresh buffer, false otherwise.
func (r *Refresher) cachedValid(cacheKey string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.cache[cacheKey]; ok && time.Now().Add(tokenRefreshBuffer).Before(c.expiresAt) {
		return c.accessToken, true
	}
	return "", false
}

// loadAndRefresh loads the token from the store and refreshes it if expired.
// Runs under the single-flight lock for its cache key.
func (r *Refresher) loadAndRefresh(ctx context.Context, serverID, tenantID uuid.UUID, userID, cacheKey string) (string, error) {
	tok, err := r.loadToken(ctx, serverID, tenantID, userID)
	if err != nil {
		slog.Warn("mcpoauth: load token failed", "server_id", serverID, "tenant_id", tenantID, "user_id", userID, "error", err)
		return "", err
	}
	if tok == nil {
		slog.Debug("mcpoauth: no token in db", "server_id", serverID, "tenant_id", tenantID, "user_id", userID)
		return "", ErrTokenExpired
	}

	// No expiry advertised → treat as non-expiring. RFC 6749 §5.1 makes
	// expires_in OPTIONAL, so a token without an expiry (common for
	// client_credentials and some long-lived grants) is still valid and must
	// not be forced through the refresh/ErrTokenExpired path.
	if tok.ExpiresAt == nil {
		r.setCache(cacheKey, tok.AccessToken, nil)
		return tok.AccessToken, nil
	}

	// Valid in DB?
	if time.Now().Add(tokenRefreshBuffer).Before(*tok.ExpiresAt) {
		r.setCache(cacheKey, tok.AccessToken, tok.ExpiresAt)
		return tok.AccessToken, nil
	}

	// Expired — try refresh.
	if tok.RefreshToken == "" {
		return "", ErrTokenExpired
	}
	newTokens, err := r.refresh(ctx, tok)
	if err != nil {
		return "", fmt.Errorf("mcpoauth: token refresh failed: %w", err)
	}

	// Persist refreshed tokens.
	now := time.Now()
	var expiresAt *time.Time
	if newTokens.ExpiresIn > 0 {
		t := now.Add(time.Duration(newTokens.ExpiresIn) * time.Second)
		expiresAt = &t
	}
	updated := *tok
	updated.AccessToken = newTokens.AccessToken
	if newTokens.RefreshToken != "" {
		updated.RefreshToken = newTokens.RefreshToken
	}
	updated.ExpiresAt = expiresAt
	updated.IssuedAt = &now
	if err := r.store.UpsertOAuthToken(ctx, &updated); err != nil {
		return "", fmt.Errorf("mcpoauth: save refreshed token: %w", err)
	}

	r.setCache(cacheKey, newTokens.AccessToken, expiresAt)
	return newTokens.AccessToken, nil
}

// InvalidateCache evicts the in-memory cached token for the given server/user.
func (r *Refresher) InvalidateCache(serverID uuid.UUID, userID string) {
	key := serverID.String() + ":" + userID
	r.mu.Lock()
	delete(r.cache, key)
	r.mu.Unlock()
}

// InvalidateServer evicts every cached token for the given server (global +
// all per-user slots). Cache keys are "<serverID>:<userID>", so all entries
// sharing the server prefix are dropped. Called when the server URL or OAuth
// config changes and the stored tokens have been purged.
func (r *Refresher) InvalidateServer(serverID uuid.UUID) {
	prefix := serverID.String() + ":"
	r.mu.Lock()
	for key := range r.cache {
		if strings.HasPrefix(key, prefix) {
			delete(r.cache, key)
		}
	}
	r.mu.Unlock()
}

func (r *Refresher) loadToken(ctx context.Context, serverID, tenantID uuid.UUID, userID string) (*store.MCPOAuthToken, error) {
	if userID == "" {
		return r.store.GetOAuthToken(ctx, serverID, tenantID)
	}
	return r.store.GetUserOAuthToken(ctx, serverID, tenantID, userID)
}

func (r *Refresher) refresh(ctx context.Context, tok *store.MCPOAuthToken) (*OAuthTokens, error) {
	params := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tok.RefreshToken},
		"client_id":     {tok.DCRClientID},
	}
	if tok.DCRClientSecret != "" {
		params.Set("client_secret", tok.DCRClientSecret)
	}
	if tok.ResourceURI != "" {
		params.Set("resource", tok.ResourceURI)
	}
	return postTokenRequest(ctx, r.client, tok.TokenEndpoint, params)
}

func (r *Refresher) setCache(key, accessToken string, expiresAt *time.Time) {
	c := &cachedToken{accessToken: accessToken}
	if expiresAt != nil {
		c.expiresAt = *expiresAt
	} else {
		c.expiresAt = time.Now().Add(time.Hour)
	}
	r.mu.Lock()
	r.cache[key] = c
	r.mu.Unlock()
}
