package crypto

import (
	"errors"
	"strings"
	"testing"
)

const (
	ringKeyA = "0123456789abcdef0123456789abcdef"
	ringKeyB = "fedcba9876543210fedcba9876543210"
)

func kidOf(t *testing.T, key string) string {
	t.Helper()
	kid, err := KeyID(key)
	if err != nil {
		t.Fatal(err)
	}
	return kid
}

func installRing(t *testing.T, keys []string, active string) *Keyring {
	t.Helper()
	kr, err := NewKeyring(keys, active)
	if err != nil {
		t.Fatal(err)
	}
	SetKeyring(kr)
	t.Cleanup(func() { SetKeyring(nil) })
	return kr
}

func TestKeyID_IsStableAndShort(t *testing.T) {
	a := kidOf(t, ringKeyA)
	if len(a) != 8 || a != kidOf(t, ringKeyA) || a == kidOf(t, ringKeyB) {
		t.Fatalf("kid %q not stable/short/distinct", a)
	}
	if _, err := KeyID("short"); err == nil {
		t.Fatal("bad key length must be rejected")
	}
}

func TestV3_RoundtripNamesTheKey(t *testing.T) {
	kidA, kidB := kidOf(t, ringKeyA), kidOf(t, ringKeyB)
	installRing(t, []string{ringKeyA, ringKeyB}, kidA)

	ct, err := Encrypt("secret", ringKeyA)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ct, "v3:aes-gcm:"+kidA+":") || IsEncrypted(ct) {
		t.Fatalf("envelope = %q", ct)
	}
	if kid, ok := EnvelopeKeyID(ct); !ok || kid != kidA {
		t.Fatalf("EnvelopeKeyID = %q %v", kid, ok)
	}
	// The key argument is only a hint for v3: the named key is used.
	if got, err := Decrypt(ct, ringKeyB); err != nil || got != "secret" {
		t.Fatalf("decrypt = %q, %v", got, err)
	}
	if kid, _ := EnvelopeKeyID(mustEncrypt(t, "x", ringKeyB)); kid != kidB {
		t.Fatalf("sealing with key B stamped %q", kid)
	}
}

func mustEncrypt(t *testing.T, pt, key string) string {
	t.Helper()
	ct, err := Encrypt(pt, key)
	if err != nil {
		t.Fatal(err)
	}
	return ct
}

func TestV3_UnknownKeyIDIsDistinctAndNeverFallsBack(t *testing.T) {
	installRing(t, []string{ringKeyA, ringKeyB}, kidOf(t, ringKeyA))
	ct := mustEncrypt(t, "secret", ringKeyB)

	// Remove B from the ring; A is still configured but must not be tried.
	installRing(t, []string{ringKeyA}, kidOf(t, ringKeyA))
	_, err := Decrypt(ct, ringKeyA)
	var unknown *UnknownKeyIDError
	if !errors.Is(err, ErrUnknownKeyID) || !errors.As(err, &unknown) || unknown.KID != kidOf(t, ringKeyB) {
		t.Fatalf("err = %v, want ErrUnknownKeyID naming %s", err, kidOf(t, ringKeyB))
	}
	if strings.Contains(err.Error(), ringKeyA) || strings.Contains(err.Error(), ringKeyB) {
		t.Fatal("error leaks key material")
	}
}

func TestV3_WorksWithoutAKeyringWhenTheHintIsTheNamedKey(t *testing.T) {
	ct := func() string {
		installRing(t, []string{ringKeyA}, kidOf(t, ringKeyA))
		defer SetKeyring(nil)
		return mustEncrypt(t, "secret", ringKeyA)
	}()
	if got, err := Decrypt(ct, ringKeyA); err != nil || got != "secret" {
		t.Fatalf("decrypt = %q, %v", got, err)
	}
	if _, err := Decrypt(ct, ringKeyB); !errors.Is(err, ErrUnknownKeyID) {
		t.Fatalf("wrong hint err = %v", err)
	}
}

