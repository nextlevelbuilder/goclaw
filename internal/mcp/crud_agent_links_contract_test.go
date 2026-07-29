package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestAgentLinkMaxConcurrentSchemaIsCompatibilityMetadata(t *testing.T) {
	srv := newTestMCPServer()
	var links store.AgentLinkStore
	registerAgentLinkCRUDTools(srv, links)

	for _, toolName := range []string{
		"goclaw_agent_links_create",
		"goclaw_agent_links_update",
	} {
		registered := srv.GetTool(toolName)
		if registered == nil {
			t.Fatalf("%s was not registered", toolName)
		}
		property, ok := registered.Tool.InputSchema.Properties["max_concurrent"]
		if !ok {
			t.Fatalf("%s omitted max_concurrent compatibility field", toolName)
		}
		encoded, err := json.Marshal(property)
		if err != nil {
			t.Fatalf("marshal %s max_concurrent schema: %v", toolName, err)
		}
		description := strings.ToLower(string(encoded))
		if !strings.Contains(description, "compatibility metadata") ||
			!strings.Contains(description, "not enforced") {
			t.Fatalf("%s exposes a misleading max_concurrent contract: %s", toolName, encoded)
		}
	}
}
