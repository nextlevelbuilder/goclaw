package agent

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestHasParseErrors_NoCalls(t *testing.T) {
	if hasParseErrors(nil) {
		t.Error("nil slice should return false")
	}
	if hasParseErrors([]providers.ToolCall{}) {
		t.Error("empty slice should return false")
	}
}

func TestHasParseErrors_AllValid(t *testing.T) {
	calls := []providers.ToolCall{
		{ID: "1", Name: "read_file", Arguments: map[string]any{"path": "/tmp"}},
		{ID: "2", Name: "exec", Arguments: map[string]any{"cmd": "ls"}},
	}
	if hasParseErrors(calls) {
		t.Error("valid tool calls should return false")
	}
}

func TestHasParseErrors_OneError(t *testing.T) {
	calls := []providers.ToolCall{
		{ID: "1", Name: "read_file", Arguments: map[string]any{"path": "/tmp"}},
		{ID: "2", Name: "write_file", ParseError: "malformed JSON (42 chars): unexpected end of JSON input"},
	}
	if !hasParseErrors(calls) {
		t.Error("should detect ParseError in second tool call")
	}
}

func TestHasParseErrors_AllErrors(t *testing.T) {
	calls := []providers.ToolCall{
		{ID: "1", Name: "write_file", ParseError: "truncated"},
		{ID: "2", Name: "exec", ParseError: "truncated"},
	}
	if !hasParseErrors(calls) {
		t.Error("should detect ParseError when all calls have errors")
	}
}
