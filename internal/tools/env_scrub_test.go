package tools

import (
	"strings"
	"testing"
)

func envContains(env []string, key string) bool {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return true
		}
	}
	return false
}

func TestScrubCredentialEnv_StripsStatic(t *testing.T) {
	in := []string{
		"HOME=/root",
		"GH_TOKEN=secret-abc",
		"PATH=/usr/bin",
		"AWS_SECRET_ACCESS_KEY=topsecret",
	}
	out := scrubCredentialEnv(in, nil)

	if envContains(out, "GH_TOKEN") {
		t.Fatalf("GH_TOKEN must be scrubbed, got: %v", out)
	}
	if envContains(out, "AWS_SECRET_ACCESS_KEY") {
		t.Fatalf("AWS_SECRET_ACCESS_KEY must be scrubbed, got: %v", out)
	}
	if !envContains(out, "HOME") || !envContains(out, "PATH") {
		t.Fatalf("essential vars must be preserved, got: %v", out)
	}
}

func TestScrubCredentialEnv_StripsDynamic(t *testing.T) {
	in := []string{
		"HOME=/root",
		"MY_CUSTOM_SECRET=hello",
		"KEEP_ME=yes",
	}
	out := scrubCredentialEnv(in, []string{"MY_CUSTOM_SECRET"})

	if envContains(out, "MY_CUSTOM_SECRET") {
		t.Fatalf("MY_CUSTOM_SECRET must be scrubbed, got: %v", out)
	}
	if !envContains(out, "KEEP_ME") {
		t.Fatalf("KEEP_ME must be preserved, got: %v", out)
	}
}

func TestScrubCredentialEnv_PreservesEssentials(t *testing.T) {
	in := []string{
		"HOME=/root",
		"PATH=/usr/bin",
		"TERM=xterm",
		"LANG=en_US.UTF-8",
		"USER=alice",
		"TZ=UTC",
	}
	out := scrubCredentialEnv(in, nil)
	for _, k := range []string{"HOME", "PATH", "TERM", "LANG", "USER", "TZ"} {
		if !envContains(out, k) {
			t.Fatalf("expected %s preserved, got: %v", k, out)
		}
	}
}

func TestScrubCredentialEnv_PreservesUnrelated(t *testing.T) {
	in := []string{
		"FOO=bar",
		"RANDOM_APP_FLAG=1",
		"NPM_TOKEN=leakme", // static deny-list → should be scrubbed
	}
	out := scrubCredentialEnv(in, nil)
	if !envContains(out, "FOO") {
		t.Fatalf("FOO must be preserved, got: %v", out)
	}
	if !envContains(out, "RANDOM_APP_FLAG") {
		t.Fatalf("RANDOM_APP_FLAG must be preserved, got: %v", out)
	}
	if envContains(out, "NPM_TOKEN") {
		t.Fatalf("NPM_TOKEN must be scrubbed (static), got: %v", out)
	}
}

// Basic smoke for the JSON key extractor used to feed dynamic keys.
func TestExtractJSONTopKeys_Simple(t *testing.T) {
	keys := extractJSONTopKeys([]byte(`{"GH_TOKEN":"x","GH_CONFIG_DIR":"/tmp"}`))
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d: %v", len(keys), keys)
	}
	want := map[string]bool{"GH_TOKEN": true, "GH_CONFIG_DIR": true}
	for _, k := range keys {
		if !want[k] {
			t.Fatalf("unexpected key %q", k)
		}
	}
}

func TestExtractJSONTopKeys_NestedIgnored(t *testing.T) {
	keys := extractJSONTopKeys([]byte(`{"A":{"nested":"v"},"B":"x"}`))
	if len(keys) != 2 {
		t.Fatalf("expected 2 top keys, got %d: %v", len(keys), keys)
	}
}

func TestExtractJSONTopKeys_Malformed(t *testing.T) {
	if keys := extractJSONTopKeys([]byte(`not json`)); keys != nil {
		t.Fatalf("expected nil on malformed input, got %v", keys)
	}
}
