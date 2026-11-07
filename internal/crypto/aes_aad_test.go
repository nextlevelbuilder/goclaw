package crypto

import (
	"errors"
	"strings"
	"testing"
)

const testAADKey = "0123456789abcdef0123456789abcdef"

func TestEncryptWithAAD_Roundtrip(t *testing.T) {
	aad := []byte("entry-1")
	ct, err := EncryptWithAAD("s3cret", testAADKey, aad)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !strings.HasPrefix(ct, "v2:aes-gcm:") || !IsEncryptedAAD(ct) {
		t.Fatalf("unexpected envelope %q", ct)
	}
	if IsEncrypted(ct) {
		t.Fatal("v2 value must not look like a legacy envelope")
	}
	got, err := DecryptWithAAD(ct, testAADKey, aad)
	if err != nil || got != "s3cret" {
		t.Fatalf("decrypt = %q, %v", got, err)
	}
}

func TestDecryptWithAAD_WrongAADFails(t *testing.T) {
	ct, err := EncryptWithAAD("s3cret", testAADKey, []byte("entry-1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecryptWithAAD(ct, testAADKey, []byte("entry-2")); err == nil {
		t.Fatal("decrypt with different AAD must fail")
	}
	if _, err := DecryptWithAAD(ct, testAADKey, nil); err == nil {
		t.Fatal("decrypt without AAD must fail")
	}
}

func TestEncryptWithAAD_NonceNeverRepeats(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		ct, err := EncryptWithAAD("same", testAADKey, []byte("a"))
		if err != nil {
			t.Fatal(err)
		}
		if seen[ct] {
			t.Fatal("identical ciphertext produced twice")
		}
		seen[ct] = true
	}
}

func TestEncryptWithAAD_RequiresKey(t *testing.T) {
	if _, err := EncryptWithAAD("x", "", nil); err == nil {
		t.Fatal("empty key must be rejected")
	}
	if _, err := DecryptWithAAD("v2:aes-gcm:AAAA", "", nil); err == nil {
		t.Fatal("empty key must be rejected on decrypt")
	}
}

func TestDecryptWithAAD_RejectsMalformed(t *testing.T) {
	for _, bad := range []string{"aes-gcm:abc", "plain", "v2:aes-gcm:!!!", "v2:aes-gcm:AAAA"} {
		if _, err := DecryptWithAAD(bad, testAADKey, nil); err == nil {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}

// The legacy Decrypt must refuse a v2 value instead of returning it as plaintext.
func TestDecrypt_V2ValueReturnsErrAADRequired(t *testing.T) {
	ct, err := EncryptWithAAD("s3cret", testAADKey, []byte("a"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(ct, testAADKey)
	if !errors.Is(err, ErrAADRequired) {
		t.Fatalf("err = %v, want ErrAADRequired", err)
	}
	if got != "" {
		t.Fatalf("Decrypt leaked %q", got)
	}
}

func TestDecrypt_LegacyPathUnchanged(t *testing.T) {
	ct, err := Encrypt("legacy", testAADKey)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Decrypt(ct, testAADKey); err != nil || got != "legacy" {
		t.Fatalf("legacy roundtrip = %q, %v", got, err)
	}
}
