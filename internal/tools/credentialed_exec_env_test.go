package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"

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

func TestMergeCredentialedEnvFlattensSensitiveValueEntries(t *testing.T) {
	binary := &store.SecureCLIBinary{
		EncryptedEnv: []byte(`{
			"TOKEN":{"kind":"sensitive","value":"secret"},
			"PUBLIC_BASE_URL":{"kind":"value","value":"https://goclaw.sh"}
		}`),
		UserEnv: []byte(`{"PUBLIC_BASE_URL":{"kind":"value","value":"https://user.example"}}`),
	}

	env, err := mergeCredentialedEnv(binary)
	if err != nil {
		t.Fatalf("mergeCredentialedEnv() error = %v", err)
	}
	if env["TOKEN"] != "secret" {
		t.Fatalf("TOKEN = %q", env["TOKEN"])
	}
	if env["PUBLIC_BASE_URL"] != "https://user.example" {
		t.Fatalf("PUBLIC_BASE_URL = %q", env["PUBLIC_BASE_URL"])
	}
}

func TestMergeCredentialedEnvUsesAgentEnvCredential(t *testing.T) {
	typ := "env"
	binary := &store.SecureCLIBinary{
		EncryptedEnv: []byte(`{"SHARED_KEY":"binary","BINARY_ONLY":"base"}`),
	}
	binary.SetEffectiveCredential([]byte(`{"SHARED_KEY":"agent","AGENT_ONLY":"scoped"}`), &typ, nil, "agent", "")

	env, err := mergeCredentialedEnv(binary)
	if err != nil {
		t.Fatalf("mergeCredentialedEnv() error = %v", err)
	}
	if env["SHARED_KEY"] != "agent" {
		t.Fatalf("SHARED_KEY = %q, want agent override", env["SHARED_KEY"])
	}
	if env["AGENT_ONLY"] != "scoped" {
		t.Fatalf("AGENT_ONLY = %q", env["AGENT_ONLY"])
	}
}

func TestMergeCredentialedEnvDoesNotFlattenTypedCredentialBlob(t *testing.T) {
	typ := "pat"
	binary := &store.SecureCLIBinary{
		EncryptedEnv: []byte(`{"PUBLIC_BASE_URL":"https://goclaw.sh"}`),
	}
	binary.SetEffectiveCredential([]byte(`{"token":"ghp_not_real_token"}`), &typ, nil, "agent", "")

	env, err := mergeCredentialedEnv(binary)
	if err != nil {
		t.Fatalf("mergeCredentialedEnv() error = %v", err)
	}
	if _, ok := env["token"]; ok {
		t.Fatalf("typed credential blob was flattened into child env: %#v", env)
	}
	if env["PUBLIC_BASE_URL"] != "https://goclaw.sh" {
		t.Fatalf("PUBLIC_BASE_URL = %q", env["PUBLIC_BASE_URL"])
	}
}

