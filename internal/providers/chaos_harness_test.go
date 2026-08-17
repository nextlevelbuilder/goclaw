package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// fakeLLMServer — a scriptable OpenAI-compatible HTTP server for chaos tests.
//
// Every provider request lands on a single handler that consumes the scripted
// steps in order and, once exhausted, repeats the LAST step forever. Each step
// may return an HTTP error (429/5xx), a plain body, an SSE stream, or
// force-close the connection. All state is mutex-guarded so the server is safe
// to share across sub-tests; ResetSteps() returns it to a clean slate.
// ---------------------------------------------------------------------------

// responseStep scripts one HTTP response. When Status != 200 the step writes
// the status line and Body, then returns. When Status == 200 and SSEFrames is
// non-empty the step streams SSE frames (with inter-frame delay) and optionally
// writes data: [DONE]. When only Body is set it is written as a plain 200
// response. CloseConn force-closes the underlying connection instead of
// returning (an abrupt EOF mid-response).
type responseStep struct {
	Status      int
	Headers     http.Header
	Body        string
	DelayBefore time.Duration
	CloseConn   bool

	SSEFrames   []sseFrame
	SSEDone     bool          // write data: [DONE] after the frames
	SSEFrameGap time.Duration // delay between frames (0 = as fast as possible)
}

// sseFrame is one SSE `data: {...}` line. Event is optional; when non-empty the
// frame is prefixed with `event: <Event>`.
type sseFrame struct {
	Event string
	Data  string
}

// requestSpec captures the latest incoming request for assertions.
type requestSpec struct {
	Method  string
	Path    string
	HasAuth bool
}

// fakeLLMServer wraps the httptest server and its scripted behavior.
type fakeLLMServer struct {
	t      *testing.T
	server *httptest.Server

	mu          sync.Mutex
	steps       []responseStep
	cursor      int
	count       int
	requestSpec requestSpec
}

// newFakeLLMServer starts a fake server pre-loaded with the given script.
func newFakeLLMServer(t *testing.T, steps ...responseStep) *fakeLLMServer {
	t.Helper()
	f := &fakeLLMServer{t: t}
	f.server = httptest.NewServer(http.HandlerFunc(f.fakeLLMServerHandler))
	t.Cleanup(f.server.Close)
	f.script(steps...)
	return f
}

// newFakeLLMServerEmpty starts a fake server with an empty script; tests
// populate it via script().
func newFakeLLMServerEmpty(t *testing.T) *fakeLLMServer {
	t.Helper()
	f := &fakeLLMServer{t: t}
	f.server = httptest.NewServer(http.HandlerFunc(f.fakeLLMServerHandler))
	t.Cleanup(f.server.Close)
	return f
}

// URL returns the server base URL.
func (f *fakeLLMServer) URL() string { return f.server.URL }

// script replaces the step list and rewinds the cursor/request count. The last
// step repeats once the list is exhausted.
func (f *fakeLLMServer) script(steps ...responseStep) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steps = steps
	f.cursor = 0
	f.count = 0
	f.requestSpec = requestSpec{}
}

// ResetSteps clears the script, cursor, and request count for re-use.
func (f *fakeLLMServer) ResetSteps() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steps = nil
	f.cursor = 0
	f.count = 0
	f.requestSpec = requestSpec{}
}

// nextStep returns the step for the next request, advancing the cursor. When
// the script is exhausted the last step is repeated forever.
func (f *fakeLLMServer) nextStep() *responseStep {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.steps) == 0 {
		return nil
	}
	if f.cursor >= len(f.steps) {
		f.cursor = len(f.steps) - 1
	}
	step := f.steps[f.cursor]
	f.cursor++
	f.count++
	return &step
}

// requestCount is the total number of requests served since the last script()/
// ResetSteps() call.
func (f *fakeLLMServer) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.count
}

// LastRequest returns the spec of the most recently served request.
func (f *fakeLLMServer) LastRequest() requestSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requestSpec
}

