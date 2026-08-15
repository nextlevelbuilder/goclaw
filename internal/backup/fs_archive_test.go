package backup

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readTarEntries reads all entries from a tar archive into a map[name]content.
func readTarEntries(t *testing.T, data []byte) map[string]string {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(data))
	entries := make(map[string]string)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read error: %v", err)
		}
		content, _ := io.ReadAll(tr)
		entries[hdr.Name] = string(content)
	}
	return entries
}

// --- ArchiveDirectory ---

func TestArchiveDirectory_EmptyDir(t *testing.T) {
	src := t.TempDir()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	files, bytes, err := ArchiveDirectory(tw, src, "workspace", nil)
	tw.Close()

	if err != nil {
		t.Fatalf("ArchiveDirectory on empty dir: %v", err)
	}
	if files != 0 {
		t.Errorf("expected 0 files, got %d", files)
	}
	if bytes != 0 {
		t.Errorf("expected 0 bytes, got %d", bytes)
	}
}

func TestArchiveDirectory_SingleFile(t *testing.T) {
	src := t.TempDir()
	content := "hello world content"
	os.WriteFile(filepath.Join(src, "hello.txt"), []byte(content), 0644)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	files, totalBytes, err := ArchiveDirectory(tw, src, "ws", nil)
	tw.Close()

	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if files != 1 {
		t.Errorf("expected 1 file, got %d", files)
	}
	if totalBytes != int64(len(content)) {
		t.Errorf("expected %d bytes, got %d", len(content), totalBytes)
	}

	entries := readTarEntries(t, buf.Bytes())
	if _, ok := entries["ws/hello.txt"]; !ok {
		t.Errorf("expected ws/hello.txt in tar, got keys: %v", keys(entries))
	}
	if entries["ws/hello.txt"] != content {
		t.Errorf("content mismatch: got %q", entries["ws/hello.txt"])
	}
}

func TestArchiveDirectory_NestedDirs(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "subdir"), 0755)
	os.WriteFile(filepath.Join(src, "top.txt"), []byte("top"), 0644)
	os.WriteFile(filepath.Join(src, "subdir", "nested.txt"), []byte("nested"), 0644)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	files, _, err := ArchiveDirectory(tw, src, "data", nil)
	tw.Close()

	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if files != 2 {
		t.Errorf("expected 2 files, got %d", files)
	}
	entries := readTarEntries(t, buf.Bytes())
	if _, ok := entries["data/top.txt"]; !ok {
		t.Errorf("missing data/top.txt, got: %v", keys(entries))
	}
	if _, ok := entries["data/subdir/nested.txt"]; !ok {
		t.Errorf("missing data/subdir/nested.txt, got: %v", keys(entries))
	}
}

func TestArchiveDirectory_SkipsHiddenFiles(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, ".hidden"), []byte("secret"), 0644)
	os.WriteFile(filepath.Join(src, "visible.txt"), []byte("visible"), 0644)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	files, _, err := ArchiveDirectory(tw, src, "ws", nil)
	tw.Close()

	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if files != 1 {
		t.Errorf("expected 1 file (hidden skipped), got %d", files)
	}
	entries := readTarEntries(t, buf.Bytes())
	if _, ok := entries["ws/.hidden"]; ok {
		t.Error("hidden file should not be in archive")
	}
}

func TestArchiveDirectory_SkipsHiddenDirs(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, ".git"), 0755)
	os.WriteFile(filepath.Join(src, ".git", "config"), []byte("git stuff"), 0644)
	os.WriteFile(filepath.Join(src, "README.txt"), []byte("readme"), 0644)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	files, _, err := ArchiveDirectory(tw, src, "ws", nil)
	tw.Close()

	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if files != 1 {
		t.Errorf("expected 1 file (.git dir skipped), got %d", files)
	}
	entries := readTarEntries(t, buf.Bytes())
	for k := range entries {
		if strings.Contains(k, ".git") {
			t.Errorf("hidden dir contents should not be archived: %q", k)
		}
	}
}

func TestArchiveDirectory_SkipsTmpDirs(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "tmp"), 0755)
	os.WriteFile(filepath.Join(src, "tmp", "temp.txt"), []byte("temp"), 0644)
	os.WriteFile(filepath.Join(src, "real.txt"), []byte("real"), 0644)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	files, _, err := ArchiveDirectory(tw, src, "ws", nil)
	tw.Close()

	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if files != 1 {
		t.Errorf("expected 1 file (tmp dir skipped), got %d", files)
	}
}

func TestArchiveDirectory_CustomSkipFn(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "keep.txt"), []byte("keep"), 0644)
	os.WriteFile(filepath.Join(src, "skip.log"), []byte("skip me"), 0644)

	skipLogs := func(path string) bool {
		return strings.HasSuffix(path, ".log")
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	files, _, err := ArchiveDirectory(tw, src, "ws", skipLogs)
	tw.Close()

	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if files != 1 {
		t.Errorf("expected 1 file (.log skipped), got %d", files)
	}
	entries := readTarEntries(t, buf.Bytes())
	if _, ok := entries["ws/keep.txt"]; !ok {
		t.Error("keep.txt should be in archive")
	}
	if _, ok := entries["ws/skip.log"]; ok {
		t.Error("skip.log should not be in archive")
	}
}

func TestArchiveDirectory_TarPrefix(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "file.txt"), []byte("x"), 0644)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	ArchiveDirectory(tw, src, "custom/prefix", nil)
	tw.Close()

	entries := readTarEntries(t, buf.Bytes())
	if _, ok := entries["custom/prefix/file.txt"]; !ok {
		t.Errorf("expected custom/prefix/file.txt in archive, got: %v", keys(entries))
	}
}

// keys returns sorted map keys for test output readability.
func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
