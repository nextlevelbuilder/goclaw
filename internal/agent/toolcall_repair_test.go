package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// schemaMockTool is a registry tool with a fixed parameters schema.
type schemaMockTool struct {
	name   string
	params map[string]any
}

func (m *schemaMockTool) Name() string        { return m.name }
func (m *schemaMockTool) Description() string { return "mock tool for repair tests" }
func (m *schemaMockTool) Parameters() map[string]any {
	if m.params == nil {
		return map[string]any{"type": "object"}
	}
	return m.params
}
func (m *schemaMockTool) Execute(_ context.Context, _ map[string]any) *tools.Result {
	return &tools.Result{ForLLM: "ok"}
}

func mustRegisterRepairTool(t *testing.T, name string, params map[string]any) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	reg.Register(&schemaMockTool{name: name, params: params})
	return reg
}

func TestRepairStrayArgFields_ArgsToArguments(t *testing.T) {
	tc := providers.ToolCall{Name: "write_file", Arguments: map[string]any{
		"arg": map[string]any{"path": "a.txt", "content": "hi"},
	}}
	if !repairStrayArgFields(&tc) {
		t.Fatal("expected repair to run")
	}
	if _, ok := tc.Arguments["arg"]; ok {
		t.Error("stray 'arg' key still present")
	}
	if tc.Arguments["path"] != "a.txt" {
		t.Errorf("expected flattened path, got %v", tc.Arguments["path"])
	}
	if tc.Arguments["content"] != "hi" {
		t.Errorf("expected flattened content, got %v", tc.Arguments["content"])
	}
}

func TestRepairStrayArgFields_SingularAndPlural(t *testing.T) {
	for _, stray := range []string{"arg", "args"} {
		tc := providers.ToolCall{Name: "t", Arguments: map[string]any{
			stray: map[string]any{"x": 1},
		}}
		if !repairStrayArgFields(&tc) {
			t.Fatalf("expected repair for %q", stray)
		}
		if tc.Arguments["x"] != 1 {
			t.Errorf("expected x=1 after flatten, got %#v", tc.Arguments["x"])
		}
	}
}

func TestRepairStrayArgFields_StringArgsParsed(t *testing.T) {
	tc := providers.ToolCall{Name: "t", Arguments: map[string]any{
		"args": `{"path": "b.txt", "mode": "w"}`,
	}}
	if !repairStrayArgFields(&tc) {
		t.Fatal("expected repair to run")
	}
	if tc.Arguments["path"] != "b.txt" {
		t.Errorf("expected parsed path, got %v", tc.Arguments["path"])
	}
}

func TestRepairStrayArgFields_NonObjectValueWrapped(t *testing.T) {
	tc := providers.ToolCall{Name: "t", Arguments: map[string]any{
		"arg": "hello",
	}}
	if !repairStrayArgFields(&tc) {
		t.Fatal("expected repair to run")
	}
	// "hello" is not an object — kept as a single-key wrapper so the
	// arguments shape stays an object.
	v, ok := tc.Arguments["arg"].(string)
	if !ok || v != "hello" {
		t.Errorf("expected wrapped string, got %#v", tc.Arguments)
	}
}

func TestRepairStrayArgFields_NestedFlatten(t *testing.T) {
	tc := providers.ToolCall{Name: "exec", Arguments: map[string]any{
		"name":      "exec",
		"arguments": map[string]any{"cmd": "ls", "timeout": 5},
	}}
	if !repairStrayArgFields(&tc) {
		t.Fatal("expected nested flatten")
	}
	if _, ok := tc.Arguments["name"]; ok {
		t.Error("nested 'name' key leaked into flattened arguments")
	}
	if tc.Arguments["cmd"] != "ls" {
		t.Errorf("expected cmd=ls, got %v", tc.Arguments["cmd"])
	}
	if tc.Arguments["timeout"] != 5 {
		t.Errorf("expected timeout=5, got %#v", tc.Arguments["timeout"])
	}
}

