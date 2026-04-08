package providers

import (
	"bufio"
	"io"
	"strings"
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
