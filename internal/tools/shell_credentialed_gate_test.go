package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// stubSecureCLIStore is a minimal in-memory SecureCLIStore used by shell gate
// unit tests. Only LookupByBinary (Phase 1) and IsRegisteredBinary (Phase 2)
// have meaningful logic; the rest return zero values to satisfy the interface.
type stubSecureCLIStore struct {
	mu              sync.Mutex
	byName          map[string]*store.SecureCLIBinary
	registered      map[string]bool
	lookupCalls     int
	isRegisteredErr error
	// isRegisteredSleep makes IsRegisteredBinary block for this duration to
	// exercise the 2s fail-closed timeout in Phase 3.
	isRegisteredSleep time.Duration
	// lastLookupName captures the exact name passed to LookupByBinary so
	// tests can assert normalization happened before the call.
	lastLookupName string
}

func newStubSecureCLIStore() *stubSecureCLIStore {
	return &stubSecureCLIStore{
		byName:     map[string]*store.SecureCLIBinary{},
		registered: map[string]bool{},
	}
}

// --- Meaningful methods ---

func (s *stubSecureCLIStore) LookupByBinary(ctx context.Context, binaryName string, agentID *uuid.UUID, userID string) (*store.SecureCLIBinary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lookupCalls++
	s.lastLookupName = binaryName
	return s.byName[binaryName], nil
}

// IsRegisteredBinary is exercised by Phase 3 tests. Keeping the stub
// implementation here keeps the interface satisfied from Phase 2 onwards.
func (s *stubSecureCLIStore) IsRegisteredBinary(ctx context.Context, binaryName string) (bool, error) {
	if s.isRegisteredSleep > 0 {
		select {
		case <-time.After(s.isRegisteredSleep):
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isRegisteredErr != nil {
		return false, s.isRegisteredErr
	}
	return s.registered[binaryName], nil
}

// --- Zero-value stubs for interface satisfaction ---

func (s *stubSecureCLIStore) Create(ctx context.Context, b *store.SecureCLIBinary) error {
	return nil
}
func (s *stubSecureCLIStore) Get(ctx context.Context, id uuid.UUID) (*store.SecureCLIBinary, error) {
	return nil, nil
}
func (s *stubSecureCLIStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	return nil
}
func (s *stubSecureCLIStore) Delete(ctx context.Context, id uuid.UUID) error { return nil }
func (s *stubSecureCLIStore) List(ctx context.Context) ([]store.SecureCLIBinary, error) {
	return nil, nil
}
func (s *stubSecureCLIStore) ListEnabled(ctx context.Context) ([]store.SecureCLIBinary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.SecureCLIBinary, 0, len(s.byName))
	for _, b := range s.byName {
		if b != nil {
			out = append(out, *b)
		}
	}
	return out, nil
}
func (s *stubSecureCLIStore) ListForAgent(ctx context.Context, agentID uuid.UUID) ([]store.SecureCLIBinary, error) {
	return nil, nil
}
func (s *stubSecureCLIStore) GetUserCredentials(ctx context.Context, binaryID uuid.UUID, userID string) (*store.SecureCLIUserCredential, error) {
	return nil, nil
}
func (s *stubSecureCLIStore) SetUserCredentials(ctx context.Context, binaryID uuid.UUID, userID string, encryptedEnv []byte) error {
	return nil
}
func (s *stubSecureCLIStore) DeleteUserCredentials(ctx context.Context, binaryID uuid.UUID, userID string) error {
	return nil
}
func (s *stubSecureCLIStore) ListUserCredentials(ctx context.Context, binaryID uuid.UUID) ([]store.SecureCLIUserCredential, error) {
	return nil, nil
}

// --- Phase 1 characterization tests ---

// TestExec_NoStoreWired_FallsThrough confirms that when secureCLIStore is nil
// (Lite edition / missing encryption key), Execute falls through to host exec.
func TestExec_NoStoreWired_FallsThrough(t *testing.T) {
	tool := NewExecTool(t.TempDir(), false)
	result := tool.Execute(context.Background(), map[string]any{"command": "echo hello"})
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "hello") {
		t.Fatalf("expected output to contain %q, got %q", "hello", result.ForLLM)
	}
}

