package clisession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/cliagent"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
)

// PermissionRequest is one tool call the CLI wants to run, awaiting a decision.
type PermissionRequest struct {
	// RequestID is the CLI's own id for this ask. It must be echoed back in the
	// answer; it is also the natural correlation id for a UI prompt.
	RequestID string
	// ToolName is the CLI's tool, e.g. "Bash", "Edit", "WebFetch".
	ToolName string
	// Input is the tool's arguments, verbatim, as a JSON object.
	Input json.RawMessage

	// DisplayName / Description are the CLI's human-readable labels, when it
	// supplied them — a prompt should prefer these over rendering raw Input.
	DisplayName string
	Description string
	// ToolUseID ties this ask to the tool_use block already streamed to the UI,
	// so the prompt can be attached to that tool chip rather than floating free.
	ToolUseID string
}

// PermissionDecision is the answer to a PermissionRequest.
type PermissionDecision struct {
	// Allow runs the tool. The ZERO VALUE IS DENY, deliberately: any code path
	// that forgets to fill this in withholds permission rather than granting it.
	Allow bool
	// UpdatedInput optionally REPLACES the tool's arguments (a JSON object). Use
	// it to narrow what was asked for — e.g. approving a read of one path instead
	// of the whole tree. Ignored unless Allow is true, and ignored if it is not a
	// non-empty JSON object, because the CLI turns a schema-invalid rewrite into a
	// denial.
	UpdatedInput json.RawMessage
	// DenyReason is shown to the model as the tool's error result, so it can
	// adapt ("the user won't let me push; ask them first") instead of retrying
	// blindly. Only used when Allow is false.
	DenyReason string
}

// PermissionFunc decides one PermissionRequest. It BLOCKS until the user (or a
// timeout, or a policy) answers — minutes are expected and fine.
//
// It is injected rather than imported so this package never depends on the
// approval broker or the gateway: that keeps the session logic unit-testable and
// keeps the policy decision (who may approve, how long to wait, what a timeout
// means) in the layer that actually knows.
//
// The ctx is the SESSION's context: it is cancelled when the session closes, so
// an implementation waiting on a human must select on ctx.Done() and give up.
// Returning the zero PermissionDecision on cancellation is the right answer.
type PermissionFunc func(context.Context, PermissionRequest) PermissionDecision

// Starter is the slice of sandbox.Sandbox that this package needs. Narrowing it
// to one method is what lets the tests drive the real Session logic from an
// in-memory fake, with no Docker daemon involved.
type Starter interface {
	ExecInteractive(ctx context.Context, command []string, workDir string, opts ...sandbox.ExecOption) (sandbox.InteractiveSession, error)
}

// SessionOpts is everything a session needs to start. Obtaining the sandbox and
// applying credentials to Env is the CALLER's job — see the package doc.
type SessionOpts struct {
	// Sandbox runs the CLI. Required.
	Sandbox Starter
	// Spec is the resolved invocation recipe. Its InteractiveArgs must be
	// non-empty; a provider without them fails loudly here rather than silently
	// degrading to a one-shot run.
	Spec cliagent.Spec
	// Mode selects the permission-mode args. cliagent.PermissionManual is what
	// makes the CLI ask; PermissionAuto lets it act unattended inside the
	// sandbox, in which case Permission is never consulted.
	Mode cliagent.PermissionMode
	// Env is the full environment for the process, INCLUDING the credential the
	// caller already injected via Spec.ApplyCredential.
	Env map[string]string
	// WorkDir is the working directory inside the container ("" = the image's).
	WorkDir string

	// OnEvent receives each normalised event. It is called from the stdout
	// reading goroutine, IN ORDER, and must not block for long — a slow consumer
	// stalls the stream. Optional.
	OnEvent func(cliagent.Event)
	// OnNotice receives a message the CLI wants the USER to see about a tool call
	// it refused — including a refusal caused by the CLI rejecting our own
	// permission response, which otherwise looks like an approval that silently
	// did nothing.
	OnNotice func(string)

	// OnStderr receives each stderr line. Diagnostics matter here: a CLI that
	// refuses to start, or loses its credential mid-session, says so on stderr
	// and nowhere else. Optional.
	OnStderr func(string)
	// Permission decides tool calls. NIL MEANS DENY EVERYTHING — a session with
	// no approval channel must not become implicit consent.
	Permission PermissionFunc
}

