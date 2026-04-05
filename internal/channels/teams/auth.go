package teams

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	openIDMetadataURL = "https://login.botframework.com/v1/.well-known/openidconfiguration"
	expectedIssuer    = "https://api.botframework.com"
	keyRefreshInterval = 1 * time.Hour
)

// jwksKey is a single JWK from the JWKS endpoint.
type jwksKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// jwksResponse is the JSON Web Key Set response.
type jwksResponse struct {
	Keys []jwksKey `json:"keys"`
}

// tokenValidator validates Bot Framework JWT tokens using JWKS keys.
type tokenValidator struct {
	botID    string
	tenantID string // empty for MultiTenant

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey // kid → public key
	lastFetch time.Time
	jwksURI   string
}

func newTokenValidator(botID, tenantID string) *tokenValidator {
	return &tokenValidator{
		botID:    botID,
		tenantID: tenantID,
		keys:     make(map[string]*rsa.PublicKey),
	}
}

// Validate parses and validates a Bot Framework JWT token.
func (v *tokenValidator) Validate(tokenString string) error {
	if err := v.refreshKeysIfStale(); err != nil {
		return fmt.Errorf("jwks refresh: %w", err)
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		// Ensure RSA signing method
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, fmt.Errorf("missing kid in JWT header")
		}

		v.mu.Lock()
		key, ok := v.keys[kid]
		v.mu.Unlock()
		if !ok {
			// Key not found — force refresh once and retry
			if err := v.forceRefreshKeys(); err != nil {
				return nil, fmt.Errorf("key refresh: %w", err)
			}
			v.mu.Lock()
			key, ok = v.keys[kid]
			v.mu.Unlock()
			if !ok {
				return nil, fmt.Errorf("unknown kid: %s", kid)
			}
		}
		return key, nil
	},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(expectedIssuer),
		jwt.WithAudience(v.botID),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return fmt.Errorf("jwt validation: %w", err)
	}

	// SingleTenant: verify tenant ID claim when present.
	// Bot Framework Connector tokens (issuer: api.botframework.com) don't carry "tid" —
	// tenant isolation is enforced at the Azure Bot Service level.
	if v.tenantID != "" {
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			return fmt.Errorf("invalid claims type")
		}
		if tid, exists := claims["tid"].(string); exists && tid != "" && tid != v.tenantID {
			return fmt.Errorf("tenant mismatch: got %q, want %q", tid, v.tenantID)
		}
	}

	return nil
}

// refreshKeysIfStale fetches JWKS keys if the cache is older than keyRefreshInterval.
func (v *tokenValidator) refreshKeysIfStale() error {
	v.mu.Lock()
	stale := time.Since(v.lastFetch) > keyRefreshInterval
	v.mu.Unlock()
	if !stale {
		return nil
	}
	return v.forceRefreshKeys()
}

// forceRefreshKeys fetches OpenID metadata and JWKS keys unconditionally.
func (v *tokenValidator) forceRefreshKeys() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	// Fetch OpenID metadata to get JWKS URI
	if v.jwksURI == "" {
		oidc, err := fetchJSON[openIDConfig](openIDMetadataURL)
		if err != nil {
			return fmt.Errorf("openid metadata: %w", err)
		}
		v.jwksURI = oidc.JWKSURI
	}

	// Fetch JWKS keys
	jwks, err := fetchJSON[jwksResponse](v.jwksURI)
	if err != nil {
		return fmt.Errorf("jwks fetch: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		pub, err := parseRSAPublicKey(k.N, k.E)
		if err != nil {
			slog.Warn("teams: skip invalid JWKS key", "kid", k.Kid, "error", err)
			continue
		}
		keys[k.Kid] = pub
	}

	v.keys = keys
	v.lastFetch = time.Now()
	slog.Debug("teams: refreshed JWKS keys", "count", len(keys))
	return nil
}

// parseRSAPublicKey builds an RSA public key from base64url-encoded N and E.
func parseRSAPublicKey(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, fmt.Errorf("decode N: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, fmt.Errorf("decode E: %w", err)
	}
	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)
	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}

// fetchJSON fetches a URL and decodes the JSON response into T.
func fetchJSON[T any](url string) (T, error) {
	var zero T
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return zero, err
	}
	return result, nil
}

// extractBearerToken extracts the token from "Bearer <token>" Authorization header.
func extractBearerToken(authHeader string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return ""
	}
	return strings.TrimPrefix(authHeader, prefix)
}
