package providers

import (
	"bufio"
	"context"
	"io"
	"strings"
	"sync"
)

// SSEScanner reads an SSE (Server-Sent Events) stream line by line,
// extracting data payloads. Shared by OpenAI, Anthropic, and Codex providers.
type SSEScanner struct {
	scanner   *bufio.Scanner
	data      string
	eventType string
	err       error
}

// NewSSEScanner creates an SSE scanner with appropriate buffer sizes.
func NewSSEScanner(r io.Reader) *SSEScanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, SSEScanBufInit), SSEScanBufMax)
	return &SSEScanner{scanner: sc}
}

// Next advances to the next data line. Returns false when the stream ends
// or "[DONE]" is encountered. After Next returns false, call Err() for errors.
func (s *SSEScanner) Next() bool {
	for s.scanner.Scan() {
		line := s.scanner.Text()

		// Track event type (e.g. "event: message_start")
		if after, ok := strings.CutPrefix(line, "event: "); ok {
			s.eventType = after
			continue
		}
		if after, ok := strings.CutPrefix(line, "event:"); ok {
			s.eventType = strings.TrimSpace(after)
			continue
		}

		// Extract data payload
		var payload string
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			payload = after
		} else if after, ok := strings.CutPrefix(line, "data:"); ok {
			payload = after
		} else {
			continue // skip empty lines, comments, other fields
		}

		// "[DONE]" is the OpenAI/Codex stream terminator
		if payload == "[DONE]" {
			return false
		}

		s.data = payload
		return true
	}
	s.err = s.scanner.Err()
	return false
}

// Data returns the current data payload (valid after Next returns true).
func (s *SSEScanner) Data() string {
	return s.data
}

// EventType returns the last seen event type (e.g. "message_start", "content_block_delta").
func (s *SSEScanner) EventType() string {
	return s.eventType
}

// Err returns the first non-EOF error encountered during scanning.
func (s *SSEScanner) Err() error {
	return s.err
}

// CtxBody wraps an http.Response.Body so that ctx cancellation closes the
// underlying socket, unblocking a goroutine stuck inside bufio.Scanner.Scan().
// Safe for concurrent Close (sync.Once). Caller MUST defer Close() to release
// the watchdog goroutine even on success.
type CtxBody struct {
	body io.ReadCloser
	done chan struct{}
	once sync.Once
}

// NewCtxBody returns a ReadCloser that closes body when ctx is cancelled.
func NewCtxBody(ctx context.Context, body io.ReadCloser) *CtxBody {
	cb := &CtxBody{body: body, done: make(chan struct{})}
	go func() {
		select {
		case <-ctx.Done():
			cb.closeOnce()
		case <-cb.done:
			// normal close path; watchdog exits cleanly
		}
	}()
	return cb
}

func (cb *CtxBody) Read(p []byte) (int, error) { return cb.body.Read(p) }

// Close closes the underlying body exactly once (safe for concurrent calls).
func (cb *CtxBody) Close() error {
	return cb.closeOnce()
}

func (cb *CtxBody) closeOnce() error {
	var err error
	cb.once.Do(func() {
		close(cb.done)
		err = cb.body.Close()
	})
	return err
}
