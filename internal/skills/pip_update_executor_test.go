package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// setupFixturePipForExecutor overrides pipBinary to the bundled fixture script
// and restores it via t.Cleanup. The fixture honours FIXTURE_PIP_EXIT and
// FIXTURE_PIP_STDERR environment variables for the `install` subcommand.
func setupFixturePipForExecutor(t *testing.T) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	fixturePath := filepath.Join(filepath.Dir(file), "testdata", "pip", "bin", "pip3")

	origBinary := pipBinary
	origLookPath := pipLookPath
	pipBinary = fixturePath
	pipLookPath = func(string) (string, error) { return fixturePath, nil }
	t.Cleanup(func() {
		pipBinary = origBinary
		pipLookPath = origLookPath
	})
}

// TestPipExecutor_ValidationReject verifies that invalid package names are
// rejected before any subprocess is spawned.
func TestPipExecutor_ValidationReject(t *testing.T) {
	setupFixturePipForExecutor(t)

	e := NewPipUpdateExecutor()
	// "typescript@latest" contains '@' which ValidatePipPackageName rejects.
	err := e.Update(context.Background(), "typescript@latest", "1.0.0", nil)
	if err == nil {
		t.Fatal("expected error for invalid package name, got nil")
	}
}

// TestPipExecutor_Success verifies that exit 0 from pip returns nil error.
func TestPipExecutor_Success(t *testing.T) {
	setupFixturePipForExecutor(t)
	// FIXTURE_PIP_EXIT defaults to 0 — no env override needed.

	e := NewPipUpdateExecutor()
	err := e.Update(context.Background(), "requests", "2.31.0", nil)
	if err != nil {
		t.Fatalf("unexpected error on success path: %v", err)
	}
}

// TestPipExecutor_ConflictStderr verifies that stderr containing "dependency resolver"
// is classified as ErrUpdatePipConflict.
func TestPipExecutor_ConflictStderr(t *testing.T) {
	setupFixturePipForExecutor(t)
	t.Setenv("FIXTURE_PIP_EXIT", "1")
	t.Setenv("FIXTURE_PIP_STDERR", "ERROR: pip's dependency resolver does not currently take into account all the packages that are installed.")

	e := NewPipUpdateExecutor()
	err := e.Update(context.Background(), "requests", "2.31.0", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUpdatePipConflict) {
		t.Errorf("errors.Is(err, ErrUpdatePipConflict) = false; err = %v", err)
	}
}

// TestPipExecutor_NetworkStderr verifies that stderr containing "Read timed out"
// is classified as ErrUpdatePipNetwork.
func TestPipExecutor_NetworkStderr(t *testing.T) {
	setupFixturePipForExecutor(t)
	t.Setenv("FIXTURE_PIP_EXIT", "1")
	t.Setenv("FIXTURE_PIP_STDERR", "Read timed out. (read timeout=15)")

	e := NewPipUpdateExecutor()
	err := e.Update(context.Background(), "numpy", "1.25.0", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUpdatePipNetwork) {
		t.Errorf("errors.Is(err, ErrUpdatePipNetwork) = false; err = %v", err)
	}
}

// TestPipExecutor_PermissionStderr verifies that stderr containing "Permission denied"
// is classified as ErrUpdatePipPermission.
func TestPipExecutor_PermissionStderr(t *testing.T) {
	setupFixturePipForExecutor(t)
	t.Setenv("FIXTURE_PIP_EXIT", "1")
	t.Setenv("FIXTURE_PIP_STDERR", "ERROR: Could not install packages due to an OSError: [Errno 13] Permission denied: '/usr/local/lib/python3.11'")

	e := NewPipUpdateExecutor()
	err := e.Update(context.Background(), "setuptools", "68.2.2", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrUpdatePipPermission) {
		t.Errorf("errors.Is(err, ErrUpdatePipPermission) = false; err = %v", err)
	}
}

// TestPipExecutor_PreReleaseFlag verifies that meta["preRelease"]=true causes
// --pre to be included in the pip install arguments.
// Strategy: the fixture script writes its received args to a temp file when
// FIXTURE_ARGS_FILE is set; the test reads and asserts on that file.
func TestPipExecutor_PreReleaseFlag(t *testing.T) {
	// Build a custom fixture that captures args to a temp file.
	argsFile := filepath.Join(t.TempDir(), "captured-args.txt")
	scriptPath := filepath.Join(t.TempDir(), "pip3")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"install\" ]; then\n" +
		"  echo \"$@\" >> \"" + argsFile + "\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 2\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write arg-capture script: %v", err)
	}

	origBinary := pipBinary
	origLookPath := pipLookPath
	pipBinary = scriptPath
	pipLookPath = func(string) (string, error) { return scriptPath, nil }
	t.Cleanup(func() {
		pipBinary = origBinary
		pipLookPath = origLookPath
	})

	e := NewPipUpdateExecutor()
	meta := map[string]any{"preRelease": true}
	err := e.Update(context.Background(), "torch", "2.0.0rc2", meta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	captured, readErr := os.ReadFile(argsFile)
	if readErr != nil {
		t.Fatalf("args file not written: %v", readErr)
	}
	argsStr := string(captured)
	if argsStr == "" {
		t.Fatal("args file is empty")
	}
	// --pre must appear in the captured install args.
	found := false
	for _, tok := range splitShellWords(argsStr) {
		if tok == "--pre" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("--pre not found in captured args: %q", argsStr)
	}
}

// TestPipExecutor_CtxCancel verifies that context cancellation kills the
// subprocess before it completes.
func TestPipExecutor_CtxCancel(t *testing.T) {
	// Build a fixture that sleeps for 60s on install — long enough to guarantee
	// the context cancel fires first.
	scriptPath := filepath.Join(t.TempDir(), "pip3")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"install\" ]; then sleep 60; exit 0; fi\n" +
		"exit 2\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write sleep script: %v", err)
	}

	origBinary := pipBinary
	origLookPath := pipLookPath
	pipBinary = scriptPath
	pipLookPath = func(string) (string, error) { return scriptPath, nil }
	t.Cleanup(func() {
		pipBinary = origBinary
		pipLookPath = origLookPath
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	e := NewPipUpdateExecutor()
	start := time.Now()
	err := e.Update(ctx, "torch", "2.0.0", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error after context cancel, got nil")
	}
	// Should complete well under the 60s sleep — allow 3s for CI overhead.
	if elapsed > 3*time.Second {
		t.Errorf("subprocess not killed promptly: elapsed %v", elapsed)
	}
}

// splitShellWords splits a whitespace-separated string into tokens.
// Sufficient for the arg-capture assertions above; not a full shell parser.
func splitShellWords(s string) []string {
	var tokens []string
	inWord := false
	start := 0
	for i, ch := range s {
		switch {
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			if inWord {
				tokens = append(tokens, s[start:i])
				inWord = false
			}
		default:
			if !inWord {
				start = i
				inWord = true
			}
		}
	}
	if inWord {
		tokens = append(tokens, s[start:])
	}
	return tokens
}