// Session is one live conversation with a CLI process.
//
// Concurrency model, which is the whole design:
//
//   - stdout is consumed by ONE goroutine (the sandbox's line callback). It
//     parses, emits events, and classifies control frames. It must never block.
//   - each permission ask is handed to its OWN goroutine, so an approval that
//     takes minutes does not stop the CLI's narration from streaming or a later
//     ask from being raised.
//   - all stdin writes funnel through InteractiveSession.WriteLine, which is
//     already mutex-guarded, so the writers above need no lock between them.
type Session struct {
	key  string
	spec cliagent.Spec

	proc   sandbox.InteractiveSession
	parser cliagent.Parser

	onEvent  func(cliagent.Event)
	onStderr func(string)
	onNotice func(string)
	perm     PermissionFunc

	// ctx/cancel bound the session. cancel is called by Close and unblocks any
	// PermissionFunc still waiting on a human.
	ctx    context.Context
	cancel context.CancelFunc

	// pending tracks in-flight approval goroutines so Close can wait for them
	// instead of racing them.
	pending sync.WaitGroup

	mu       sync.Mutex
	closed   bool
	lastUsed time.Time

	closeOnce sync.Once
	closeErr  error
}

// newSession starts the CLI and begins streaming. It returns once the process is
// running; the conversation itself is driven by Send.
func newSession(ctx context.Context, key string, opts SessionOpts) (*Session, error) {
	if opts.Sandbox == nil {
		return nil, errors.New("clisession: no sandbox — the caller must resolve one and pass it in SessionOpts")
	}
	mode := opts.Mode
	if mode == "" {
		mode = cliagent.PermissionAuto
	}
	command, err := opts.Spec.InteractiveCommand(mode)
	if err != nil {
		return nil, err
	}

	// The session's own context, derived from the caller's: cancelling either one
	// ends the session. Deliberately NOT given a timeout — an interactive session
	// is long-lived by design and the idle reaper is what bounds its life.
	sctx, cancel := context.WithCancel(ctx)

	s := &Session{
		key:      key,
		spec:     opts.Spec,
		parser:   cliagent.NewParser(opts.Spec.Output),
		onEvent:  opts.OnEvent,
		onStderr: opts.OnStderr,
		onNotice: opts.OnNotice,
		perm:     opts.Permission,
		ctx:      sctx,
		cancel:   cancel,
		lastUsed: time.Now(),
	}

	proc, err := opts.Sandbox.ExecInteractive(sctx, command, opts.WorkDir,
		sandbox.WithEnv(opts.Env),
		sandbox.WithStdoutLine(s.handleStdout),
		sandbox.WithStderrLine(s.handleStderr),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("clisession: could not start %s: %w", opts.Spec.Binary, err)
	}
	s.proc = proc

	// Release the derived context once the process is gone, so a session whose
	// CLI exited on its own does not leak the ctx or wedge a waiting approver.
	go func() {
		<-proc.Done()
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		cancel()
	}()

	slog.Debug("clisession started", "key", key, "provider", opts.Spec.Provider, "mode", string(mode))
	return s, nil
}

// Send delivers one user turn. It returns sandbox.ErrSessionClosed (wrapped) if
// the CLI has exited or the session was closed.
func (s *Session) Send(ctx context.Context, text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("clisession: empty message — nothing to send")
	}
	// Check the caller's ctx first so a cancelled request doesn't inject a turn
	// the user has already navigated away from.
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return fmt.Errorf("clisession: %w", sandbox.ErrSessionClosed)
	}

	line, err := encodeLine(newUserMessage(text))
	if err != nil {
		return fmt.Errorf("clisession: could not encode user message: %w", err)
	}
	// Logged verbatim (bounded) because the response SHAPE is the thing most likely
	// to be wrong against a CLI version we have not tested: without this, a
	// rejected response is indistinguishable from one that was never sent.
	slog.Debug("clisession: control response", "key", s.key, "line", truncate(line, 500))
	if err := s.proc.WriteLine(line); err != nil {
		return fmt.Errorf("clisession: %w", err)
	}
	s.touch()
	return nil
}

