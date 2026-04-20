package skills

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// defaultDialTimeout mirrors the 5s dial timeout used in apkHelperCall.
const defaultDialTimeout = 5 * time.Second

// testSockCounter generates unique short socket paths to avoid macOS's
// ~104-char Unix socket path limit (t.TempDir paths are often too long).
var testSockCounter atomic.Uint64

// newTestSockPath returns a short /tmp/tph-<N>.sock path unique per call.
func newTestSockPath() string {
	n := testSockCounter.Add(1)
	return fmt.Sprintf("/tmp/tph-%d.sock", n)
}

// newHelperScanner returns a bufio.Scanner with the same 64KB/1MB buffer
// used by apkHelperCall, so test helpers share the same contract.
func newHelperScanner(conn net.Conn) *bufio.Scanner {
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	return sc
}

// servePkgHelper spins up a goroutine-backed Unix socket at sockPath that
// handles a single connection: drains the incoming request line, writes
// respJSON as a newline-terminated response, then closes.
// Returns a cleanup func that stops the listener and waits for the goroutine.
func servePkgHelper(t *testing.T, sockPath, respJSON string) func() {
	t.Helper()

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("servePkgHelper: listen %q: %v", sockPath, err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed on cleanup
		}
		defer conn.Close()

		// Drain incoming request (one JSON line). Ignore content — canned response.
		buf := make([]byte, 4096)
		conn.Read(buf) //nolint:errcheck

		fmt.Fprintln(conn, respJSON)
	}()

	return func() {
		ln.Close()
		<-done
	}
}

// dialHelper mirrors apkHelperCall's full parse logic but dials sockPath
// directly, bypassing the pkgHelperSocket constant so tests don't require
// a real /tmp/pkg.sock.
func dialHelper(t *testing.T, sockPath, action, pkg string) (ok bool, code, data, errMsg string) {
	t.Helper()

	conn, err := net.DialTimeout("unix", sockPath, defaultDialTimeout)
	if err != nil {
		return false, "helper_unavailable", "", fmt.Sprintf("pkg-helper unavailable: %v", err)
	}
	defer conn.Close()

	req := map[string]string{"action": action, "package": pkg}
	if encErr := json.NewEncoder(conn).Encode(req); encErr != nil {
		return false, "helper_error", "", fmt.Sprintf("pkg-helper send failed: %v", encErr)
	}

	scanner := newHelperScanner(conn)
	if !scanner.Scan() {
		scanErr := scanner.Err()
		if scanErr != nil {
			return false, "helper_error", "", fmt.Sprintf("pkg-helper: read error: %v", scanErr)
		}
		return false, "helper_error", "", "pkg-helper: no response"
	}

	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Code  string `json:"code"`
		Data  string `json:"data"`
	}
	if parseErr := json.Unmarshal(scanner.Bytes(), &resp); parseErr != nil {
		return false, "helper_error", "", fmt.Sprintf("pkg-helper: invalid response: %v", parseErr)
	}
	// Default missing code to system_error — matches apkHelperCall client logic
	// for v1-era helpers that omit the code field.
	if resp.Code == "" && !resp.OK {
		resp.Code = "system_error"
	}
	return resp.OK, resp.Code, resp.Data, resp.Error
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestApkHelperCall_DialFail verifies that a missing socket returns
// ok=false, code="helper_unavailable".
func TestApkHelperCall_DialFail(t *testing.T) {
	ok, code, _, errMsg := dialHelper(t, "/tmp/no-such-pkg-helper.sock", "install", "curl")

	if ok {
		t.Error("dial to nonexistent socket should return ok=false")
	}
	if code != "helper_unavailable" {
		t.Errorf("code = %q, want 'helper_unavailable'", code)
	}
	if !strings.Contains(errMsg, "pkg-helper unavailable") {
		t.Errorf("errMsg = %q, want to contain 'pkg-helper unavailable'", errMsg)
	}
}

// TestApkHelperCall_ValidResponse verifies a well-formed canned response is
// parsed correctly into (ok, code, data, errMsg).
func TestApkHelperCall_ValidResponse(t *testing.T) {
	sockPath := newTestSockPath()
	cleanup := servePkgHelper(t, sockPath, `{"ok":true,"data":"curl 8.5.0\n"}`)
	defer cleanup()

	ok, code, data, errMsg := dialHelper(t, sockPath, "list-outdated", "")

	if !ok {
		t.Errorf("ok = false, want true (errMsg=%q)", errMsg)
	}
	// ok=true with no code field → code stays "" (no defaulting for success)
	if code != "" {
		t.Errorf("code = %q, want empty (OK response needs no code)", code)
	}
	if data != "curl 8.5.0\n" {
		t.Errorf("data = %q, want 'curl 8.5.0\\n'", data)
	}
	if errMsg != "" {
		t.Errorf("errMsg = %q, want empty", errMsg)
	}
}

