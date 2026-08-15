package store

import (
	"encoding/json"
	"testing"
)

func TestParseSecureCLIEnvLegacyMapDefaultsSensitive(t *testing.T) {
	env, err := ParseSecureCLIEnv([]byte(`{"TOKEN":"secret","PUBLIC_BASE_URL":"https://goclaw.sh"}`))
	if err != nil {
		t.Fatalf("ParseSecureCLIEnv() error = %v", err)
	}
	if got := env["TOKEN"].Kind; got != SecureCLIEnvKindSensitive {
		t.Fatalf("TOKEN kind = %q, want %q", got, SecureCLIEnvKindSensitive)
	}
	if got := env["TOKEN"].Value; got != "secret" {
		t.Fatalf("TOKEN value = %q", got)
	}
	if got := env["PUBLIC_BASE_URL"].Kind; got != SecureCLIEnvKindSensitive {
		t.Fatalf("PUBLIC_BASE_URL kind = %q, want default sensitive", got)
	}
}

func TestParseSecureCLIEnvLegacyScalarsDefaultSensitive(t *testing.T) {
	env, err := ParseSecureCLIEnv([]byte(`{"MAX_UPLOAD_SIZE_MB":100,"DEBUG":true}`))
	if err != nil {
		t.Fatalf("ParseSecureCLIEnv() error = %v", err)
	}
	if got := env["MAX_UPLOAD_SIZE_MB"]; got.Kind != SecureCLIEnvKindSensitive || got.Value != "100" {
		t.Fatalf("MAX_UPLOAD_SIZE_MB = %#v, want sensitive 100", got)
	}
	if got := env["DEBUG"]; got.Kind != SecureCLIEnvKindSensitive || got.Value != "true" {
		t.Fatalf("DEBUG = %#v, want sensitive true", got)
	}
}

func TestSanitizeSecureCLIEnvMasksSensitiveAndReturnsValues(t *testing.T) {
	env := map[string]SecureCLIEnvEntry{
		"TOKEN":           {Kind: SecureCLIEnvKindSensitive, Value: "secret"},
		"PUBLIC_BASE_URL": {Kind: SecureCLIEnvKindValue, Value: "https://goclaw.sh"},
	}
	got := SanitizeSecureCLIEnv(env)

	if got["TOKEN"].Value != nil {
		t.Fatalf("sensitive value leaked: %q", *got["TOKEN"].Value)
	}
	if !got["TOKEN"].Masked {
		t.Fatalf("sensitive masked = false")
	}
	if got["PUBLIC_BASE_URL"].Value == nil || *got["PUBLIC_BASE_URL"].Value != "https://goclaw.sh" {
		t.Fatalf("value entry not returned: %#v", got["PUBLIC_BASE_URL"])
	}
	if got["PUBLIC_BASE_URL"].Masked {
		t.Fatalf("value entry masked = true")
	}
}

func TestMergeSecureCLIEnvPreservesExistingSensitiveOnEmptyValue(t *testing.T) {
	existing := []byte(`{"TOKEN":{"kind":"sensitive","value":"old"},"PUBLIC_BASE_URL":{"kind":"value","value":"https://old.example"}}`)
	incoming := json.RawMessage(`{"TOKEN":{"kind":"sensitive","value":""},"PUBLIC_BASE_URL":{"kind":"value","value":"https://new.example"}}`)

	merged, err := MergeSecureCLIEnv(existing, incoming)
	if err != nil {
		t.Fatalf("MergeSecureCLIEnv() error = %v", err)
	}
	env, err := ParseSecureCLIEnv(merged)
	if err != nil {
		t.Fatalf("ParseSecureCLIEnv(merged) error = %v", err)
	}
	if got := env["TOKEN"].Value; got != "old" {
		t.Fatalf("TOKEN value = %q, want preserved old", got)
	}
	if got := env["PUBLIC_BASE_URL"].Value; got != "https://new.example" {
		t.Fatalf("PUBLIC_BASE_URL = %q", got)
	}
	if got := env["PUBLIC_BASE_URL"].Kind; got != SecureCLIEnvKindValue {
		t.Fatalf("PUBLIC_BASE_URL kind = %q", got)
	}
}

func TestFlattenSecureCLIEnvSupportsEntryShape(t *testing.T) {
	got, err := FlattenSecureCLIEnv([]byte(`{
		"TOKEN":{"kind":"sensitive","value":"secret"},
		"PUBLIC_BASE_URL":{"kind":"value","value":"https://goclaw.sh"}
	}`))
	if err != nil {
		t.Fatalf("FlattenSecureCLIEnv() error = %v", err)
	}
	if got["TOKEN"] != "secret" {
		t.Fatalf("TOKEN = %q", got["TOKEN"])
	}
	if got["PUBLIC_BASE_URL"] != "https://goclaw.sh" {
		t.Fatalf("PUBLIC_BASE_URL = %q", got["PUBLIC_BASE_URL"])
	}
}

func TestParseSecureCLIEnvRejectsInvalidKind(t *testing.T) {
	_, err := ParseSecureCLIEnv([]byte(`{"TOKEN":{"kind":"plain","value":"secret"}}`))
	if err == nil {
		t.Fatalf("ParseSecureCLIEnv() error = nil, want invalid kind error")
	}
}
