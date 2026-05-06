package max

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

// computeHash is a test helper that produces the HMAC the Max API would.
func computeHash(token, vcf string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(vcf))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyContactHash_Valid(t *testing.T) {
	token := "test-bot-token"
	vcf := "BEGIN:VCARD\nVERSION:3.0\nFN:Test User\nTEL:+71234567890\nEND:VCARD"
	hash := computeHash(token, vcf)

	if err := VerifyContactHash(token, vcf, hash); err != nil {
		t.Fatalf("expected valid hash, got: %v", err)
	}
}

func TestVerifyContactHash_TamperedVCF(t *testing.T) {
	token := "test-bot-token"
	vcf := "BEGIN:VCARD\nVERSION:3.0\nFN:Real User\nEND:VCARD"
	hash := computeHash(token, vcf)

	// Attacker swaps vcf, keeping the original hash.
	tampered := "BEGIN:VCARD\nVERSION:3.0\nFN:Evil User\nEND:VCARD"
	err := VerifyContactHash(token, tampered, hash)
	if !errors.Is(err, ErrInvalidContactHash) {
		t.Fatalf("expected ErrInvalidContactHash, got: %v", err)
	}
}

func TestVerifyContactHash_DifferentToken(t *testing.T) {
	vcf := "BEGIN:VCARD\nEND:VCARD"
	hash := computeHash("token-a", vcf)

	err := VerifyContactHash("token-b", vcf, hash)
	if !errors.Is(err, ErrInvalidContactHash) {
		t.Fatalf("expected ErrInvalidContactHash for wrong token, got: %v", err)
	}
}

func TestVerifyContactHash_EmptyHash(t *testing.T) {
	err := VerifyContactHash("token", "vcf", "")
	if !errors.Is(err, ErrInvalidContactHash) {
		t.Fatalf("expected ErrInvalidContactHash for empty hash, got: %v", err)
	}
}

func TestVerifyContactHash_InvalidHex(t *testing.T) {
	err := VerifyContactHash("token", "vcf", "not-valid-hex-string!")
	if !errors.Is(err, ErrInvalidHashFormat) {
		t.Fatalf("expected ErrInvalidHashFormat, got: %v", err)
	}
}

func TestVerifyContactHash_EmptyToken(t *testing.T) {
	err := VerifyContactHash("", "vcf", "abcd")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestVerifyContactHash_EmptyVCF(t *testing.T) {
	err := VerifyContactHash("token", "", "abcd")
	if err == nil {
		t.Fatal("expected error for empty vcf")
	}
}

func TestVerifyContactHash_TruncatedHash(t *testing.T) {
	// Half of a real hash — must be rejected.
	token := "tok"
	vcf := "data"
	full := computeHash(token, vcf)
	truncated := full[:32] // half-length

	err := VerifyContactHash(token, vcf, truncated)
	if !errors.Is(err, ErrInvalidContactHash) {
		t.Fatalf("expected ErrInvalidContactHash for truncated hash, got: %v", err)
	}
}