// TestExec_UnregisteredBinary_FallsThrough: stub wired but zero entries —
// unregistered binary should fall through to host exec unchanged.
func TestExec_UnregisteredBinary_FallsThrough(t *testing.T) {
	stub := newStubSecureCLIStore()
	tool := NewExecTool(t.TempDir(), false)
	tool.SetSecureCLIStore(stub)

	ctx := store.WithTenantID(store.WithAgentID(context.Background(), uuid.New()), uuid.New())
	result := tool.Execute(ctx, map[string]any{"command": "echo hello"})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "hello") {
		t.Fatalf("expected output to contain %q, got %q", "hello", result.ForLLM)
	}
	stub.mu.Lock()
	calls := stub.lookupCalls
	stub.mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected lookupCalls == 1 (gate consulted), got %d", calls)
	}
}

// TestExec_GrantedBinary_UsesCredentialedPath — Red Team F9 applied:
// sentinel binary name (not echo) proves the credentialed branch was taken.
func TestExec_GrantedBinary_UsesCredentialedPath(t *testing.T) {
	const sentinel = "xyz_goclaw_sentinel"
	stub := newStubSecureCLIStore()
	stub.byName[sentinel] = &store.SecureCLIBinary{
		BinaryName:     sentinel,
		EncryptedEnv:   []byte("{}"),
		TimeoutSeconds: 10,
		DenyArgs:       json.RawMessage("[]"),
		DenyVerbose:    json.RawMessage("[]"),
	}

	tool := NewExecTool(t.TempDir(), false)
	tool.SetSecureCLIStore(stub)

	ctx := store.WithTenantID(store.WithAgentID(context.Background(), uuid.New()), uuid.New())
	result := tool.Execute(ctx, map[string]any{"command": sentinel + " --help"})

	stub.mu.Lock()
	calls := stub.lookupCalls
	stub.mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected lookupCalls == 1 (gate consulted store), got %d", calls)
	}
	if !result.IsError {
		t.Fatalf("expected IsError=true (credentialed branch entered but binary not on PATH), got %+v", result)
	}
	// Soft sanity: the error must NOT come from sh fall-through. A fall-through
	// error would typically contain "sh: 1:" or the shell path — proving we
	// took a different code path than intended.
	if strings.Contains(result.ForLLM, "sh: 1:") || strings.Contains(result.ForLLM, "/bin/sh") {
		t.Fatalf("expected credentialed-path error, got shell fall-through error: %s", result.ForLLM)
	}
}

// --- Phase 3 gate-enforcement tests ---

// helper: build a gate-wired ExecTool with a fresh stub + ctx.
func newGateTestTool(t *testing.T) (*ExecTool, *stubSecureCLIStore, context.Context) {
	t.Helper()
	stub := newStubSecureCLIStore()
	tool := NewExecTool(t.TempDir(), false)
	tool.SetSecureCLIStore(stub)
	ctx := store.WithTenantID(store.WithAgentID(context.Background(), uuid.New()), uuid.New())
	return tool, stub, ctx
}

