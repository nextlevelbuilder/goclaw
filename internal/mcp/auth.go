package mcp

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Runtime identity shared between GoClaw runtime and MCP servers.
// MCP servers verify tokens signed with this private key.

var (
	runtimePrivateKey ed25519.PrivateKey
	runtimeKeyID      string
)

func init() {
	keyStr := os.Getenv("MCP_RUNTIME_PRIVATE_KEY")
	runtimeKeyID = os.Getenv("MCP_RUNTIME_KEY_ID")
	if runtimeKeyID == "" {
		runtimeKeyID = "rtkey_123"
	}

	if keyStr != "" {
		key, err := parseEd25519PrivateKey(keyStr)
		if err != nil {
			slog.Error("failed to parse MCP_RUNTIME_PRIVATE_KEY, generating temporary key", "error", err)
			_, fallbackKey, _ := ed25519.GenerateKey(nil)
			runtimePrivateKey = fallbackKey
		} else {
			runtimePrivateKey = key
		}
	} else {
		slog.Warn("MCP_RUNTIME_PRIVATE_KEY is not set, generating a temporary key")
		_, fallbackKey, _ := ed25519.GenerateKey(nil)
		runtimePrivateKey = fallbackKey
	}
}

func parseEd25519PrivateKey(keyStr string) (ed25519.PrivateKey, error) {
	// Try PEM first
	block, _ := pem.Decode([]byte(keyStr))
	var der []byte
	if block != nil {
		der = block.Bytes
	} else {
		// Try base64
		var err error
		der, err = base64.StdEncoding.DecodeString(keyStr)
		if err != nil {
			// Try raw string
			der = []byte(keyStr)
		}
	}

	// Parse PKCS8
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if edKey, ok := key.(ed25519.PrivateKey); ok {
			return edKey, nil
		}
	}

	// Fallback: If raw key length is 32 (seed) or 64 (private key)
	if len(der) == 32 {
		return ed25519.NewKeyFromSeed(der), nil
	} else if len(der) == 64 {
		return ed25519.PrivateKey(der), nil
	}

	return nil, errors.New("invalid private key format")
}

// RuntimeClaims represents the runtime identity.
// MCP servers should verify:
// - signature
// - issuer
// - expiration
type RuntimeClaims struct {
	RuntimeID string `json:"runtime_id"`
	AgentID   string `json:"agent_id"`
	UserID    string `json:"user_id"`
	jwt.RegisteredClaims
}

// ContextAwareRoundTripper reads identity from the request context
// and signs a fresh JWT with it for every outgoing HTTP call.
type ContextAwareRoundTripper struct {
	Proxied http.RoundTripper
}

func (c *ContextAwareRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Read per-call identity from context (set by BridgeTool.Execute)
	agentID := store.AgentIDFromContext(req.Context())
	userID := store.UserIDFromContext(req.Context())

	token := signRuntimeJWT(agentID, userID)
	req.Header.Set("Authorization", "Bearer "+token)

	transport := c.Proxied
	if transport == nil {
		transport = http.DefaultTransport
	}
	return transport.RoundTrip(req)
}

// signRuntimeJWT creates a signed JWT identifying this GoClaw runtime.
func signRuntimeJWT(agentID uuid.UUID, userID string) string {
	now := time.Now()

	claims := RuntimeClaims{
		RuntimeID: "goclaw-runtime",
		AgentID:   agentID.String(),
		UserID:    userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "goclaw",
			Subject:   "runtime",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = runtimeKeyID

	signed, err := token.SignedString(runtimePrivateKey)
	if err != nil {
		slog.Error("failed to sign runtime JWT", "error", err)
		return ""
	}

	return signed
}

// verifyRuntimeJWT validates a runtime JWT.
// This should be used on the MCP server side.
func verifyRuntimeJWT(tokenString string) (*RuntimeClaims, error) {
	token, err := jwt.ParseWithClaims(
		tokenString,
		&RuntimeClaims{},
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodEdDSA {
				return nil, errors.New("unexpected signing method")
			}
			return runtimePrivateKey.Public(), nil
		},
	)

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*RuntimeClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}

	return claims, nil
}
