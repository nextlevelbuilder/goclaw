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

// aadPrefixV2 marks values sealed with additional authenticated data. They are
// produced only by EncryptWithAAD and must be opened with DecryptWithAAD.
const aadPrefixV2 = "v2:aes-gcm:"

// ErrAADRequired is returned by Decrypt for a value that was sealed with AAD.
// Without this guard such a value would fall through the "no aes-gcm: prefix"
// branch and be returned verbatim as if it were plaintext.
var ErrAADRequired = errors.New("crypto: value uses AAD envelope, use DecryptWithAAD")

// Encrypt encrypts plaintext using AES-256-GCM.
// With a keyring installed that holds key it returns the v3 envelope, which
// names the key; otherwise "aes-gcm:" + base64(nonce + ciphertext + tag).
// If key is empty, returns plaintext unchanged.
func Encrypt(plaintext, key string) (string, error) {
	if key == "" || plaintext == "" {
		return plaintext, nil
	}

	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	if kid, ok := sealingKID(key); ok {
		return v3Prefix + kid + ":" + base64.RawURLEncoding.EncodeToString(sealed), nil
	}
	return prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt decrypts ciphertext produced by Encrypt.
// If the value does not have an encryption prefix, it is returned as-is
// (backward compatibility with plain text values).
// If key is empty, returns ciphertext unchanged.
//
// A v3 value is opened with the key it names (ErrUnknownKeyID when that key is
// not configured). A value without a key id is opened with key and, if that
// fails, with the other keyring keys, so data written before a rotation stays
// readable until it is rewrapped.
func Decrypt(ciphertext, key string) (string, error) {
	if key == "" || ciphertext == "" {
		return ciphertext, nil
	}

	if IsEncryptedAAD(ciphertext) {
		return "", ErrAADRequired
	}

	if IsEncryptedV3(ciphertext) {
		return openV3(ciphertext, key, nil)
	}

	if !IsEncrypted(ciphertext) {
		slog.Warn("crypto.unencrypted_value_read",
			"hint", "value lacks aes-gcm: prefix — may be legacy plaintext or tampered",
		)
		return ciphertext, nil
	}

	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, prefix))
	if err != nil {
		slog.Warn("crypto.invalid_base64_in_encrypted_value")
		return ciphertext, nil
	}
	if len(data) < gcm.NonceSize() {
		slog.Warn("crypto.encrypted_value_too_short", "len", len(data), "min", gcm.NonceSize())
		return ciphertext, nil
	}

	plaintext, err := openWithFallback(data, key, nil)
	if err != nil {
		return "", errors.New("decrypt failed: invalid key or corrupted data")
	}
	return plaintext, nil
}

// IsEncrypted returns true if the value has the "aes-gcm:" encryption prefix.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, prefix)
}

// IsEncryptedAAD returns true if the value carries the "v2:aes-gcm:" prefix.
func IsEncryptedAAD(value string) bool {
	return strings.HasPrefix(value, aadPrefixV2)
}

func newGCM(key string) (cipher.AEAD, error) {
	keyBytes, err := DeriveKey(key)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// openRaw opens nonce||ciphertext||tag with one key.
func openRaw(data []byte, key string, aad []byte) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize()+gcm.Overhead() {
		return "", errors.New("crypto: envelope too short")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], aad)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// openWithFallback opens a value that does not name its key: first with key,
// then with the other keyring keys. Authentication makes a wrong key harmless.
func openWithFallback(data []byte, key string, aad []byte) (string, error) {
	plain, err := openRaw(data, key, aad)
	if err == nil {
		return plain, nil
	}
	kid, _ := KeyID(key)
	for _, other := range otherKeys(kid) {
		if p, oerr := openRaw(data, other, aad); oerr == nil {
			return p, nil
		}
	}
	return "", err
}

// openV3 opens a "v3:aes-gcm:<kid>:<payload>" value with the key it names.
func openV3(value, hint string, aad []byte) (string, error) {
	kid, ok := EnvelopeKeyID(value)
	if !ok {
		return "", errors.New("crypto: malformed v3 envelope")
	}
	key, err := keyForKID(kid, hint)
	if err != nil {
		return "", err
	}
	payload := strings.TrimPrefix(value, v3Prefix+kid+":")
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", errors.New("crypto: invalid v3 envelope encoding")
	}
	plain, err := openRaw(data, key, aad)
	if err != nil {
		return "", errors.New("decrypt failed: invalid key, wrong binding or corrupted data")
	}
	return plain, nil
}

// EncryptWithAAD encrypts plaintext with AES-256-GCM, authenticating aad.
// Returns the v3 envelope when a keyring holds key, otherwise
// "v2:aes-gcm:" + base64url-no-pad(nonce || ciphertext || tag).
// Unlike Encrypt it never passes plaintext through: an empty key is an error,
// because callers use it for values that must never be stored in clear.
func EncryptWithAAD(plaintext, key string, aad []byte) (string, error) {
	if key == "" {
		return "", errors.New("crypto: encryption key required")
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), aad)
	enc := base64.RawURLEncoding.EncodeToString(sealed)
	if kid, ok := sealingKID(key); ok {
		return v3Prefix + kid + ":" + enc, nil
	}
	return aadPrefixV2 + enc, nil
}

// DecryptWithAAD opens a value produced by EncryptWithAAD (v2 or v3). The same
// aad must be supplied; any difference fails authentication.
func DecryptWithAAD(ciphertext, key string, aad []byte) (string, error) {
	if key == "" {
		return "", errors.New("crypto: encryption key required")
	}
	if IsEncryptedV3(ciphertext) {
		return openV3(ciphertext, key, aad)
	}
	if !IsEncryptedAAD(ciphertext) {
		return "", errors.New("crypto: value is not an AAD envelope")
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(ciphertext, aadPrefixV2))
	if err != nil {
		return "", errors.New("crypto: invalid AAD envelope encoding")
	}
	if _, err := newGCM(key); err != nil {
		return "", err
	}
	plain, err := openWithFallback(data, key, aad)
	if err != nil {
		return "", errors.New("decrypt failed: invalid key, wrong binding or corrupted data")
	}
	return plain, nil
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
