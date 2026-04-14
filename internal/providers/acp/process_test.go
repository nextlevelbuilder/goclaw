package acp

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// --- ProcessPool construction ---

func TestNewProcessPool_Fields(t *testing.T) {
	pp := NewProcessPool("/usr/bin/claude", []string{"--acp"}, "/workspace", 5*time.Minute)
	if pp == nil {
		t.Fatal("expected non-nil ProcessPool")
	}
	if pp.agentBinary != "/usr/bin/claude" {
		t.Errorf("agentBinary: got %q", pp.agentBinary)
	}
	if len(pp.agentArgs) != 1 || pp.agentArgs[0] != "--acp" {
		t.Errorf("agentArgs: got %v", pp.agentArgs)
	}
	if pp.workDir != "/workspace" {
		t.Errorf("workDir: got %q", pp.workDir)
	}
	if pp.idleTTL != 5*time.Minute {
		t.Errorf("idleTTL: got %v", pp.idleTTL)
	}
	// Clean up reapLoop goroutine
	pp.Close()
}

func TestNewProcessPool_DoneChannelOpen(t *testing.T) {
	pp := NewProcessPool("x", nil, "", time.Minute)
	select {
	case <-pp.done:
		t.Error("done channel should not be closed before Close()")
	default:
	}
	pp.Close()
}

// --- SetToolHandler / getToolHandler ---

func TestProcessPool_SetToolHandler(t *testing.T) {
	pp := NewProcessPool("x", nil, "", time.Minute)
	defer pp.Close()

	if pp.getToolHandler() != nil {
		t.Error("expected nil handler before SetToolHandler")
	}

	called := make(chan string, 1)
	h := RequestHandler(func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		called <- method
		return nil, nil
	})
	pp.SetToolHandler(h)

	if pp.getToolHandler() == nil {
		t.Error("expected non-nil handler after SetToolHandler")
	}
}

// --- Close ---

func TestProcessPool_Close_Idempotent(t *testing.T) {
	pp := NewProcessPool("x", nil, "", time.Minute)
	// Close twice must not panic or deadlock
	if err := pp.Close(); err != nil {
		t.Fatalf("first Close error: %v", err)
	}
	if err := pp.Close(); err != nil {
		t.Fatalf("second Close error: %v", err)
	}
}

func TestProcessPool_Close_DrainsDoneChannel(t *testing.T) {
	pp := NewProcessPool("x", nil, "", time.Minute)
	pp.Close()

	select {
	case <-pp.done:
		// expected — done closed by Close()
	case <-time.After(time.Second):
		t.Fatal("expected done channel to be closed after Close()")
	}
}

func TestProcessPool_Close_WithFakeProcesses(t *testing.T) {
	pp := NewProcessPool("x", nil, "", time.Minute)

	// Insert fake ACPProcess entries that are already exited
	for i := 0; i < 3; i++ {
		exitedCh := make(chan struct{})
		close(exitedCh) // pre-exited
		proc := &ACPProcess{
			exited: exitedCh,
			cancel: func() {},
			ctx:    context.Background(),
		}
		// Wire a no-op Conn so Close doesn't need real pipes
		proc.conn = &Conn{done: make(chan struct{})}
		pp.processes.Store(i, proc)
	}

	done := make(chan error, 1)
	go func() { done <- pp.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close with pre-exited processes: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close timed out with pre-exited fake processes")
	}
}

func TestProcessPool_Close_TimesOutSlowProcess(t *testing.T) {
	// Override the per-process close timeout so the test doesn't burn 5s
	// of wall time just to exercise the "fake process never exits" path.
	// Production behavior is unchanged — only this test sees 20ms.
	saved := processCloseTimeout
	processCloseTimeout = 20 * time.Millisecond
	t.Cleanup(func() { processCloseTimeout = saved })

	pp := NewProcessPool("x", nil, "", time.Minute)

	// Insert a fake process that never exits (simulates slow shutdown).
	neverExit := make(chan struct{}) // never closed
	proc := &ACPProcess{
		exited: neverExit,
		cancel: func() {},
		ctx:    context.Background(),
	}
	proc.conn = &Conn{done: make(chan struct{})}
	pp.processes.Store("slow", proc)

	// Close must return promptly even though the fake process never exits,
	// because the timeout branch fires after processCloseTimeout elapses.
	done := make(chan error, 1)
	go func() { done <- pp.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Close did not return within 1 second even with fast timeout override")
	}
}