// Close ends the session: it cancels the context (which unblocks any approver
// still waiting on a human), waits for the in-flight approval goroutines to
// finish so none of them writes to a dead pipe, and then terminates the process.
// Idempotent and safe to call concurrently with everything else.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()

		// Cancel BEFORE waiting: the pending approvals are exactly the goroutines
		// that may be blocked on a human, and their ctx is how they learn to stop.
		s.cancel()
		s.pending.Wait()

		if s.proc != nil {
			s.closeErr = s.proc.Close()
		}
	})
	return s.closeErr
}

// Done is closed once the CLI process has exited.
func (s *Session) Done() <-chan struct{} { return s.proc.Done() }

// ExitCode reports the process's exit status; meaningful only after Done.
func (s *Session) ExitCode() int { return s.proc.ExitCode() }

// LastUsed is when this session last saw activity — a user turn or anything from
// the CLI. The manager's idle reaper reads it.
func (s *Session) LastUsed() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastUsed
}

// touch marks the session active. Both directions count: a CLI that is
// mid-build for twenty minutes is not idle, and neither is one waiting on an
// approval the user is still reading.
func (s *Session) touch() {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()
}

// isClosed reports whether the session is done accepting work.
func (s *Session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// ---------------------------------------------------------------------------
// stdout / stderr
// ---------------------------------------------------------------------------

// handleStdout processes one stdout line. THIS IS THE READ LOOP — it runs on the
// sandbox's stream-copying goroutine, so it must return promptly. Everything
// that can take time (i.e. an approval) is moved onto its own goroutine.
func (s *Session) handleStdout(line string) {
	s.touch()

	p := classifyLine(line)
	switch {
	case p.Control != nil:
		s.dispatchControl(p.Control)
	case p.Narration:
		// The parser is touched only from here, so it needs no lock.
		for _, ev := range s.parser.ParseLine(line) {
			s.emit(ev)
		}
	default:
		// Neither a control frame nor narration. Mostly protocol-internal noise —
		// but this is also where the CLI reports that it REJECTED our permission
		// response, as a system/permission_denied frame carrying the reason. That
		// used to be dropped in silence, which is the worst possible place to be
		// quiet: the user sees an approval succeed, the model is told the tool
		// failed, and nothing anywhere says why. Logged, and surfaced to the
		// consumer so it can be shown in the transcript.
		s.noteUnhandled(line)
	}
}

// noteUnhandled logs a stdout line the session did not act on. Kept cheap: only
// the frame's type/subtype is parsed, and only a permission refusal is promoted
// above debug.
func (s *Session) noteUnhandled(line string) {
	var head struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Message string `json:"message"`
		Tool    string `json:"tool_name"`
		Reason  string `json:"decision_reason_type"`
	}
	if json.Unmarshal([]byte(line), &head) != nil {
		slog.Debug("clisession: unparseable stdout line", "key", s.key, "line", truncate(line, 300))
		return
	}
	if head.Type == "system" && head.Subtype == "permission_denied" {
		// The CLI refused the call despite our answer — either the user denied, or
		// (the case that cost real debugging time) it did not accept our response.
		slog.Info("clisession: CLI reported a permission denial",
			"key", s.key, "tool", head.Tool, "reason_type", head.Reason,
			"message", truncate(head.Message, 400))
		if s.onNotice != nil && strings.TrimSpace(head.Message) != "" {
			s.onNotice(head.Message)
		}
		return
	}
	slog.Debug("clisession: unhandled stdout frame",
		"key", s.key, "type", head.Type, "subtype", head.Subtype, "line", truncate(line, 300))
}

// handleStderr forwards a diagnostic line.
func (s *Session) handleStderr(line string) {
	// Deliberately does NOT touch(): stderr can carry a repeating warning from a
	// process nobody is talking to, and letting that keep an abandoned session
	// alive forever would defeat the idle reaper.
	if s.onStderr == nil {
		return
	}
	s.onStderr(line)
}

// emit hands one event to the consumer, if there is one.
func (s *Session) emit(ev cliagent.Event) {
	if s.onEvent == nil {
		return
	}
	s.onEvent(ev)
}

// ---------------------------------------------------------------------------
// permission control frames
// ---------------------------------------------------------------------------

