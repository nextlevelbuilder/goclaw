package clisession

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/cliagent"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
)

// testDeadline bounds every "wait for the implementation to do something"
// assertion. It is deliberately generous: the tests are all in-memory, so a
// second is already three orders of magnitude of slack, and a failure here means
// the work never happened rather than that the machine was busy.
const testDeadline = 5 * time.Second

// ---------------------------------------------------------------------------
// fakeInteractive — an in-memory sandbox.InteractiveSession
// ---------------------------------------------------------------------------

// fakeInteractive stands in for the Docker-backed session so the real Session
// logic can be driven with no daemon anywhere near the test:
//
//   - WriteLine captures the lines the session sends on stdin, and returns
//     sandbox.ErrSessionClosed once the "process" is gone, exactly as the
//     InteractiveSession contract promises (never a panic on a dead pipe);
//   - pushStdout/pushStderr let the test play the CLI's side of the conversation;
//   - exit() simulates the process dying on its own.
//
// The one structural detail that matters: stdout is delivered by a SINGLE pump
// goroutine, mirroring the real sandbox's one stream-copying goroutine. So if
// the session's stdout handler ever blocked — say, waiting on a human's approval
// — every later line would queue up behind it and never be delivered. That is
// what makes TestSession_SlowPermissionDoesNotStallReadLoop a real test rather
// than a tautology.
type fakeInteractive struct {
	stdoutCB func(string)
	stderrCB func(string)

	stdoutCh chan string
	stderrCh chan string

	quit chan struct{} // closed on exit; stops the pumps
	done chan struct{} // the InteractiveSession.Done channel

	// writes carries each captured stdin line so a test can block for the next
	// one instead of polling.
	writes chan string

	mu       sync.Mutex
	written  []string
	closed   bool
	exitCode int

	exitOnce sync.Once
	pumps    sync.WaitGroup
}

// Compile-time proof that the fake really is the interface under test.
var _ sandbox.InteractiveSession = (*fakeInteractive)(nil)

func newFakeInteractive(ctx context.Context, o sandbox.ExecOpts) *fakeInteractive {
	f := &fakeInteractive{
		stdoutCB: o.StdoutLine,
		stderrCB: o.StderrLine,
		stdoutCh: make(chan string, 256),
		stderrCh: make(chan string, 256),
		quit:     make(chan struct{}),
		done:     make(chan struct{}),
		writes:   make(chan string, 256),
	}

	f.pumps.Add(2)
	go f.pump(f.stdoutCh, f.stdoutCB)
	go f.pump(f.stderrCh, f.stderrCB)

	// The real sandbox kills the process when the ctx ExecInteractive was given
	// is cancelled. Mirroring that is what lets the manager tests assert a
	// session outlives the request that created it.
	go func() {
		select {
		case <-ctx.Done():
			f.exit(-1)
		case <-f.quit:
		}
	}()

	return f
}

func (f *fakeInteractive) pump(ch chan string, cb func(string)) {
	defer f.pumps.Done()
	for {
		select {
		case <-f.quit:
			return
		case line := <-ch:
			if cb != nil {
				cb(line)
			}
		}
	}
}

func (f *fakeInteractive) WriteLine(s string) error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return sandbox.ErrSessionClosed
	}
	// The contract says WriteLine appends the newline itself; strip it so tests
	// compare payloads rather than framing.
	line := s
	if n := len(line); n > 0 && line[n-1] == '\n' {
		line = line[:n-1]
	}
	f.written = append(f.written, line)
	f.mu.Unlock()

	select {
	case f.writes <- line:
	default: // never block the writer in a test
	}
	return nil
}

func (f *fakeInteractive) Wait() (*sandbox.ExecResult, error) {
	<-f.done
	f.mu.Lock()
	defer f.mu.Unlock()
	return &sandbox.ExecResult{ExitCode: f.exitCode}, nil
}

func (f *fakeInteractive) Close() error {
	f.exit(-1)
	f.pumps.Wait()
	return nil
}

func (f *fakeInteractive) Done() <-chan struct{} { return f.done }

func (f *fakeInteractive) ExitCode() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exitCode
}

