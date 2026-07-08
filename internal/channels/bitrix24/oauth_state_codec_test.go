package bitrix24

import (
	"strings"
	"testing"
	"time"
)

func testOAuthKey() []byte {
	return []byte("01234567890123456789012345678901") // 32 bytes, test-only
}

func TestOAuthState_RoundTrip(t *testing.T) {
	key := testOAuthKey()
	want := oauthStatePayload{
		UserID:      "1058",
		TenantID:    "0193a5b0-7000-7000-8000-000000000001",
		ChannelName: "b24-syn",
		Domain:      "tamgiac.bitrix24.com",
		DialogID:    "chat4878",
		ExpiresAt:   time.Now().Add(10 * time.Minute).Unix(),
	}

	state, err := encodeOAuthState(want, key)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	got, err := decodeOAuthState(state, key)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if *got != want {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", *got, want)
	}
}

func TestOAuthState_TamperedSignature(t *testing.T) {
	key := testOAuthKey()
	state, err := encodeOAuthState(oauthStatePayload{
		UserID: "1", ExpiresAt: time.Now().Add(time.Minute).Unix(),
	}, key)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	idx := strings.LastIndex(state, ".")
	tampered := state[:idx] + ".deadbeef"
	if _, err := decodeOAuthState(tampered, key); err == nil {
		t.Fatal("expected error for tampered signature, got nil")
	}
}

func TestOAuthState_TamperedPayload(t *testing.T) {
	key := testOAuthKey()
	state, err := encodeOAuthState(oauthStatePayload{
		UserID: "1", ExpiresAt: time.Now().Add(time.Minute).Unix(),
	}, key)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	idx := strings.LastIndex(state, ".")
	payload, sig := state[:idx], state[idx:]
	// Flip the last payload byte — invalidates the signature over that payload.
	tampered := payload[:len(payload)-1] + "x" + sig
	if _, err := decodeOAuthState(tampered, key); err == nil {
		t.Fatal("expected error for tampered payload, got nil")
	}
}

func TestOAuthState_Expired(t *testing.T) {
	key := testOAuthKey()
	state, err := encodeOAuthState(oauthStatePayload{
		UserID: "1", ExpiresAt: time.Now().Add(-time.Minute).Unix(), // already expired
	}, key)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	if _, err := decodeOAuthState(state, key); err == nil {
		t.Fatal("expected error for expired state, got nil")
	}
}

func TestOAuthState_Malformed(t *testing.T) {
	key := testOAuthKey()
	cases := []string{
		"",
		"no-dot-separator",
		"not-base64!!!.deadbeef",
		"validbase64part.not-hex-signature",
	}
	for _, c := range cases {
		if _, err := decodeOAuthState(c, key); err == nil {
			t.Fatalf("expected error for malformed state %q, got nil", c)
		}
	}
}

func TestOAuthState_WrongKeyRejected(t *testing.T) {
	state, err := encodeOAuthState(oauthStatePayload{
		UserID: "1", ExpiresAt: time.Now().Add(time.Minute).Unix(),
	}, testOAuthKey())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	otherKey := []byte("98765432109876543210987654321098")
	if _, err := decodeOAuthState(state, otherKey); err == nil {
		t.Fatal("expected error when decoding with a different key, got nil")
	}
}
