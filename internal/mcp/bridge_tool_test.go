package mcp

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

func TestInputSchemaToMap(t *testing.T) {
	schema := mcpgo.ToolInputSchema{
		Type: "object",
		Properties: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query",
			},
		},
		Required: []string{"query"},
	}

	m := inputSchemaToMap(schema)

	if m["type"] != "object" {
		t.Errorf("expected type=object, got %v", m["type"])
	}

	props, ok := m["properties"].(map[string]any)
	if !ok || props == nil {
		t.Fatal("expected properties map")
	}
	if _, ok := props["query"]; !ok {
		t.Error("expected 'query' in properties")
	}

	req, ok := m["required"].([]string)
	if !ok || len(req) != 1 || req[0] != "query" {
		t.Errorf("expected required=[query], got %v", m["required"])
	}
}

func TestInputSchemaToMap_EmptyType(t *testing.T) {
	schema := mcpgo.ToolInputSchema{}
	m := inputSchemaToMap(schema)

	if m["type"] != "object" {
		t.Errorf("expected default type=object, got %v", m["type"])
	}
}

func TestInputSchemaToMap_ObjectNoProperties(t *testing.T) {
	schema := mcpgo.ToolInputSchema{Type: "object"}
	m := inputSchemaToMap(schema)

	props, ok := m["properties"].(map[string]any)
	if !ok || props == nil {
		t.Fatal("expected empty properties map for object schema, got nil — OpenAI rejects object schemas without properties")
	}
	if len(props) != 0 {
		t.Errorf("expected empty properties, got %v", props)
	}
}

func TestExtractTextContent(t *testing.T) {
	result := &mcpgo.CallToolResult{
		Content: []mcpgo.Content{
			mcpgo.TextContent{Type: "text", Text: "hello"},
			mcpgo.TextContent{Type: "text", Text: "world"},
		},
	}

	got := extractTextContent(result)
	if got != "hello\nworld" {
		t.Errorf("expected 'hello\\nworld', got %q", got)
	}
}

func TestExtractTextContent_Nil(t *testing.T) {
	if got := extractTextContent(nil); got != "" {
		t.Errorf("expected empty for nil, got %q", got)
	}

	result := &mcpgo.CallToolResult{}
	if got := extractTextContent(result); got != "" {
		t.Errorf("expected empty for no content, got %q", got)
	}
}

func TestBridgeToolNaming(t *testing.T) {
	mcpTool := mcpgo.Tool{
		Name:        "query",
		Description: "Run a query",
		InputSchema: mcpgo.ToolInputSchema{Type: "object"},
	}

	// Without prefix → auto-derived from server name
	bt := NewBridgeTool("myserver", mcpTool, nil, "", 30, nil, uuid.Nil, nil)
	if bt.Name() != "mcp_myserver__query" {
		t.Errorf("expected name=mcp_myserver__query, got %s", bt.Name())
	}
	if bt.ServerName() != "myserver" {
		t.Errorf("expected serverName=myserver, got %s", bt.ServerName())
	}
	if bt.OriginalName() != "query" {
		t.Errorf("expected originalName=query, got %s", bt.OriginalName())
	}

	// With non-mcp_ prefix → gets mcp_ prepended
	bt2 := NewBridgeTool("myserver", mcpTool, nil, "pg", 0, nil, uuid.Nil, nil)
	if bt2.Name() != "mcp_pg__query" {
		t.Errorf("expected name=mcp_pg__query, got %s", bt2.Name())
	}
	if bt2.OriginalName() != "query" {
		t.Errorf("expected originalName=query, got %s", bt2.OriginalName())
	}

	// With mcp_ prefix → unchanged
	bt3 := NewBridgeTool("myserver", mcpTool, nil, "mcp_pg", 0, nil, uuid.Nil, nil)
	if bt3.Name() != "mcp_pg__query" {
		t.Errorf("expected name=mcp_pg__query, got %s", bt3.Name())
	}

	// Server name with hyphens → sanitized to underscores
	bt4 := NewBridgeTool("my-server", mcpTool, nil, "", 0, nil, uuid.Nil, nil)
	if bt4.Name() != "mcp_my_server__query" {
		t.Errorf("expected name=mcp_my_server__query, got %s", bt4.Name())
	}

	// Default timeout
	if bt2.timeoutSec != 60 {
		t.Errorf("expected default timeout=60, got %d", bt2.timeoutSec)
	}
}

