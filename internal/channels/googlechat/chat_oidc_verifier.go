package googlechat

import (
	"context"
	"crypto"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// chatJWKURL is the JWK endpoint for Google Chat's signing keys.
	// Standard idtoken.Validate() uses googleapis.com/oauth2/v3/certs which does NOT
	// include Chat's signing keys, causing "could not find matching cert keyId" errors.
	chatJWKURL = "https://www.googleapis.com/service_accounts/v1/jwk/chat@system.gserviceaccount.com"

	// chatIssuer is the expected issuer claim in Google Chat JWT tokens.
	chatIssuer = "chat@system.gserviceaccount.com"

	// jwkCacheTTL is how long to cache fetched JWKs before refreshing.
	jwkCacheTTL = 1 * time.Hour

	// clockSkewLeeway allows for minor clock differences between servers.
	clockSkewLeeway = 5 * time.Minute
)

// chatCertCache stores fetched Google Chat JWKs with a TTL.
var chatCertCache = &jwkCache{keys: make(map[string]*rsa.PublicKey)}

type jwkCache struct {
	mu      sync.RWMutex
	keys    map[string]*rsa.PublicKey // kid → public key
	fetched time.Time
}

// jwkSet is the JSON structure from Google's JWK endpoint.
type jwkSet struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// verifyChatToken verifies a Google Chat JWT token against the Chat-specific JWK endpoint.
// audiences is a list of acceptable audience values (webhook URL, project number, etc.).
func verifyChatToken(ctx context.Context, token string, audiences []string) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fmt.Errorf("invalid JWT format")
	}

	// 1. Parse header to get kid
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("decode header: %w", err)
	}
	var header struct {
		Kid string `json:"kid"`
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return fmt.Errorf("parse header: %w", err)
	}
	if header.Alg != "RS256" {
		return fmt.Errorf("unsupported algorithm: %s", header.Alg)
	}

	// 2. Get signing key (fetch + cache, force-refresh on miss)
	pubKey, err := getChatSigningKey(ctx, header.Kid)
	if err != nil {
		return err
	}

	// 3. Verify RS256 signature
	signed := []byte(parts[0] + "." + parts[1])
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	h := crypto.SHA256.New()
	h.Write(signed)
	if err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, h.Sum(nil), sig); err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

	// 4. Validate claims
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("decode claims: %w", err)
	}
	var claims struct {
		Iss string `json:"iss"`
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Iat int64  `json:"iat"`
	}
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return fmt.Errorf("parse claims: %w", err)
	}

	if claims.Iss != chatIssuer {
		return fmt.Errorf("invalid issuer: %s", claims.Iss)
	}

	now := time.Now().Unix()
	if now > claims.Exp+int64(clockSkewLeeway.Seconds()) {
		return fmt.Errorf("token expired")
	}
	// Reject tokens issued in the future (clock skew tolerance applied).
	if claims.Iat > 0 && now < claims.Iat-int64(clockSkewLeeway.Seconds()) {
		return fmt.Errorf("token not yet valid")
	}

	for _, aud := range audiences {
		if claims.Aud == aud {
			return nil
		}
	}
	return fmt.Errorf("audience mismatch: got %s", claims.Aud)
}

// getChatSigningKey returns the RSA public key for the given kid.
// Fetches from cache first; on miss, force-refreshes the cache and retries.
func getChatSigningKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	keys, err := fetchChatJWKs(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("fetch certs: %w", err)
	}
	if key, ok := keys[kid]; ok {
		return key, nil
	}

	// Key not found — force refresh (Google may have rotated keys)
	keys, err = fetchChatJWKs(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("refresh certs: %w", err)
	}
	if key, ok := keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("unknown signing key: %s", kid)
}

// fetchChatJWKs fetches Google Chat JWKs with caching.
// forceRefresh bypasses the cache TTL.
func fetchChatJWKs(ctx context.Context, forceRefresh bool) (map[string]*rsa.PublicKey, error) {
	chatCertCache.mu.RLock()
	if !forceRefresh && time.Since(chatCertCache.fetched) < jwkCacheTTL && len(chatCertCache.keys) > 0 {
		keys := chatCertCache.keys
		chatCertCache.mu.RUnlock()
		return keys, nil
	}
	chatCertCache.mu.RUnlock()

	chatCertCache.mu.Lock()
	defer chatCertCache.mu.Unlock()

	// Double-check after acquiring write lock
	if !forceRefresh && time.Since(chatCertCache.fetched) < jwkCacheTTL && len(chatCertCache.keys) > 0 {
		return chatCertCache.keys, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, chatJWKURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch JWKs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWK endpoint returned %d", resp.StatusCode)
	}

	var jwks jwkSet
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("decode JWKs: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" {
			continue
		}
		pub, err := rsaPubFromJWK(k.N, k.E)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}

	chatCertCache.keys = keys
	chatCertCache.fetched = time.Now()
	return keys, nil
}

// rsaPubFromJWK converts base64url-encoded RSA modulus and exponent to an rsa.PublicKey.
func rsaPubFromJWK(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, err
	}

	e := 0
	for _, b := range eBytes {
		e = e*256 + int(b)
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: e,
	}, nil
}
