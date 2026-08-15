package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestExecDelegationArtifactRejectsNativeHostRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell command fixture")
	}

	ctx, _, outputs := delegationArtifactToolContext(t)
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "outside.txt")
	const outsideContents = "delegated-native-host-read"
	if err := os.WriteFile(outsideFile, []byte(outsideContents), 0o600); err != nil {
		t.Fatal(err)
	}

	result := NewExecTool(outputs, true).Execute(ctx, map[string]any{
		"command": "cat " + strconv.Quote(outsideFile),
	})

	if !result.IsError {
		t.Fatalf("delegated native exec read an outside host file: %#v", result)
	}
	if result.ForLLM != delegatedExecSandboxRequiredError {
		t.Fatalf("error = %q, want %q", result.ForLLM, delegatedExecSandboxRequiredError)
	}
	if strings.Contains(result.ForLLM, outsideContents) {
		t.Fatal("delegated native exec exposed outside file contents")
	}
}

func TestExecDelegationArtifactRejectsCredentialedHostPathBeforeLookup(t *testing.T) {
	ctx, _, outputs := delegationArtifactToolContext(t)
	secureCLIStore := newStubSecureCLIStore()
	secureCLIStore.byName["delegated-cli"] = &store.SecureCLIBinary{
		BinaryName:     "delegated-cli",
		TimeoutSeconds: 30,
		Enabled:        true,
		IsGlobal:       true,
	}
	tool := NewExecTool(outputs, true)
	tool.SetSecureCLIStore(secureCLIStore)

	result := tool.Execute(ctx, map[string]any{"command": "delegated-cli status"})

	if !result.IsError || result.ForLLM != delegatedExecSandboxRequiredError {
		t.Fatalf("delegated credentialed exec result = %#v, want stable sandbox-required error", result)
	}
	secureCLIStore.mu.Lock()
	lookupCalls := secureCLIStore.lookupCalls
	secureCLIStore.mu.Unlock()
	if lookupCalls != 0 {
		t.Fatalf("credential lookup calls = %d, want guard before credentialed routing", lookupCalls)
	}
}

func TestExecDelegationArtifactRejectsMissingSandboxKey(t *testing.T) {
	ctx, _, outputs := delegationArtifactToolContext(t)
	manager := &recordingSandboxManager{}
	tool := NewSandboxedExecTool(outputs, true, manager)

	result := tool.Execute(ctx, map[string]any{"command": "echo should-not-run"})

	if !result.IsError || result.ForLLM != delegatedExecSandboxRequiredError {
		t.Fatalf("delegated exec without sandbox key = %#v, want stable sandbox-required error", result)
	}
	if manager.key != "" {
		t.Fatalf("sandbox manager called with key %q despite missing context key", manager.key)
	}
}

func TestExecDelegationArtifactUsesSandboxWithManagerAndKey(t *testing.T) {
	ctx, outputs := delegatedExecSandboxContext(t)
	ctx = WithToolSandboxKey(ctx, "delegated-session")
	manager := &recordingSandboxManager{}
	tool := NewSandboxedExecTool(outputs, true, manager)

	result := tool.Execute(ctx, map[string]any{"command": "echo sandboxed"})

	if result.IsError {
		t.Fatalf("delegated sandbox exec failed: %s", result.ForLLM)
	}
	if manager.key != "delegated-session" {
		t.Fatalf("sandbox manager key = %q, want delegated-session", manager.key)
	}
	if got := strings.Join(manager.sandbox.command, " "); got != "sh -c echo sandboxed" {
		t.Fatalf("sandbox command = %q, want sandbox shell path", got)
	}
}

type disabledDelegationSandboxManager struct {
	getCalls int
}

func (m *disabledDelegationSandboxManager) Get(context.Context, string, string, *sandbox.Config, ...sandbox.GetOption) (sandbox.Sandbox, error) {
	m.getCalls++
	return nil, sandbox.ErrSandboxDisabled
}

func (*disabledDelegationSandboxManager) Release(context.Context, string) error { return nil }
func (*disabledDelegationSandboxManager) ReleaseAll(context.Context) error      { return nil }
func (*disabledDelegationSandboxManager) Stop()                                 {}
func (*disabledDelegationSandboxManager) Stats() map[string]any                 { return nil }

func TestExecDelegationArtifactDoesNotFallbackWhenSandboxDisabled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX touch command fixture")
	}

	ctx, outputs := delegatedExecSandboxContext(t)
	ctx = WithToolSandboxKey(ctx, "delegated-session")
	marker := filepath.Join(t.TempDir(), "process-ran")
	manager := &disabledDelegationSandboxManager{}
	tool := NewSandboxedExecTool(outputs, true, manager)

	result := tool.Execute(ctx, map[string]any{
		"command": "touch " + strconv.Quote(marker),
	})

	if !result.IsError || result.ForLLM != delegatedExecSandboxRequiredError {
		t.Fatalf("disabled delegated sandbox result = %#v, want stable sandbox-required error", result)
	}
	if manager.getCalls != 1 {
		t.Fatalf("sandbox manager Get calls = %d, want 1", manager.getCalls)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("delegated exec fell back to host; marker stat error = %v", err)
	}
}

func TestExecNonDelegatedSandboxDisabledStillFallsBackToHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX echo command fixture")
	}

	workspace := t.TempDir()
	canonicalWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	manager := &disabledDelegationSandboxManager{}
	tool := NewSandboxedExecTool(canonicalWorkspace, true, manager)
	ctx := WithToolSandboxKey(context.Background(), "ordinary-session")

	result := tool.Execute(ctx, map[string]any{"command": "echo ordinary-fallback"})

	if result.IsError {
		t.Fatalf("non-delegated sandbox fallback changed: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "ordinary-fallback") {
		t.Fatalf("non-delegated host fallback output = %q", result.ForLLM)
	}
}

func delegatedExecSandboxContext(t *testing.T) (context.Context, string) {
	t.Helper()
	delegationID := uuid.New()
	root := filepath.Join(t.TempDir(), "collaboration", "delegations", delegationID.String())
	inputs := filepath.Join(root, "inputs")
	outputs := filepath.Join(root, "outputs")
	if err := os.MkdirAll(inputs, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outputs, 0o750); err != nil {
		t.Fatal(err)
	}
	canonicalOutputs, err := filepath.EvalSymlinks(outputs)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithDelegationID(context.Background(), delegationID.String())
	ctx = WithDelegationArtifactInputs(ctx, inputs)
	ctx = WithToolWorkspace(ctx, canonicalOutputs)
	return ctx, canonicalOutputs
}