// exit simulates the process terminating with code. Idempotent — the first
// caller wins, so a test's explicit exit(9) is not overwritten by the -1 that
// Session.Close's context cancellation produces.
func (f *fakeInteractive) exit(code int) {
	f.exitOnce.Do(func() {
		f.mu.Lock()
		f.closed = true
		f.exitCode = code
		f.mu.Unlock()
		close(f.quit)
		close(f.done)
	})
}

// pushStdout queues one line as if the CLI had printed it. Lines pushed after
// the process exited are dropped, as they would be in reality.
func (f *fakeInteractive) pushStdout(line string) {
	select {
	case f.stdoutCh <- line:
	case <-f.quit:
	}
}

func (f *fakeInteractive) pushStderr(line string) {
	select {
	case f.stderrCh <- line:
	case <-f.quit:
	}
}

// nextWrite blocks for the next line the session wrote to stdin.
func (f *fakeInteractive) nextWrite(t *testing.T) string {
	t.Helper()
	select {
	case s := <-f.writes:
		return s
	case <-time.After(testDeadline):
		t.Fatalf("timed out after %s waiting for a line on stdin; captured so far: %q", testDeadline, f.allWrites())
		return ""
	}
}

func (f *fakeInteractive) allWrites() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.written)
}

// ---------------------------------------------------------------------------
// fakeStarter — the Starter seam SessionOpts already exposes
// ---------------------------------------------------------------------------

// fakeStarter records every ExecInteractive call, which is how the manager tests
// assert that exactly ONE process was started for a key rather than merely that
// one pointer was handed back twice.
type fakeStarter struct {
	mu       sync.Mutex
	starts   int
	procs    []*fakeInteractive
	commands [][]string
	workDirs []string
	envs     []map[string]string
	err      error // when set, every start fails
}

var _ Starter = (*fakeStarter)(nil)

func newFakeStarter() *fakeStarter { return &fakeStarter{} }

func (fs *fakeStarter) ExecInteractive(ctx context.Context, command []string, workDir string, opts ...sandbox.ExecOption) (sandbox.InteractiveSession, error) {
	o := sandbox.ApplyExecOpts(opts)

	fs.mu.Lock()
	if fs.err != nil {
		err := fs.err
		fs.mu.Unlock()
		return nil, err
	}
	fs.starts++
	fs.commands = append(fs.commands, slices.Clone(command))
	fs.workDirs = append(fs.workDirs, workDir)
	fs.envs = append(fs.envs, o.Env)
	fs.mu.Unlock()

	p := newFakeInteractive(ctx, o)

	fs.mu.Lock()
	fs.procs = append(fs.procs, p)
	fs.mu.Unlock()
	return p, nil
}

func (fs *fakeStarter) startCount() int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.starts
}

func (fs *fakeStarter) proc(i int) *fakeInteractive {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if i >= len(fs.procs) {
		return nil
	}
	return fs.procs[i]
}

func (fs *fakeStarter) command(i int) []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if i >= len(fs.commands) {
		return nil
	}
	return fs.commands[i]
}

func (fs *fakeStarter) workDir(i int) string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if i >= len(fs.workDirs) {
		return ""
	}
	return fs.workDirs[i]
}

func (fs *fakeStarter) env(i int) map[string]string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if i >= len(fs.envs) {
		return nil
	}
	return fs.envs[i]
}

// ---------------------------------------------------------------------------
// sink — a callback recorder a test can block on
// ---------------------------------------------------------------------------

type sink[T any] struct {
	mu  sync.Mutex
	got []T
	ch  chan T
}

func newSink[T any]() *sink[T] { return &sink[T]{ch: make(chan T, 256)} }

func (s *sink[T]) add(v T) {
	s.mu.Lock()
	s.got = append(s.got, v)
	s.mu.Unlock()
	select {
	case s.ch <- v:
	default: // must never stall the caller (the read loop)
	}
}

func (s *sink[T]) next(t *testing.T) T {
	t.Helper()
	select {
	case v := <-s.ch:
		return v
	case <-time.After(testDeadline):
		t.Fatalf("timed out after %s waiting for a callback; received so far: %+v", testDeadline, s.snapshot())
		var zero T
		return zero
	}
}

func (s *sink[T]) snapshot() []T {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.got)
}

func (s *sink[T]) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.got)
}

// ---------------------------------------------------------------------------
// shared helpers
// ---------------------------------------------------------------------------

