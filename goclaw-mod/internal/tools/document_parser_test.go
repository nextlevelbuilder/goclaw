package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// writeStubScript writes an executable POSIX stub and returns its path.
func writeStubScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", name, err)
	}
	return p
}

// stubLookPath returns a lookPath that resolves only the given binary→path map.
func stubLookPath(m map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if p, ok := m[name]; ok {
			return p, nil
		}
		return "", errors.New("not found: " + name)
	}
}

func newStubParser(cfg LocalExtractConfig, lp func(string) (string, error)) *LocalExtractParser {
	p := NewLocalExtractParser(cfg)
	p.lookPath = lp
	return p
}

func TestLocalExtractParser_SupportsRespectsEnabledAndBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX stub scripts not runnable on Windows")
	}
	dir := t.TempDir()
	pdftotext := writeStubScript(t, dir, "pdftotext", "#!/bin/sh\nprintf 'x'\n")

	// Disabled => never supports, even with a resolvable binary.
	off := newStubParser(LocalExtractConfig{Enabled: false}, stubLookPath(map[string]string{"pdftotext": pdftotext}))
	if off.Supports(mimePDF) {
		t.Error("disabled parser should not support any mime")
	}

	// Enabled + binary present => supported; binary absent => not.
	on := newStubParser(LocalExtractConfig{Enabled: true}, stubLookPath(map[string]string{"pdftotext": pdftotext}))
	if !on.Supports(mimePDF) {
		t.Error("expected PDF supported when pdftotext resolvable")
	}
	if on.Supports(mimeDOCX) {
		t.Error("expected DOCX unsupported when pandoc absent")
	}
	if on.Supports("text/plain") {
		t.Error("expected unsupported mime to be false")
	}
}

func TestLocalExtractParser_ExtractSuccessPDFAndDOCX(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX stub scripts not runnable on Windows")
	}
	dir := t.TempDir()
	pdftotext := writeStubScript(t, dir, "pdftotext", "#!/bin/sh\nprintf 'pdf text content here'\n")
	pandoc := writeStubScript(t, dir, "pandoc", "#!/bin/sh\nprintf 'docx text content here'\n")
	p := newStubParser(LocalExtractConfig{Enabled: true},
		stubLookPath(map[string]string{"pdftotext": pdftotext, "pandoc": pandoc}))

	got, err := p.Extract(context.Background(), "/tmp/doc.pdf", mimePDF)
	if err != nil {
		t.Fatalf("pdf extract: %v", err)
	}
	if got != "pdf text content here" {
		t.Errorf("pdf text = %q", got)
	}

	got, err = p.Extract(context.Background(), "/tmp/doc.docx", mimeDOCX)
	if err != nil {
		t.Fatalf("docx extract: %v", err)
	}
	if got != "docx text content here" {
		t.Errorf("docx text = %q", got)
	}
}

func TestLocalExtractParser_ExtractEmptyOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX stub scripts not runnable on Windows")
	}
	dir := t.TempDir()
	empty := writeStubScript(t, dir, "pdftotext", "#!/bin/sh\nprintf '   \\n  \\t '\n")
	p := newStubParser(LocalExtractConfig{Enabled: true}, stubLookPath(map[string]string{"pdftotext": empty}))

	_, err := p.Extract(context.Background(), "/tmp/scan.pdf", mimePDF)
	if !errors.Is(err, ErrParserEmpty) {
		t.Fatalf("expected ErrParserEmpty, got %v", err)
	}
}

func TestLocalExtractParser_ExtractBinaryAbsent(t *testing.T) {
	p := newStubParser(LocalExtractConfig{Enabled: true}, stubLookPath(map[string]string{}))
	_, err := p.Extract(context.Background(), "/tmp/doc.pdf", mimePDF)
	if !errors.Is(err, ErrParserUnavailable) {
		t.Fatalf("expected ErrParserUnavailable, got %v", err)
	}
}

func TestLocalExtractParser_ExtractDisabled(t *testing.T) {
	p := newStubParser(LocalExtractConfig{Enabled: false}, stubLookPath(map[string]string{}))
	_, err := p.Extract(context.Background(), "/tmp/doc.pdf", mimePDF)
	if !errors.Is(err, ErrParserUnsupported) {
		t.Fatalf("expected ErrParserUnsupported when disabled, got %v", err)
	}
}

func TestLocalExtractParser_ExtractUnsupportedMime(t *testing.T) {
	p := newStubParser(LocalExtractConfig{Enabled: true}, stubLookPath(map[string]string{}))
	_, err := p.Extract(context.Background(), "/tmp/a.txt", "text/plain")
	if !errors.Is(err, ErrParserUnsupported) {
		t.Fatalf("expected ErrParserUnsupported, got %v", err)
	}
}

func TestLocalExtractParser_ExtractNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX stub scripts not runnable on Windows")
	}
	dir := t.TempDir()
	failing := writeStubScript(t, dir, "pdftotext", "#!/bin/sh\necho boom >&2\nexit 1\n")
	p := newStubParser(LocalExtractConfig{Enabled: true}, stubLookPath(map[string]string{"pdftotext": failing}))

	_, err := p.Extract(context.Background(), "/tmp/doc.pdf", mimePDF)
	if err == nil {
		t.Fatal("expected error on non-zero exit")
	}
	// Must not be a sentinel that hides the real failure.
	if errors.Is(err, ErrParserEmpty) {
		t.Errorf("non-zero exit should not map to ErrParserEmpty: %v", err)
	}
}

func TestLocalExtractParser_ExtractTruncatesAtCap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX stub scripts not runnable on Windows")
	}
	dir := t.TempDir()
	// Emit well over the 500KB cap so the draining writer marks overflow.
	big := writeStubScript(t, dir, "pdftotext", "#!/bin/sh\nyes A | head -c 600000\n")
	p := newStubParser(LocalExtractConfig{Enabled: true}, stubLookPath(map[string]string{"pdftotext": big}))

	got, err := p.Extract(context.Background(), "/tmp/big.pdf", mimePDF)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !strings.HasSuffix(got, docTruncationMarker) {
		t.Errorf("expected truncation marker suffix; tail=%q", got[max(0, len(got)-40):])
	}
	if len(got) > documentMaxTextBytes+len(docTruncationMarker) {
		t.Errorf("output %d exceeds cap %d + marker", len(got), documentMaxTextBytes)
	}
}

// TestLocalExtractParser_TimeoutReapsProcessGroup proves a timeout kills the
// whole process group (not just the direct child): the stub forks a grandchild
// sleep, records its PID, and the test asserts that PID is gone afterwards.
func TestLocalExtractParser_TimeoutReapsProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group kill not supported on Windows")
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	body := fmt.Sprintf("#!/bin/sh\nsleep 30 &\necho $! > %s\nwait\n", pidFile)
	slow := writeStubScript(t, dir, "pdftotext", body)

	p := newStubParser(LocalExtractConfig{Enabled: true, Timeout: 1 * time.Second},
		stubLookPath(map[string]string{"pdftotext": slow}))

	got, err := p.Extract(context.Background(), "/tmp/slow.pdf", mimePDF)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if got != "" {
		t.Errorf("expected no partial text on timeout, got %d bytes", len(got))
	}

	pids := waitForRecordedPIDs(t, pidFile, 1, 2*time.Second)
	time.Sleep(200 * time.Millisecond) // let the OS reap the killed group
	if orphans := findLivePIDs(t, pids); len(orphans) > 0 {
		t.Errorf("grandchild sleep not reaped after timeout: %v", orphans)
	}
}