func TestBridgeToolWithHints(t *testing.T) {
	mcpTool := mcpgo.Tool{
		Name:        "search",
		Description: "Run a search",
		InputSchema: mcpgo.ToolInputSchema{Type: "object"},
	}

	// No hints → original description unchanged
	bt := NewBridgeTool("srv", mcpTool, nil, "", 30, nil, uuid.Nil, nil)
	if bt.Description() != "Run a search" {
		t.Errorf("expected unchanged description, got %q", bt.Description())
	}

	// Global hint only
	bt2 := NewBridgeTool("srv", mcpTool, nil, "", 30, nil, uuid.Nil, nil).
		WithHints("No trailing semicolons.", "")
	got := bt2.Description()
	if got != "Run a search\n\n[Server hint] No trailing semicolons." {
		t.Errorf("global-only mismatch:\n%q", got)
	}

	// Per-tool hint only
	bt3 := NewBridgeTool("srv", mcpTool, nil, "", 30, nil, uuid.Nil, nil).
		WithHints("", "Use arrow func.")
	if bt3.Description() != "Run a search\n\n[Tool hint] Use arrow func." {
		t.Errorf("tool-only mismatch: %q", bt3.Description())
	}

	// Both hints — order: global then tool
	bt4 := NewBridgeTool("srv", mcpTool, nil, "", 30, nil, uuid.Nil, nil).
		WithHints("G.", "T.")
	if bt4.Description() != "Run a search\n\n[Server hint] G.\n\n[Tool hint] T." {
		t.Errorf("combined mismatch: %q", bt4.Description())
	}

	// Whitespace-only hints → treated as empty (no suffix)
	bt5 := NewBridgeTool("srv", mcpTool, nil, "", 30, nil, uuid.Nil, nil).
		WithHints("  \n ", "\t")
	if bt5.Description() != "Run a search" {
		t.Errorf("whitespace-only hints should render no suffix, got %q", bt5.Description())
	}

	// WithHints can be chained and reset by re-calling
	bt6 := NewBridgeTool("srv", mcpTool, nil, "", 30, nil, uuid.Nil, nil).
		WithHints("first", "hint")
	bt6.WithHints("", "")
	if bt6.Description() != "Run a search" {
		t.Errorf("calling WithHints with empty should clear suffix, got %q", bt6.Description())
	}
}

func TestIsPlaceholderValue(t *testing.T) {
	// Should be detected as placeholder.
	placeholders := []string{
		"null", "None", "nil", "UNDEFINED", "n/a",
		"optional", "Optional", "OPTIONAL",
		"skip", "Skip",
		"__OMIT__", "__skip__", "__EMPTY__",
		"http://example.com", "https://example.com",
		"http://localhost", "https://localhost",
		"PLACEHOLDER", "NOT_SET", "DO_NOT_SEND",
	}
	for _, s := range placeholders {
		if !isPlaceholderValue(s) {
			t.Errorf("expected isPlaceholderValue(%q) = true", s)
		}
	}

	// Should NOT be detected as placeholder (real values).
	realValues := []string{
		"", // empty string handled separately by type-aware check
		"sk-abc123",
		"my-proxy.example.com",
		"https://api.reviewweb.site/v1",
		"gpt-4o-mini",
		"bullet",
		"hello world",
		"ab", // too short for all-caps check
	}
	for _, s := range realValues {
		if isPlaceholderValue(s) {
			t.Errorf("expected isPlaceholderValue(%q) = false", s)
		}
	}
}