// waitFor blocks on ch until testDeadline, failing with msg if nothing arrives.
func waitFor[T any](t *testing.T, ch <-chan T, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(testDeadline):
		t.Fatalf("%s (waited %s)", msg, testDeadline)
	}
}

// testSpec is the real shipped claude_code spec, so the tests exercise the argv
// and output format production actually uses.
func testSpec() cliagent.Spec {
	return cliagent.Defaults()[cliagent.ProviderClaudeCode]
}

// testOpts is a complete SessionOpts with no callbacks — the manager tests only
// care about lifecycle.
func testOpts(st *fakeStarter) SessionOpts {
	return SessionOpts{
		Sandbox: st,
		Spec:    testSpec(),
		Mode:    cliagent.PermissionManual,
		Env:     map[string]string{"HOME": "/tmp"},
		WorkDir: "/workspace",
	}
}

// setLastUsed backdates a session's activity clock. Faking the clock this way is
// what keeps the idle-reaper tests off wall-clock time entirely.
func setLastUsed(s *Session, at time.Time) {
	s.mu.Lock()
	s.lastUsed = at
	s.mu.Unlock()
}

// ---------------------------------------------------------------------------
// wire fixtures
// ---------------------------------------------------------------------------

// canUseToolLine builds the control_request the CLI sends when it wants to run a
// tool. inputJSON is spliced in raw so a test can supply a non-object on purpose.
func canUseToolLine(requestID, tool, inputJSON string) string {
	return fmt.Sprintf(
		`{"type":"control_request","request_id":%q,"request":{"subtype":"can_use_tool","tool_name":%q,"input":%s,"display_name":"Bash","description":"List the workspace","tool_use_id":"toolu_01"}}`,
		requestID, tool, inputJSON)
}

// assistantTextLine builds one narration line in claude stream-json shape.
func assistantTextLine(text string) string {
	b, err := json.Marshal(text)
	if err != nil {
		panic(err)
	}
	return `{"type":"assistant","message":{"content":[{"type":"text","text":` + string(b) + `}]}}`
}

// wireControlResponse is the response as it appears ON THE WIRE. The inner
// handler result is kept as a raw map on purpose: the tests must be able to
// assert that a key (updatedInput) is ABSENT, which a typed struct cannot express.
type wireControlResponse struct {
	Type     string `json:"type"`
	Response struct {
		Subtype   string                     `json:"subtype"`
		RequestID string                     `json:"request_id"`
		Response  map[string]json.RawMessage `json:"response"`
		Error     string                     `json:"error"`
	} `json:"response"`
}

// decodeControlResponse decodes one stdin line as a control_response, checking
// the envelope shape (including that there is nothing else at the top level).
func decodeControlResponse(t *testing.T, line string) wireControlResponse {
	t.Helper()

	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &top); err != nil {
		t.Fatalf("stdin line is not a JSON object: %v\nline: %s", err, line)
	}
	if _, ok := top["type"]; !ok {
		t.Fatalf("control_response has no %q discriminator: %s", "type", line)
	}
	if _, ok := top["response"]; !ok {
		t.Fatalf("control_response has no %q envelope: %s", "response", line)
	}
	if len(top) != 2 {
		t.Fatalf("control_response must carry exactly {type,response}, got %d keys: %s", len(top), line)
	}

	var got wireControlResponse
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("control_response does not decode: %v\nline: %s", err, line)
	}
	if got.Type != typeControlResponse {
		t.Fatalf("type = %q, want %q: %s", got.Type, typeControlResponse, line)
	}
	return got
}

// permissionVerdict pulls the behaviour and message out of a can_use_tool
// response, failing if the handler result is missing or shapeless.
func permissionVerdict(t *testing.T, got wireControlResponse) (behavior, message string) {
	t.Helper()
	if got.Response.Response == nil {
		t.Fatalf("control_response carries no handler result: %+v", got)
	}
	raw, ok := got.Response.Response["behavior"]
	if !ok {
		t.Fatalf("handler result has no %q: %v", "behavior", got.Response.Response)
	}
	if err := json.Unmarshal(raw, &behavior); err != nil {
		t.Fatalf("behavior is not a string (%s): %v", raw, err)
	}
	if raw, ok := got.Response.Response["message"]; ok {
		if err := json.Unmarshal(raw, &message); err != nil {
			t.Fatalf("message is not a string (%s): %v", raw, err)
		}
	}
	return behavior, message
}
