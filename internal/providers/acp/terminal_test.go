package acp

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- cappedBuffer tests ---

func TestCappedBuffer_BasicWrite(t *testing.T) {
	cb := &cappedBuffer{max: 20}
	n, err := cb.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected n=5, got %d", n)
	}
	if cb.String() != "hello" {
		t.Errorf("expected 'hello', got %q", cb.String())
	}
}

func TestCappedBuffer_ExactMax(t *testing.T) {
	cb := &cappedBuffer{max: 5}
	cb.Write([]byte("hello")) // exactly 5
	if cb.String() != "hello" {
		t.Errorf("expected 'hello', got %q", cb.String())
	}
}

func TestCappedBuffer_SmallOverflow(t *testing.T) {
	// buf has 4 bytes, new 3-byte write causes overflow=2
	// overflow < buf.Len() → truncate from front: keep buf[2:] + new data
	cb := &cappedBuffer{max: 5}
	cb.Write([]byte("abcd")) // buf="abcd" (4 bytes)
	cb.Write([]byte("xyz"))  // overflow = 4+3-5 = 2 < 4 → keep buf[2:]="cd", append "xyz"
	got := cb.String()
	if len(got) > 5 {
		t.Errorf("expected len ≤ 5, got %d: %q", len(got), got)
	}
	// Tail should end with the new data
	if !strings.HasSuffix(got, "xyz") {
		t.Errorf("expected suffix 'xyz', got %q", got)
	}
}

func TestCappedBuffer_LargeOverflow(t *testing.T) {
	// Write that is larger than max alone
	cb := &cappedBuffer{max: 5}
	cb.Write([]byte("hello world 123456789")) // 21 bytes, max 5
	got := cb.String()
	if len(got) > 5 {
		t.Errorf("expected len ≤ 5, got %d: %q", len(got), got)
	}
	// Should contain the last 5 bytes of "hello world 123456789" = "56789"
	if got != "56789" {
		t.Errorf("expected '56789', got %q", got)
	}
}

func TestCappedBuffer_OverflowExceedsExistingBuf(t *testing.T) {
	// overflow >= buf.Len() branch: reset buf, truncate p to last max bytes
	cb := &cappedBuffer{max: 3}
	cb.Write([]byte("ab"))  // buf="ab" (2)
	cb.Write([]byte("xyz")) // overflow = 2+3-3 = 2 >= buf.Len(2) → reset buf, write "xyz"
	got := cb.String()
	if got != "xyz" {
		t.Errorf("expected 'xyz', got %q", got)
	}
}

func TestCappedBuffer_MultipleSmallWrites(t *testing.T) {
	cb := &cappedBuffer{max: 10}
	for range 5 {
		cb.Write([]byte("ab"))
	}
	got := cb.String()
	if len(got) > 10 {
		t.Errorf("expected len ≤ 10, got %d", len(got))
	}
}

func TestCappedBuffer_ZeroMax(t *testing.T) {
	cb := &cappedBuffer{max: 0}
	// cappedBuffer with max=0 truncates all data: p becomes p[len(p)-0:] = []
	// bytes.Buffer.Write([]) returns n=0
	_, err := cb.Write([]byte("data"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if cb.String() != "" {
		t.Errorf("expected empty buffer with max=0, got %q", cb.String())
	}
}

func TestCappedBuffer_ConcurrentWrites(t *testing.T) {
	cb := &cappedBuffer{max: 100}
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			cb.Write([]byte("data"))
		})
	}
	wg.Wait()
	s := cb.String()
	if len(s) > 100 {
		t.Errorf("expected len ≤ 100 after concurrent writes, got %d", len(s))
	}
}

// --- allowedTerminalBinaries ---

func TestAllowedTerminalBinaries_Contains(t *testing.T) {
	allowed := []string{
		"sh", "bash", "zsh", "node", "python3", "go", "git",
		"ls", "cat", "grep", "rg", "find", "curl", "docker",
		"npm", "pip", "jq",
	}
	for _, bin := range allowed {
		if !allowedTerminalBinaries[bin] {
			t.Errorf("expected %q to be in allowed binaries", bin)
		}
	}
}

func TestAllowedTerminalBinaries_NotContains(t *testing.T) {
	denied := []string{"rm", "dd", "mkfs", "fsck", "shutdown", "reboot", "nc", "netcat"}
	for _, bin := range denied {
		if allowedTerminalBinaries[bin] {
			t.Errorf("expected %q to NOT be in allowed binaries", bin)
		}
	}
}

// --- terminalOutput / releaseTerminal / killTerminal via ToolBridge ---

func makeRunningTerminal(t *testing.T, id string) *Terminal {
	t.Helper()
	term := &Terminal{
		id:     id,
		output: &cappedBuffer{max: 1024},
		exited: make(chan struct{}),
		cancel: func() {},
	}
	return term
}

func makeExitedTerminal(t *testing.T, id string, code int) *Terminal {
	t.Helper()
	term := &Terminal{
		id:       id,
		output:   &cappedBuffer{max: 1024},
		exited:   make(chan struct{}),
		exitCode: code,
		cancel:   func() {},
	}
	close(term.exited) // pre-exited
	return term
}