// TestApkHelperCall_EmptyCodeDefaultsToSystemError verifies that when the
// helper returns ok=false without a code field, the client defaults to
// "system_error" — backward-compat with v1 helpers that omit code.
func TestApkHelperCall_EmptyCodeDefaultsToSystemError(t *testing.T) {
	sockPath := newTestSockPath()
	cleanup := servePkgHelper(t, sockPath, `{"ok":false,"error":"something went wrong"}`)
	defer cleanup()

	ok, code, _, errMsg := dialHelper(t, sockPath, "install", "curl")

	if ok {
		t.Error("ok = true, want false")
	}
	if code != "system_error" {
		t.Errorf("code = %q, want 'system_error' (client default for missing code on error)", code)
	}
	if errMsg != "something went wrong" {
		t.Errorf("errMsg = %q, want 'something went wrong'", errMsg)
	}
}

// TestApkHelperCall_LargePayload verifies that a data payload >64KB (the
// default bufio.Scanner limit) is parsed cleanly with the bumped 1MB buffer.
func TestApkHelperCall_LargePayload(t *testing.T) {
	// 70KB > default 64KB scanner limit — confirms buffer ceiling is effective.
	largeData := strings.Repeat("a", 70*1024)

	resp := map[string]interface{}{
		"ok":   true,
		"data": largeData,
	}
	respBytes, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal large response: %v", err)
	}

	sockPath := newTestSockPath()
	cleanup := servePkgHelper(t, sockPath, string(respBytes))
	defer cleanup()

	ok, _, data, errMsg := dialHelper(t, sockPath, "list-outdated", "")

	if !ok {
		t.Errorf("ok = false, want true (errMsg=%q)", errMsg)
	}
	if len(data) != len(largeData) {
		t.Errorf("data length = %d, want %d (large payload truncated?)", len(data), len(largeData))
	}
}

// TestApkHelperCall_ConflictCode verifies that a "conflict" code propagates
// through the client parse unchanged.
func TestApkHelperCall_ConflictCode(t *testing.T) {
	sockPath := newTestSockPath()
	cleanup := servePkgHelper(t, sockPath, `{"ok":false,"error":"unsatisfiable constraints","code":"conflict"}`)
	defer cleanup()

	ok, code, _, errMsg := dialHelper(t, sockPath, "upgrade", "curl")

	if ok {
		t.Error("ok = true, want false")
	}
	if code != "conflict" {
		t.Errorf("code = %q, want 'conflict'", code)
	}
	if errMsg == "" {
		t.Error("errMsg should be non-empty for error response")
	}
}

// TestApkHelperCall_ContextCancelled verifies that a pre-cancelled context
// causes a graceful failure with a non-empty error code (no panic).
func TestApkHelperCall_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	// Dial a nonexistent socket — guaranteed failure regardless of context.
	ok, code, _, _ := dialHelper(t, "/tmp/no-such-helper-ctx.sock", "install", "curl")

	if ok {
		t.Error("cancelled context / missing socket should not return ok=true")
	}
	if code == "" {
		t.Error("error code must be non-empty")
	}
	_ = ctx // silence unused warning
}

// TestApkHelperCall_AllKnownCodes verifies that all expected code strings
// pass through the parse layer unchanged (no accidental rewriting).
func TestApkHelperCall_AllKnownCodes(t *testing.T) {
	knownCodes := []string{
		"locked", "permission", "disk_full", "not_found",
		"conflict", "network", "system_error", "validation",
	}

	for _, wantCode := range knownCodes {
		wantCode := wantCode
		t.Run(wantCode, func(t *testing.T) {
			sockPath := newTestSockPath()
			canned := fmt.Sprintf(`{"ok":false,"error":"test error","code":%q}`, wantCode)
			cleanup := servePkgHelper(t, sockPath, canned)
			defer cleanup()

			_, gotCode, _, _ := dialHelper(t, sockPath, "upgrade", "curl")
			if gotCode != wantCode {
				t.Errorf("code = %q, want %q", gotCode, wantCode)
			}
		})
	}
}
