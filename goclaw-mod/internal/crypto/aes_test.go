package crypto

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

// testKey32 is a deterministic 32-byte raw key for tests.
const testKey32 = "01234567890123456789012345678901" // exactly 32 bytes

func testKeyHex() string {
	return hex.EncodeToString([]byte(testKey32)) // 64 hex chars
}

func testKeyBase64() string {
	return base64.StdEncoding.EncodeToString([]byte(testKey32)) // 44 chars ending with =
}

// --- DeriveKey ---

func TestDeriveKey_Hex64(t *testing.T) {
	k, err := DeriveKey(testKeyHex())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(k) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(k))
	}
}

func TestDeriveKey_Base64_44(t *testing.T) {
	k, err := DeriveKey(testKeyBase64())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(k) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(k))
	}
}

func TestDeriveKey_Raw32(t *testing.T) {
	k, err := DeriveKey(testKey32)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(k) != testKey32 {
		t.Fatalf("raw key mismatch")
	}
}

func TestDeriveKey_InvalidLength(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"too_short", "abc"},
		{"16_bytes", "0123456789abcdef"},
		{"48_bytes", strings.Repeat("a", 48)},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DeriveKey(tt.key)
			if err == nil {
				t.Fatalf("expected error for key len %d, got nil", len(tt.key))
			}
		})
	}
}

func TestDeriveKey_Hex64_InvalidHex(t *testing.T) {
	// 64 chars but not valid hex → should fall through to raw 32 check, then fail
	key := strings.Repeat("zz", 32) // 64 chars, invalid hex
	_, err := DeriveKey(key)
	if err == nil {
		t.Fatal("expected error for invalid hex, got nil")
	}
}

// --- Encrypt with invalid key ---

func TestEncrypt_InvalidKey(t *testing.T) {
	_, err := Encrypt("secret", "too-short-key")
	if err == nil {
		t.Fatal("expected error for invalid key length")
	}
}

// --- Encrypt / Decrypt roundtrip ---

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	tests := []struct {
		name      string
		plaintext string
	}{
		{"simple_ascii", "my-secret-api-key-12345"},
		{"unicode", "你好世界🌍emoji日本語"},
		{"special_chars", `key="value"&foo=bar<>'"!@#$%`},
		{"long_string", strings.Repeat("abcdefghij", 1000)},
		{"single_char", "x"},
		{"whitespace", "  \t\n  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc, err := Encrypt(tt.plaintext, testKey32)
			if err != nil {
				t.Fatalf("encrypt failed: %v", err)
			}
			if !strings.HasPrefix(enc, prefix) {
				t.Fatalf("encrypted value missing prefix, got: %s", enc[:20])
			}

			dec, err := Decrypt(enc, testKey32)
			if err != nil {
				t.Fatalf("decrypt failed: %v", err)
			}
			if dec != tt.plaintext {
				t.Fatalf("roundtrip mismatch: got %q, want %q", dec, tt.plaintext)
			}
		})
	}
}

func TestEncryptDecrypt_EmptyPlaintext(t *testing.T) {
	enc, err := Encrypt("", testKey32)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc != "" {
		t.Fatalf("expected empty string, got %q", enc)
	}
}

func TestEncryptDecrypt_EmptyKey(t *testing.T) {
	// Empty key → plaintext returned unchanged for both encrypt and decrypt
	enc, err := Encrypt("secret", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc != "secret" {
		t.Fatalf("expected plaintext passthrough, got %q", enc)
	}

	dec, err := Decrypt("aes-gcm:someciphertext", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec != "aes-gcm:someciphertext" {
		t.Fatalf("expected ciphertext passthrough, got %q", dec)
	}
}

// --- Nonce uniqueness: same plaintext + key → different ciphertexts ---

func TestEncrypt_NonceUniqueness(t *testing.T) {
	const plaintext = "identical-plaintext"
	enc1, _ := Encrypt(plaintext, testKey32)
	enc2, _ := Encrypt(plaintext, testKey32)
	if enc1 == enc2 {
		t.Fatal("two encryptions of same plaintext must produce different ciphertext (unique nonce)")
	}
}

// --- Backward compatibility: decrypt unencrypted string returns as-is ---

func TestDecrypt_BackwardCompat_PlainText(t *testing.T) {
	plain := "sk-ant-not-encrypted-just-raw"
	dec, err := Decrypt(plain, testKey32)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec != plain {
		t.Fatalf("expected passthrough for unencrypted value, got %q", dec)
	}
}

// --- Error cases ---

func TestDecrypt_CorruptedCiphertext(t *testing.T) {
	// Valid prefix but garbage base64 → should return as-is (line 68: treat as plain text)
	garbage := prefix + "!!!not-base64!!!"
	dec, err := Decrypt(garbage, testKey32)
	if err != nil {
		t.Fatalf("unexpected error for garbage base64: %v", err)
	}
	if dec != garbage {
		t.Fatalf("expected passthrough for invalid base64, got %q", dec)
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	enc, _ := Encrypt("secret", testKey32)

	otherKey := "98765432109876543210987654321098" // different 32-byte key
	_, err := Decrypt(enc, otherKey)
	if err == nil {
		t.Fatal("expected error for wrong key, got nil")
	}
	if !strings.Contains(err.Error(), "decrypt failed") {
		t.Fatalf("expected 'decrypt failed' error, got: %v", err)
	}
}

func TestDecrypt_TooShortCiphertext(t *testing.T) {
	// Valid prefix + valid base64 but too short for nonce → return as-is
	short := prefix + base64.StdEncoding.EncodeToString([]byte("x"))
	dec, err := Decrypt(short, testKey32)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec != short {
		t.Fatalf("expected passthrough for too-short ciphertext, got %q", dec)
	}
}

// --- IsEncrypted ---

func TestIsEncrypted(t *testing.T) {
	if !IsEncrypted("aes-gcm:abc123") {
		t.Fatal("expected true for prefixed value")
	}
	if IsEncrypted("plain-text-value") {
		t.Fatal("expected false for unprefixed value")
	}
	if IsEncrypted("") {
		t.Fatal("expected false for empty string")
	}
}

// --- Cross-key-format roundtrip: encrypt with one format, decrypt with another ---

func TestEncryptDecrypt_CrossKeyFormats(t *testing.T) {
	// All three key formats represent the same 32-byte key
	keyRaw := testKey32
	keyHex := testKeyHex()
	keyB64 := testKeyBase64()

	plaintext := "cross-format-test-value"

	// Encrypt with raw, decrypt with hex
	enc, _ := Encrypt(plaintext, keyRaw)
	dec, err := Decrypt(enc, keyHex)
	if err != nil {
		t.Fatalf("hex decrypt failed: %v", err)
	}
	if dec != plaintext {
		t.Fatalf("cross-format mismatch: raw→hex")
	}

	// Encrypt with hex, decrypt with base64
	enc, _ = Encrypt(plaintext, keyHex)
	dec, err = Decrypt(enc, keyB64)
	if err != nil {
		t.Fatalf("base64 decrypt failed: %v", err)
	}
	if dec != plaintext {
		t.Fatalf("cross-format mismatch: hex→base64")
	}
}
