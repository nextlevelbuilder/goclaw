// Package crypto provides AES-256-GCM encryption for sensitive data (API keys, tokens).
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
)

const prefix = "aes-gcm:"

// Encrypt encrypts plaintext using AES-256-GCM.
// Returns "aes-gcm:" + base64(nonce + ciphertext + tag).
// If key is empty, returns plaintext unchanged.
func Encrypt(plaintext, key string) (string, error) {
	if key == "" || plaintext == "" {
		return plaintext, nil
	}

	keyBytes, err := DeriveKey(key)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return prefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts ciphertext produced by Encrypt.
// If the value does not have the "aes-gcm:" prefix, it is returned as-is
// (backward compatibility with plain text values).
// If key is empty, returns ciphertext unchanged.
func Decrypt(ciphertext, key string) (string, error) {
	if key == "" || ciphertext == "" {
		return ciphertext, nil
	}

	if !IsEncrypted(ciphertext) {
		slog.Warn("crypto.unencrypted_value_read",
			"hint", "value lacks aes-gcm: prefix — may be legacy plaintext or tampered",
		)
		return ciphertext, nil
	}

	keyBytes, err := DeriveKey(key)
	if err != nil {
		return "", err
	}

	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, prefix))
	if err != nil {
		slog.Warn("crypto.invalid_base64_in_encrypted_value")
		return ciphertext, nil
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		slog.Warn("crypto.encrypted_value_too_short", "len", len(data), "min", nonceSize)
		return ciphertext, nil
	}

	plaintext, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return "", errors.New("decrypt failed: invalid key or corrupted data")
	}

	return string(plaintext), nil
}

// IsEncrypted returns true if the value has the "aes-gcm:" encryption prefix.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, prefix)
}

// DeriveKey converts the input string to a 32-byte AES key.
// Accepts: hex-encoded (64 chars), base64-encoded (44 chars), or raw 32 bytes.
func DeriveKey(input string) ([]byte, error) {
	// Hex-encoded: 64 hex chars = 32 bytes
	if len(input) == 64 {
		if b, err := hex.DecodeString(input); err == nil {
			return b, nil
		}
	}

	// Base64-encoded: 44 chars = 32 bytes
	if len(input) == 44 && strings.HasSuffix(input, "=") {
		if b, err := base64.StdEncoding.DecodeString(input); err == nil && len(b) == 32 {
			return b, nil
		}
	}

	// Raw 32 bytes
	if len(input) == 32 {
		return []byte(input), nil
	}

	return nil, errors.New("encryption key must be 32 bytes (hex-encoded 64 chars, base64 44 chars, or raw 32 bytes)")
}