func TestTerminalOutput_Running(t *testing.T) {
	tb, _ := newTestBridge(t)
	term := makeRunningTerminal(t, "t1")
	term.output.Write([]byte("some output"))
	tb.terminals.Store("t1", term)

	resp, err := tb.terminalOutput(TerminalOutputRequest{TerminalID: "t1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Output != "some output" {
		t.Errorf("expected 'some output', got %q", resp.Output)
	}
	if resp.ExitStatus != nil {
		t.Error("expected nil ExitStatus for running terminal")
	}
}

func TestTerminalOutput_Exited(t *testing.T) {
	tb, _ := newTestBridge(t)
	term := makeExitedTerminal(t, "t2", 42)
	term.output.Write([]byte("done"))
	tb.terminals.Store("t2", term)

	resp, err := tb.terminalOutput(TerminalOutputRequest{TerminalID: "t2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ExitStatus == nil {
		t.Fatal("expected non-nil ExitStatus for exited terminal")
	}
	if *resp.ExitStatus != 42 {
		t.Errorf("expected exit code 42, got %d", *resp.ExitStatus)
	}
}

func TestTerminalOutput_NotFound(t *testing.T) {
	tb, _ := newTestBridge(t)
	_, err := tb.terminalOutput(TerminalOutputRequest{TerminalID: "missing"})
	if err == nil {
		t.Error("expected error for missing terminal")
	}
}

func TestReleaseTerminal_ExistingCancels(t *testing.T) {
	tb, _ := newTestBridge(t)
	cancelled := false
	var mu sync.Mutex
	term := makeRunningTerminal(t, "t3")
	term.cancel = func() {
		mu.Lock()
		cancelled = true
		mu.Unlock()
	}
	tb.terminals.Store("t3", term)

	resp, err := tb.releaseTerminal(ReleaseTerminalRequest{TerminalID: "t3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	mu.Lock()
	wasCancelled := cancelled
	mu.Unlock()

	if !wasCancelled {
		t.Error("expected cancel to be called on release")
	}

	// Terminal should be removed
	if _, ok := tb.terminals.Load("t3"); ok {
		t.Error("expected terminal to be removed after release")
	}
}

func TestReleaseTerminal_NotFound(t *testing.T) {
	tb, _ := newTestBridge(t)
	resp, err := tb.releaseTerminal(ReleaseTerminalRequest{TerminalID: "none"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response even for missing terminal")
	}
}

func TestKillTerminal_Cancels(t *testing.T) {
	tb, _ := newTestBridge(t)
	killed := false
	var mu sync.Mutex
	term := makeRunningTerminal(t, "tk1")
	term.cancel = func() {
		mu.Lock()
		killed = true
		mu.Unlock()
	}
	tb.terminals.Store("tk1", term)

	resp, err := tb.killTerminal(KillTerminalRequest{TerminalID: "tk1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	mu.Lock()
	wasKilled := killed
	mu.Unlock()

	if !wasKilled {
		t.Error("expected cancel to be called on kill")
	}
}

func TestKillTerminal_NotFound(t *testing.T) {
	tb, _ := newTestBridge(t)
	_, err := tb.killTerminal(KillTerminalRequest{TerminalID: "missing"})
	if err == nil {
		t.Error("expected error for missing terminal")
	}
}

func TestWaitForExit_Exited(t *testing.T) {
	tb, _ := newTestBridge(t)
	term := makeExitedTerminal(t, "tw1", 0)
	tb.terminals.Store("tw1", term)

	resp, err := tb.waitForExit(context.Background(), WaitForTerminalExitRequest{TerminalID: "tw1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ExitStatus != 0 {
		t.Errorf("expected exit 0, got %d", resp.ExitStatus)
	}
}

func TestWaitForExit_ContextCancel(t *testing.T) {
	tb, _ := newTestBridge(t)
	term := makeRunningTerminal(t, "tw2") // never exits
	tb.terminals.Store("tw2", term)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tb.waitForExit(ctx, WaitForTerminalExitRequest{TerminalID: "tw2"})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestWaitForExit_NotFound(t *testing.T) {
	tb, _ := newTestBridge(t)
	_, err := tb.waitForExit(context.Background(), WaitForTerminalExitRequest{TerminalID: "missing"})
	if err == nil {
		t.Error("expected error for missing terminal")
	}
}

// --- createTerminal security: deny patterns and binary allowlist ---

func TestCreateTerminal_AllowlistDenied(t *testing.T) {
	tb, _ := newTestBridge(t)
	_, err := tb.createTerminal(CreateTerminalRequest{Command: "rm", Args: []string{"-rf", "/"}})
	if err == nil {
		t.Fatal("expected error for disallowed binary")
	}
	if !strings.Contains(err.Error(), "not in allowed binary list") {
		t.Errorf("expected allowlist error, got %q", err.Error())
	}
}

func TestCreateTerminal_DenyPattern(t *testing.T) {
	pat := regexp.MustCompile(`rm -rf`)
	tb, _ := newTestBridge(t, WithDenyPatterns([]*regexp.Regexp{pat}))

	// "bash" is allowed but the combined command matches deny pattern
	_, err := tb.createTerminal(CreateTerminalRequest{
		Command: "bash",
		Args:    []string{"-c", "rm -rf /tmp/test"},
	})
	if err == nil {
		t.Fatal("expected deny pattern to block command")
	}
	if !strings.Contains(err.Error(), "denied by safety policy") {
		t.Errorf("expected safety policy error, got %q", err.Error())
	}
}

func TestCreateTerminal_InvalidCwd(t *testing.T) {
	tb, _ := newTestBridge(t)
	// Path traversal in cwd should be denied
	_, err := tb.createTerminal(CreateTerminalRequest{
		Command: "ls",
		Cwd:     "../../etc",
	})
	if err == nil {
		t.Fatal("expected error for cwd outside workspace")
	}
}
