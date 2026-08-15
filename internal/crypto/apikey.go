package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const apiKeyPrefix = "goclaw_"

// GenerateAPIKey creates a new API key with format "goclaw_<32hex>".
// Returns the raw key (show once), its SHA-256 hash, and 8-char display prefix.
func GenerateAPIKey() (raw, hash, displayPrefix string, err error) {
	b := make([]byte, 16) // 16 bytes = 32 hex chars
	if _, err = rand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("generate random bytes: %w", err)
	}

	raw = apiKeyPrefix + hex.EncodeToString(b)
	hash = HashAPIKey(raw)
	displayPrefix = hex.EncodeToString(b[:4]) // first 8 hex chars of the random part
	return raw, hash, displayPrefix, nil
}

// HashAPIKey returns the SHA-256 hex digest of a raw API key.
func HashAPIKey(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
