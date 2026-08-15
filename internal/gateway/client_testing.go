package gateway

import (
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
)

// NewTestClient returns a minimally-wired Client for unit tests in other
// packages. Role + tenant are set directly because the underlying fields are
// unexported. SendResponse is safe because the send channel is nil — the
// writer hits the default branch of the select and drops the frame silently.
//
// Not for production use. Any non-test caller should use NewClient instead.
func NewTestClient(role permissions.Role, tenantID uuid.UUID, userID string) *Client {
	return &Client{
		id:            uuid.NewString(),
		authenticated: true,
		role:          role,
		userID:        userID,
		tenantID:      tenantID,
	}
}

// NewCapturingTestClient is the variant that buffers outbound frames so a test
// can read back what the handler sent. The returned channel is buffered large
// enough to hold the response frames of a typical handler invocation without
// blocking the writer — increase the size argument if a test expects more.
func NewCapturingTestClient(role permissions.Role, tenantID uuid.UUID, userID string, bufSize int) (*Client, <-chan []byte) {
	if bufSize <= 0 {
		bufSize = 4
	}
	ch := make(chan []byte, bufSize)
	c := &Client{
		id:            uuid.NewString(),
		authenticated: true,
		role:          role,
		userID:        userID,
		tenantID:      tenantID,
		send:          ch,
	}
	return c, ch
}
