//go:build integration

package ws_methods

import "testing"

// CONTRACT: config.get response MUST include config, hash, path.
// Requires owner + master scope.
func TestContract_WS_ConfigGet(t *testing.T) {
	wsURL, _ := getTestServer(t)
	client, _ := connect(t, wsURL, nil)

	resp := client.send(t, "config.get", map[string]any{})

	// Required fields
	assertField(t, resp, "config", "object")
	assertField(t, resp, "hash", "string")
	assertField(t, resp, "path", "string")
}