func TestLegacyValuesStayReadableAfterRotation(t *testing.T) {
	v1 := mustEncrypt(t, "old-secret", ringKeyA) // no keyring: v1
	if !IsEncrypted(v1) {
		t.Fatalf("expected v1, got %q", v1)
	}
	aad := []byte("entry")
	v2, err := EncryptWithAAD("old-bound", ringKeyA, aad)
	if err != nil || !IsEncryptedAAD(v2) {
		t.Fatalf("v2 = %q, %v", v2, err)
	}

	// B becomes active; A stays in the ring for old data.
	installRing(t, []string{ringKeyA, ringKeyB}, kidOf(t, ringKeyB))
	if got, err := Decrypt(v1, ringKeyB); err != nil || got != "old-secret" {
		t.Fatalf("v1 after rotation = %q, %v", got, err)
	}
	if got, err := DecryptWithAAD(v2, ringKeyB, aad); err != nil || got != "old-bound" {
		t.Fatalf("v2 after rotation = %q, %v", got, err)
	}

	// Without A in the ring the old values are genuinely unreadable.
	installRing(t, []string{ringKeyB}, kidOf(t, ringKeyB))
	if _, err := Decrypt(v1, ringKeyB); err == nil {
		t.Fatal("v1 must not open without its key")
	}
}

func TestV3_AADBindingIsEnforced(t *testing.T) {
	installRing(t, []string{ringKeyA}, kidOf(t, ringKeyA))
	ct, err := EncryptWithAAD("bound", ringKeyA, []byte("one"))
	if err != nil || !IsEncryptedV3(ct) {
		t.Fatalf("ct = %q, %v", ct, err)
	}
	if got, err := DecryptWithAAD(ct, ringKeyA, []byte("one")); err != nil || got != "bound" {
		t.Fatalf("decrypt = %q, %v", got, err)
	}
	if _, err := DecryptWithAAD(ct, ringKeyA, []byte("two")); err == nil {
		t.Fatal("different AAD must fail")
	}
	if _, err := Decrypt(ct, ringKeyA); err == nil {
		t.Fatal("plain Decrypt must not open an AAD-bound value")
	}
}

func TestParseKeyring(t *testing.T) {
	kidA, kidB := kidOf(t, ringKeyA), kidOf(t, ringKeyB)

	t.Run("single legacy key is active", func(t *testing.T) {
		kr, err := ParseKeyring("", "", ringKeyA)
		if err != nil || kr.ActiveKID() != kidA || kr.ActiveKey() != ringKeyA {
			t.Fatalf("kr = %+v, %v", kr, err)
		}
	})
	t.Run("nothing configured", func(t *testing.T) {
		if kr, err := ParseKeyring("", "", ""); kr != nil || err != nil {
			t.Fatalf("kr = %v, err = %v", kr, err)
		}
	})
	t.Run("keys list with explicit active", func(t *testing.T) {
		kr, err := ParseKeyring(kidA+"="+ringKeyA+", "+kidB+"="+ringKeyB, kidB, "")
		if err != nil || kr.ActiveKID() != kidB || len(kr.KIDs()) != 2 {
			t.Fatalf("kr = %+v, %v", kr, err)
		}
	})
	t.Run("legacy key plus list defaults to the legacy key", func(t *testing.T) {
		kr, err := ParseKeyring(kidB+"="+ringKeyB, "", ringKeyA)
		if err != nil || kr.ActiveKID() != kidA || len(kr.KIDs()) != 2 {
			t.Fatalf("kr = %+v, %v", kr, err)
		}
	})
	for name, c := range map[string]struct{ keys, active, legacy string }{
		"several keys, no active":  {kidA + "=" + ringKeyA + "," + kidB + "=" + ringKeyB, "", ""},
		"active not in ring":       {kidA + "=" + ringKeyA, kidB, ""},
		"label does not match key": {"deadbeef=" + ringKeyA, "", ""},
		"bad key length":           {kidA + "=short", "", ""},
		"missing equals":           {ringKeyA, "", ""},
		"bad legacy key":           {"", "", "short"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseKeyring(c.keys, c.active, c.legacy)
			if err == nil {
				t.Fatal("want an error")
			}
			for _, k := range []string{ringKeyA, ringKeyB} {
				if strings.Contains(err.Error(), k) {
					t.Fatalf("error leaks key material: %v", err)
				}
			}
		})
	}
	t.Run("same key twice is one key", func(t *testing.T) {
		kr, err := ParseKeyring(kidA+"="+ringKeyA, "", ringKeyA)
		if err != nil || len(kr.KIDs()) != 1 {
			t.Fatalf("kr = %+v, %v", kr, err)
		}
	})
}

func TestNewKeyring_RejectsDuplicatesAndEmpty(t *testing.T) {
	if _, err := NewKeyring(nil, "x"); err == nil {
		t.Fatal("empty ring must be rejected")
	}
	if _, err := NewKeyring([]string{ringKeyA, ringKeyA}, kidOf(t, ringKeyA)); err == nil {
		t.Fatal("duplicate key must be rejected")
	}
}
