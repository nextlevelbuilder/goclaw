package clisession

import (
	"encoding/json"
	"testing"
)

// classifyLine is the tolerance surface: a line it cannot make sense of must be
// skipped, never fatal, and protocol plumbing must never be rendered as chat.
func TestClassifyLine(t *testing.T) {
	cases := []struct {
		name          string
		line          string
		wantControl   bool
		wantNarration bool
		wantRequestID string
		wantSubtype   string
	}{
		{name: "empty"},
		{name: "whitespace", line: "   \t  "},
		{name: "plain text is narration", line: "Building project…", wantNarration: true},
		{name: "json array is narration", line: `[1,2,3]`, wantNarration: true},
		{name: "malformed json is narration", line: `{"type":`, wantNarration: true},
		{name: "truncated json is narration", line: `{"type":"assistant","message":{`, wantNarration: true},
		{name: "assistant is narration", line: `{"type":"assistant","message":{"content":[]}}`, wantNarration: true},
		{name: "result is narration", line: `{"type":"result","result":"ok"}`, wantNarration: true},
		{name: "unknown future type is narration", line: `{"type":"something_new"}`, wantNarration: true},
		{name: "no type at all is narration", line: `{"hello":"world"}`, wantNarration: true},
		{
			// A control_response answers a request we never sent: plumbing, not chat.
			name: "control_response is skipped",
			line: `{"type":"control_response","response":{"subtype":"success","request_id":"r1"}}`,
		},
		{
			// We cannot answer a request with no id, and a guessed id gets dropped by
			// the CLI — so this is skipped rather than narrated.
			name: "control_request without an id is skipped",
			line: `{"type":"control_request","request":{"subtype":"can_use_tool"}}`,
		},
		{
			name: "control_request with an empty id is skipped",
			line: `{"type":"control_request","request_id":"","request":{"subtype":"can_use_tool"}}`,
		},
		{
			name:          "can_use_tool",
			line:          canUseToolLine("r-7", "Bash", `{"command":"ls"}`),
			wantControl:   true,
			wantRequestID: "r-7",
			wantSubtype:   subtypeCanUseTool,
		},
		{
			name:          "leading whitespace is tolerated",
			line:          "  " + canUseToolLine("r-8", "Read", `{"file_path":"/a"}`),
			wantControl:   true,
			wantRequestID: "r-8",
			wantSubtype:   subtypeCanUseTool,
		},
		{
			name:          "unknown subtype still needs an answer",
			line:          `{"type":"control_request","request_id":"r-9","request":{"subtype":"hook_callback"}}`,
			wantControl:   true,
			wantRequestID: "r-9",
			wantSubtype:   "hook_callback",
		},
		{
			// Fields we do not model must be ignored, not rejected.
			name:          "unknown fields are ignored",
			line:          `{"type":"control_request","request_id":"r-10","brand_new":1,"request":{"subtype":"can_use_tool","tool_name":"Bash","input":{},"matched_ask_rule":{"x":1}}}`,
			wantControl:   true,
			wantRequestID: "r-10",
			wantSubtype:   subtypeCanUseTool,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyLine(tc.line)

			if (got.Control != nil) != tc.wantControl {
				t.Fatalf("Control != nil = %v, want %v", got.Control != nil, tc.wantControl)
			}
			if got.Narration != tc.wantNarration {
				t.Errorf("Narration = %v, want %v", got.Narration, tc.wantNarration)
			}
			if got.Control != nil && got.Narration {
				t.Error("a control frame must never also be narration — it would be rendered as chat text")
			}
			if got.Control != nil {
				if got.Control.RequestID != tc.wantRequestID {
					t.Errorf("RequestID = %q, want %q", got.Control.RequestID, tc.wantRequestID)
				}
				if got.Control.Request.Subtype != tc.wantSubtype {
					t.Errorf("Subtype = %q, want %q", got.Control.Request.Subtype, tc.wantSubtype)
				}
			}
		})
	}
}