// fakeLLMServerHandler serves the scripted steps.
func (f *fakeLLMServer) fakeLLMServerHandler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requestSpec = requestSpec{
		Method:  r.Method,
		Path:    r.URL.Path,
		HasAuth: r.Header.Get("Authorization") != "",
	}
	f.mu.Unlock()

	step := f.nextStep()
	if step == nil {
		http.Error(w, "no script configured", http.StatusServiceUnavailable)
		return
	}

	if step.DelayBefore > 0 {
		time.Sleep(step.DelayBefore)
	}

	for k, vs := range step.Headers {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}

	if step.CloseConn {
		// Hijack and drop the socket so the client observes an abrupt EOF
		// mid-response (before any HTTP status is written).
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijack unsupported", http.StatusInternalServerError)
			return
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			f.t.Errorf("hijack failed: %v", err)
			return
		}
		if buf != nil {
			buf.Flush()
		}
		conn.Close()
		return
	}

	// HTTP error: write the status line and body, then return.
	if step.Status != http.StatusOK {
		w.WriteHeader(step.Status)
		if step.Body != "" {
			fmt.Fprint(w, step.Body)
		}
		return
	}

	// SSE mode: stream frames with optional inter-frame delay, then terminator.
	if len(step.SSEFrames) > 0 {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			f.t.Error("ResponseWriter does not implement http.Flusher")
			return
		}
		flusher.Flush() // flush headers so the client finishes its connection phase
		for _, frame := range step.SSEFrames {
			if frame.Event != "" {
				fmt.Fprint(w, "event: "+frame.Event+"\n")
			}
			fmt.Fprint(w, "data: "+frame.Data+"\n\n")
			flusher.Flush()
			if step.SSEFrameGap > 0 {
				time.Sleep(step.SSEFrameGap)
			}
		}
		if step.SSEDone {
			fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
		}
		return
	}

	// Plain response (200).
	w.WriteHeader(http.StatusOK)
	if step.Body != "" {
		fmt.Fprint(w, step.Body)
	}
}

// openAIChatChunk builds a single OpenAI-compat SSE chat.completion.chunk event.
func (f *fakeLLMServer) openAIChatChunk(delta map[string]any, finishReason string) sseFrame {
	return sseFrame{
		Data: f.mustJSON(map[string]any{
			"choices": []map[string]any{{
				"index":         0,
				"delta":         delta,
				"finish_reason": finishReason,
			}},
		}),
	}
}

// openAIReasoningDelta builds an SSE event carrying reasoning (thinking) only.
func (f *fakeLLMServer) openAIReasoningDelta(text string) sseFrame {
	return f.openAIChatChunk(map[string]any{"reasoning_content": text}, "")
}

// openAITextDelta builds an SSE event carrying a content (text) delta.
func (f *fakeLLMServer) openAITextDelta(text string) sseFrame {
	return f.openAIChatChunk(map[string]any{"content": text}, "")
}

// openAIStopDelta builds the finish chunk that terminates a chat.completion.chunk stream.
func (f *fakeLLMServer) openAIStopDelta() sseFrame {
	return f.openAIChatChunk(map[string]any{}, "stop")
}

// openAICompleteStep returns a 200 step carrying a non-streamed chat completion
// with the given content.
func (f *fakeLLMServer) openAICompleteStep(content string) responseStep {
	return responseStep{
		Status:  http.StatusOK,
		Headers: http.Header{"Content-Type": []string{"application/json"}},
		Body: f.mustJSON(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			}},
		}),
	}
}

// httpErrorStep returns an HTTP error step with an optional Retry-After header
// (in seconds; 0 = header absent).
func (f *fakeLLMServer) httpErrorStep(status, retryAfterSeconds int, body string) responseStep {
	step := responseStep{Status: status, Body: body}
	if step.Body == "" {
		step.Body = fmt.Sprintf("provider error status=%d", status)
	}
	if retryAfterSeconds > 0 {
		step.Headers = http.Header{"Retry-After": []string{fmt.Sprintf("%d", retryAfterSeconds)}}
	}
	return step
}

// mustJSON marshals v to a JSON string, failing the test on error. It reuses
// the same-package helper semantics with a test-aware error path.
func (f *fakeLLMServer) mustJSON(v any) string {
	f.t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		f.t.Fatalf("fake server: marshal JSON: %v", err)
	}
	return string(b)
}
