package teams

import (
	"crypto/rand"
	"crypto/rsa"
	"math/big"
	"testing"
)

func TestParseRSAPublicKey(t *testing.T) {
	// Generate a real RSA key and verify round-trip
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	// Encode N and E as base64url (matching JWKS format)
	nB64 := base64RawURLEncode(privKey.PublicKey.N.Bytes())
	eB64 := base64RawURLEncode(big.NewInt(int64(privKey.PublicKey.E)).Bytes())

	pub, err := parseRSAPublicKey(nB64, eB64)
	if err != nil {
		t.Fatalf("parseRSAPublicKey: %v", err)
	}
	if pub.N.Cmp(privKey.PublicKey.N) != 0 {
		t.Error("N mismatch")
	}
	if pub.E != privKey.PublicKey.E {
		t.Errorf("E = %d, want %d", pub.E, privKey.PublicKey.E)
	}
}

func TestParseRSAPublicKey_InvalidBase64(t *testing.T) {
	_, err := parseRSAPublicKey("not-valid-base64!!!", "AQAB")
	if err == nil {
		t.Error("expected error for invalid N base64")
	}

	_, err = parseRSAPublicKey("AQAB", "not-valid!!!")
	if err == nil {
		t.Error("expected error for invalid E base64")
	}
}

func TestNewTokenValidator(t *testing.T) {
	v := newTokenValidator("bot-id", "tenant-id")
	if v.botID != "bot-id" {
		t.Errorf("botID = %q, want %q", v.botID, "bot-id")
	}
	if v.tenantID != "tenant-id" {
		t.Errorf("tenantID = %q, want %q", v.tenantID, "tenant-id")
	}
	if v.keys == nil {
		t.Error("keys map should be initialized")
	}
}

func TestNewTokenValidator_EmptyTenant(t *testing.T) {
	v := newTokenValidator("bot-id", "")
	if v.tenantID != "" {
		t.Errorf("tenantID = %q, want empty", v.tenantID)
	}
}

// base64RawURLEncode encodes bytes to base64url without padding.
func base64RawURLEncode(data []byte) string {
	const enc = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	result := make([]byte, 0, (len(data)*4+2)/3)
	for i := 0; i < len(data); i += 3 {
		val := uint(data[i]) << 16
		if i+1 < len(data) {
			val |= uint(data[i+1]) << 8
		}
		if i+2 < len(data) {
			val |= uint(data[i+2])
		}
		result = append(result, enc[(val>>18)&0x3F])
		result = append(result, enc[(val>>12)&0x3F])
		if i+1 < len(data) {
			result = append(result, enc[(val>>6)&0x3F])
		}
		if i+2 < len(data) {
			result = append(result, enc[val&0x3F])
		}
	}
	return string(result)
}
