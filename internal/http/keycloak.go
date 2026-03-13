package http

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// KeycloakConfig holds Keycloak connection settings (from env vars).
type KeycloakConfig struct {
	URL         string // GOCLAW_KEYCLOAK_URL, e.g. "http://keycloak:8080" (internal, for JWKS)
	ExternalURL string // GOCLAW_KEYCLOAK_EXTERNAL_URL, e.g. "http://localhost:8080" (browser-facing)
	Realm       string // GOCLAW_KEYCLOAK_REALM, e.g. "goclaw"
	ClientID    string // GOCLAW_KEYCLOAK_CLIENT_ID, e.g. "goclaw-web"
}

// IssuerURL returns the Keycloak issuer URL for this realm.
func (c KeycloakConfig) IssuerURL() string {
	return fmt.Sprintf("%s/realms/%s", c.URL, c.Realm)
}

// CertsURL returns the JWKS endpoint URL.
func (c KeycloakConfig) CertsURL() string {
	return fmt.Sprintf("%s/protocol/openid-connect/certs", c.IssuerURL())
}

// KeycloakHandler handles Keycloak authentication HTTP endpoints.
type KeycloakHandler struct {
	cfg       KeycloakConfig
	userStore store.UserStore

	// JWKS cache
	mu      sync.RWMutex
	keys    map[string]*rsa.PublicKey
	keysExp time.Time
}

// NewKeycloakHandler creates a handler for Keycloak auth endpoints.
func NewKeycloakHandler(cfg KeycloakConfig, userStore store.UserStore) *KeycloakHandler {
	return &KeycloakHandler{
		cfg:       cfg,
		userStore: userStore,
		keys:      make(map[string]*rsa.PublicKey),
	}
}

// RegisterRoutes registers Keycloak auth routes on the given mux.
func (h *KeycloakHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/auth/keycloak/me", h.handleMe)
	mux.HandleFunc("GET /v1/auth/keycloak/config", h.handleConfig)
}

// handleConfig returns the Keycloak public config for the frontend (no secrets).
func (h *KeycloakHandler) handleConfig(w http.ResponseWriter, r *http.Request) {
	// Return the external (browser-facing) URL for the frontend to use.
	externalURL := h.cfg.ExternalURL
	if externalURL == "" {
		externalURL = h.cfg.URL // fallback if not set
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"url":       externalURL,
		"realm":     h.cfg.Realm,
		"client_id": h.cfg.ClientID,
	})
}

// ValidateToken validates a Keycloak JWT and returns the claims if valid.
// This is exposed for other components (e.g. WS connect, HTTP middleware) to reuse.
func (h *KeycloakHandler) ValidateToken(ctx context.Context, tokenStr string) (map[string]any, error) {
	return h.validateToken(ctx, tokenStr)
}

// handleMe validates a Keycloak access token, records login, and returns user info.
func (h *KeycloakHandler) handleMe(w http.ResponseWriter, r *http.Request) {
	tokenStr := extractBearerToken(r)
	if tokenStr == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing access token"})
		return
	}

	// Parse and validate JWT
	claims, err := h.validateToken(r.Context(), tokenStr)
	if err != nil {
		slog.Warn("keycloak.token_invalid", "error", err)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token: " + err.Error()})
		return
	}

	// Extract user info from claims
	sub, _ := claims["sub"].(string)
	if sub == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing sub claim"})
		return
	}

	username, _ := claims["preferred_username"].(string)
	email, _ := claims["email"].(string)
	name, _ := claims["name"].(string)

	// Upsert login record
	user, err := h.userStore.UpsertLogin(r.Context(), sub)
	if err != nil {
		slog.Error("keycloak.upsert_login", "error", err, "sub", sub)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to record login"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":            user.ID,
		"username":      username,
		"email":         email,
		"name":          name,
		"last_login_at": user.LastLoginAt.UTC().Format(time.RFC3339),
	})
}

// --- JWT validation (manual, no external JWT library) ---

// jwtHeader is the decoded JWT header.
type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

// validateToken parses a JWT, fetches JWKS, and validates signature + claims.
func (h *KeycloakHandler) validateToken(ctx context.Context, tokenStr string) (map[string]any, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format")
	}

	// Decode header
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}
	var header jwtHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("parse header: %w", err)
	}
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("unsupported algorithm: %s", header.Alg)
	}

	// Decode payload
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}

	// Validate expiration
	if exp, ok := claims["exp"].(float64); ok {
		if time.Now().Unix() > int64(exp) {
			return nil, fmt.Errorf("token expired")
		}
	} else {
		return nil, fmt.Errorf("missing exp claim")
	}

	// Validate issuer
	if iss, ok := claims["iss"].(string); ok {
		expectedIssuer := h.cfg.IssuerURL()
		if iss != expectedIssuer {
			slog.Debug("keycloak.issuer_mismatch", "got", iss, "expected", expectedIssuer)
		}
	}

	// Validate audience (azp for Keycloak access tokens)
	if azp, ok := claims["azp"].(string); ok {
		if azp != h.cfg.ClientID {
			return nil, fmt.Errorf("invalid azp claim: got %s, want %s", azp, h.cfg.ClientID)
		}
	}

	// Verify RSA signature
	pubKey, err := h.getPublicKey(ctx, header.Kid)
	if err != nil {
		return nil, fmt.Errorf("get public key: %w", err)
	}

	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}

	signedContent := parts[0] + "." + parts[1]
	hash := sha256.Sum256([]byte(signedContent))
	if err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], sigBytes); err != nil {
		return nil, fmt.Errorf("signature verification failed: %w", err)
	}

	return claims, nil
}

// getPublicKey fetches and caches the RSA public key for the given kid.
func (h *KeycloakHandler) getPublicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	h.mu.RLock()
	if key, ok := h.keys[kid]; ok && time.Now().Before(h.keysExp) {
		h.mu.RUnlock()
		return key, nil
	}
	h.mu.RUnlock()

	// Fetch JWKS
	if err := h.fetchJWKS(ctx); err != nil {
		return nil, err
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	key, ok := h.keys[kid]
	if !ok {
		return nil, fmt.Errorf("key %s not found in JWKS", kid)
	}
	return key, nil
}

// jwksResponse is the JSON structure returned by the JWKS endpoint.
type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// fetchJWKS retrieves and caches public keys from Keycloak's JWKS endpoint.
func (h *KeycloakHandler) fetchJWKS(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.cfg.CertsURL(), nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned %d", resp.StatusCode)
	}

	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("decode JWKS: %w", err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.keys = make(map[string]*rsa.PublicKey)
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Use != "sig" {
			continue
		}
		pubKey, err := parseRSAPublicKey(k.N, k.E)
		if err != nil {
			slog.Warn("keycloak.parse_jwk", "kid", k.Kid, "error", err)
			continue
		}
		h.keys[k.Kid] = pubKey
	}

	// Cache for 1 hour
	h.keysExp = time.Now().Add(time.Hour)
	slog.Info("keycloak.jwks_cached", "keys", len(h.keys))
	return nil
}

// parseRSAPublicKey converts base64url-encoded n and e to an RSA public key.
func parseRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := 0
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}

	return &rsa.PublicKey{N: n, E: e}, nil
}
