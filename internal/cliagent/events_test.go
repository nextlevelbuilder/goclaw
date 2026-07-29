package cliagent

import (
	"strings"
	"testing"
)

// runLines feeds a fixture through a parser the way the sandbox feeds stdout —
// one line at a time — and returns every event in order.
func runLines(p Parser, lines []string) []Event {
	var out []Event
	for _, l := range lines {
		out = append(out, p.ParseLine(l)...)
	}
	return out
}

func assertEvents(t *testing.T, got, want []Event) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("event count = %d, want %d\ngot:  %s\nwant: %s", len(got), len(want), format(got), format(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Kind != w.Kind || g.Text != w.Text || g.ToolID != w.ToolID || g.ToolName != w.ToolName ||
			g.ToolResult != w.ToolResult || g.IsError != w.IsError || string(g.ToolInput) != string(w.ToolInput) {
			t.Errorf("event %d mismatch\n got: %+v\nwant: %+v", i, g, w)
		}
	}
}

func format(evs []Event) string {
	var b strings.Builder
	for i, e := range evs {
		if i > 0 {
			b.WriteString(" | ")
		}
		b.WriteString(string(e.Kind))
		switch e.Kind {
		case EventToolCall, EventToolResult:
			b.WriteString("(" + e.ToolName + "/" + e.ToolID + ")")
		default:
			b.WriteString("(" + strings.ReplaceAll(e.Text, "\n", `\n`) + ")")
		}
	}
	return b.String()
}

// claudeFixture is a realistic Claude Code --output-format stream-json
// --include-partial-messages transcript: init, token deltas, the duplicate
// whole-block assistant text, a tool_use / tool_result pair, and the terminal
// result event (whose cost/usage fields we must ignore).
var claudeFixture = []string{
	`{"type":"system","subtype":"init","session_id":"s1","tools":["Read","Edit"]}`,
	`{"type":"stream_event","event":{"type":"message_start","message":{"id":"m1"}}}`,
	`{"type":"stream_event","event":{"type":"content_block_start","index":0}}`,
	`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Looking at "}}}`,
	`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"the repository structure now"}}}`,
	`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" to plan."}}}`,
	`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"file_"}}}`,
	`{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`,
	`{"type":"assistant","message":{"content":[{"type":"text","text":"Looking at the repository structure now to plan."},{"type":"tool_use","id":"toolu_01","name":"Read","input":{"file_path":"/repo/main.go"}}]}}`,
	`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_01","content":[{"type":"text","text":"package main"}]}]}}`,
	`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Done.\n"}}}`,
	`{"type":"assistant","message":{"content":[{"type":"text","text":"Done."}]}}`,
	``,
	`not json at all`,
	`{"type":"result","subtype":"success","is_error":false,"result":"Ported 3 files.","total_cost_usd":0.42,"usage":{"input_tokens":100,"output_tokens":20}}`,
}

func TestClaudeParserFixture(t *testing.T) {
	p := NewParser(OutputClaudeStreamJSON)
	got := runLines(p, claudeFixture)

	assertEvents(t, got, []Event{
		// Deltas coalesced: flushed exactly when the buffer reaches deltaFlushBytes.
		{Kind: EventText, Text: "Looking at the repository structure now to plan."},
		// Whole-block assistant text is SUPPRESSED (deltas already streamed it);
		// only the tool_use survives from that event.
		{Kind: EventToolCall, ToolID: "toolu_01", ToolName: "Read", ToolInput: []byte(`{"file_path":"/repo/main.go"}`)},
		{Kind: EventToolResult, ToolID: "toolu_01", ToolName: "Read", ToolResult: "package main"},
		// A newline in a delta flushes immediately, without waiting for 48 bytes.
		{Kind: EventText, Text: "Done.\n"},
		{Kind: EventFinal, Text: "Ported 3 files."},
	})

	text, isErr := p.Final()
	if text != "Ported 3 files." || isErr {
		t.Fatalf("Final() = %q, %v", text, isErr)
	}
}

func TestClaudeParserWithoutPartialMessages(t *testing.T) {
	// Older CLI (no --include-partial-messages): the whole-block text is the ONLY
	// narration, so it must be emitted, with a trailing newline between turns.
	p := NewParser(OutputClaudeStreamJSON)
	got := runLines(p, []string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"  First I will read the file.  "}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":""},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"ls: no such file"}]}}`,
		`{"type":"result","is_error":true,"result":"failed"}`,
	})
	assertEvents(t, got, []Event{
		{Kind: EventText, Text: "First I will read the file.\n"},
		{Kind: EventToolCall, ToolID: "t1", ToolName: "Bash", ToolInput: []byte(`{"command":"ls"}`)},
		{Kind: EventToolResult, ToolID: "t1", ToolName: "Bash", ToolResult: "ls: no such file", IsError: true},
		{Kind: EventFinal, Text: "failed", IsError: true},
	})
	if text, isErr := p.Final(); text != "failed" || !isErr {
		t.Fatalf("Final() = %q, %v", text, isErr)
	}
}

func TestClaudeParserIgnoresNoise(t *testing.T) {
	p := NewParser(OutputClaudeStreamJSON)
	got := runLines(p, []string{"", "   ", "hello", `["array"]`, `{"broken":`, `{"type":"unknown_event"}`})
	if len(got) != 0 {
		t.Fatalf("expected no events, got %s", format(got))
	}
	if text, isErr := p.Final(); text != "" || isErr {
		t.Fatalf("Final() = %q, %v", text, isErr)
	}
}

