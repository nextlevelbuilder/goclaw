package tools

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestMergeCredentialedEnvPerUserOverridesGrantEnv(t *testing.T) {
	binary := &store.SecureCLIBinary{
		EncryptedEnv: []byte(`{"SHARED_KEY":"binary","BINARY_ONLY":"base"}`),
	}
	binary.MergeGrantOverrides(&store.SecureCLIAgentGrant{
		EncryptedEnv: []byte(`{"SHARED_KEY":"grant","GRANT_ONLY":"agent"}`),
	})
	binary.UserEnv = []byte(`{"SHARED_KEY":"user","USER_ONLY":"personal"}`)

	env, err := mergeCredentialedEnv(binary)
	if err != nil {
		t.Fatalf("mergeCredentialedEnv returned error: %v", err)
	}

	if got := env["SHARED_KEY"]; got != "user" {
		t.Fatalf("expected per-user env to win for duplicate key, got %q", got)
	}
	if got := env["GRANT_ONLY"]; got != "agent" {
		t.Fatalf("expected grant env key to remain, got %q", got)
	}
	if got := env["USER_ONLY"]; got != "personal" {
		t.Fatalf("expected per-user env key to remain, got %q", got)
	}
	if _, ok := env["BINARY_ONLY"]; ok {
		t.Fatal("expected agent grant env to replace binary default env")
	}
}

func TestMergeCredentialedEnvFailsClosedOnInvalidUserEnv(t *testing.T) {
	_, err := mergeCredentialedEnv(&store.SecureCLIBinary{
		EncryptedEnv: []byte(`{"SHARED_KEY":"grant"}`),
		UserEnv:      []byte(`{broken json`),
	})
	if err == nil {
		t.Fatal("expected invalid per-user env JSON to fail closed")
	}
}
