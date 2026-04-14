//go:build integration

package ws_methods

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// CONTRACT: connect response MUST include these fields with correct types.
// Breaking changes require protocol version bump.
func TestContract_WS_Connect(t *testing.T) {
	wsURL, _ := getTestServer(t)
	_, resp := connect(t, wsURL, nil)

	// Required fields
	assertField(t, resp, "protocol", "number")
	assertField(t, resp, "role", "string")
	assertField(t, resp, "user_id", "string")
	assertField(t, resp, "tenant_id", "string")
	assertField(t, resp, "is_owner", "bool")

	// Nested server object
	assertField(t, resp, "server", "object")
	assertField(t, resp, "server.name", "string")
	assertField(t, resp, "server.version", "string")

	// Value checks
	assertFieldValue(t, resp, "protocol", float64(protocol.ProtocolVersion))
	assertFieldValue(t, resp, "server.name", "goclaw")
}