// TestClaudeParserFlushOnKilledRun: a run killed at the timeout never emits a
// result event, so the buffered narration is only reachable via Flush.
func TestClaudeParserFlushOnKilledRun(t *testing.T) {
	p := NewParser(OutputClaudeStreamJSON)
	got := runLines(p, []string{
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"partial"}}}`,
	})
	if len(got) != 0 {
		t.Fatalf("short delta should stay buffered, got %s", format(got))
	}
	f, ok := p.(Flusher)
	if !ok {
		t.Fatal("claude parser must implement Flusher")
	}
	assertEvents(t, f.Flush(), []Event{{Kind: EventText, Text: "partial"}})
	if len(f.Flush()) != 0 {
		t.Error("second Flush should be empty")
	}
}

func TestJSONLParser(t *testing.T) {
	tests := []struct {
		name      string
		lines     []string
		want      []Event
		wantFinal string
		wantErr   bool
	}{
		{
			name:      "nested item with text",
			lines:     []string{`{"type":"item.completed","item":{"type":"agent_message","text":"Hello there"}}`},
			want:      []Event{{Kind: EventText, Text: "Hello there"}},
			wantFinal: "Hello there",
		},
		{
			name:  "tool call with arguments",
			lines: []string{`{"type":"tool_call","tool_name":"shell","call_id":"c1","arguments":{"command":"ls"}}`},
			want:  []Event{{Kind: EventToolCall, ToolID: "c1", ToolName: "shell", ToolInput: []byte(`{"command":"ls"}`)}},
		},
		{
			name:  "tool result",
			lines: []string{`{"type":"tool_result","tool_use_id":"c1","tool_name":"shell","result":"a\nb","is_error":false}`},
			want:  []Event{{Kind: EventToolResult, ToolID: "c1", ToolName: "shell", ToolResult: "a\nb"}},
		},
		{
			name:      "content block array",
			lines:     []string{`{"type":"message","content":[{"type":"text","text":"hi"}]}`},
			want:      []Event{{Kind: EventText, Text: "hi"}},
			wantFinal: "hi",
		},
		{
			name:      "explicit final wins over accumulated text",
			lines:     []string{`{"type":"message","text":"working"}`, `{"type":"final","text":"All done"}`},
			want:      []Event{{Kind: EventText, Text: "working"}, {Kind: EventFinal, Text: "All done"}},
			wantFinal: "All done",
		},
		{
			name:      "error flag is remembered",
			lines:     []string{`{"type":"error","error":"boom"}`},
			want:      nil,
			wantFinal: "",
			wantErr:   true,
		},
		{
			name:      "unrecognised shapes are ignored, not guessed",
			lines:     []string{`{"type":"token_count","usage":{"input":10}}`, `plain text line`, ``, `{"nope":`},
			want:      nil,
			wantFinal: "",
		},
		{
			name: "text accumulates across lines",
			lines: []string{
				`{"text":"one"}`,
				`{"text":"two"}`,
			},
			want:      []Event{{Kind: EventText, Text: "one"}, {Kind: EventText, Text: "two"}},
			wantFinal: "one\ntwo",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewParser(OutputJSONL)
			assertEvents(t, runLines(p, tc.lines), tc.want)
			text, isErr := p.Final()
			if text != tc.wantFinal || isErr != tc.wantErr {
				t.Fatalf("Final() = %q, %v; want %q, %v", text, isErr, tc.wantFinal, tc.wantErr)
			}
		})
	}
}

func TestTextParser(t *testing.T) {
	p := NewParser(OutputText)
	got := runLines(p, []string{"Applied edit to main.go", "", "   ", "Tokens: 1.2k sent", "Done\r"})
	assertEvents(t, got, []Event{
		{Kind: EventText, Text: "Applied edit to main.go\n"},
		{Kind: EventText, Text: "Tokens: 1.2k sent\n"},
		{Kind: EventText, Text: "Done\n"},
	})
	text, isErr := p.Final()
	if text != "Applied edit to main.go\nTokens: 1.2k sent\nDone" || isErr {
		t.Fatalf("Final() = %q, %v", text, isErr)
	}
}

// TestNewParserFallback: an unexpected format must still surface output rather
// than silently dropping the whole run (Resolve rejects unknown formats anyway).
func TestNewParserFallback(t *testing.T) {
	p := NewParser(OutputFormat("something-else"))
	assertEvents(t, p.ParseLine("hello"), []Event{{Kind: EventText, Text: "hello\n"}})
}

// TestParsersNeverEchoCredentials: parsers are fed the CLI's stdout, which can
// mention env var names, but nothing in this package copies a secret into an
// event. This pins that the fixture-driven path stays secret-free.
func TestParsersNeverEchoCredentials(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"using CLAUDE_CODE_OAUTH_TOKEN from the env"}]}}`
	for _, f := range []OutputFormat{OutputClaudeStreamJSON, OutputJSONL, OutputText} {
		p := NewParser(f)
		for _, e := range p.ParseLine(line) {
			if strings.Contains(e.Text+e.ToolResult, testSecret) {
				t.Errorf("%s parser produced the secret", f)
			}
		}
		final, _ := p.Final()
		if strings.Contains(final, testSecret) {
			t.Errorf("%s Final() produced the secret", f)
		}
	}
}