func TestClassifyLine_DecodesCanUseToolFields(t *testing.T) {
	got := classifyLine(canUseToolLine("r-1", "Bash", `{"command":"ls -la","timeout":5}`))
	if got.Control == nil {
		t.Fatal("expected a control frame")
	}
	body := got.Control.Request
	if body.ToolName != "Bash" {
		t.Errorf("ToolName = %q, want Bash", body.ToolName)
	}
	if string(body.Input) != `{"command":"ls -la","timeout":5}` {
		t.Errorf("Input = %s, want the object verbatim", body.Input)
	}
	if body.DisplayName != "Bash" {
		t.Errorf("DisplayName = %q, want Bash", body.DisplayName)
	}
	if body.Description != "List the workspace" {
		t.Errorf("Description = %q", body.Description)
	}
	if body.ToolUseID != "toolu_01" {
		t.Errorf("ToolUseID = %q, want toolu_01", body.ToolUseID)
	}
}

func TestIsJSONObject(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{`{"a":1}`, true},
		{` {"a":1} `, true},
		{`{"a":{"b":[1,2]}}`, true},
		{``, false},
		{`{}`, false}, // no fields to substitute
		{` { } `, false},
		{`null`, false},
		{`[]`, false},
		{`["a"]`, false},
		{`"a"`, false},
		{`42`, false},
		{`{`, false},
		{`{"a":}`, false},
	}
	for _, tc := range cases {
		if got := isJSONObject(json.RawMessage(tc.raw)); got != tc.want {
			t.Errorf("isJSONObject(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

// The outbound frames are what the live binary was verified to accept, so their
// shape is a contract: the doubly-nested `response` in particular.
func TestOutboundFrameShapes(t *testing.T) {
	t.Run("user message", func(t *testing.T) {
		line, err := encodeLine(newUserMessage("hi"))
		if err != nil {
			t.Fatalf("encodeLine: %v", err)
		}
		if want := `{"type":"user","message":{"role":"user","content":"hi"},"parent_tool_use_id":null}`; line != want {
			t.Errorf("\n got %s\nwant %s", line, want)
		}
	})

	t.Run("allow", func(t *testing.T) {
		line, err := encodeLine(newAllowResponse("r1", json.RawMessage(`{"command":"ls"}`)))
		if err != nil {
			t.Fatalf("encodeLine: %v", err)
		}
		want := `{"type":"control_response","response":{"subtype":"success","request_id":"r1","response":{"behavior":"allow","updatedInput":{"command":"ls"}}}}`
		if line != want {
			t.Errorf("\n got %s\nwant %s", line, want)
		}
	})

	t.Run("allow without a rewrite", func(t *testing.T) {
		line, err := encodeLine(newAllowResponse("r1", nil))
		if err != nil {
			t.Fatalf("encodeLine: %v", err)
		}
		want := `{"type":"control_response","response":{"subtype":"success","request_id":"r1","response":{"behavior":"allow"}}}`
		if line != want {
			t.Errorf("\n got %s\nwant %s", line, want)
		}
	})

	t.Run("deny", func(t *testing.T) {
		line, err := encodeLine(newDenyResponse("r2", "not from chat"))
		if err != nil {
			t.Fatalf("encodeLine: %v", err)
		}
		want := `{"type":"control_response","response":{"subtype":"success","request_id":"r2","response":{"behavior":"deny","message":"not from chat"}}}`
		if line != want {
			t.Errorf("\n got %s\nwant %s", line, want)
		}
	})

	t.Run("error", func(t *testing.T) {
		line, err := encodeLine(newErrorResponse("r3", "nope"))
		if err != nil {
			t.Fatalf("encodeLine: %v", err)
		}
		want := `{"type":"control_response","response":{"subtype":"error","request_id":"r3","error":"nope"}}`
		if line != want {
			t.Errorf("\n got %s\nwant %s", line, want)
		}
	})
}

// Every outbound frame must be exactly one line — the transport is line-delimited.
func TestOutboundFramesAreSingleLines(t *testing.T) {
	frames := []any{
		newUserMessage("multi\nline\nmessage"),
		newAllowResponse("r", json.RawMessage(`{"cmd":"echo a\nb"}`)),
		newDenyResponse("r", "reason\nwith a newline"),
		newErrorResponse("r", "err\nor"),
	}
	for i, f := range frames {
		line, err := encodeLine(f)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		for _, c := range line {
			if c == '\n' || c == '\r' {
				t.Errorf("frame %d contains a raw newline, which would split it into two frames: %s", i, line)
				break
			}
		}
		if !json.Valid([]byte(line)) {
			t.Errorf("frame %d is not valid JSON: %s", i, line)
		}
	}
}
