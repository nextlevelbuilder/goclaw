package tools

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestPassthroughAdapter_NoOp(t *testing.T) {
	a := AdapterFor("passthrough")
	if a.Name() != "passthrough" {
		t.Fatalf("Name()=%q, want passthrough", a.Name())
	}

	cases := [][]string{
		nil,
		{},
		{"clone", "https://github.com/x/y.git"},
		{"--help"},
	}
	for _, argv := range cases {
		if a.ShouldInject(argv) {
			t.Fatalf("ShouldInject(%v)=true, want false (passthrough must never inject)", argv)
		}
	}

	inj, err := a.Prepare(context.Background(), &store.SecureCLIBinary{}, nil, []string{"clone"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if inj == nil {
		t.Fatalf("Prepare returned nil Injection")
	}
	if len(inj.ArgvPrefix) != 0 || len(inj.Env) != 0 || len(inj.ScrubValues) != 0 {
		t.Fatalf("passthrough produced non-empty Injection: %+v", inj)
	}
	if inj.Cleanup != nil {
		t.Fatalf("passthrough must not register Cleanup")
	}
}

func TestAdapterFor_FallsBackToPassthrough(t *testing.T) {
	cases := []string{"", "nonexistent", "git-not-registered-yet"}
	for _, name := range cases {
		a := AdapterFor(name)
		if a.Name() != "passthrough" {
			t.Fatalf("AdapterFor(%q)=%q, want passthrough", name, a.Name())
		}
	}
}

// recordingAdapter captures Prepare calls so the hook-point test can verify
// the runtime wired the adapter correctly.
type recordingAdapter struct {
	name       string
	shouldFn   func([]string) bool
	prepareErr error
	calls      int32
	gotArgv    []string
}

func (r *recordingAdapter) Name() string { return r.name }
func (r *recordingAdapter) ShouldInject(argv []string) bool {
	if r.shouldFn != nil {
		return r.shouldFn(argv)
	}
	return true
}
func (r *recordingAdapter) Prepare(_ context.Context, _ *store.SecureCLIBinary, _ *store.SecureCLIUserCredential, argv []string) (*Injection, error) {
	atomic.AddInt32(&r.calls, 1)
	r.gotArgv = append([]string(nil), argv...)
	if r.prepareErr != nil {
		return nil, r.prepareErr
	}
	return &Injection{}, nil
}

func TestRegisterAdapter_RoundTrip(t *testing.T) {
	name := "test-roundtrip-adapter"
	t.Cleanup(func() {
		adaptersMu.Lock()
		delete(adapters, name)
		adaptersMu.Unlock()
	})

	r := &recordingAdapter{name: name}
	RegisterAdapter(r)

	got := AdapterFor(name)
	if got.Name() != name {
		t.Fatalf("AdapterFor(%q)=%q, want %q", name, got.Name(), name)
	}
}

func TestRegisterAdapter_NilIgnored(t *testing.T) {
	// Should not panic, should not affect registry.
	before := AdapterFor("passthrough")
	RegisterAdapter(nil)
	after := AdapterFor("passthrough")
	if before.Name() != after.Name() {
		t.Fatalf("nil RegisterAdapter changed passthrough resolution")
	}
}

func TestInjection_StructShape(t *testing.T) {
	// Locks Injection field types so future refactors can't silently change
	// the contract the adapter pipeline depends on.
	flag := false
	inj := Injection{
		ArgvPrefix:  []string{"-c", "foo=bar"},
		Env:         map[string]string{"GIT_TERMINAL_PROMPT": "0"},
		Cleanup:     func() error { flag = true; return nil },
		ScrubValues: []string{"secretvalue"},
	}
	if inj.ArgvPrefix[0] != "-c" || inj.ArgvPrefix[1] != "foo=bar" {
		t.Fatalf("ArgvPrefix not preserved")
	}
	if inj.Env["GIT_TERMINAL_PROMPT"] != "0" {
		t.Fatalf("Env not preserved")
	}
	if err := inj.Cleanup(); err != nil || !flag {
		t.Fatalf("Cleanup did not fire: err=%v flag=%v", err, flag)
	}
	if inj.ScrubValues[0] != "secretvalue" {
		t.Fatalf("ScrubValues not preserved")
	}
}

func TestHashHostScope(t *testing.T) {
	if got := hashHostScope(nil); got != "none" {
		t.Fatalf("hashHostScope(nil)=%q, want none", got)
	}
	empty := ""
	if got := hashHostScope(&empty); got != "none" {
		t.Fatalf("hashHostScope(&\"\")=%q, want none", got)
	}

	a := "github.com"
	b := "gitlab.com"
	ha1 := hashHostScope(&a)
	ha2 := hashHostScope(&a)
	hb := hashHostScope(&b)

	if ha1 != ha2 {
		t.Fatalf("same input produced different hashes: %q vs %q", ha1, ha2)
	}
	if ha1 == hb {
		t.Fatalf("different inputs collided: %q == %q", ha1, hb)
	}
	if len(ha1) != 8 {
		t.Fatalf("hash length=%d, want 8 hex chars", len(ha1))
	}
	// Must not contain the plaintext hostname anywhere.
	if ha1 == a {
		t.Fatalf("hash leaked plaintext hostname")
	}
}

func TestSortedKeys_Deterministic(t *testing.T) {
	in := map[string]string{
		"GIT_SSH_COMMAND": "ssh -i /tmp/x",
		"GIT_TERMINAL_PROMPT": "0",
		"GIT_CONFIG_COUNT": "1",
	}
	got := sortedKeys(in)
	want := []string{"GIT_CONFIG_COUNT", "GIT_SSH_COMMAND", "GIT_TERMINAL_PROMPT"}
	if len(got) != len(want) {
		t.Fatalf("sortedKeys len=%d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("sortedKeys[%d]=%q, want %q", i, got[i], want[i])
		}
	}

	if sortedKeys(nil) != nil {
		t.Fatalf("sortedKeys(nil) must return nil")
	}
	if sortedKeys(map[string]string{}) != nil {
		t.Fatalf("sortedKeys(empty map) must return nil")
	}
}

func TestScrubBag_PerRequestIsolation(t *testing.T) {
	// Two parallel contexts must not see each other's scrub values.
	ctxA := WithScrubBag(context.Background())
	ctxB := WithScrubBag(context.Background())

	AddScrubValuesCtx(ctxA, "secretAAAAAA")
	AddScrubValuesCtx(ctxB, "secretBBBBBB")

	gotA := ScrubCredentialsCtx(ctxA, "value: secretAAAAAA leaked + secretBBBBBB visible")
	gotB := ScrubCredentialsCtx(ctxB, "value: secretAAAAAA visible + secretBBBBBB leaked")

	// A's bag redacts A's secret, leaves B's untouched.
	if got := gotA; got != "value: [REDACTED] leaked + secretBBBBBB visible" {
		t.Fatalf("ctxA scrub leaked across tenants: %q", got)
	}
	if got := gotB; got != "value: secretAAAAAA visible + [REDACTED] leaked" {
		t.Fatalf("ctxB scrub leaked across tenants: %q", got)
	}
}

func TestScrubBag_NoBagInContextIsNoop(t *testing.T) {
	// AddScrubValuesCtx without a bag must not panic.
	AddScrubValuesCtx(context.Background(), "should-not-crash")
	// ScrubCredentialsCtx without a bag still runs regex pass.
	got := ScrubCredentialsCtx(context.Background(), "api_key=ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if got == "api_key=ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("regex pass should still scrub even without bag, got %q", got)
	}
}

func TestScrubBag_ConcurrentAdds(t *testing.T) {
	// Race-detector smoke test for the per-bag mutex.
	ctx := WithScrubBag(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			AddScrubValuesCtx(ctx, "concurrentvalueXXXXXX")
		}(i)
	}
	wg.Wait()
	out := ScrubCredentialsCtx(ctx, "echo concurrentvalueXXXXXX done")
	if out != "echo [REDACTED] done" {
		t.Fatalf("concurrent bag adds failed: %q", out)
	}
}

func TestScrubBag_ShortValuesIgnored(t *testing.T) {
	ctx := WithScrubBag(context.Background())
	AddScrubValuesCtx(ctx, "x", "ab", "abc", "abcde") // all < 6 chars
	got := ScrubCredentialsCtx(ctx, "abc abcde x ab")
	if got != "abc abcde x ab" {
		t.Fatalf("short values were redacted (false positive): %q", got)
	}
}

// recordingAdapter is the kind of probe a future hook-point integration test
// would register. The compile-time assertion here keeps the interface stable.
func TestRecordingAdapter_ImplementsInterface(t *testing.T) {
	var _ CredentialAdapter = (*recordingAdapter)(nil)
	// Sanity: an adapter returning prepareErr does not panic.
	r := &recordingAdapter{name: "boom", prepareErr: errors.New("forced")}
	if _, err := r.Prepare(context.Background(), nil, nil, []string{"x"}); err == nil {
		t.Fatalf("Prepare did not return forced error")
	}
}
