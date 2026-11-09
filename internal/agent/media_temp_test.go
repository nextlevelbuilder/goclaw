package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

func TestPersistMediaRemovesTempSource(t *testing.T) {
	workspace := t.TempDir()
	src := filepath.Join(t.TempDir(), "download.bin")
	if err := os.WriteFile(src, []byte("temp bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	var loop Loop
	refs := loop.persistMedia("session", []bus.MediaFile{{
		Path:     src,
		MimeType: "application/octet-stream",
		Filename: "download.bin",
	}}, workspace)
	if len(refs) != 1 {
		t.Fatalf("refs = %d, want 1", len(refs))
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("temp source still exists: %v", err)
	}
}

func TestPersistMediaKeepsNonTempSource(t *testing.T) {
	base, err := os.MkdirTemp(".", "goclaw-media-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)

	workspace := filepath.Join(base, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(base, "user-file.bin")
	if err := os.WriteFile(src, []byte("user bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	var loop Loop
	refs := loop.persistMedia("session", []bus.MediaFile{{
		Path:     src,
		MimeType: "application/octet-stream",
		Filename: "user-file.bin",
	}}, workspace)
	if len(refs) != 1 {
		t.Fatalf("refs = %d, want 1", len(refs))
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("non-temp source was removed: %v", err)
	}
}