// dispatchControl answers a control_request WITHOUT blocking the read loop.
//
// Every branch must produce exactly one response, including the ones we refuse:
// an unanswered control_request leaves the CLI blocked on us for as long as the
// session lives.
func (s *Session) dispatchControl(req *controlRequest) {
	if req.Request.Subtype != subtypeCanUseTool {
		// A subtype we do not implement (user dialogs, hook callbacks, MCP relays…).
		// Say so rather than staying silent, so the CLI can fall back or fail fast.
		s.replyUnsupported(req)
		return
	}

	pr := PermissionRequest{
		RequestID:   req.RequestID,
		ToolName:    req.Request.ToolName,
		Input:       req.Request.Input,
		DisplayName: req.Request.DisplayName,
		Description: req.Request.Description,
		ToolUseID:   req.Request.ToolUseID,
	}

	// No approval channel → refuse, and say why. This is the one place where
	// "we don't know" must not become "yes": a missing PermissionFunc means
	// nobody can consent, so nobody has.
	if s.perm == nil {
		slog.Warn("security.clisession_approval_channel_missing", "key", s.key, "tool", pr.ToolName)
		s.reply(newDenyResponse(req.RequestID,
			"No approval channel is connected to this session, so this tool call could not be authorised. Ask the user to approve it from the dashboard, or work without this tool."))
		return
	}

	// Reject early if we are already shutting down — starting a goroutine that
	// will only find a dead pipe wastes a slot on the WaitGroup Close is waiting
	// on, and the CLI is about to die anyway.
	if s.isClosed() {
		s.reply(newDenyResponse(req.RequestID, "The session was closed before this action could be approved."))
		return
	}

	s.pending.Add(1)
	go func() {
		defer s.pending.Done()

		// The approval may sit here for minutes. The read loop, meanwhile, keeps
		// draining stdout and can raise further asks in parallel.
		decision := s.perm(s.ctx, pr)

		// The session may have ended while we waited. Writing is still the right
		// move — WriteLine reports ErrSessionClosed rather than panicking, and
		// skipping the write would leave a live CLI hanging.
		switch {
		case decision.Allow:
			// The allow branch REQUIRES updatedInput, so the tool's original input is
			// echoed when the approver did not rewrite it. If neither is usable we
			// must not fake one — running a tool with empty arguments after the user
			// pressed Approve is a worse outcome than an honest refusal.
			resp, ok := newAllowResponse(req.RequestID, decision.UpdatedInput, pr.Input)
			if !ok {
				slog.Warn("security.clisession_allow_without_input",
					"key", s.key, "tool", pr.ToolName, "input", truncate(string(pr.Input), 200))
				s.reply(newDenyResponse(req.RequestID,
					"The approval could not be completed because this tool call arrived without a usable input object. Try the action again."))
				break
			}
			s.reply(resp)
		default:
			s.reply(newDenyResponse(req.RequestID, decision.DenyReason))
		}
		// A resolved approval is activity: the user just interacted with this
		// session, so it is not idle even if the CLI has been quiet.
		s.touch()
	}()
}

// replyUnsupported answers a control_request subtype this package does not
// implement.
func (s *Session) replyUnsupported(req *controlRequest) {
	slog.Debug("clisession: unsupported control request", "key", s.key, "subtype", req.Request.Subtype)
	s.reply(newErrorResponse(req.RequestID,
		fmt.Sprintf("Unsupported control request subtype: %s", req.Request.Subtype)))
}

// reply writes one control_response. Failures are logged, not returned: the
// caller is either the read loop (which must not stop) or an approval goroutine
// (whose result nobody is waiting on), and the usual cause is simply that the
// CLI has already exited.
func (s *Session) reply(resp controlResponse) {
	line, err := encodeLine(resp)
	if err != nil {
		slog.Warn("clisession: could not encode control response", "key", s.key, "error", err)
		return
	}
	// Logged verbatim (bounded) because the response SHAPE is the thing most likely
	// to be wrong against a CLI version we have not tested: without this, a
	// rejected response is indistinguishable from one that was never sent.
	slog.Debug("clisession: control response", "key", s.key, "line", truncate(line, 500))
	if err := s.proc.WriteLine(line); err != nil {
		if errors.Is(err, sandbox.ErrSessionClosed) {
			slog.Debug("clisession: control response dropped, session closed", "key", s.key)
			return
		}
		slog.Warn("clisession: could not send control response", "key", s.key, "error", err)
	}
}

// truncate shortens s for a log field, marking that it was cut.
func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
