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
	reader    *bufio.Reader
	data      string
	eventType string
	err       error
	done      bool
}

// NewSSEScanner creates an SSE scanner.
//
// It reads lines with bufio.Reader.ReadString, which grows to fit a line of
// any length, instead of bufio.Scanner, whose fixed max-token size (formerly
// SSEScanBufMax) overflowed with "token too long" on a single data line
// carrying a full base64 image — image generation streams multi-MB frames in
// a single SSE line. SSEScanBufInit is reused as the reader's initial size.
func NewSSEScanner(r io.Reader) *SSEScanner {
	return &SSEScanner{reader: bufio.NewReaderSize(r, SSEScanBufInit)}
}

// Next advances to the next data line. Returns false when the stream ends
// or "[DONE]" is encountered. After Next returns false, call Err() for errors.
func (s *SSEScanner) Next() bool {
	if s.done {
		return false
	}
	for {
		line, err := s.reader.ReadString('\n')
		if len(line) > 0 {
			// Trim the trailing newline ("\n" or "\r\n").
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")

			if after, ok := strings.CutPrefix(line, "event: "); ok {
				s.eventType = after
			} else if after, ok := strings.CutPrefix(line, "event:"); ok {
				s.eventType = strings.TrimSpace(after)
			} else {
				payload, isData := "", false
				if after, ok := strings.CutPrefix(line, "data: "); ok {
					payload, isData = after, true
				} else if after, ok := strings.CutPrefix(line, "data:"); ok {
					payload, isData = after, true
				}
				if isData {
					// "[DONE]" is the OpenAI/Codex stream terminator.
					if payload == "[DONE]" {
						s.done = true
						return false
					}
					s.data = payload
					// A final line may arrive together with io.EOF (no trailing
					// newline). Deliver it now; the next call reports the end.
					if err != nil {
						s.done = true
						if err != io.EOF {
							s.err = err
						}
					}
					return true
				}
				// Non event/data line (blank line, ":" comment, other field): skip.
			}
		}
		if err != nil {
			if err != io.EOF {
				s.err = err
			}
			s.done = true
			return false
		}
	}
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
// underlying socket, unblocking a goroutine stuck inside a blocking read.
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