func TestStripEmptyOptionalArgs(t *testing.T) {
	bt := &BridgeTool{
		requiredSet: map[string]bool{"url": true},
		inputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":      map[string]any{"type": "string"},
				"api_key":  map[string]any{"type": "string"},
				"timeout":  map[string]any{"type": "number"},
				"debug":    map[string]any{"type": "boolean"},
				"keywords": map[string]any{"type": "string"},
			},
		},
	}

	args := map[string]any{
		"url":      "https://example.com",
		"api_key":  "optional", // placeholder → strip
		"timeout":  nil,        // nil → strip
		"debug":    true,       // real boolean → keep
		"keywords": "",         // empty string for string-typed → keep
	}

	cleaned := bt.stripEmptyOptionalArgs(args)

	if cleaned["url"] != "https://example.com" {
		t.Error("required param 'url' should be preserved")
	}
	if _, ok := cleaned["api_key"]; ok {
		t.Error("placeholder 'optional' should be stripped for api_key")
	}
	if _, ok := cleaned["timeout"]; ok {
		t.Error("nil should be stripped for timeout")
	}
	if cleaned["debug"] != true {
		t.Error("real boolean value should be preserved")
	}
	if v, ok := cleaned["keywords"]; !ok || v != "" {
		t.Error("empty string should be kept for string-typed optional param 'keywords'")
	}
}

func TestStripEmptyOptionalArgs_EmptyStringNonString(t *testing.T) {
	bt := &BridgeTool{
		requiredSet: map[string]bool{},
		inputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"timeout": map[string]any{"type": "number"},
				"count":   map[string]any{"type": "integer"},
			},
		},
	}

	args := map[string]any{
		"timeout": "",
		"count":   "",
	}

	cleaned := bt.stripEmptyOptionalArgs(args)

	if _, ok := cleaned["timeout"]; ok {
		t.Error("empty string should be stripped for number-typed param")
	}
	if _, ok := cleaned["count"]; ok {
		t.Error("empty string should be stripped for integer-typed param")
	}
}

func TestNormalizeArgsForSchema_CoercesStringifiedLarkTaskPatchArgs(t *testing.T) {
	bt := &BridgeTool{
		inputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"task_guid": map[string]any{"type": "string"},
					},
				},
				"params": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"user_id_type": map[string]any{"type": "string"},
					},
				},
				"data": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"task": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"completed_at": map[string]any{"type": "string"},
							},
						},
						"update_fields": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
	}

	args := map[string]any{
		"path":   `{"task_guid":"2d446187-084d-4d45-883b-5d238242b9fc"}`,
		"params": `{"user_id_type":"open_id"}`,
		"data":   `{"task":{"completed_at":"1782906226000"},"update_fields":["completed_at"]}`,
	}

	normalized, coercedPaths, err := bt.normalizeArgsForSchema(args)
	if err != nil {
		t.Fatalf("normalizeArgsForSchema returned error: %v", err)
	}

	pathArg, ok := normalized["path"].(map[string]any)
	if !ok {
		t.Fatalf("path should be object after normalization, got %T", normalized["path"])
	}
	if pathArg["task_guid"] != "2d446187-084d-4d45-883b-5d238242b9fc" {
		t.Errorf("unexpected task_guid: %v", pathArg["task_guid"])
	}

	dataArg, ok := normalized["data"].(map[string]any)
	if !ok {
		t.Fatalf("data should be object after normalization, got %T", normalized["data"])
	}
	taskArg, ok := dataArg["task"].(map[string]any)
	if !ok {
		t.Fatalf("data.task should be object after normalization, got %T", dataArg["task"])
	}
	if taskArg["completed_at"] != "1782906226000" {
		t.Errorf("unexpected completed_at: %v", taskArg["completed_at"])
	}

	wantPaths := map[string]bool{"$.path": true, "$.params": true, "$.data": true}
	for _, path := range coercedPaths {
		delete(wantPaths, path)
	}
	if len(wantPaths) != 0 {
		t.Errorf("missing coerced paths: %v; got %v", wantPaths, coercedPaths)
	}
}

