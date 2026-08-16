package agent

import (
	"bytes"
	"encoding/json"
	"strings"
)

// repairJSON attempts a strict-safe repair of malformed JSON. It only fixes
// errors that are certain (unterminated string / unbalanced brackets), never
// guesses meaning, and never touches the interior of a payload.
//
// Pipeline (C2):
//
//	parse → repair → 1 compact-error retry → fail
//
// The repaired bytes are verified with a full json.Unmarshal (into any) before
// being returned, so a repair that does not actually parse is rejected.
// ErrModelInvalidJSON is surfaced by the caller (think-stage / loop retry
// path) when both the original and the repair fail — this function stays
// silent about the failure mode and leaves the decision to the caller.
//
// Bounded: one repair pass, one retry with a compacted error message, then
// fail — mirrors the existing maxTruncRetries=3 spirit without looping.
func repairJSON(raw []byte) ([]byte, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	if json.Valid(raw) {
		return raw, true
	}
	repaired := strictFix(raw)
	if repaired == nil {
		return nil, false
	}
	var v any
	if err := json.Unmarshal(repaired, &v); err != nil {
		return nil, false
	}
	return repaired, true
}

// strictFix applies the certain fixes and returns nil when none apply:
//
//   - truncated tail: an unterminated string is closed, then unclosed
//     object/array brackets are closed (e.g. `{"a":"b` → `{"a":"b"}`),
//   - unbalanced closing brackets past the last value are dropped
//     (e.g. `{"a":1}}` → `{"a":1}`),
//   - a trailing comma before a closing bracket is dropped,
//   - a JSONP-style `)]}'` prefix (up to 4 bytes) is dropped.
//
// Interior content is never modified. Whitespace-only, dangling-escape and
// non-JSON-looking input are rejected.
func strictFix(raw []byte) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}

	// Drop a JSONP-ish prefix that some providers append before the payload.
	// Track the strip: when the prefix alone was the problem, the stripped
	// payload may already be valid — return it as the repair.
	if !isJSONStart(trimmed[0]) {
		if i := firstNonNoise(trimmed); i > 0 && i <= 4 {
			trimmed = trimmed[i:]
		} else {
			return nil
		}
		if json.Valid(trimmed) {
			return trimmed
		}
	}

	fixed, truncated := closeTruncated(trimmed)
	fixed = stripTrailingComma(fixed)
	if !truncated {
		fixed = dropExcessClosing(fixed)
	}
	if bytes.Equal(fixed, trimmed) {
		return nil
	}
	if !isJSONStart(fixed[0]) {
		return nil
	}
	// The final fix must balance — verify cheaply before returning.
	if !balanced(fixed) {
		return nil
	}
	return fixed
}

// isJSONStart reports whether b opens a JSON value.
func isJSONStart(b byte) bool {
	return b == '{' || b == '[' || b == '"' ||
		b == 't' || b == 'f' || b == 'n' || b == '-' ||
		(b >= '0' && b <= '9')
}

// firstNonNoise returns the index of the first byte that is neither
// whitespace nor a JSONP-`)]}'`-style noise char within the first 5 bytes
// (the full `)]}'` prefix is 4 chars, so the payload starts at index 4),
// or -1.
func firstNonNoise(b []byte) int {
	max := len(b)
	if max > 5 {
		max = 5
	}
	for i := 0; i < max; i++ {
		c := b[i]
		if isSpace(c) || c == ')' || c == ']' || c == '}' || c == '\'' || c == ';' || c == ',' {
			continue
		}
		return i
	}
	return -1
}

// scanState walks one byte at a time, skipping string contents. Returns the
// stack of currently open brackets, the bracket token stream, and false when
// a closing bracket mismatches or a string never closes.
type scanState struct {
	stack  []byte
	tokens []byte
	ok     bool
}

