package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// scopedCtx builds a context carrying a tenant + user, as a real delegated run has.
func scopedCtx(tenant uuid.UUID, user string) context.Context {
	ctx := context.Background()
	if tenant != uuid.Nil {
		ctx = store.WithTenantID(ctx, tenant)
	}
	if user != "" {
		ctx = store.WithUserID(ctx, user)
	}
	return ctx
}

// The sandbox layer TRUNCATES container keys at 50 chars keeping the prefix
// (sandbox.sanitizeKey). Overrunning that budget is silently destructive: the
// worker label falls off the end and every parallel worker collapses into one
// shared container, disabling the fan-out with no visible error. Worst-case
// inputs must therefore still fit.
func TestSandboxKeyFitsTruncationBudget(t *testing.T) {
	longUser := strings.Repeat("a1b2c3d4", 8)  // 64 chars, e.g. a Cognito sub
	longWorker := strings.Repeat("worker-", 6) // 42 chars
	connUUID := uuid.New().String()            // 36 chars

	cases := []struct {
		name   string
		ctx    context.Context
		conn   string
		worker string
	}{
		{"tenant+user+worker", scopedCtx(uuid.New(), longUser), connUUID, longWorker},
		{"no worker", scopedCtx(uuid.New(), longUser), connUUID, ""},
		{"no scope at all", context.Background(), connUUID, longWorker},
		{"legacy conn id", scopedCtx(uuid.New(), longUser), "conn_cyew7ro5", "integrate"},
	}
	for _, tc := range cases {
		got := buildSandboxKey(tc.ctx, tc.conn, tc.worker)
		if len(got) > maxSandboxKeyLen {
			t.Errorf("%s: key %q is %d chars, over the %d budget — the worker suffix would be truncated away",
				tc.name, got, len(got), maxSandboxKeyLen)
		}
	}
}

// Every component that must isolate a container has to change the key. A missed
// one silently shares a warm container that persists the CLI's own credential and
// session files under HOME=/tmp.
func TestSandboxKeyIsolatesEveryDimension(t *testing.T) {
	tA, tB := uuid.New(), uuid.New()
	connA, connB := uuid.New().String(), uuid.New().String()

	base := buildSandboxKey(scopedCtx(tA, "user-A"), connA, "1")

	diff := map[string]string{
		"different tenant":     buildSandboxKey(scopedCtx(tB, "user-A"), connA, "1"),
		"different user":       buildSandboxKey(scopedCtx(tA, "user-B"), connA, "1"),
		"different connection": buildSandboxKey(scopedCtx(tA, "user-A"), connB, "1"),
		"different worker":     buildSandboxKey(scopedCtx(tA, "user-A"), connA, "2"),
		"no worker":            buildSandboxKey(scopedCtx(tA, "user-A"), connA, ""),
	}
	for what, got := range diff {
		if got == base {
			t.Errorf("%s produced the SAME sandbox key %q — containers would be shared", what, got)
		}
	}

	// Same inputs must be stable, or a warm container is never reused.
	if again := buildSandboxKey(scopedCtx(tA, "user-A"), connA, "1"); again != base {
		t.Errorf("key not stable across calls: %q vs %q", base, again)
	}
}

// The parallel fan-out issues several workers for one connection in one turn;
// each needs its own container, and all of them must survive truncation.
func TestSandboxKeyDistinctAcrossFanOutWorkers(t *testing.T) {
	ctx := scopedCtx(uuid.New(), strings.Repeat("sub-", 16))
	conn := uuid.New().String()

	seen := map[string]string{}
	for _, w := range []string{"setup", "1", "2", "3", "4", "5", "6", "7", "8", "integrate"} {
		k := buildSandboxKey(ctx, conn, w)
		if len(k) > maxSandboxKeyLen {
			t.Fatalf("worker %q key too long (%d): %q", w, len(k), k)
		}
		if prev, dup := seen[k]; dup {
			t.Fatalf("workers %q and %q share sandbox key %q — the fan-out would run in one container", prev, w, k)
		}
		seen[k] = w
	}
}
