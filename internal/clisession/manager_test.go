package clisession

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/cliagent"
)

// newTestManager returns a manager whose reaper will never fire on its own, so a
// test can drive reap() with a synthetic clock instead of racing wall time.
func newTestManager(t *testing.T, idleTTL time.Duration) *Manager {
	t.Helper()
	m := NewManager(ManagerOpts{IdleTTL: idleTTL, ReapInterval: time.Hour})
	t.Cleanup(m.Shutdown)
	return m
}

func TestNewManager_ZeroOptsUseDefaults(t *testing.T) {
	m := NewManager(ManagerOpts{})
	t.Cleanup(m.Shutdown)

	if m.opts.IdleTTL != DefaultIdleTTL {
		t.Errorf("IdleTTL = %s, want %s", m.opts.IdleTTL, DefaultIdleTTL)
	}
	if m.opts.ReapInterval != DefaultReapInterval {
		t.Errorf("ReapInterval = %s, want %s", m.opts.ReapInterval, DefaultReapInterval)
	}

	// Negatives are nonsense, not "never reap" — a zero ticker interval panics.
	neg := NewManager(ManagerOpts{IdleTTL: -1, ReapInterval: -1})
	t.Cleanup(neg.Shutdown)
	if neg.opts.IdleTTL != DefaultIdleTTL || neg.opts.ReapInterval != DefaultReapInterval {
		t.Errorf("negative opts = %+v, want the defaults", neg.opts)
	}
}

// ---------------------------------------------------------------------------
// 12. one session per key
// ---------------------------------------------------------------------------

func TestManager_GetOrCreateReusesPerKey(t *testing.T) {
	m := newTestManager(t, time.Hour)
	st := newFakeStarter()
	ctx := context.Background()

	first, err := m.GetOrCreate(ctx, "chat-a", testOpts(st))
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	second, err := m.GetOrCreate(ctx, "chat-a", testOpts(st))
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if first != second {
		t.Error("the same key produced two different sessions — the conversation would be split across two CLIs")
	}
	if got := st.startCount(); got != 1 {
		t.Errorf("started %d processes for one key, want 1", got)
	}

	other, err := m.GetOrCreate(ctx, "chat-b", testOpts(st))
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if other == first {
		t.Error("two different keys share a session")
	}
	if got := st.startCount(); got != 2 {
		t.Errorf("started %d processes for two keys, want 2", got)
	}
	if got := m.Len(); got != 2 {
		t.Errorf("Len = %d, want 2", got)
	}

	// Get never creates.
	if got, ok := m.Get("chat-a"); !ok || got != first {
		t.Errorf("Get(chat-a) = %v/%v, want the live session", got, ok)
	}
	if _, ok := m.Get("chat-nobody"); ok {
		t.Error("Get invented a session for an unknown key")
	}
}

// ---------------------------------------------------------------------------
// 13. concurrent creation starts exactly one process
// ---------------------------------------------------------------------------

