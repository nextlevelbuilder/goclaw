package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

const testVaultKey = "0123456789abcdef0123456789abcdef"

func TestSecretPayloadRoundtrip(t *testing.T) {
	in := SecretVaultPayload{Username: "alice", Secret: "s3cr3t-value"}
	enc, err := EncodeSecretPayload(in, testVaultKey)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(enc, "s3cr3t-value") || strings.Contains(enc, "alice") {
		t.Fatal("encoded payload leaks plaintext")
	}
	out, err := DecodeSecretPayload(enc, testVaultKey)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatal("roundtrip mismatch")
	}
}

func TestSecretPayloadFailsClosed(t *testing.T) {
	if _, err := EncodeSecretPayload(SecretVaultPayload{Secret: "x"}, ""); !errors.Is(err, ErrSecretVaultNoKey) {
		t.Fatalf("empty key on encode: %v", err)
	}
	if _, err := DecodeSecretPayload("whatever", ""); !errors.Is(err, ErrSecretVaultNoKey) {
		t.Fatalf("empty key on decode: %v", err)
	}
	if _, err := DecodeSecretPayload(`{"s":"plain"}`, testVaultKey); !errors.Is(err, ErrSecretVaultNotEncrypted) {
		t.Fatalf("unencrypted stored value must be rejected: %v", err)
	}
	enc, _ := EncodeSecretPayload(SecretVaultPayload{Secret: "x"}, testVaultKey)
	if _, err := DecodeSecretPayload(enc, "fedcba9876543210fedcba9876543210"); err == nil {
		t.Fatal("wrong key must fail")
	}
}

func TestNormalizeSecretDomains(t *testing.T) {
	got, err := NormalizeSecretDomains([]string{" API.Example.com ", "api.example.com", "*.example.org"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "api.example.com" || got[1] != "*.example.org" {
		t.Fatalf("got %v", got)
	}
	bad := [][]string{nil, {}, {"https://a.com"}, {"a.com/path"}, {"a.com:443"}, {"*"}, {"*."}, {"-a.com"}, {""}}
	for _, b := range bad {
		if _, err := NormalizeSecretDomains(b); !errors.Is(err, ErrSecretVaultInvalid) {
			t.Errorf("%v: want invalid, got %v", b, err)
		}
	}
	many := make([]string, 21)
	for i := range many {
		many[i] = fmt.Sprintf("h%d.example.com", i)
	}
	if _, err := NormalizeSecretDomains(many); !errors.Is(err, ErrSecretVaultInvalid) {
		t.Fatal("too many domains must fail")
	}
}

func TestValidateSecretVaultEntryAndPayload(t *testing.T) {
	ok := func() *SecretVaultEntry {
		return &SecretVaultEntry{Name: "github-token", Kind: SecretKindAPIKey, AllowedDomains: []string{"api.github.com"}, CreatedBy: "user1"}
	}
	if err := ValidateSecretVaultEntry(ok()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "Upper", "-lead", "has space", strings.Repeat("a", 65)} {
		e := ok()
		e.Name = name
		if err := ValidateSecretVaultEntry(e); !errors.Is(err, ErrSecretVaultInvalid) {
			t.Errorf("name %q: %v", name, err)
		}
	}
	e := ok()
	e.Kind = "password"
	if err := ValidateSecretVaultEntry(e); !errors.Is(err, ErrSecretVaultInvalid) {
		t.Error("bad kind accepted")
	}
	e = ok()
	e.Hints = strings.Repeat("h", 501)
	if err := ValidateSecretVaultEntry(e); !errors.Is(err, ErrSecretVaultInvalid) {
		t.Error("long hints accepted")
	}

	cases := []struct {
		kind string
		p    SecretVaultPayload
		ok   bool
	}{
		{SecretKindAPIKey, SecretVaultPayload{Secret: "k"}, true},
		{SecretKindAPIKey, SecretVaultPayload{Username: "u", Secret: "k"}, false},
		{SecretKindAPIKey, SecretVaultPayload{}, false},
		{SecretKindLogin, SecretVaultPayload{Username: "u", Secret: "p"}, true},
		{SecretKindLogin, SecretVaultPayload{Secret: "p"}, false},
		{"other", SecretVaultPayload{Secret: "p"}, false},
	}
	for _, c := range cases {
		err := ValidateSecretVaultPayload(c.kind, c.p)
		if (err == nil) != c.ok {
			t.Errorf("%s: unexpected result err=%v", c.kind, err)
		}
	}
}

func TestSecretPayloadNeverFormatsValue(t *testing.T) {
	p := SecretVaultPayload{Username: "alice", Secret: "topsecret"}
	for _, s := range []string{fmt.Sprintf("%v", p), fmt.Sprintf("%+v", p), fmt.Sprintf("%#v", p), fmt.Sprint(&p)} {
		if strings.Contains(s, "topsecret") || strings.Contains(s, "alice") {
			t.Fatalf("payload leaked via formatting: %s", s)
		}
	}
}
