package skills

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTarGz builds a .tar.gz file with the given entries.
func writeTarGz(t *testing.T, entries map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()

	f, err := os.CreateTemp("", "goclaw-test-*.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

// writeZip builds a .zip with the given entries.
func writeZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()

	f, err := os.CreateTemp("", "goclaw-test-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

func TestExtractTarGz_HappyPath(t *testing.T) {
	path := writeTarGz(t, map[string]string{
		"lazygit":  "ELF\x7fhello",
		"LICENSE":  "MIT",
		"README.md": "readme",
	})
	files, err := ExtractArchive(path, 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("want 3 files, got %d", len(files))
	}
	var gotNames []string
	for _, f := range files {
		gotNames = append(gotNames, f.Name)
	}
	for _, want := range []string{"lazygit", "LICENSE", "README.md"} {
		found := false
		for _, g := range gotNames {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %s in %v", want, gotNames)
		}
	}
}

func TestExtractZip_HappyPath(t *testing.T) {
	path := writeZip(t, map[string]string{
		"rg":      "binary-content",
		"doc.md":  "doc",
	})
	files, err := ExtractArchive(path, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("want 2, got %d", len(files))
	}
}

// -------- Security: path traversal --------

func TestSanitizePath_Malicious(t *testing.T) {
	bad := []string{
		"../../../etc/passwd",
		"/etc/passwd",
		"..",
		"../outside",
		"safe/../../etc/shadow",
		"C:\\Windows\\cmd.exe",
		"with\x00null",
		"",
	}
	for _, p := range bad {
		if _, err := sanitizePath(p); err == nil {
			t.Errorf("sanitizePath(%q) should reject", p)
		}
	}
	ok := []string{"lazygit", "dir/sub/file", "nested/tool.bin"}
	for _, p := range ok {
		if _, err := sanitizePath(p); err != nil {
			t.Errorf("sanitizePath(%q) should accept: %v", p, err)
		}
	}
}

func TestExtractTarGz_PathTraversal(t *testing.T) {
	path := writeTarGz(t, map[string]string{"../../../etc/evil": "pwn"})
	_, err := ExtractArchive(path, 1024)
	if !errors.Is(err, ErrUnsafePath) {
		t.Errorf("want ErrUnsafePath, got %v", err)
	}
}

func TestExtractZip_PathTraversal(t *testing.T) {
	path := writeZip(t, map[string]string{"../escape.txt": "pwn"})
	_, err := ExtractArchive(path, 1024)
	if !errors.Is(err, ErrUnsafePath) {
		t.Errorf("want ErrUnsafePath, got %v", err)
	}
}

// -------- Security: zip bomb / size cap --------

func TestExtractTarGz_ZipBomb_ByCumulativeSize(t *testing.T) {
	entries := map[string]string{}
	body := strings.Repeat("A", 1024)
	for i := 0; i < 100; i++ {
		entries[fileNameN(i)] = body // 100 KB total
	}
	path := writeTarGz(t, entries)
	_, err := ExtractArchive(path, 10*1024) // 10 KB cap
	if !errors.Is(err, ErrZipBomb) {
		t.Errorf("want ErrZipBomb, got %v", err)
	}
}

func TestExtractZip_ZipBomb(t *testing.T) {
	entries := map[string]string{}
	body := strings.Repeat("X", 1024)
	for i := 0; i < 100; i++ {
		entries[fileNameN(i)] = body
	}
	path := writeZip(t, entries)
	_, err := ExtractArchive(path, 10*1024)
	if !errors.Is(err, ErrZipBomb) {
		t.Errorf("want ErrZipBomb, got %v", err)
	}
}

// -------- Security: ELF validation --------

func TestValidateELF_NonELFRejected(t *testing.T) {
	vectors := map[string][]byte{
		"PDF":        []byte("%PDF-1.4\n"),
		"shell":      []byte("#!/bin/bash\necho hi\n"),
		"PE":         []byte("MZ\x90\x00"),
		"machO":      {0xcf, 0xfa, 0xed, 0xfe, 0x07, 0x00, 0x00, 0x01},
		"truncated":  {0x7f, 0x45, 0x4c},
		"empty":      {},
	}
	for name, v := range vectors {
		if err := validateELF(v); err == nil {
			t.Errorf("vector %s should be rejected as non-ELF", name)
		}
	}
}

// -------- Security: extractRaw for unknown bytes --------

func TestExtractRaw_NonArchive(t *testing.T) {
	tmp, err := os.CreateTemp("", "raw-*.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	tmp.Write([]byte("plain bytes"))
	tmp.Close()
	files, err := ExtractArchive(tmp.Name(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || string(files[0].Content) != "plain bytes" {
		t.Errorf("unexpected files: %+v", files)
	}
}

// -------- Helpers --------

func fileNameN(i int) string {
	// Avoid importing "strconv" for tiny helper.
	return filepath.Join("d", []string{"a", "b", "c"}[i%3], "f", rune2s(i))
}
func rune2s(i int) string {
	var buf [6]byte
	n := 0
	if i == 0 {
		return "0"
	}
	for i > 0 {
		buf[n] = byte('0' + i%10)
		n++
		i /= 10
	}
	// reverse
	var out []byte
	for k := n - 1; k >= 0; k-- {
		out = append(out, buf[k])
	}
	return string(out)
}

// Ensure the compress/bytes/io imports are kept — used only in helpers above.
var _ = io.EOF

// -------- P1.1: Raw ELF uses caller-supplied logical name --------

func TestExtractArchiveAs_RawELFUsesFallbackName(t *testing.T) {
	// Write a tiny "ELF" (magic bytes only — format parsing is done by
	// validateELF in callers, extractRaw just copies bytes).
	tmp, err := os.CreateTemp("", "goclaw-gh-asset-*.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	tmp.Write([]byte{0x7f, 0x45, 0x4c, 0x46, 0x02, 0x01, 0x01, 0x00}) // \x7fELF header stub
	tmp.Close()

	// Without fallback → takes temp basename (the bug we're fixing).
	files, err := ExtractArchive(tmp.Name(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(files[0].Name), "goclaw-gh-asset-") {
		t.Errorf("expected temp basename leakage when no fallback, got %q", files[0].Name)
	}

	// With fallback → records logical name.
	files, err = ExtractArchiveAs(tmp.Name(), "lazygit", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if files[0].Name != "lazygit" {
		t.Errorf("ExtractArchiveAs with fallbackName=lazygit recorded %q, want %q",
			files[0].Name, "lazygit")
	}
}

// -------- P1.3: Archive entry count cap (DoS protection) --------

func TestExtractTarGz_EntryCountCap(t *testing.T) {
	// Build a tar archive with more than maxArchiveEntries tiny entries.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for i := 0; i < maxArchiveEntries+5; i++ {
		name := "f" + rune2s(i)
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte{'x'})
	}
	tw.Close()
	gz.Close()

	f, err := os.CreateTemp("", "many-*.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.Write(buf.Bytes())
	f.Close()

	_, err = ExtractArchive(f.Name(), 100*1024*1024) // Large size cap — only entry cap should trip.
	if !errors.Is(err, ErrTooManyEntries) {
		t.Errorf("want ErrTooManyEntries, got %v", err)
	}
}

func TestExtractZip_EntryCountCap(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := 0; i < maxArchiveEntries+5; i++ {
		w, err := zw.Create("f" + rune2s(i))
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte{'x'})
	}
	zw.Close()

	f, err := os.CreateTemp("", "many-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.Write(buf.Bytes())
	f.Close()

	_, err = ExtractArchive(f.Name(), 100*1024*1024)
	if !errors.Is(err, ErrTooManyEntries) {
		t.Errorf("want ErrTooManyEntries, got %v", err)
	}
}

// TestPeekZipEntryCount covers the DoS-preemption path: the EOCD-level check
// must reject oversized archives BEFORE zip.OpenReader allocates
// []*zip.File of declared capacity.
func TestPeekZipEntryCount(t *testing.T) {
	// Small zip: peek returns exact count.
	small := writeZip(t, map[string]string{"a": "1", "b": "2", "c": "3"})
	n, err := peekZipEntryCount(small)
	if err != nil {
		t.Fatalf("peek small: %v", err)
	}
	if n != 3 {
		t.Errorf("peek small count = %d, want 3", n)
	}

	// Non-zip file: peek returns -1 gracefully (no false positive).
	raw, err := os.CreateTemp("", "raw-*.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(raw.Name())
	raw.Write([]byte("not a zip file, just random bytes"))
	raw.Close()
	n, err = peekZipEntryCount(raw.Name())
	if err != nil {
		t.Fatalf("peek raw: %v", err)
	}
	if n != -1 {
		t.Errorf("peek raw should return -1 (EOCD not found), got %d", n)
	}
}