func TestManager_ConcurrentGetOrCreateStartsOneSession(t *testing.T) {
	m := newTestManager(t, time.Hour)
	st := newFakeStarter()

	const n = 10
	start := make(chan struct{})
	got := make([]*Session, n)
	errs := make([]error, n)

	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			got[i], errs[i] = m.GetOrCreate(context.Background(), "hot-key", testOpts(st))
		}(i)
	}
	close(start)
	wg.Wait()

	for i := range n {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if got[i] == nil {
			t.Fatalf("goroutine %d got a nil session", i)
		}
		if got[i] != got[0] {
			t.Fatalf("goroutine %d got a different session than goroutine 0", i)
		}
	}
	// The assertion that matters: pointer equality alone would still hold if a
	// second process had been started and thrown away.
	if c := st.startCount(); c != 1 {
		t.Fatalf("%d CLI processes were started for one key, want exactly 1", c)
	}
	if got := m.Len(); got != 1 {
		t.Errorf("Len = %d, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// 14. a dead session is replaced, never handed back
// ---------------------------------------------------------------------------

func TestManager_ReplacesExitedSession(t *testing.T) {
	m := newTestManager(t, time.Hour)
	st := newFakeStarter()
	ctx := context.Background()

	dead, err := m.GetOrCreate(ctx, "chat", testOpts(st))
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	// The CLI exits (finished, crashed, OOM-killed).
	st.proc(0).exit(137)
	waitFor(t, dead.ctx.Done(), "the session never noticed its process had exited")

	// A dead session must not be looked up either.
	if _, ok := m.Get("chat"); ok {
		t.Error("Get returned a session whose process is gone")
	}

	fresh, err := m.GetOrCreate(ctx, "chat", testOpts(st))
	if err != nil {
		t.Fatalf("GetOrCreate after exit: %v", err)
	}
	if fresh == dead {
		t.Fatal("a session whose process had exited was handed back — every Send on it would fail")
	}
	if c := st.startCount(); c != 2 {
		t.Errorf("started %d processes, want 2 (the corpse plus its replacement)", c)
	}
	if got := m.Len(); got != 1 {
		t.Errorf("Len = %d, want 1 (the corpse must be untracked)", got)
	}
	if fresh.isClosed() {
		t.Error("the replacement session is already closed")
	}
	if err := fresh.Send(ctx, "hello again"); err != nil {
		t.Errorf("the replacement session cannot be used: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 15. idle reaping
// ---------------------------------------------------------------------------

func TestManager_ReapClosesIdleAndSparesActive(t *testing.T) {
	const ttl = 30 * time.Minute
	m := newTestManager(t, ttl)
	st := newFakeStarter()
	ctx := context.Background()

	idle, err := m.GetOrCreate(ctx, "abandoned", testOpts(st))
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	busy, err := m.GetOrCreate(ctx, "in-use", testOpts(st))
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	// Backdating the activity clock is how these tests avoid wall-clock waits:
	// "idle" has not been touched for twice the TTL, "in-use" was touched just now.
	now := time.Now()
	setLastUsed(idle, now.Add(-2*ttl))
	setLastUsed(busy, now.Add(-ttl/2))

	m.reap(now)

	if _, ok := m.Get("abandoned"); ok {
		t.Error("an idle session survived the reaper")
	}
	if !idle.isClosed() {
		t.Error("the reaped session was untracked but not closed — its container would leak")
	}
	waitFor(t, idle.Done(), "the reaped session's process was not terminated")

	if _, ok := m.Get("in-use"); !ok {
		t.Error("a session active inside the TTL was reaped")
	}
	if busy.isClosed() {
		t.Error("a session active inside the TTL was closed")
	}
	if got := m.Len(); got != 1 {
		t.Errorf("Len = %d, want 1", got)
	}

	// Right on the boundary the session is still fresh (Before, not !After).
	setLastUsed(busy, now.Add(-ttl))
	m.reap(now)
	if busy.isClosed() {
		t.Error("a session exactly at the TTL boundary was reaped")
	}

	// One tick past it, it goes.
	setLastUsed(busy, now.Add(-ttl-time.Nanosecond))
	m.reap(now)
	if !busy.isClosed() {
		t.Error("a session past the TTL was not reaped")
	}
	if got := m.Len(); got != 0 {
		t.Errorf("Len = %d, want 0", got)
	}
}

// reap also untracks sessions whose process died, so the next GetOrCreate starts
// clean rather than tripping over a corpse.
func TestManager_ReapUntracksExitedSessionsRegardlessOfIdle(t *testing.T) {
	m := newTestManager(t, time.Hour)
	st := newFakeStarter()

	s, err := m.GetOrCreate(context.Background(), "chat", testOpts(st))
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	st.proc(0).exit(0)
	waitFor(t, s.ctx.Done(), "the session never noticed its process had exited")

	// Freshly used, so only the exited check can catch it.
	setLastUsed(s, time.Now())
	m.reap(time.Now())

	if got := m.Len(); got != 0 {
		t.Errorf("Len = %d, want 0 — an exited session must be untracked", got)
	}
}

// The reaper must actually be wired to the ticker, not merely reachable.
func TestManager_ReapLoopFiresOnItsOwn(t *testing.T) {
	m := NewManager(ManagerOpts{IdleTTL: time.Millisecond, ReapInterval: time.Millisecond})
	t.Cleanup(m.Shutdown)
	st := newFakeStarter()

	s, err := m.GetOrCreate(context.Background(), "chat", testOpts(st))
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	// The session is idle from birth, so the first tick past the 1ms TTL takes it.
	waitFor(t, s.Done(), "the background reaper never closed an idle session")
}

// ---------------------------------------------------------------------------
// 16. shutdown
// ---------------------------------------------------------------------------

func TestManager_ShutdownClosesEverythingAndRefusesNewWork(t *testing.T) {
	m := NewManager(ManagerOpts{IdleTTL: time.Hour, ReapInterval: time.Hour})
	st := newFakeStarter()
	ctx := context.Background()

	var live []*Session
	for _, key := range []string{"a", "b", "c"} {
		s, err := m.GetOrCreate(ctx, key, testOpts(st))
		if err != nil {
			t.Fatalf("GetOrCreate(%s): %v", key, err)
		}
		live = append(live, s)
	}

	m.Shutdown()

	for i, s := range live {
		if !s.isClosed() {
			t.Errorf("session %d is still open after Shutdown", i)
		}
		waitFor(t, s.Done(), "a session's process survived Shutdown")
	}
	if got := m.Len(); got != 0 {
		t.Errorf("Len = %d after Shutdown, want 0", got)
	}

	_, err := m.GetOrCreate(ctx, "d", testOpts(st))
	if !errors.Is(err, ErrManagerStopped) {
		t.Fatalf("GetOrCreate after Shutdown = %v, want errors.Is ErrManagerStopped", err)
	}
	if got := st.startCount(); got != 3 {
		t.Errorf("started %d processes, want 3 — Shutdown must refuse before starting anything", got)
	}

	// Idempotent, and safe from several goroutines (the gateway's shutdown path may
	// race with a session's own cleanup).
	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() { defer wg.Done(); m.Shutdown() }()
		}
		wg.Wait()
	}()
	waitFor(t, done, "concurrent Shutdown calls deadlocked")
}

// Shutdown must not be blocked by an approver still waiting on a human.
func TestManager_ShutdownWithOutstandingApproval(t *testing.T) {
	m := NewManager(ManagerOpts{IdleTTL: time.Hour, ReapInterval: time.Hour})
	st := newFakeStarter()

	entered := make(chan struct{}, 1)
	opts := testOpts(st)
	opts.Permission = func(ctx context.Context, _ PermissionRequest) PermissionDecision {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return PermissionDecision{}
	}

	if _, err := m.GetOrCreate(context.Background(), "chat", opts); err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	st.proc(0).pushStdout(canUseToolLine("req-1", "Bash", `{"command":"make"}`))
	waitFor(t, entered, "the PermissionFunc was never called")

	done := make(chan struct{})
	go func() { defer close(done); m.Shutdown() }()
	waitFor(t, done, "Shutdown hung on an approval that was waiting for a human")
}

// ---------------------------------------------------------------------------
// lifetime + error paths
// ---------------------------------------------------------------------------

// The session must outlive the request that created it: the first user turn's
// handler returning cannot be allowed to kill the conversation.
func TestManager_SessionOutlivesCallerContext(t *testing.T) {
	m := newTestManager(t, time.Hour)
	st := newFakeStarter()

	ctx, cancel := context.WithCancel(context.Background())
	s, err := m.GetOrCreate(ctx, "chat", testOpts(st))
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	// context cancellation propagates to children synchronously, so if the caller's
	// ctx were the session's parent this would already be non-nil.
	cancel()
	if err := s.ctx.Err(); err != nil {
		t.Fatalf("the session ctx was cancelled with the caller's: %v", err)
	}
	if s.isClosed() {
		t.Fatal("the session closed when the request that created it returned")
	}
	if err := s.Send(context.Background(), "still here"); err != nil {
		t.Fatalf("Send after the creating request returned: %v", err)
	}
	st.proc(0).nextWrite(t)
}

func TestManager_GetOrCreateDoesNotTrackAFailedStart(t *testing.T) {
	m := newTestManager(t, time.Hour)

	bad := testOpts(newFakeStarter())
	bad.Spec.InteractiveArgs = nil

	if _, err := m.GetOrCreate(context.Background(), "chat", bad); !errors.Is(err, cliagent.ErrInteractiveUnsupported) {
		t.Fatalf("err = %v, want errors.Is cliagent.ErrInteractiveUnsupported", err)
	}
	if got := m.Len(); got != 0 {
		t.Errorf("Len = %d after a failed start, want 0", got)
	}
	if _, ok := m.Get("chat"); ok {
		t.Error("a failed start left a session behind")
	}
}

func TestManager_CloseKey(t *testing.T) {
	m := newTestManager(t, time.Hour)
	st := newFakeStarter()
	ctx := context.Background()

	// Closing a key that was never opened is the caller's intent either way.
	if err := m.Close("never-existed"); err != nil {
		t.Errorf("Close of an unknown key = %v, want nil", err)
	}

	s, err := m.GetOrCreate(ctx, "chat", testOpts(st))
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if err := m.Close("chat"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !s.isClosed() {
		t.Error("Close(key) did not close the session")
	}
	if got := m.Len(); got != 0 {
		t.Errorf("Len = %d after Close, want 0", got)
	}

	// The next GetOrCreate starts a brand new process.
	again, err := m.GetOrCreate(ctx, "chat", testOpts(st))
	if err != nil {
		t.Fatalf("GetOrCreate after Close: %v", err)
	}
	if again == s {
		t.Error("Close(key) left the old session reachable")
	}
	if got := st.startCount(); got != 2 {
		t.Errorf("started %d processes, want 2", got)
	}
}
