package mcp

import (
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ContextAwareRoundTripper reads MCP_RUNTIME_ACCESS_TOKEN and attaches it to the header.
type ContextAwareRoundTripper struct {
	Proxied http.RoundTripper
}

func (c *ContextAwareRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	token := os.Getenv("MCP_RUNTIME_ACCESS_TOKEN")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	agentID := store.AgentIDFromContext(req.Context())
	userID := store.UserIDFromContext(req.Context())

	if agentID != uuid.Nil {
		req.Header.Set("X-GoClaw-Agent-Id", agentID.String())
	}
	if userID != "" {
		req.Header.Set("X-GoClaw-User-Id", userID)
	}

	transport := c.Proxied
	if transport == nil {
		transport = http.DefaultTransport
	}
	return transport.RoundTrip(req)
}