func scan(b []byte) scanState {
	st := scanState{ok: true}
	inStr := false
	esc := false
	for _, c := range b {
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{', '[':
			st.stack = append(st.stack, c)
			st.tokens = append(st.tokens, c)
		case '}', ']':
			if len(st.stack) == 0 {
				st.ok = false
				return st
			}
			top := st.stack[len(st.stack)-1]
			if (c == '}' && top != '{') || (c == ']' && top != '[') {
				st.ok = false
				return st
			}
			st.stack = st.stack[:len(st.stack)-1]
			st.tokens = append(st.tokens, c)
		}
	}
	if inStr {
		st.ok = false
	}
	return st
}

// closeTruncated fixes the certain truncation shapes and reports whether
// anything was synthesized:
//
//   - `{"a":"b` (string unterminated at end) → close the quote, then the
//     unclosed object/array brackets,
//   - `{"a":1` (truncated at end, no open string) → close the brackets.
//
// A dangling escape at the end (odd run of backslashes) is NOT certain —
// left untouched, same for a mismatched closer mid-payload (that is a
// semantic corruption, not a truncation).
func closeTruncated(b []byte) ([]byte, bool) {
	if danglingEscape(b) {
		return b, false
	}
	out := append([]byte{}, b...)
	inStr := false
	esc := false
	for _, c := range out {
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
		}
	}
	if inStr {
		out = append(out, '"')
	}
	st := scan(out)
	if !st.ok {
		return b, false
	}
	if len(st.stack) == 0 {
		return b, false
	}
	for i := len(st.stack) - 1; i >= 0; i-- {
		switch st.stack[i] {
		case '{':
			out = append(out, '}')
		case '[':
			out = append(out, ']')
		}
	}
	return out, true
}

// danglingEscape reports whether b ends with an odd run of backslashes,
// meaning a string escape is still open — not a certain fix.
func danglingEscape(b []byte) bool {
	n := 0
	for i := len(b) - 1; i >= 0 && b[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

// stripTrailingComma drops every comma that is directly followed by a closing
// bracket — the only trailing-comma shape JSON parsers reject
// (e.g. `{"a": 1,}` and `[1, 2,]`). Interior commas are never touched.
func stripTrailingComma(b []byte) []byte {
	out := make([]byte, 0, len(b))
	inStr := false
	esc := false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inStr {
			out = append(out, c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			out = append(out, c)
			continue
		}
		if c == ',' {
			if j := nextNonSpace(b, i+1); j >= 0 && isClosing(b[j]) {
				continue // trailing comma before a closer — drop it
			}
		}
		out = append(out, c)
	}
	return out
}

// nextNonSpace returns the index of the first non-whitespace byte at or after
// i, or -1.
func nextNonSpace(b []byte, i int) int {
	for ; i < len(b); i++ {
		if !isSpace(b[i]) {
			return i
		}
	}
	return -1
}

// dropExcessClosing removes trailing closing brackets that overflow the open
// ones — e.g. `{"a": 1}}` → `{"a": 1}`. Each trailing closer is kept only
// when the prefix still has a matching open bracket; otherwise it is dropped.
func dropExcessClosing(b []byte) []byte {
	out := append([]byte{}, b...)
	depth, ok := netDepth(out)
	if !ok || depth >= 0 {
		return out
	}
	// More closers than openers — drop the trailing excess one by one.
	// A dropped closer contributes +1 to the net depth.
	for depth < 0 {
		out = dropLast(out)
		depth++
	}
	return out
}

// netDepth counts open brackets minus close brackets across the whole
// payload, ignoring string contents. ok=false when a string never closes.
// A closing bracket with no matching opener still contributes -1 (the excess
// is what dropExcessClosing removes).
func netDepth(b []byte) (int, bool) {
	depth := 0
	inStr := false
	esc := false
	for _, c := range b {
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		}
	}
	return depth, !inStr
}

// dropLast removes the last byte and trailing whitespace after it.
func dropLast(b []byte) []byte {
	i := len(b) - 1
	for i >= 0 && isSpace(b[i]) {
		i--
	}
	if i < 0 {
		return b[:0]
	}
	return b[:i]
}

// balanced verifies that brackets balance and no string is left open.
func balanced(b []byte) bool {
	st := scan(b)
	return st.ok && len(st.stack) == 0
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func isClosing(c byte) bool { return c == '}' || c == ']' }

// compactError builds the 1-shot compact retry hint (C2): a one-line, bounded
// message pointing at malformed arguments without echoing them. It is the
// message a caller can append to the retry prompt after a failed repair.
func compactError(err error) string {
	if err == nil {
		return "malformed JSON: tool call arguments could not be parsed"
	}
	msg := err.Error()
	if i := strings.Index(msg, "unexpected end of JSON input"); i >= 0 {
		return "truncated JSON: tool call arguments ended before the closing bracket"
	}
	if len(msg) > 160 {
		msg = msg[:160]
	}
	return "malformed JSON: " + msg
}