func TestRepairStrayArgFields_NestedArgumentsWinsOverStray(t *testing.T) {
	// The deterministic decision: the nested "arguments" object IS the payload
	// — flatten it even when a stray "arg" also exists.
	tc := providers.ToolCall{Name: "t", Arguments: map[string]any{
		"arguments": map[string]any{"x": 1},
		"arg":       map[string]any{"x": 2},
	}}
	if !repairStrayArgFields(&tc) {
		t.Fatal("expected flatten of nested arguments")
	}
	if tc.Arguments["x"] != 1 {
		t.Errorf("expected nested arguments value, got %#v", tc.Arguments["x"])
	}
}

func TestRepairStrayArgFields_NoChangeForCleanArgs(t *testing.T) {
	tc := providers.ToolCall{Name: "t", Arguments: map[string]any{"x": 1}}
	if repairStrayArgFields(&tc) {
		t.Fatal("expected no repair for clean arguments")
	}
}

func TestRepairToolCallArgs_TruncatedRawRepaired(t *testing.T) {
	reg := mustRegisterRepairTool(t, "write_file", map[string]any{
		"type":     "object",
		"required": []string{"path", "content"},
		"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		},
	})
	l := &Loop{registry: reg}
	tc := providers.ToolCall{
		Name:       "write_file",
		ParseError: `malformed JSON (28 chars): unexpected end of JSON input`,
		Metadata:   map[string]string{toolCallRepairRawArgumentsKey: `{"path": "a.txt", "content": "hi`},
	}
	if !l.repairToolCallArgs(&tc) {
		t.Fatal("expected truncated raw args to be repaired")
	}
	if tc.ParseError != "" {
		t.Errorf("expected ParseError cleared, got %q", tc.ParseError)
	}
	if tc.Arguments["path"] != "a.txt" || tc.Arguments["content"] != "hi" {
		t.Errorf("unexpected repaired args: %#v", tc.Arguments)
	}
}

func TestRepairToolCallArgs_TruncatedRawSchemaMismatchKeepsError(t *testing.T) {
	reg := mustRegisterRepairTool(t, "write_file", map[string]any{
		"type":     "object",
		"required": []string{"path", "content"},
		"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		},
	})
	l := &Loop{registry: reg}
	tc := providers.ToolCall{
		Name:       "write_file",
		ParseError: "malformed JSON (24 chars): unexpected end of JSON input",
		Metadata:   map[string]string{toolCallRepairRawArgumentsKey: `{"path": "a.txt"`}, // missing required content
	}
	if l.repairToolCallArgs(&tc) {
		t.Fatal("expected no repair when repaired args miss schema-required fields")
	}
	if tc.ParseError == "" {
		t.Error("expected ParseError preserved")
	}
}

func TestRepairToolCallArgs_NoRawRetainedIsNoop(t *testing.T) {
	l := &Loop{}
	tc := providers.ToolCall{
		Name:       "write_file",
		ParseError: "malformed JSON (10 chars): unexpected end of JSON input",
	}
	if l.repairToolCallArgs(&tc) {
		t.Fatal("expected no repair without raw arguments")
	}
	if tc.ParseError == "" {
		t.Error("expected ParseError preserved")
	}
}

func TestRepairToolCallArgs_NilCallIsNoop(t *testing.T) {
	l := &Loop{}
	if l.repairToolCallArgs(nil) {
		t.Fatal("expected nil no-op")
	}
}

func TestRepairToolCallArgs_StrayFieldClearsError(t *testing.T) {
	l := &Loop{}
	tc := providers.ToolCall{
		Name:       "t",
		Arguments:  map[string]any{"args": map[string]any{"x": 1}},
		ParseError: "malformed JSON (11 chars): unexpected end of JSON input",
	}
	if !l.repairToolCallArgs(&tc) {
		t.Fatal("expected stray-field repair")
	}
	if tc.ParseError != "" {
		t.Errorf("expected ParseError cleared after field repair, got %q", tc.ParseError)
	}
}

