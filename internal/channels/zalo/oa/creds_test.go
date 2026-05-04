package oa

import (
	"encoding/json"
	"testing"
	"time"
)

func TestLoadCreds_PlaintextRoundtrip(t *testing.T) {
	t.Parallel()

	// Plaintext JSON inside the store-encrypted blob (mirrors zalo bot factory).
	in := []byte(`{
		"app_id": "1234567890",
		"secret_key": "shh-dummy",
		"oa_id": "9999",
		"access_token": "at_old",
		"refresh_token": "rt_old",
		"expires_at": "2026-04-19T23:00:00Z",
		"last_refresh_at": "2026-04-19T22:00:00Z"
	}`)

	c, err := LoadCreds(in)
	if err != nil {
		t.Fatalf("LoadCreds: %v", err)
	}
	if c.AppID != "1234567890" {
		t.Errorf("AppID = %q", c.AppID)
	}
	if c.SecretKey != "shh-dummy" {
		t.Errorf("SecretKey = %q", c.SecretKey)
	}
	if c.AccessToken != "at_old" {
		t.Errorf("AccessToken = %q", c.AccessToken)
	}
	if c.OAID != "9999" {
		t.Errorf("OAID = %q", c.OAID)
	}
	wantExp, _ := time.Parse(time.RFC3339, "2026-04-19T23:00:00Z")
	if !c.ExpiresAt.Equal(wantExp) {
		t.Errorf("ExpiresAt = %v, want %v", c.ExpiresAt, wantExp)
	}

	out, err := c.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	c2, err := LoadCreds(out)
	if err != nil {
		t.Fatalf("LoadCreds(out): %v", err)
	}
	if *c != *c2 {
		t.Errorf("round-trip mismatch:\n in=%+v\nout=%+v", c, c2)
	}
}

func TestWithTokens_MutatesAndStampsRefreshTime(t *testing.T) {
	t.Parallel()

	c := &ChannelCreds{AppID: "x", SecretKey: "y", AccessToken: "old_at", RefreshToken: "old_rt"}
	tok := &Tokens{
		AccessToken:  "new_at",
		RefreshToken: "new_rt",
		ExpiresAt:    time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC),
	}

	before := time.Now()
	c.WithTokens(tok)
	if c.AccessToken != "new_at" || c.RefreshToken != "new_rt" {
		t.Errorf("tokens not updated: %+v", c)
	}
	if !c.ExpiresAt.Equal(tok.ExpiresAt) {
		t.Errorf("ExpiresAt not updated: %v", c.ExpiresAt)
	}
	if c.LastRefreshAt.Before(before) {
		t.Errorf("LastRefreshAt not stamped: %v", c.LastRefreshAt)
	}
}

func TestLoadCreds_InvalidJSON(t *testing.T) {
	t.Parallel()
	if _, err := LoadCreds([]byte(`{not json`)); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMarshal_NoFieldEncryption(t *testing.T) {
	// Guards against accidental field-level encryption — the store layer
	// already encrypts the entire blob; doing it twice would break decode.
	t.Parallel()

	c := &ChannelCreds{
		AppID:        "1234",
		SecretKey:    "RAW-IN-JSON",
		AccessToken:  "RAW-AT",
		RefreshToken: "RAW-RT",
	}
	b, err := c.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw["secret_key"] != "RAW-IN-JSON" {
		t.Errorf("secret_key not plaintext: %v", raw["secret_key"])
	}
	if raw["access_token"] != "RAW-AT" {
		t.Errorf("access_token not plaintext: %v", raw["access_token"])
	}
}
