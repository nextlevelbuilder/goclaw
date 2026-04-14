package mcp

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// --- mapToEnvSlice ---

func TestMapToEnvSlice_Empty(t *testing.T) {
	got := mapToEnvSlice(nil)
	if got != nil {
		t.Errorf("nil map should return nil, got %v", got)
	}
	got = mapToEnvSlice(map[string]string{})
	if got != nil {
		t.Errorf("empty map should return nil, got %v", got)
	}
}

func TestMapToEnvSlice_Singles(t *testing.T) {
	got := mapToEnvSlice(map[string]string{"FOO": "bar"})
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	if got[0] != "FOO=bar" {
		t.Errorf("expected FOO=bar, got %q", got[0])
	}
}

func TestMapToEnvSlice_MultipleEntries(t *testing.T) {
	in := map[string]string{"A": "1", "B": "2", "C": "3"}
	got := mapToEnvSlice(in)
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}
	// All should be in KEY=VALUE format
	set := make(map[string]bool)
	for _, e := range got {
		set[e] = true
	}
	for _, want := range []string{"A=1", "B=2", "C=3"} {
		if !set[want] {
			t.Errorf("missing %q in output %v", want, got)
		}
	}
}

// --- toSet ---

func TestToSet_Nil(t *testing.T) {
	got := toSet(nil)
	if got != nil {
		t.Errorf("nil slice should return nil, got %v", got)
	}
}

func TestToSet_Empty(t *testing.T) {
	got := toSet([]string{})
	if got != nil {
		t.Errorf("empty slice should return nil, got %v", got)
	}
}

func TestToSet_Values(t *testing.T) {
	got := toSet([]string{"a", "b", "c"})
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}
	for _, v := range []string{"a", "b", "c"} {
		if _, ok := got[v]; !ok {
			t.Errorf("missing %q in set", v)
		}
	}
}

func TestToSet_Deduplication(t *testing.T) {
	got := toSet([]string{"a", "a", "b"})
	if len(got) != 2 {
		t.Errorf("expected 2 unique entries, got %d", len(got))
	}
}

// --- joinErrors ---

func TestJoinErrors_Empty(t *testing.T) {
	got := joinErrors(nil)
	if got != "" {
		t.Errorf("empty slice should return empty string, got %q", got)
	}
}

func TestJoinErrors_Single(t *testing.T) {
	got := joinErrors([]string{"error one"})
	if got != "error one" {
		t.Errorf("single error: got %q", got)
	}
}

func TestJoinErrors_Multiple(t *testing.T) {
	got := joinErrors([]string{"err1", "err2", "err3"})
	want := "err1; err2; err3"
	if got != want {
		t.Errorf("joinErrors: got %q, want %q", got, want)
	}
}

// --- jsonBytesToStringSlice ---

func TestJSONBytesToStringSlice_Valid(t *testing.T) {
	data, _ := json.Marshal([]string{"a", "b", "c"})
	got := jsonBytesToStringSlice(data)
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("got %v", got)
	}
}

func TestJSONBytesToStringSlice_Empty(t *testing.T) {
	got := jsonBytesToStringSlice(nil)
	if got != nil {
		t.Errorf("nil input should return nil, got %v", got)
	}
	got = jsonBytesToStringSlice([]byte{})
	if got != nil {
		t.Errorf("empty input should return nil, got %v", got)
	}
}

func TestJSONBytesToStringSlice_Invalid(t *testing.T) {
	got := jsonBytesToStringSlice([]byte(`{not valid json`))
	if got != nil {
		t.Errorf("invalid JSON should return nil, got %v", got)
	}
}

// --- jsonBytesToStringMap ---

func TestJSONBytesToStringMap_Valid(t *testing.T) {
	data, _ := json.Marshal(map[string]string{"key": "val", "foo": "bar"})
	got := jsonBytesToStringMap(data)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got["key"] != "val" {
		t.Errorf("key: got %q", got["key"])
	}
	if got["foo"] != "bar" {
		t.Errorf("foo: got %q", got["foo"])
	}
}

func TestJSONBytesToStringMap_Empty(t *testing.T) {
	got := jsonBytesToStringMap(nil)
	if got != nil {
		t.Errorf("nil input should return nil, got %v", got)
	}
}

func TestJSONBytesToStringMap_Invalid(t *testing.T) {
	got := jsonBytesToStringMap([]byte(`[not a map]`))
	if got != nil {
		t.Errorf("invalid JSON should return nil, got %v", got)
	}
}

// resolveEnvVars tests are in manager_env_test.go (covers nil, env:, missing cases).

// --- requireUserCreds ---

