//go:build dockerint

package sandbox

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Real-daemon coverage for ExecInteractive. Everything else in this package
// exercises the session state machine against local processes; this is the only
// test that proves the actual `docker exec -i` plumbing works — stdin stays open,
// output streams back line by line, and the container is reused across writes.
//
// Guarded by the `dockerint` tag because it needs a working Docker daemon:
//
//	go test -tags dockerint -run TestDockerExecInteractive ./internal/sandbox/ -v
func TestDockerExecInteractive(t *testing.T) {
	img := os.Getenv("SANDBOX_TEST_IMAGE")
	if img == "" {
		img = "alpine:latest"
	}
	wd, err := os.MkdirTemp("", "sbxint")
	if err != nil {
		t.Fatalf("tempdir: %v", err)
	}
	defer os.RemoveAll(wd)

	mgr := NewDockerManager(Config{
		Mode:            ModeAll,
		Image:           img,
		WorkspaceAccess: AccessNone,
		ContainerPrefix: "goclaw-itest-",
		NetworkEnabled:  false,
		TimeoutSec:      60,
		MemoryMB:        512,
	})
	defer mgr.ReleaseAll(context.Background())

	ctx := context.Background()
	sb, err := mgr.Get(ctx, "interactive-itest", wd, nil)
	if err != nil {
		t.Skipf("cannot start sandbox container (is Docker running?): %v", err)
	}

	var (
		mu    sync.Mutex
		lines []string
	)
	got := make(chan string, 32)
	sess, err := sb.ExecInteractive(ctx, []string{"cat"}, "",
		WithStdoutLine(func(l string) {
			mu.Lock()
			lines = append(lines, l)
			mu.Unlock()
			select {
			case got <- l:
			default:
			}
		}))
	if err != nil {
		t.Fatalf("ExecInteractive: %v", err)
	}
	defer sess.Close()

	// The whole point: the process must stay alive between writes, so a second
	// write reaches the SAME process rather than a fresh one.
	for _, want := range []string{"first", "second", "third"} {
		if err := sess.WriteLine(want); err != nil {
			t.Fatalf("WriteLine(%q): %v", want, err)
		}
		select {
		case l := <-got:
			if strings.TrimSpace(l) != want {
				t.Fatalf("echoed %q, want %q", l, want)
			}
		case <-time.After(20 * time.Second):
			mu.Lock()
			seen := append([]string(nil), lines...)
			mu.Unlock()
			t.Fatalf("timed out waiting for %q; saw %v", want, seen)
		}
	}

	// Closing stdin must let `cat` exit cleanly — and a clean exit must NOT be
	// reported as an error (the context.Canceled trap the unit tests caught).
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-sess.Done():
	case <-time.After(20 * time.Second):
		t.Fatal("session did not finish after Close")
	}
	// Exit 0 specifically: closing stdin must let the process finish on its own.
	// A -1 here means Close() killed it instead of honouring the EOF, which for a
	// stream-json CLI can drop the final result event.
	if code := sess.ExitCode(); code != 0 {
		t.Fatalf("exit code %d, want 0 — Close() killed the process instead of letting stdin EOF end it", code)
	}
	t.Logf("real docker exec -i verified: 3 round trips, graceful exit 0")
}
