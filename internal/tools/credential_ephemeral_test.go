package tools

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

func TestMaterializeEphemeral_WritesAndCleansUp(t *testing.T) {
	want := []byte("ephemeral-payload\n")
	path, cleanup, err := materializeEphemeral(context.Background(), want, "test")
	if err != nil {
		t.Fatalf("materializeEphemeral: %v", err)
	}

	// Path must live under os.TempDir() (per-user on POSIX, %TEMP% on Windows).
	if dir := filepath.Dir(path); dir != filepath.Clean(os.TempDir()) {
		t.Fatalf("ephemeral parent dir=%q, want %q", dir, filepath.Clean(os.TempDir()))
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("content=%q, want %q", got, want)
	}

	// POSIX-only: mode 0600. On Windows the mode bits do not reflect ACLs;
	// the explicit Chmod is a documented no-op.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("file mode=%o, want 0600", perm)
		}
	}

	if err := cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("file still exists after cleanup: stat err=%v", err)
	}

	// Second cleanup is a no-op, not an error.
	if err := cleanup(); err != nil {
		t.Fatalf("idempotent cleanup returned err: %v", err)
	}
}

func TestMaterializeEphemeral_ConcurrentCleanup(t *testing.T) {
	path, cleanup, err := materializeEphemeral(context.Background(), []byte("x"), "concurrent")
	if err != nil {
		t.Fatalf("materializeEphemeral: %v", err)
	}

	var nilCount, errCount atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := cleanup(); err == nil {
				nilCount.Add(1)
			} else {
				errCount.Add(1)
			}
		}()
	}
	wg.Wait()

	// All callers see nil (idempotent latch swallows redundant deletes).
	if got := nilCount.Load(); got != 64 {
		t.Fatalf("concurrent cleanup nil count=%d, want 64", got)
	}
	if got := errCount.Load(); got != 0 {
		t.Fatalf("concurrent cleanup err count=%d, want 0", got)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("file still exists after concurrent cleanup")
	}
}

func TestMaterializeEphemeral_ZeroContentOK(t *testing.T) {
	path, cleanup, err := materializeEphemeral(context.Background(), nil, "empty")
	if err != nil {
		t.Fatalf("materializeEphemeral: %v", err)
	}
	t.Cleanup(func() { _ = cleanup() })

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("size=%d, want 0", info.Size())
	}
}