func TestExec_RapidAPIMissingRequiredEnvFailsBeforeBinaryResolution(t *testing.T) {
	stub := newStubSecureCLIStore()
	stub.byName["rapidapi"] = &store.SecureCLIBinary{
		BinaryName:     "rapidapi",
		EncryptedEnv:   []byte("{}"),
		TimeoutSeconds: 10,
		DenyArgs:       json.RawMessage("[]"),
		DenyVerbose:    json.RawMessage("[]"),
	}

	tool := NewExecTool(t.TempDir(), false)
	tool.SetSecureCLIStore(stub)

	ctx := store.WithTenantID(store.WithAgentID(context.Background(), uuid.New()), uuid.New())
	ctx = store.WithCredentialUserID(ctx, "tenant-user-rapidapi")
	result := tool.Execute(ctx, map[string]any{"command": "rapidapi search weather"})

	if !result.IsError {
		t.Fatalf("expected missing RAPIDAPI_KEY to fail")
	}
	if !strings.Contains(result.ForLLM, "missing required credential env") {
		t.Fatalf("expected actionable missing-env error, got: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "RAPIDAPI_KEY") {
		t.Fatalf("expected missing key name in error, got: %s", result.ForLLM)
	}
	if strings.Contains(result.ForLLM, "not found in PATH") || strings.Contains(result.ForLLM, "Binary resolution failed") {
		t.Fatalf("missing env should be reported before binary resolution, got: %s", result.ForLLM)
	}
	if strings.Contains(result.ForLLM, "tenant-user-rapidapi") {
		t.Fatalf("credential user id leaked into user-facing error: %s", result.ForLLM)
	}
}

func TestExec_RapidAPIWithRequiredEnvReachesDirectExec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is POSIX-only")
	}

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "rapidapi")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\n[ -n \"$RAPIDAPI_KEY\" ] || exit 43\nprintf 'rapidapi args:%s\\n' \"$*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	stub := newStubSecureCLIStore()
	stub.byName["rapidapi"] = &store.SecureCLIBinary{
		BinaryName:     "rapidapi",
		BinaryPath:     &binPath,
		EncryptedEnv:   []byte("{}"),
		UserEnv:        []byte(`{"RAPIDAPI_KEY":"test-key-not-real"}`),
		TimeoutSeconds: 10,
		DenyArgs:       json.RawMessage("[]"),
		DenyVerbose:    json.RawMessage("[]"),
	}

	tool := NewExecTool(t.TempDir(), false)
	tool.SetSecureCLIStore(stub)

	ctx := store.WithTenantID(store.WithAgentID(context.Background(), uuid.New()), uuid.New())
	result := tool.Execute(ctx, map[string]any{"command": "rapidapi search weather"})

	if result.IsError {
		t.Fatalf("expected rapidapi direct exec to run, got: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "rapidapi args:search weather") {
		t.Fatalf("expected script output, got: %s", result.ForLLM)
	}
	if strings.Contains(result.ForLLM, "test-key-not-real") {
		t.Fatalf("credential value leaked into output: %s", result.ForLLM)
	}
}

func TestExec_GoClawGatewayTokenRawOutputIsScrubbed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is POSIX-only")
	}

	const token = "plain-gateway-token-SHOULD-NOT-LEAK-12345"
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "goclaw")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nprintf '%s\\n' \"$GOCLAW_GATEWAY_TOKEN\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	stub := newStubSecureCLIStore()
	stub.byName["goclaw"] = &store.SecureCLIBinary{
		BinaryName: "goclaw",
		BinaryPath: &binPath,
		EncryptedEnv: []byte(`{
			"GOCLAW_GATEWAY_TOKEN":{"kind":"sensitive","value":"` + token + `"},
			"GOCLAW_SERVER":{"kind":"sensitive","value":"http://127.0.0.1:18790"}
		}`),
		TimeoutSeconds: 10,
		DenyArgs:       json.RawMessage("[]"),
		DenyVerbose:    json.RawMessage("[]"),
	}

	tool := NewExecTool(t.TempDir(), false)
	tool.SetSecureCLIStore(stub)

	ctx := store.WithTenantID(store.WithAgentID(context.Background(), uuid.New()), uuid.New())
	result := tool.Execute(ctx, map[string]any{"command": "goclaw agent list"})

	if result.IsError {
		t.Fatalf("expected goclaw direct exec to run, got: %s", result.ForLLM)
	}
	if strings.Contains(result.ForLLM, token) {
		t.Fatalf("raw gateway token leaked into output: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "[REDACTED]") {
		t.Fatalf("expected gateway token to be redacted, got: %s", result.ForLLM)
	}
}

func TestExec_GHMissingRequiredEnvFailsBeforeRawAuth(t *testing.T) {
	stub := newStubSecureCLIStore()
	stub.byName["gh"] = &store.SecureCLIBinary{
		BinaryName:     "gh",
		EncryptedEnv:   []byte("{}"),
		TimeoutSeconds: 10,
		DenyArgs:       json.RawMessage("[]"),
		DenyVerbose:    json.RawMessage("[]"),
	}

	tool := NewExecTool(t.TempDir(), false)
	tool.SetSecureCLIStore(stub)

	ctx := store.WithTenantID(store.WithAgentID(context.Background(), uuid.New()), uuid.New())
	result := tool.Execute(ctx, map[string]any{"command": "gh issue list --repo digitopvn/goclaw --limit 1"})

	if !result.IsError {
		t.Fatalf("expected missing GH_TOKEN to fail")
	}
	if !strings.Contains(result.ForLLM, "missing required credential env") {
		t.Fatalf("expected actionable missing-env error, got: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "GH_TOKEN") {
		t.Fatalf("expected GH_TOKEN in error, got: %s", result.ForLLM)
	}
	if strings.Contains(result.ForLLM, "gh auth login") {
		t.Fatalf("raw gh auth guidance leaked through: %s", result.ForLLM)
	}
}

func TestExec_GitRemoteCommandWithoutTypedCredentialFailsBeforeRawAuth(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is POSIX-only")
	}

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "git")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nprintf 'raw git reached\\n' >&2\nexit 128\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	adapterName := "git"
	stub := newStubSecureCLIStore()
	stub.byName["git"] = &store.SecureCLIBinary{
		BinaryName:     "git",
		BinaryPath:     &binPath,
		EncryptedEnv:   []byte("{}"),
		TimeoutSeconds: 10,
		DenyArgs:       json.RawMessage("[]"),
		DenyVerbose:    json.RawMessage("[]"),
		AdapterName:    &adapterName,
	}

	tool := NewExecTool(t.TempDir(), false)
	tool.SetSecureCLIStore(stub)

	ctx := store.WithTenantID(store.WithAgentID(context.Background(), uuid.New()), uuid.New())
	result := tool.Execute(ctx, map[string]any{"command": "git push -u origin feature"})

	if !result.IsError {
		t.Fatalf("expected missing typed git credential to fail")
	}
	if !strings.Contains(result.ForLLM, "Git credential resolution failed") {
		t.Fatalf("expected git credential diagnostic, got: %s", result.ForLLM)
	}
	if strings.Contains(result.ForLLM, "raw git reached") {
		t.Fatalf("raw git command executed before credential resolution failed: %s", result.ForLLM)
	}
}
