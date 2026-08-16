package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRepairJSON_ValidPassesThrough(t *testing.T) {
	cases := []string{
		`{}`,
		`[]`,
		`{"a": 1}`,
		`{"a": "b", "c": [1, 2, 3]}`,
		`null`,
		`"str"`,
		`42`,
		`[{"x": {"y": [true, false, null]}}]`,
	}
	for _, in := range cases {
		out, ok := repairJSON([]byte(in))
		if !ok {
			t.Errorf("repairJSON(%s): expected success", in)
			continue
		}
		if string(out) != in {
			t.Errorf("repairJSON(%s): changed valid input → %s", in, out)
		}
	}
}

func TestRepairJSON_TruncatedObjectClosed(t *testing.T) {
	cases := map[string]string{
		`{"a": 1`:                       `{"a": 1}`,
		`{"a": "b", "c": [1, 2`:         `{"a": "b", "c": [1, 2]}`,
		`{"a": {"b": {"c": 1`:           `{"a": {"b": {"c": 1}}}`,
		`[1, 2, 3`:                      `[1, 2, 3]`,
		`{"cmd": "echo hi", "timeout": 5`: `{"cmd": "echo hi", "timeout": 5}`,
	}
	for raw, want := range cases {
		out, ok := repairJSON([]byte(raw))
		if !ok {
			t.Errorf("repairJSON(%q): expected success", raw)
			continue
		}
		if string(out) != want {
			t.Errorf("repairJSON(%q): got %q, want %q", raw, out, want)
		}
		var v any
		if err := json.Unmarshal(out, &v); err != nil {
			t.Errorf("repairJSON(%q): repaired output still invalid: %v", raw, err)
		}
	}
}

func TestRepairJSON_TruncatedStringClosed(t *testing.T) {
	// The classic truncated tool-call shape: a string cut mid-flight.
	cases := map[string]string{
		`{"path": "a.txt", "content": "hi`:  `{"path": "a.txt", "content": "hi"}`,
		`{"content": "line1\nline2`:         `{"content": "line1\nline2"}`,
	}
	for raw, want := range cases {
		out, ok := repairJSON([]byte(raw))
		if !ok {
			t.Errorf("repairJSON(%q): expected success", raw)
			continue
		}
		if string(out) != want {
			t.Errorf("repairJSON(%q): got %q, want %q", raw, out, want)
		}
	}
}

func TestRepairJSON_UnterminatedQuoteIsStringContent(t *testing.T) {
	// `{"a": 1, "b": "unfinished` is a TRUNCATED string, not an unterminated
	// quote — the fix closes the string and the object.
	raw := `{"a": 1, "b": "unfinished`
	out, ok := repairJSON([]byte(raw))
	if !ok {
		t.Fatal("expected success")
	}
	want := `{"a": 1, "b": "unfinished"}`
	if string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestRepairJSON_ExcessClosingDropped(t *testing.T) {
	cases := map[string]string{
		`{"a": 1}}`:    `{"a": 1}`,
		`[1, 2]]`:      `[1, 2]`,
		`{"a": [1]}}]`: `{"a": [1]}`,
	}
	for raw, want := range cases {
		out, ok := repairJSON([]byte(raw))
		if !ok {
			t.Errorf("repairJSON(%q): expected success", raw)
			continue
		}
		if string(out) != want {
			t.Errorf("repairJSON(%q): got %q, want %q", raw, out, want)
		}
	}
}

func TestRepairJSON_TrailingCommaDropped(t *testing.T) {
	cases := map[string]string{
		`{"a": 1,}`:    `{"a": 1}`,
		`[1, 2,]`:      `[1, 2]`,
		`{"a": [1,],}`: `{"a": [1]}`,
	}
	for raw, want := range cases {
		out, ok := repairJSON([]byte(raw))
		if !ok {
			t.Errorf("repairJSON(%q): expected success", raw)
			continue
		}
		if string(out) != want {
			t.Errorf("repairJSON(%q): got %q, want %q", raw, out, want)
		}
	}
}

func TestRepairJSON_JSONPPrefixStripped(t *testing.T) {
	raw := `)]}'` + `{"a": 1}`
	out, ok := repairJSON([]byte(raw))
	if !ok {
		t.Fatal("expected success")
	}
	if string(out) != `{"a": 1}` {
		t.Errorf("got %q", out)
	}
}

func TestRepairJSON_NoRepairForCorruption(t *testing.T) {
	// Never guess meaning: these are semantic corruptions, not truncations.
	for _, raw := range []string{
		``,
		`   `,
		`hello world`,            // not JSON at all
		`{"a": 1, "b": }`,        // value cut mid-token — not a certain fix
		`{"a": 1, "b": "x" "c":`, // double value — not certain
	} {
		if out, ok := repairJSON([]byte(raw)); ok {
			t.Errorf("repairJSON(%q): expected failure, got %q", raw, out)
		}
	}
}

func TestRepairJSON_DanglingEscapeRejected(t *testing.T) {
	// A trailing backslash could be escaping the closing quote — the fix is
	// not certain, so reject.
	raw := `{"a": "x\`
	if _, ok := repairJSON([]byte(raw)); ok {
		t.Fatal("expected failure for dangling escape")
	}
}

func TestRepairJSON_RetryOneShot(t *testing.T) {
	// The pipeline is: parse → repair → 1 compact-error retry → fail.
	// Simulate the caller flow: the first repair succeeds, a compact hint is
	// only issued when it fails. The retry mechanism itself is bounded at the
	// caller (maxTruncRetries); repairJSON itself never loops.
	out, ok := repairJSON([]byte(`{"a": "b`))
	if !ok {
		t.Fatal("expected repair to succeed")
	}
	if string(out) != `{"a": "b"}` {
		t.Errorf("got %q", out)
	}
	// Same input again — the repair is deterministic and does not loop.
	out2, ok2 := repairJSON([]byte(`{"a": "b`))
	if !ok2 || string(out2) != `{"a": "b"}` {
		t.Errorf("repair not deterministic: %q %v", out2, ok2)
	}
}

func TestRepairJSON_FailClean(t *testing.T) {
	// A corrupted payload fails cleanly: ok=false, no partial output.
	raw := `{"a": 1, "b": "x" "c"`
	if out, ok := repairJSON([]byte(raw)); ok {
		t.Errorf("expected clean failure, got %q", out)
	}
}

func TestCompactError_BoundedAndTruncationHint(t *testing.T) {
	long := strings.Repeat("x", 500)
	msg := compactErrorString(long)
	if len(msg) > 200 {
		t.Errorf("compact error too long: %d chars", len(msg))
	}
	trunc := compactErrorString("unexpected end of JSON input")
	if !strings.Contains(trunc, "truncated") {
		t.Errorf("expected truncation hint, got %q", trunc)
	}
	if !strings.Contains(compactErrorString(""), "malformed JSON") {
		t.Error("expected malformed JSON hint for empty error")
	}
}

// compactErrorString adapts compactError for the string-typed test input.
func compactErrorString(msg string) string {
	return compactError(errStr(msg))
}

type errString string

func (e errString) Error() string { return string(e) }

func errStr(s string) error {
	if s == "" {
		return nil
	}
	return errString(s)
}