func TestNormalizeArgsForSchema_CoercesNestedStringifiedContainers(t *testing.T) {
	bt := &BridgeTool{
		inputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"data": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"task": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"completed_at": map[string]any{"type": "string"},
							},
						},
						"update_fields": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
	}

	args := map[string]any{
		"data": map[string]any{
			"task":          `{"completed_at":"1782906226000"}`,
			"update_fields": `["completed_at"]`,
		},
	}

	normalized, coercedPaths, err := bt.normalizeArgsForSchema(args)
	if err != nil {
		t.Fatalf("normalizeArgsForSchema returned error: %v", err)
	}

	dataArg := normalized["data"].(map[string]any)
	if _, ok := dataArg["task"].(map[string]any); !ok {
		t.Fatalf("data.task should be object after normalization, got %T", dataArg["task"])
	}
	if fields, ok := dataArg["update_fields"].([]any); !ok || len(fields) != 1 || fields[0] != "completed_at" {
		t.Fatalf("update_fields should be array after normalization, got %#v", dataArg["update_fields"])
	}

	wantPaths := map[string]bool{"$.data.task": true, "$.data.update_fields": true}
	for _, path := range coercedPaths {
		delete(wantPaths, path)
	}
	if len(wantPaths) != 0 {
		t.Errorf("missing coerced paths: %v; got %v", wantPaths, coercedPaths)
	}
}

func TestNormalizeArgsForSchema_RejectsStringifiedContainerWithWrongShape(t *testing.T) {
	bt := &BridgeTool{
		inputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "object"},
			},
		},
	}

	_, _, err := bt.normalizeArgsForSchema(map[string]any{"path": `["not","object"]`})
	if err == nil {
		t.Fatal("expected error for array string when schema requires object")
	}
	if !strings.Contains(err.Error(), "$.path must be object") {
		t.Fatalf("expected actionable path/type error, got: %v", err)
	}
}

func TestNormalizeArgsForSchema_DoesNotCoerceStringTypedProperty(t *testing.T) {
	bt := &BridgeTool{
		inputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{"type": "string"},
			},
		},
	}

	args := map[string]any{"content": `{"literal":"json text"}`}
	normalized, coercedPaths, err := bt.normalizeArgsForSchema(args)
	if err != nil {
		t.Fatalf("normalizeArgsForSchema returned error: %v", err)
	}
	if normalized["content"] != `{"literal":"json text"}` {
		t.Errorf("string-typed property should remain unchanged, got %#v", normalized["content"])
	}
	if len(coercedPaths) != 0 {
		t.Errorf("string-typed property should not be coerced, got paths %v", coercedPaths)
	}
}

func TestEnsureMCPPrefix(t *testing.T) {
	tests := []struct {
		name       string
		prefix     string
		serverName string
		want       string
	}{
		{"empty prefix", "", "vnstock", "mcp_vnstock"},
		{"empty prefix hyphenated server", "", "my-server", "mcp_my_server"},
		{"non-mcp prefix", "pg", "postgres", "mcp_pg"},
		{"already mcp_ prefix", "mcp_pg", "postgres", "mcp_pg"},
		{"mcp prefix without underscore", "mcp", "x", "mcp_mcp"},
		{"custom prefix with underscores", "vnstock", "vnstock", "mcp_vnstock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ensureMCPPrefix(tt.prefix, tt.serverName)
			if got != tt.want {
				t.Errorf("ensureMCPPrefix(%q, %q) = %q, want %q", tt.prefix, tt.serverName, got, tt.want)
			}
		})
	}
}
