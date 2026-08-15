package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

// ContentHash returns SHA-256 hex digest of content bytes.
func ContentHash(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

// ContentHashFile reads file at path and returns SHA-256 hex digest.
func ContentHashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("hash file: %w", err)
	}
	return ContentHash(data), nil
}