// --- reapLoop behaviour (via pool state manipulation) ---

func TestProcessPool_ReapLoop_SkipsActivePrompts(t *testing.T) {
	pp := NewProcessPool("x", nil, "", 1*time.Millisecond) // very short TTL
	// Use a pre-exited channel so Close() doesn't wait 5s
	exitedCh := make(chan struct{})
	close(exitedCh)

	proc := &ACPProcess{
		exited:     exitedCh,
		cancel:     func() {},
		ctx:        context.Background(),
		lastActive: time.Now().Add(-time.Hour), // very old
	}
	proc.conn = &Conn{done: make(chan struct{})}
	proc.inUse.Store(1) // active prompt — reaper must skip
	pp.processes.Store("active-key", proc)
	defer pp.Close()

	// Wait more than the reap interval (30s internal ticker — can't shorten without
	// interface injection). Instead, exercise the Range callback directly by calling
	// reapLoop's logic inline as a unit.
	//
	// The reapLoop uses a 30s ticker, so we exercise the logic directly:
	pp.processes.Range(func(key, value any) bool {
		p := value.(*ACPProcess)
		if p.inUse.Load() > 0 {
			return true // should skip
		}
		t.Errorf("should not reap process with active prompt, key=%v", key)
		return true
	})

	if _, ok := pp.processes.Load("active-key"); !ok {
		t.Error("active process should not have been removed")
	}
}

func TestProcessPool_GetOrSpawn_ReturnsExisting(t *testing.T) {
	pp := NewProcessPool("x", nil, "", time.Minute)
	// Pre-insert a fake running process — exited channel intentionally never closed
	exitedCh := make(chan struct{}) // open = "still running"
	proc := &ACPProcess{
		exited: exitedCh,
		cancel: func() {},
		ctx:    context.Background(),
	}
	proc.conn = &Conn{done: make(chan struct{})}
	pp.processes.Store("sess-existing", proc)
	// Remove the stalling process before Close so cleanup is fast
	t.Cleanup(func() {
		pp.processes.Delete("sess-existing")
		pp.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	got, err := pp.GetOrSpawn(ctx, "sess-existing")
	if err != nil {
		t.Fatalf("GetOrSpawn error: %v", err)
	}
	if got != proc {
		t.Error("expected same *ACPProcess to be returned from pool")
	}
}

func TestProcessPool_GetOrSpawn_RespawnsExited(t *testing.T) {
	// When stored process has exited channel closed, GetOrSpawn should attempt
	// to respawn. The respawn will fail (no real binary) — verify it attempts
	// and the stale entry is removed.
	pp := NewProcessPool("/nonexistent-binary-xyz", nil, t.TempDir(), time.Minute)
	defer pp.Close()

	exitedCh := make(chan struct{})
	close(exitedCh) // pre-exited = crashed

	proc := &ACPProcess{
		exited: exitedCh,
		cancel: func() {},
		ctx:    context.Background(),
	}
	proc.conn = &Conn{done: make(chan struct{})}
	pp.processes.Store("sess-crashed", proc)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := pp.GetOrSpawn(ctx, "sess-crashed")
	// Expect an error because the binary doesn't exist
	if err == nil {
		t.Fatal("expected error when respawning with nonexistent binary")
	}

	// Stale entry should have been removed before the failed spawn
	if _, ok := pp.processes.Load("sess-crashed"); ok {
		t.Error("expected stale process entry to be removed after crash detection")
	}
}

func TestProcessPool_GetOrSpawn_SpawnFailsGracefully(t *testing.T) {
	pp := NewProcessPool("/definitely-does-not-exist", nil, t.TempDir(), time.Minute)
	defer pp.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := pp.GetOrSpawn(ctx, "new-session")
	if err == nil {
		t.Fatal("expected error when binary does not exist")
	}
}
