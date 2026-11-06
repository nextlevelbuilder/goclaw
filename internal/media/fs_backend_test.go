package media

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFSBackend_SaveLoadDelete is the core round-trip: write a file,
// look it up by ID, read it back, then drop the session.
func TestFSBackend_SaveLoadDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	base := t.TempDir()
	b, err := NewFSBackend(base)
	if err != nil {
		t.Fatalf("NewFSBackend: %v", err)
	}

	src := filepath.Join(t.TempDir(), "src.png")
	if err := os.WriteFile(src, []byte("PNG-bytes"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	id, ext, err := b.Save(ctx, "session-a", src, "image/png")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id == "" || ext != ".png" {
		t.Fatalf("unexpected save result: id=%q ext=%q", id, ext)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("expected src to be consumed, got err=%v", err)
	}

	p, err := b.LocalPath(ctx, id)
	if err != nil {
		t.Fatalf("LocalPath: %v", err)
	}
	if !strings.HasPrefix(p, base) {
		t.Fatalf("LocalPath %q outside base %q", p, base)
	}

	rc, err := b.Open(ctx, id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	body, _ := io.ReadAll(rc)
	rc.Close()
	if string(body) != "PNG-bytes" {
		t.Fatalf("Open returned %q, want PNG-bytes", body)
	}

	if err := b.Delete(ctx, "session-a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := b.LocalPath(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after Delete LocalPath err = %v, want ErrNotFound", err)
	}
}

// TestFSBackend_MimeOverridesExtension confirms ExtFromMime wins over
// the source file's own extension — important because some upstream
// callers hand us a tempfile with no extension at all.
func TestFSBackend_MimeOverridesExtension(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b, _ := NewFSBackend(t.TempDir())

	src := filepath.Join(t.TempDir(), "no-ext-here")
	_ = os.WriteFile(src, []byte("x"), 0o644)

	_, ext, err := b.Save(ctx, "s", src, "audio/mpeg")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if ext != ".mp3" {
		t.Fatalf("ext = %q, want .mp3", ext)
	}
}

// TestFSBackend_LoadPathMissingReturnsErrNotFound is what callers
// (media handlers, agent loop) match on when a stale ID slips through.
func TestFSBackend_LoadPathMissingReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	b, _ := NewFSBackend(t.TempDir())
	_, err := b.LocalPath(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