func TestRequireUserCreds_Nil(t *testing.T) {
	if requireUserCreds(nil) {
		t.Error("nil settings should not require user creds")
	}
}

func TestRequireUserCreds_Empty(t *testing.T) {
	if requireUserCreds(json.RawMessage(`{}`)) {
		t.Error("empty settings should not require user creds")
	}
}

func TestRequireUserCreds_True(t *testing.T) {
	settings := json.RawMessage(`{"require_user_credentials": true}`)
	if !requireUserCreds(settings) {
		t.Error("should require user credentials")
	}
}

func TestRequireUserCreds_False(t *testing.T) {
	settings := json.RawMessage(`{"require_user_credentials": false}`)
	if requireUserCreds(settings) {
		t.Error("should not require user credentials when false")
	}
}

func TestRequireUserCreds_InvalidJSON(t *testing.T) {
	// Invalid JSON → returns false (safe default)
	if requireUserCreds(json.RawMessage(`{invalid}`)) {
		t.Error("invalid JSON should return false")
	}
}

// --- mcpBM25Index ---

func TestMCPBM25Index_EmptyIndex(t *testing.T) {
	idx := newMCPBM25Index()
	results := idx.search("anything", 5)
	if len(results) != 0 {
		t.Errorf("empty index should return no results, got %d", len(results))
	}
	if idx.docCount() != 0 {
		t.Errorf("empty index docCount should be 0, got %d", idx.docCount())
	}
}

func TestMCPBM25Index_Build_SingleTool(t *testing.T) {
	idx := newMCPBM25Index()
	bt := makeBridgeToolWithDesc("myserver", "search_web", "Search the web for information")
	idx.build([]*BridgeTool{bt})

	if idx.docCount() != 1 {
		t.Errorf("expected 1 doc, got %d", idx.docCount())
	}
}

func TestMCPBM25Index_Search_ExactMatch(t *testing.T) {
	idx := newMCPBM25Index()
	tools := []*BridgeTool{
		makeBridgeToolWithDesc("server1", "search_web", "Search the web using DuckDuckGo"),
		makeBridgeToolWithDesc("server2", "run_code", "Execute Python code in sandbox"),
		makeBridgeToolWithDesc("server3", "read_file", "Read file from filesystem"),
	}
	idx.build(tools)

	results := idx.search("web search", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'web search'")
	}
	if results[0].ServerName != "server1" {
		t.Errorf("expected server1 first, got %q", results[0].ServerName)
	}
}

func TestMCPBM25Index_Search_ZeroResults(t *testing.T) {
	idx := newMCPBM25Index()
	idx.build([]*BridgeTool{
		makeBridgeToolWithDesc("s", "tool", "does something"),
	})
	results := idx.search("xyzzy_unknown_query_abc", 5)
	if len(results) != 0 {
		t.Errorf("unknown query should return 0 results, got %d", len(results))
	}
}

func TestMCPBM25Index_Search_EmptyQuery(t *testing.T) {
	idx := newMCPBM25Index()
	idx.build([]*BridgeTool{
		makeBridgeToolWithDesc("s", "tool", "description"),
	})
	results := idx.search("", 5)
	if len(results) != 0 {
		t.Errorf("empty query should return 0 results, got %d", len(results))
	}
}

func TestMCPBM25Index_Search_MaxResultsRespected(t *testing.T) {
	idx := newMCPBM25Index()
	tools := make([]*BridgeTool, 10)
	for i := range tools {
		tools[i] = makeBridgeToolWithDesc("server", "search_tool", "search web data tool")
	}
	idx.build(tools)

	results := idx.search("search", 3)
	if len(results) > 3 {
		t.Errorf("maxResults=3 exceeded, got %d", len(results))
	}
}

func TestMCPBM25Index_Search_ResultFields(t *testing.T) {
	idx := newMCPBM25Index()
	bt := makeBridgeToolWithDesc("my-server", "query_db", "Query the database")
	bt.registeredName = "mcp_my_server__query_db"
	idx.build([]*BridgeTool{bt})

	results := idx.search("query database", 5)
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	r := results[0]
	if r.OriginalName != "query_db" {
		t.Errorf("OriginalName: got %q", r.OriginalName)
	}
	if r.ServerName != "my-server" {
		t.Errorf("ServerName: got %q", r.ServerName)
	}
	if r.RegisteredName != "mcp_my_server__query_db" {
		t.Errorf("RegisteredName: got %q", r.RegisteredName)
	}
}

