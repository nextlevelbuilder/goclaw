package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPairingDeviceRequest_HappyAndInvalidSenderID(t *testing.T) {
	pairing := newFakePairingStore()
	srv := newTestMCPServer()
	registerPairingCRUDTools(srv, pairing)

	result := callTool(t, srv, "goclaw_pairing_device_request", map[string]any{
		"sender_id": "user-123", "channel": "telegram",
	})
	require.False(t, toolIsError(result), toolResultText(result))
	assert.Contains(t, toolResultText(result), "code")

	invalid := callTool(t, srv, "goclaw_pairing_device_request", map[string]any{
		"sender_id": "; rm -rf /", "channel": "telegram",
	})
	assert.True(t, toolIsError(invalid))
	assert.Contains(t, toolResultText(invalid), "invalid sender_id format")
}

func TestPairingDeviceApproveDenyList(t *testing.T) {
	pairing := newFakePairingStore()
	srv := newTestMCPServer()
	registerPairingCRUDTools(srv, pairing)

	req := callTool(t, srv, "goclaw_pairing_device_request", map[string]any{"sender_id": "user-1", "channel": "telegram"})
	require.False(t, toolIsError(req))

	approved := callTool(t, srv, "goclaw_pairing_device_approve", map[string]any{"code": "CODE123"})
	require.False(t, toolIsError(approved))

	list := callTool(t, srv, "goclaw_pairing_device_list", map[string]any{})
	require.False(t, toolIsError(list))
	assert.Contains(t, toolResultText(list), "user-1")

	deny := callTool(t, srv, "goclaw_pairing_device_deny", map[string]any{"code": "no-such-code"})
	assert.True(t, toolIsError(deny))
}

func TestPairingDeviceRevoke(t *testing.T) {
	pairing := newFakePairingStore()
	srv := newTestMCPServer()
	registerPairingCRUDTools(srv, pairing)

	// Not paired: revoke should fail.
	result := callTool(t, srv, "goclaw_pairing_device_revoke", map[string]any{"sender_id": "user-9", "channel": "telegram"})
	assert.True(t, toolIsError(result))
}

func TestPairingBrowserStatus_Expired(t *testing.T) {
	pairing := newFakePairingStore()
	srv := newTestMCPServer()
	registerPairingCRUDTools(srv, pairing)

	result := callTool(t, srv, "goclaw_pairing_browser_status", map[string]any{"sender_id": "user-1"})
	require.False(t, toolIsError(result))
	assert.Contains(t, toolResultText(result), "expired")
}
