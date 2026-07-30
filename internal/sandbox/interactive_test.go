package sandbox

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// lineWriter — the shared line splitter behind WithStdoutLine/WithStderrLine
// ---------------------------------------------------------------------------

func TestLineWriter_HoldsPartialLineUntilNewline(t *testing.T) {
	var got []string
	w := &lineWriter{cb: func(s string) { got = append(got, s) }}

	if _, err := w.Write([]byte("par")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := w.Write([]byte("tial")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("partial line emitted early: %q", got)
	}

	w.Write([]byte(" line\n"))
	if !slices.Equal(got, []string{"partial line"}) {
		t.Fatalf("got %q, want [\"partial line\"]", got)
	}
}

func TestLineWriter_SplitsMultiLineWrite(t *testing.T) {
	var got []string
	w := &lineWriter{cb: func(s string) { got = append(got, s) }}

	// Three complete lines plus a partial tail in a single Write.
	w.Write([]byte("one\ntwo\nthree\ntai"))
	if !slices.Equal(got, []string{"one", "two", "three"}) {
		t.Fatalf("got %q, want [one two three]", got)
	}

	// Empty lines are preserved (a blank line is a line).
	w.Write([]byte("l\n\n"))
	if !slices.Equal(got, []string{"one", "two", "three", "tail", ""}) {
		t.Fatalf("got %q, want [one two three tail \"\"]", got)
	}
}

func TestLineWriter_FlushSurfacesUnterminatedLine(t *testing.T) {
	var got []string
	w := &lineWriter{cb: func(s string) { got = append(got, s) }}

	w.Write([]byte("done\nno trailing newline"))
	if !slices.Equal(got, []string{"done"}) {
		t.Fatalf("got %q before flush, want [done]", got)
	}

	w.flush()
	if !slices.Equal(got, []string{"done", "no trailing newline"}) {
		t.Fatalf("got %q after flush, want [done, no trailing newline]", got)
	}

	// Flushing again must not re-emit the line (Exec defers flush while the
	// session path also flushes on reap).
	w.flush()
	if len(got) != 2 {
		t.Fatalf("second flush re-emitted: %q", got)
	}
}

func TestLineWriter_StripsCR(t *testing.T) {
	var got []string
	w := &lineWriter{cb: func(s string) { got = append(got, s) }}

	w.Write([]byte("crlf one\r\ncrlf two\r\nmid\rline\r\n"))
	w.Write([]byte("unterminated\r"))
	w.flush()

	want := []string{"crlf one", "crlf two", "mid\rline", "unterminated"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLineWriter_NilCallbackIsSafe(t *testing.T) {
	w := &lineWriter{}
	if _, err := w.Write([]byte("a\nb")); err != nil {
		t.Fatalf("write: %v", err)
	}
	w.flush()
	w.flushIfSet()
	var nilW *lineWriter
	nilW.flushIfSet() // must not panic
}

func TestTeeLines_CapturesAndStreams(t *testing.T) {
	buf := &limitedBuffer{max: 1 << 10}
	var got []string
	wr, lw := teeLines(buf, func(s string) { got = append(got, s) })
	if lw == nil {
		t.Fatal("expected a lineWriter when a callback is supplied")
	}
	wr.Write([]byte("x\ny"))
	lw.flush()

	if buf.String() != "x\ny" {
		t.Errorf("full capture = %q, want %q", buf.String(), "x\ny")
	}
	if !slices.Equal(got, []string{"x", "y"}) {
		t.Errorf("streamed %q, want [x y]", got)
	}

	// No callback → the buffer itself is the writer and there is nothing to flush.
	wr2, lw2 := teeLines(buf, nil)
	if lw2 != nil {
		t.Error("expected nil lineWriter with no callback")
	}
	if wr2 != any(buf) {
		t.Error("expected the writer to be the buffer itself with no callback")
	}
}

// ---------------------------------------------------------------------------
// argv construction — Exec and ExecInteractive must differ only by -i
// ---------------------------------------------------------------------------

func TestBuildDockerExecArgs_Shape(t *testing.T) {
	command := []string{"claude", "--input-format", "stream-json"}
	env := map[string]string{"B_TOKEN": "two", "A_TOKEN": "one"}

	got := buildDockerExecArgs("c0ffee", command, "/workspace/sub", env, false)
	want := []string{
		"exec",
		"-e", "A_TOKEN=one",
		"-e", "B_TOKEN=two",
		"-w", "/workspace/sub",
		"c0ffee",
		"claude", "--input-format", "stream-json",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("non-interactive argv:\n got %q\nwant %q", got, want)
	}
}

func TestBuildDockerExecArgs_InteractiveAddsOnlyStdinFlag(t *testing.T) {
	command := []string{"claude", "--output-format", "stream-json"}
	env := map[string]string{"ANTHROPIC_API_KEY": "sk-test", "HOME": "/tmp"}

	nonInteractive := buildDockerExecArgs("c0ffee", command, "/workspace", env, false)
	interactive := buildDockerExecArgs("c0ffee", command, "/workspace", env, true)

	if !slices.Contains(interactive, "-i") {
		t.Fatalf("interactive argv missing -i: %q", interactive)
	}
	if slices.Contains(nonInteractive, "-i") {
		t.Fatalf("non-interactive argv must not contain -i: %q", nonInteractive)
	}

	// The ONLY difference must be "-i" inserted right after "exec".
	want := slices.Insert(slices.Clone(nonInteractive), 1, "-i")
	if !slices.Equal(interactive, want) {
		t.Fatalf("interactive argv differs beyond -i:\n got %q\nwant %q", interactive, want)
	}
}

func TestBuildDockerExecArgs_OmitsEmptyWorkdirAndEnv(t *testing.T) {
	got := buildDockerExecArgs("abc", []string{"sh", "-lc", "echo hi"}, "", nil, true)
	want := []string{"exec", "-i", "abc", "sh", "-lc", "echo hi"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildDockerExecArgs_EnvValuesPassedVerbatim(t *testing.T) {
	// Values reach docker as separate argv entries, so "=" / spaces / quotes must
	// survive without any shell interpretation.
	env := map[string]string{"GIT_CONFIG_VALUE_0": `!f() { echo password=$GH_TOKEN; }; f`}
	got := buildDockerExecArgs("abc", []string{"true"}, "", env, true)
	want := []string{"exec", "-i", "-e", `GIT_CONFIG_VALUE_0=!f() { echo password=$GH_TOKEN; }; f`, "abc", "true"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNormalizeMaxOutput(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{0, 1 << 20}, {-5, 1 << 20}, {64, 64}} {
		if got := normalizeMaxOutput(tc.in); got != tc.want {
			t.Errorf("normalizeMaxOutput(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// session state machine — exercised against a LOCAL process (no Docker daemon)
// via the startInteractiveSession seam.
// ---------------------------------------------------------------------------

// lineCollector records streamed lines and lets a test block for the next one.
type lineCollector struct {
	mu    sync.Mutex
	lines []string
	ch    chan string
}

func newLineCollector() *lineCollector {
	return &lineCollector{ch: make(chan string, 256)}
}

func (c *lineCollector) add(s string) {
	c.mu.Lock()
	c.lines = append(c.lines, s)
	c.mu.Unlock()
	select {
	case c.ch <- s:
	default: // never block the process's output pump in a test
	}
}

func (c *lineCollector) next(t *testing.T) string {
	t.Helper()
	select {
	case s := <-c.ch:
		return s
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for a streamed line")
		return ""
	}
}

func (c *lineCollector) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.lines)
}

// startTestSession starts a real local process through the same code path
// ExecInteractive uses, so the state machine is tested without Docker.
func startTestSession(t *testing.T, ctx context.Context, o ExecOpts, maxOut int, argv ...string) *interactiveSession {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("interactive session tests use POSIX helper binaries")
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		t.Skipf("%s not available: %v", argv[0], err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	s, err := startInteractiveSession(cmd, cancel, maxOut, o)
	if err != nil {
		cancel()
		t.Fatalf("startInteractiveSession: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestInteractiveSession_WriteReadRoundTrip(t *testing.T) {
	out := newLineCollector()
	s := startTestSession(t, context.Background(), ExecOpts{StdoutLine: out.add}, 0, "cat")

	if err := s.WriteLine("hello"); err != nil {
		t.Fatalf("WriteLine: %v", err)
	}
	if got := out.next(t); got != "hello" {
		t.Fatalf("echoed %q, want %q", got, "hello")
	}

	// An explicit trailing newline must not be doubled.
	if err := s.WriteLine("second\n"); err != nil {
		t.Fatalf("WriteLine: %v", err)
	}
	if got := out.next(t); got != "second" {
		t.Fatalf("echoed %q, want %q", got, "second")
	}

	// Still alive: the process outlives a single exchange (the whole point).
	select {
	case <-s.Done():
		t.Fatal("session exited after a round trip")
	default:
	}
	if s.ExitCode() != 0 {
		t.Errorf("ExitCode() before exit = %d, want 0", s.ExitCode())
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-s.Done():
	default:
		t.Fatal("Done not closed after Close returned")
	}

	res, err := s.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if res.Stdout != "hello\nsecond\n" {
		t.Errorf("captured stdout = %q, want %q", res.Stdout, "hello\nsecond\n")
	}
}

func TestInteractiveSession_WriteLineAfterCloseFails(t *testing.T) {
	s := startTestSession(t, context.Background(), ExecOpts{}, 0, "cat")

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err := s.WriteLine("too late")
	if !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("WriteLine after Close = %v, want ErrSessionClosed", err)
	}
}

func TestInteractiveSession_DoubleCloseIsSafe(t *testing.T) {
	s := startTestSession(t, context.Background(), ExecOpts{}, 0, "cat")

	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	// Close concurrent with Wait must not deadlock or double-close the pipe.
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Close()
			if _, err := s.Wait(); err != nil {
				t.Errorf("Wait: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestInteractiveSession_DoneAndExitCodeOnNaturalExit(t *testing.T) {
	out, errOut := newLineCollector(), newLineCollector()
	s := startTestSession(t, context.Background(),
		ExecOpts{StdoutLine: out.add, StderrLine: errOut.add}, 0,
		"sh", "-c", "echo out-line; echo err-line 1>&2; exit 3")

	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("Done not closed after the process exited")
	}

	if s.ExitCode() != 3 {
		t.Errorf("ExitCode() = %d, want 3", s.ExitCode())
	}
	res, err := s.Wait()
	if err != nil {
		t.Fatalf("Wait returned an error for a plain non-zero exit: %v", err)
	}
	if res.ExitCode != 3 {
		t.Errorf("result exit code = %d, want 3", res.ExitCode)
	}
	if res.Stdout != "out-line\n" {
		t.Errorf("stdout = %q, want %q", res.Stdout, "out-line\n")
	}
	if res.Stderr != "err-line\n" {
		t.Errorf("stderr = %q, want %q", res.Stderr, "err-line\n")
	}
	if got := out.all(); !slices.Equal(got, []string{"out-line"}) {
		t.Errorf("streamed stdout = %q, want [out-line]", got)
	}
	if got := errOut.all(); !slices.Equal(got, []string{"err-line"}) {
		t.Errorf("streamed stderr = %q, want [err-line]", got)
	}

	// Writing to an exited process is a clear error, not a panic on a dead pipe.
	if err := s.WriteLine("post mortem"); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("WriteLine after exit = %v, want ErrSessionClosed", err)
	}
	// Wait is re-entrant.
	if res2, _ := s.Wait(); res2 != res {
		t.Error("second Wait returned a different result")
	}
}

func TestInteractiveSession_FlushesUnterminatedFinalLines(t *testing.T) {
	out, errOut := newLineCollector(), newLineCollector()
	s := startTestSession(t, context.Background(),
		ExecOpts{StdoutLine: out.add, StderrLine: errOut.add}, 0,
		"sh", "-c", `printf 'tail-out'; printf 'tail-err' 1>&2`)

	if _, err := s.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got := out.all(); !slices.Equal(got, []string{"tail-out"}) {
		t.Errorf("stdout lines = %q, want [tail-out]", got)
	}
	if got := errOut.all(); !slices.Equal(got, []string{"tail-err"}) {
		t.Errorf("stderr lines = %q, want [tail-err]", got)
	}
}

func TestInteractiveSession_RespectsMaxOutputBytes(t *testing.T) {
	s := startTestSession(t, context.Background(), ExecOpts{}, 5,
		"sh", "-c", `printf 'aaaaaaaaaaaa'; printf 'bbbbbbbbbbbb' 1>&2`)

	res, err := s.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if res.Stdout != "aaaaa\n...[output truncated]" {
		t.Errorf("stdout = %q, want truncated 5 bytes + marker", res.Stdout)
	}
	if res.Stderr != "bbbbb\n...[output truncated]" {
		t.Errorf("stderr = %q, want truncated 5 bytes + marker", res.Stderr)
	}
}

func TestInteractiveSession_ConcurrentWrites(t *testing.T) {
	out := newLineCollector()
	s := startTestSession(t, context.Background(), ExecOpts{StdoutLine: out.add}, 0, "cat")

	const n = 25
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.WriteLine(strings.Repeat("x", i+1)); err != nil {
				t.Errorf("concurrent WriteLine %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	// Every write must arrive as its own line, whatever the interleaving: the
	// stdin mutex must keep lines from being spliced together.
	seen := map[string]bool{}
	for range n {
		seen[out.next(t)] = true
	}
	for i := range n {
		if !seen[strings.Repeat("x", i+1)] {
			t.Fatalf("line of length %d was lost or spliced; got %q", i+1, out.all())
		}
	}
}

func TestInteractiveSession_ContextCancellationKillsProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := startTestSession(t, ctx, ExecOpts{}, 0, "cat")

	cancel()

	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("cancelling the caller's ctx did not kill the process")
	}
	// Death by signal is reported through the exit code, as in Exec.
	res, err := s.Wait()
	if err != nil {
		t.Errorf("Wait after a signalled kill returned an error: %v", err)
	}
	if res.ExitCode != -1 || s.ExitCode() != -1 {
		t.Errorf("exit code = %d / %d, want -1 (killed)", res.ExitCode, s.ExitCode())
	}
	if err := s.WriteLine("x"); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("WriteLine after kill = %v, want ErrSessionClosed", err)
	}
}