func TestMCPBM25Index_Rebuild(t *testing.T) {
	idx := newMCPBM25Index()
	idx.build([]*BridgeTool{
		makeBridgeToolWithDesc("s", "old_tool", "old description search"),
	})

	// Rebuild with new tools
	idx.build([]*BridgeTool{
		makeBridgeToolWithDesc("s", "new_tool", "new description query"),
	})

	results := idx.search("old", 5)
	if len(results) != 0 {
		t.Errorf("after rebuild, old tools should be gone, got %d", len(results))
	}
	results = idx.search("new", 5)
	if len(results) == 0 {
		t.Error("after rebuild, new tools should be searchable")
	}
}

// --- tokenizeMCP ---

func TestTokenizeMCP_BasicTokenization(t *testing.T) {
	tests := []struct {
		input  string
		expect []string
	}{
		{"search web", []string{"search", "web"}},
		{"Search Web", []string{"search", "web"}},         // lowercased
		{"query_db tool", []string{"query", "db", "tool"}}, // underscore as separator
		{"", nil},
		{"a b c", nil}, // single-char tokens filtered
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := tokenizeMCP(tt.input)
			if len(got) != len(tt.expect) {
				t.Errorf("tokenizeMCP(%q) = %v, want %v", tt.input, got, tt.expect)
				return
			}
			for i, tok := range got {
				if tok != tt.expect[i] {
					t.Errorf("tokenizeMCP(%q)[%d] = %q, want %q", tt.input, i, tok, tt.expect[i])
				}
			}
		})
	}
}

// --- Pool key helpers ---

func TestPoolKey(t *testing.T) {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	key := poolKey(id, "my-server")
	if key != "00000000-0000-0000-0000-000000000001/my-server" {
		t.Errorf("poolKey: got %q", key)
	}
}

func TestUserPoolKey(t *testing.T) {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	key := UserPoolKey(id, "server", "user123")
	if key != "00000000-0000-0000-0000-000000000001/server/user:user123" {
		t.Errorf("UserPoolKey: got %q", key)
	}
}

// --- Pool: NewPool / Stop ---

func TestNewPool_Defaults(t *testing.T) {
	p := NewPool(PoolConfig{})
	// Should start without panic and stop cleanly
	t.Cleanup(func() {
		p.Stop()
	})
	// Default config applied
	if p.cfg.MaxSize != 200 {
		t.Errorf("default MaxSize: got %d", p.cfg.MaxSize)
	}
	if p.cfg.MaxIdle != 20 {
		t.Errorf("default MaxIdle: got %d", p.cfg.MaxIdle)
	}
}

func TestPool_Release_NonExistent(t *testing.T) {
	p := NewPool(PoolConfig{MaxSize: 5})
	t.Cleanup(func() { p.Stop() })
	// Release on non-existent key should not panic
	p.Release("nonexistent/key")
}

func TestPool_ReleaseUser_NonExistent(t *testing.T) {
	p := NewPool(PoolConfig{MaxSize: 5})
	t.Cleanup(func() { p.Stop() })
	p.ReleaseUser("nonexistent/key")
}

func TestPool_Stop_EmptyPool(t *testing.T) {
	p := NewPool(PoolConfig{MaxSize: 5})
	// Stop on empty pool should not panic
	p.Stop()
}

// --- Manager: NewManager / Stop ---

func TestNewManager_Empty(t *testing.T) {
	m := NewManager(nil)
	if m == nil {
		t.Fatal("NewManager should not return nil")
	}
	if m.servers == nil {
		t.Error("servers map should be initialized")
	}
}

func TestManager_Stop_Empty(t *testing.T) {
	m := NewManager(nil)
	// Stop on empty manager should not panic
	m.Stop()
}

func TestManager_ServerStatus_Empty(t *testing.T) {
	m := NewManager(nil)
	statuses := m.ServerStatus()
	if len(statuses) != 0 {
		t.Errorf("empty manager should have 0 server statuses, got %d", len(statuses))
	}
}

func TestManager_IsSearchMode_Default(t *testing.T) {
	m := NewManager(nil)
	if m.IsSearchMode() {
		t.Error("new manager should not be in search mode")
	}
}

func TestManager_DeferredToolInfos_Empty(t *testing.T) {
	m := NewManager(nil)
	tools := m.DeferredToolInfos()
	if len(tools) != 0 {
		t.Errorf("empty manager should have 0 deferred tools, got %d", len(tools))
	}
}

// --- helpers ---

// makeBridgeTool builds a minimal BridgeTool for testing.
func makeBridgeToolWithDesc(serverName, toolName, description string) *BridgeTool {
	return &BridgeTool{
		serverName:     serverName,
		toolName:       toolName,
		registeredName: "mcp_" + serverName + "__" + toolName,
		description:    description,
		inputSchema:    map[string]any{"type": "object", "properties": map[string]any{}},
		requiredSet:    map[string]bool{},
	}
}

