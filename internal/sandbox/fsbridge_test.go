package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDockerScript is a fake `docker` binary used in WriteFile tests.
// It logs all argv to $DOCKER_LOG and, when invoked with a `tee` sub-command,
// captures stdin to $DOCKER_STDIN.
// `realpath -e -- <path>` echoes the path back (simulates existing path).
// `mkdir` exits 0 silently.
const fakeDockerScript = `#!/bin/sh
{
  echo CALL
  i=0
  for arg in "$@"; do
    echo "ARG[$i]=$arg"
    i=$((i + 1))
  done
} >> "$DOCKER_LOG"

# realpath: echo the last arg back as the resolved path
for j in $(seq 0 $#); do
  eval "a=\${$j}"
  if [ "$a" = "realpath" ]; then
    # print the last argument (the path)
    eval "echo \"\${$#}\""
    exit 0
  fi
done

# tee: capture stdin
for arg in "$@"; do
  if [ "$arg" = "tee" ]; then
    if [ -n "$DOCKER_STDIN" ]; then
      cat > "$DOCKER_STDIN"
    else
      cat > /dev/null
    fi
    exit 0
  fi
done

exit 0
`

// installFakeDocker writes the fake docker script to tmp, prepends tmp to PATH,
// and sets DOCKER_LOG and optionally DOCKER_STDIN env vars for the test.
func installFakeDocker(t *testing.T, tmp, logPath, stdinPath string) {
	t.Helper()
	dockerPath := filepath.Join(tmp, "docker")
	if err := os.WriteFile(dockerPath, []byte(fakeDockerScript), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_LOG", logPath)
	if stdinPath != "" {
		t.Setenv("DOCKER_STDIN", stdinPath)
	}
}

// TestFsBridgeWriteFileDoesNotInvokeShell asserts that WriteFile passes the
// resolved path as a discrete argv entry to `tee -- <path>` and does NOT
// build a shell command string (no sh / -c / shell metachar expansion).
func TestFsBridgeWriteFileDoesNotInvokeShell(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "docker.log")
	stdinPath := filepath.Join(tmp, "stdin.txt")

	installFakeDocker(t, tmp, logPath, stdinPath)

	bridge := NewFsBridge("container-id", "/workspace")
	maliciousPath := `nested/evil$(touch /tmp/goclaw-fsbridge-pwned);name.txt`
	content := "safe content"

	if err := bridge.WriteFile(context.Background(), maliciousPath, content, false); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read docker log: %v", err)
	}
	log := string(logBytes)

	// Must NOT have invoked sh or built a shell string.
	if strings.Contains(log, "=sh") || strings.Contains(log, "=-c") || strings.Contains(log, "cat >") {
		t.Fatalf("WriteFile invoked shell command path; log:\n%s", log)
	}
	// Must have called tee.
	if !strings.Contains(log, "=tee") {
		t.Fatalf("expected write command to use tee without shell; log:\n%s", log)
	}
	// Must have included -- to terminate option parsing.
	if !strings.Contains(log, "=--") {
		t.Fatalf("expected tee delimiter (--) before filename; log:\n%s", log)
	}
	// The malicious path must appear verbatim as a single argv entry (not split/executed).
	resolved := "/workspace/" + maliciousPath
	if !strings.Contains(log, "="+resolved) {
		t.Fatalf("expected malicious filename to remain one argv entry; log:\n%s", log)
	}

	stdinBytes, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatalf("read captured stdin: %v", err)
	}
	if string(stdinBytes) != content {
		t.Fatalf("stdin content = %q, want %q", string(stdinBytes), content)
	}
}

// TestFsBridgeWriteFileAppendUsesTeeAppendArg asserts that append mode passes
// -a before -- in the tee argv, i.e. `tee -a -- <path>`.
func TestFsBridgeWriteFileAppendUsesTeeAppendArg(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "docker.log")

	installFakeDocker(t, tmp, logPath, "")

	bridge := NewFsBridge("container-id", "/workspace")
	if err := bridge.WriteFile(context.Background(), "append.txt", "more", true); err != nil {
		t.Fatalf("WriteFile append returned error: %v", err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read docker log: %v", err)
	}
	log := string(logBytes)

	// Must have: tee, then -a, then --.
	if !strings.Contains(log, "=tee") {
		t.Fatalf("expected tee in argv; log:\n%s", log)
	}
	if !strings.Contains(log, "=-a") {
		t.Fatalf("expected -a flag for append mode; log:\n%s", log)
	}
	if !strings.Contains(log, "=--") {
		t.Fatalf("expected -- delimiter in argv; log:\n%s", log)
	}
}

// TestFsBridgeWriteTeeArgs_Overwrite checks the tee args slice for overwrite mode.
func TestFsBridgeWriteTeeArgs_Overwrite(t *testing.T) {
	args := fsBridgeWriteTeeArgs("/workspace/file.txt", false)
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d: %v", len(args), args)
	}
	if args[0] != "tee" {
		t.Errorf("args[0] = %q, want tee", args[0])
	}
	if args[1] != "--" {
		t.Errorf("args[1] = %q, want --", args[1])
	}
	if args[2] != "/workspace/file.txt" {
		t.Errorf("args[2] = %q, want /workspace/file.txt", args[2])
	}
	// Must not contain -a in overwrite mode.
	for _, a := range args {
		if a == "-a" {
			t.Errorf("overwrite mode must not contain -a: %v", args)
		}
	}
}

// TestFsBridgeWriteTeeArgs_Append checks the tee args slice for append mode.
func TestFsBridgeWriteTeeArgs_Append(t *testing.T) {
	args := fsBridgeWriteTeeArgs("/workspace/file.txt", true)
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}
	if args[0] != "tee" {
		t.Errorf("args[0] = %q, want tee", args[0])
	}
	if args[1] != "-a" {
		t.Errorf("args[1] = %q, want -a", args[1])
	}
	if args[2] != "--" {
		t.Errorf("args[2] = %q, want --", args[2])
	}
	if args[3] != "/workspace/file.txt" {
		t.Errorf("args[3] = %q, want /workspace/file.txt", args[3])
	}
}