// TestExec_BlocksRegisteredBinaryWhenNoGrant: registered without grant → deny.
func TestExec_BlocksRegisteredBinaryWhenNoGrant(t *testing.T) {
	tool, stub, ctx := newGateTestTool(t)
	stub.registered["gh"] = true

	result := tool.Execute(ctx, map[string]any{"command": "gh auth status"})
	if !result.IsError {
		t.Fatalf("expected deny, got success: %+v", result)
	}
	if !strings.Contains(result.ForLLM, "requires a secure CLI grant") {
		t.Fatalf("expected grant-required message, got: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "gh") {
		t.Fatalf("expected binary name in message, got: %s", result.ForLLM)
	}
}

// TestExec_AllowsUnregisteredBinary: gate passes through when no match.
func TestExec_AllowsUnregisteredBinary(t *testing.T) {
	tool, _, ctx := newGateTestTool(t)
	result := tool.Execute(ctx, map[string]any{"command": "echo hello"})
	if result.IsError {
		t.Fatalf("expected pass-through, got error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "hello") {
		t.Fatalf("expected echo output, got: %s", result.ForLLM)
	}
}

// TestExec_BinaryNameNormalization (Red Team F5/F8): case + path variants all match.
func TestExec_BinaryNameNormalization(t *testing.T) {
	cases := []string{
		"gh version",
		"/usr/bin/gh version",
		"./gh version",
		"GH version",
		"Gh version",
		"  gh version  ",
	}
	for _, cmd := range cases {
		t.Run(cmd, func(t *testing.T) {
			tool, stub, ctx := newGateTestTool(t)
			stub.registered["gh"] = true
			result := tool.Execute(ctx, map[string]any{"command": cmd})
			if !result.IsError {
				t.Fatalf("expected deny for %q, got success: %+v", cmd, result)
			}
			if !strings.Contains(result.ForLLM, "requires a secure CLI grant") {
				t.Fatalf("expected grant-required message for %q, got: %s", cmd, result.ForLLM)
			}
		})
	}
}

// Shell-wrapper denials (Red Team F1).
func TestExec_BlocksShellWrapper_ShDashC(t *testing.T) {
	tool, stub, ctx := newGateTestTool(t)
	stub.registered["gh"] = true
	result := tool.Execute(ctx, map[string]any{"command": "sh -c 'gh auth status'"})
	if !result.IsError || !strings.Contains(result.ForLLM, "requires a secure CLI grant") {
		t.Fatalf("expected deny through sh -c wrapper, got: %+v", result)
	}
}

func TestExec_BlocksShellWrapper_BashDashC(t *testing.T) {
	tool, stub, ctx := newGateTestTool(t)
	stub.registered["gh"] = true
	result := tool.Execute(ctx, map[string]any{"command": `bash -c "gh api repos/x/y"`})
	if !result.IsError || !strings.Contains(result.ForLLM, "requires a secure CLI grant") {
		t.Fatalf("expected deny through bash -c wrapper, got: %+v", result)
	}
}

func TestExec_BlocksShellWrapper_SlashBinSh(t *testing.T) {
	tool, stub, ctx := newGateTestTool(t)
	stub.registered["gh"] = true
	result := tool.Execute(ctx, map[string]any{"command": "/bin/sh -c 'gh auth status'"})
	if !result.IsError || !strings.Contains(result.ForLLM, "requires a secure CLI grant") {
		t.Fatalf("expected deny through /bin/sh -c, got: %+v", result)
	}
}

func TestExec_BlocksEnvWrapper(t *testing.T) {
	tool, stub, ctx := newGateTestTool(t)
	stub.registered["gh"] = true
	result := tool.Execute(ctx, map[string]any{"command": "env GH_TOKEN=x gh auth status"})
	if !result.IsError || !strings.Contains(result.ForLLM, "requires a secure CLI grant") {
		t.Fatalf("expected deny through env wrapper, got: %+v", result)
	}
}

func TestExec_BlocksEnvUsrBinEnvSh(t *testing.T) {
	tool, stub, ctx := newGateTestTool(t)
	stub.registered["gh"] = true
	result := tool.Execute(ctx, map[string]any{"command": "/usr/bin/env sh -c 'gh api'"})
	if !result.IsError || !strings.Contains(result.ForLLM, "requires a secure CLI grant") {
		t.Fatalf("expected deny through env→sh wrapper, got: %+v", result)
	}
}

func TestExec_BlocksNestedWrapper_Depth3(t *testing.T) {
	tool, stub, ctx := newGateTestTool(t)
	stub.registered["gh"] = true
	// bash -c "sh -c 'env GH_TOKEN=x gh auth'"
	cmd := `bash -c "sh -c 'env GH_TOKEN=x gh auth'"`
	result := tool.Execute(ctx, map[string]any{"command": cmd})
	if !result.IsError || !strings.Contains(result.ForLLM, "requires a secure CLI grant") {
		t.Fatalf("expected deny through depth-3 wrapper, got: %+v", result)
	}
}

// TestExec_RejectsWrapperDepthCap (Validation Session 1): depth 4+ → unconditional deny.
func TestExec_RejectsWrapperDepthCap(t *testing.T) {
	tool, _, ctx := newGateTestTool(t)
	// 4-level nesting: bash -c 'sh -c "bash -c \"sh -c gh\"..."'
	cmd := `bash -c "sh -c 'bash -c \"sh -c gh\"'"`
	result := tool.Execute(ctx, map[string]any{"command": cmd})
	if !result.IsError {
		t.Fatalf("expected deny for depth-4+ nesting, got: %+v", result)
	}
	if !strings.Contains(result.ForLLM, "Command nesting too deep") {
		t.Fatalf("expected nesting-too-deep message, got: %s", result.ForLLM)
	}
}

func TestExec_AllowsShellWrapperWithUnregisteredInner(t *testing.T) {
	tool, _, ctx := newGateTestTool(t)
	// registered empty, inner is echo (not registered) → fall-through.
	result := tool.Execute(ctx, map[string]any{"command": "sh -c 'echo hi'"})
	if result.IsError {
		t.Fatalf("expected pass-through when inner unregistered, got error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "hi") {
		t.Fatalf("expected echo output, got: %s", result.ForLLM)
	}
}

// Normalization consistency at LookupByBinary (Red Team F5).
func TestExec_LookupPathNormalizes(t *testing.T) {
	const sentinel = "xyz_goclaw_sentinel"
	tool, stub, ctx := newGateTestTool(t)
	stub.byName[sentinel] = &store.SecureCLIBinary{
		BinaryName:     sentinel,
		EncryptedEnv:   []byte("{}"),
		TimeoutSeconds: 10,
		DenyArgs:       json.RawMessage("[]"),
		DenyVerbose:    json.RawMessage("[]"),
	}
	// Provide path-prefixed, mixed-case, padded form.
	_ = tool.Execute(ctx, map[string]any{"command": "  /usr/local/bin/XYZ_GOCLAW_SENTINEL --help "})
	stub.mu.Lock()
	got := stub.lastLookupName
	stub.mu.Unlock()
	if got != sentinel {
		t.Fatalf("expected LookupByBinary called with normalized %q, got %q", sentinel, got)
	}
}

// Fail-CLOSED on gate DB error (Red Team F7).
func TestExec_GateDbError_FailsClosed(t *testing.T) {
	tool, stub, ctx := newGateTestTool(t)
	stub.isRegisteredErr = errors.New("simulated db outage")
	result := tool.Execute(ctx, map[string]any{"command": "echo hello"})
	if !result.IsError {
		t.Fatalf("expected fail-closed, got success: %+v", result)
	}
	if !strings.Contains(result.ForLLM, "Secure CLI gate temporarily unavailable") {
		t.Fatalf("expected gate-unavailable message, got: %s", result.ForLLM)
	}
	if strings.Contains(result.ForLLM, "hello") {
		t.Fatalf("command must NOT execute on gate error, got: %s", result.ForLLM)
	}
}

// Fail-CLOSED on gate DB timeout (Red Team F7).
func TestExec_GateDbTimeout_FailsClosed(t *testing.T) {
	tool, stub, ctx := newGateTestTool(t)
	stub.isRegisteredSleep = 3 * time.Second
	start := time.Now()
	result := tool.Execute(ctx, map[string]any{"command": "echo hello"})
	elapsed := time.Since(start)
	if !result.IsError {
		t.Fatalf("expected fail-closed on timeout, got success: %+v", result)
	}
	if !strings.Contains(result.ForLLM, "Secure CLI gate temporarily unavailable") {
		t.Fatalf("expected gate-unavailable message, got: %s", result.ForLLM)
	}
	// 2s context timeout + some slack. Must not wait full 3s sleep.
	if elapsed > 2500*time.Millisecond {
		t.Fatalf("expected gate timeout ≤2.5s, took %s", elapsed)
	}
}

// Env scrub in fall-through exec (Red Team F4): GH_TOKEN must not leak into child.
func TestExec_FallThrough_ScrubsGHToken(t *testing.T) {
	tool, _, ctx := newGateTestTool(t)
	t.Setenv("GH_TOKEN", "supersecretvalue")
	// Use single-quote printf so shell sees the literal; our gate lets "sh"
	// fall through (sh is not registered, echo is not registered).
	result := tool.Execute(ctx, map[string]any{"command": `sh -c 'echo "token=$GH_TOKEN"'`})
	if result.IsError {
		t.Fatalf("expected pass-through, got: %s", result.ForLLM)
	}
	if strings.Contains(result.ForLLM, "supersecretvalue") {
		t.Fatalf("GH_TOKEN leaked to child process: %s", result.ForLLM)
	}
}