func TestRepairLRUCache_SecondAttemptSameShapeSkipped(t *testing.T) {
	l := &Loop{}
	key := repairKey{toolName: "write_file", schemaHash: "abc"}
	if !l.repairAttemptAllowed(key, 20) {
		t.Fatal("first attempt should be allowed")
	}
	if l.repairAttemptAllowed(key, 20) {
		t.Fatal("second attempt with same raw length should be skipped (LRU guard)")
	}
	if !l.repairAttemptAllowed(key, 30) {
		t.Fatal("attempt with different raw length should be allowed")
	}
}

func TestRepairLRUCache_UnknownSchemaNotCached(t *testing.T) {
	l := &Loop{}
	key := repairKey{toolName: "write_file", schemaHash: ""}
	if !l.repairAttemptAllowed(key, 20) {
		t.Fatal("first attempt should be allowed")
	}
	if !l.repairAttemptAllowed(key, 20) {
		t.Fatal("unknown schema must not be cached — attempts stay allowed")
	}
}

func TestRepairLRUCache_CapacityEviction(t *testing.T) {
	l := &Loop{}
	cache := l.repairCache()
	cache.cap = 3
	cache.keys = nil
	cache.attrs = make(map[string]repairEntry)
	// Fill past capacity.
	for i := 0; i < 5; i++ {
		l.repairAttemptAllowed(repairKey{toolName: "t", schemaHash: string(rune('a' + i))}, i)
	}
	if len(cache.attrs) > 3 {
		t.Fatalf("cache exceeded capacity: %d entries", len(cache.attrs))
	}
	if _, ok := cache.attrs["t/a"]; ok {
		t.Error("oldest entry should have been evicted")
	}
}

func TestSchemaMatchesArgs_RequiredAndMissing(t *testing.T) {
	params := map[string]any{
		"type":     "object",
		"required": []string{"path"},
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
		},
	}
	if !schemaMatchesArgs(params, map[string]any{"path": "x"}) {
		t.Error("expected match when required field present")
	}
	if schemaMatchesArgs(params, map[string]any{}) {
		t.Error("expected mismatch when required field missing")
	}
	if !schemaMatchesArgs(map[string]any{"type": "object"}, map[string]any{}) {
		t.Error("expected pass when schema has no required list")
	}
}

func TestRepairToolCallArgs_IdempotentAcrossNormalizeCalls(t *testing.T) {
	reg := mustRegisterRepairTool(t, "write_file", map[string]any{
		"type":     "object",
		"required": []string{"path", "content"},
		"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		},
	})
	l := &Loop{registry: reg, id: "test-agent"}
	tc := providers.ToolCall{
		ID:         "tc-1",
		Name:       "write_file",
		Arguments:  map[string]any{"args": map[string]any{"path": "a.txt", "content": "hi"}},
		ParseError: "malformed JSON (11 chars): unexpected end of JSON input",
	}
	// normalizeToolCall is called at multiple sites per tool call (sequential
	// execute, parallel raw, process result) — repair must converge on the
	// first call and be a no-op afterwards.
	first := l.normalizeToolCall(tc)
	if first.Arguments["path"] != "a.txt" {
		t.Fatalf("first normalize did not repair: %#v", first.Arguments)
	}
	second := l.normalizeToolCall(first)
	if second.Arguments["path"] != "a.txt" || len(second.Arguments) != 2 {
		t.Fatalf("second normalize changed repaired call: %#v", second.Arguments)
	}
	third := l.normalizeToolCall(second)
	if third.Arguments["path"] != "a.txt" || len(third.Arguments) != 2 {
		t.Fatalf("third normalize changed repaired call: %#v", third.Arguments)
	}
}

func TestRepairJSON_ParsesValidWithoutChange(t *testing.T) {
	raw := []byte(`{"a": [1, 2, {"b": "c"}]}`)
	repaired, ok := repairJSON(raw)
	if !ok {
		t.Fatal("expected parse success")
	}
	var a, b map[string]any
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(repaired, &b); err != nil {
		t.Fatal(err)
	}
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	if string(ra) != string(rb) {
		t.Errorf("repair changed a valid payload: %s → %s", ra, rb)
	}
}
