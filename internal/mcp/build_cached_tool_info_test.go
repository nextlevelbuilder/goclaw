package mcp

import (
	"encoding/json"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// TestBuildCachedToolInfo_CapturesRealSchema proves that buildCachedToolInfo
// (used by both connectServer and connectViaPool's caching goroutines in
// manager_connect.go) captures the tool's real input JSON Schema alongside
// its description, not just a bare description string.
func TestBuildCachedToolInfo_CapturesRealSchema(t *testing.T) {
	mcpTools := []mcpgo.Tool{
		{
			Name:        "pg_query",
			Description: "Run PostgreSQL queries",
			InputSchema: mcpgo.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"query": map[string]any{"type": "string"},
				},
				Required: []string{"query"},
			},
		},
	}

	got := buildCachedToolInfo(mcpTools)

	entry, ok := got["pg_query"]
	if !ok {
		t.Fatalf("expected pg_query entry, got %+v", got)
	}
	if entry.Description != "Run PostgreSQL queries" {
		t.Errorf("unexpected description: %q", entry.Description)
	}
	if len(entry.Parameters) == 0 {
		t.Fatal("expected non-empty Parameters schema")
	}

	var schema map[string]any
	if err := json.Unmarshal(entry.Parameters, &schema); err != nil {
		t.Fatalf("unmarshal parameters: %v", err)
	}
	if schema["type"] != "object" {
		t.Errorf("expected type=object, got %v", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties in schema, got %+v", schema)
	}
	if _, ok := props["query"]; !ok {
		t.Errorf("expected 'query' property in schema, got %+v", props)
	}
	required, ok := schema["required"].([]any)
	if !ok || len(required) != 1 || required[0] != "query" {
		t.Errorf("expected required=[query], got %v", schema["required"])
	}
}

// TestBuildCachedToolInfo_EmptySchemaFallsBackToObject proves that a tool
// with no declared properties still gets a valid (non-nil) object schema,
// matching the same convention used by inputSchemaToMap for the live path.
func TestBuildCachedToolInfo_EmptySchemaFallsBackToObject(t *testing.T) {
	mcpTools := []mcpgo.Tool{
		{Name: "no_args_tool", Description: "Takes no arguments"},
	}

	got := buildCachedToolInfo(mcpTools)

	entry, ok := got["no_args_tool"]
	if !ok {
		t.Fatalf("expected no_args_tool entry, got %+v", got)
	}
	var schema map[string]any
	if err := json.Unmarshal(entry.Parameters, &schema); err != nil {
		t.Fatalf("unmarshal parameters: %v", err)
	}
	if schema["type"] != "object" {
		t.Errorf("expected type=object fallback, got %v", schema["type"])
	}
}
