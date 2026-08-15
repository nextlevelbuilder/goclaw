//go:build sqliteonly

package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

const (
	serviceName = "goclaw-desktop"
	keyEncKey   = "encryption_key"
	keyGwToken  = "gateway_token"
)

// EnsureSecrets retrieves or generates the encryption key and gateway token.
// Primary storage: OS keyring. Fallback: file-based storage in ~/.goclaw/secrets/.
func EnsureSecrets() (encKey, gwToken string, err error) {
	encKey, err = getOrCreateSecret(keyEncKey, 32)
	if err != nil {
		return "", "", fmt.Errorf("encryption key: %w", err)
	}
	gwToken, err = getOrCreateSecret(keyGwToken, 32)
	if err != nil {
		return "", "", fmt.Errorf("gateway token: %w", err)
	}
	return encKey, gwToken, nil
}

func getOrCreateSecret(key string, numBytes int) (string, error) {
	// Try OS keyring first.
	val, err := keyring.Get(serviceName, key)
	if err == nil && val != "" {
		return val, nil
	}

	// Try file-based fallback.
	val, err = readSecretFile(key)
	if err == nil && val != "" {
		return val, nil
	}

	// Generate a new random secret.
	val = generateHex(numBytes)

	// Persist to keyring; fall back to file if keyring unavailable.
	if kerr := keyring.Set(serviceName, key, val); kerr != nil {
		slog.Warn("keyring unavailable, using file fallback", "key", key, "error", kerr)
		if ferr := writeSecretFile(key, val); ferr != nil {
			return "", fmt.Errorf("failed to store secret: %w", ferr)
		}
	}

	return val, nil
}

func generateHex(numBytes int) string {
	b := make([]byte, numBytes)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func secretsDir() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".goclaw", "secrets")
	os.MkdirAll(dir, 0700)
	return dir
}

func readSecretFile(key string) (string, error) {
	data, err := os.ReadFile(filepath.Join(secretsDir(), key))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func writeSecretFile(key, value string) error {
	return os.WriteFile(filepath.Join(secretsDir(), key), []byte(value), 0600)
}
