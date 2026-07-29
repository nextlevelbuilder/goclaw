package cliagent

import (
	"encoding/json"
	"strings"
)

// EventKind is the normalised vocabulary every provider's output is reduced to,
// so one UI renders a run of any connected CLI.
type EventKind string

const (
	// EventText is narration the CLI produced (a token-delta flush or a whole
	// assistant text block).
	EventText EventKind = "text"
	// EventToolCall is the CLI invoking one of its own tools.
	EventToolCall EventKind = "tool_call"
	// EventToolResult is the answer to a previous EventToolCall.
	EventToolResult EventKind = "tool_result"
	// EventFinal is the run's terminal answer. It repeats what Parser.Final
	// returns — consumers should render one or the other, never both.
	EventFinal EventKind = "final"
)

// Event is one normalised step of a connected CLI's run.
type Event struct {
	Kind EventKind
	Text string // text / final

	ToolID     string
	ToolName   string
	ToolInput  json.RawMessage
	ToolResult string

	IsError bool
}

// Parser turns a provider's stdout, fed one line at a time as it is produced,
// into normalised Events. Final reports the run's terminal answer once the
// stream has ended.
type Parser interface {
	ParseLine(line string) []Event
	Final() (text string, isError bool)
}

// Flusher is optionally implemented by parsers that buffer output (the
// claude_stream_json parser coalesces token deltas). Callers that want the last
// partial buffer after the stream ends — e.g. when the CLI was killed before it
// emitted a terminal event — may type-assert for it.
type Flusher interface {
	Flush() []Event
}

// NewParser returns the parser for an output format. An unrecognised format
// falls back to the plain-text parser, which never loses output; Resolve rejects
// unknown formats up front, so this is a belt-and-braces default.
func NewParser(f OutputFormat) Parser {
	switch f {
	case OutputClaudeStreamJSON:
		return newClaudeParser()
	case OutputJSONL:
		return &jsonlParser{}
	default:
		return &textParser{}
	}
}

// ---------------------------------------------------------------------------
// claude_stream_json
// ---------------------------------------------------------------------------

// deltaFlushBytes is the coalescing threshold: buffered narration is flushed
// once it crosses this many bytes (or at a content-block/newline boundary). It
// turns per-token deltas into a handful of events per second — real-time feel
// without re-rendering the UI on every character.
const deltaFlushBytes = 48

// claudeStreamEvent is the subset of Claude Code's stream-json events we consume.
type claudeStreamEvent struct {
	Type    string `json:"type"`
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
	Message struct {
		Content []claudeContent `json:"content"`
	} `json:"message"`
	// Event carries the wrapped Anthropic streaming event when Type is
	// "stream_event" (--include-partial-messages). Pointer so it's nil for the
	// message-level event types (assistant/user/result).
	Event *claudeInnerEvent `json:"event"`
}

// claudeInnerEvent is the Anthropic SSE event nested inside a stream_event —
// content_block_delta (text_delta) is the one we stream token-by-token.
type claudeInnerEvent struct {
	Type  string `json:"type"` // content_block_delta | content_block_stop | message_stop | …
	Delta struct {
		Type string `json:"type"` // text_delta | input_json_delta | thinking_delta
		Text string `json:"text"`
	} `json:"delta"`
}

type claudeContent struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`          // tool_use id
	Name      string          `json:"name"`        // tool name
	Input     json.RawMessage `json:"input"`       // tool_use input
	ToolUseID string          `json:"tool_use_id"` // tool_result → the use it answers
	IsError   bool            `json:"is_error"`
	Text      string          `json:"text"`    // assistant text block
	Content   json.RawMessage `json:"content"` // tool_result content (string or [{text}])
}

// claudeParser ports internal/tools/delegate_external_stream.go onto the
// normalised Event stream. Behaviour is deliberately identical, including the
// two subtleties that file documents:
//   - whole-block assistant text is SUPPRESSED once token deltas have been seen,
//     because it duplicates what already streamed word-by-word;
//   - deltas are coalesced (flush at deltaFlushBytes, at a newline, or before
//     any structured event) so the socket isn't flooded one token at a time.
type claudeParser struct {
	pending map[string]string // tool_use id → tool name, to label results

	finalResult  string
	finalIsError bool

	// partialActive flips true once token-level stream_event deltas start
	// arriving (--include-partial-messages).
	partialActive bool
	textBuf       strings.Builder
}

func newClaudeParser() *claudeParser {
	return &claudeParser{pending: map[string]string{}}
}

func (p *claudeParser) ParseLine(line string) []Event {
	line = strings.TrimSpace(line)
	if line == "" || line[0] != '{' {
		return nil
	}
	var ev claudeStreamEvent
	if json.Unmarshal([]byte(line), &ev) != nil {
		return nil
	}

	var out []Event
	switch ev.Type {
	case "stream_event":
		// Token-level delta. Stream narration word-by-word; ignore tool-input json
		// deltas (the complete tool_use arrives on the assistant event).
		if e := ev.Event; e != nil && e.Type == "content_block_delta" && e.Delta.Type == "text_delta" {
			p.partialActive = true
			out = p.appendDelta(out, e.Delta.Text)
		} else if e != nil && (e.Type == "content_block_stop" || e.Type == "message_stop") {
			out = p.flush(out)
		}
	case "assistant":
		out = p.flush(out) // land buffered narration before this turn's tool chips
		for _, c := range ev.Message.Content {
			switch c.Type {
			case "tool_use":
				p.pending[c.ID] = c.Name
				out = append(out, Event{Kind: EventToolCall, ToolID: c.ID, ToolName: c.Name, ToolInput: c.Input})
			case "text":
				// Duplicate of the deltas when they are streaming — skip. Older CLIs
				// without partial messages still deliver text here.
				if p.partialActive {
					continue
				}
				if t := strings.TrimSpace(c.Text); t != "" {
					// The trailing newline keeps successive turns readable, matching the
					// whole-block path in delegate_external_stream.go.
					out = append(out, Event{Kind: EventText, Text: t + "\n"})
				}
			}
		}
	case "user":
		out = p.flush(out)
		for _, c := range ev.Message.Content {
			if c.Type != "tool_result" {
				continue
			}
			out = append(out, Event{
				Kind:       EventToolResult,
				ToolID:     c.ToolUseID,
				ToolName:   p.pending[c.ToolUseID],
				IsError:    c.IsError,
				ToolResult: decodeToolResult(c.Content),
			})
			delete(p.pending, c.ToolUseID)
		}
	case "result":
		out = p.flush(out)
		// NOTE: the "result" event also carries the CLI's own token usage/cost. We
		// deliberately do NOT read or report it: a connected CLI runs on the USER'S
		// credential (BYOK), so those tokens are on the user's account and must
		// NEVER be metered as platform usage. Do not add usage parsing here.
		p.finalResult = ev.Result
		p.finalIsError = ev.IsError
		out = append(out, Event{Kind: EventFinal, Text: ev.Result, IsError: ev.IsError})
	}
	return out
}

// appendDelta buffers a token delta, emitting a coalesced EventText once the
// buffer crosses deltaFlushBytes or hits a line break.
func (p *claudeParser) appendDelta(out []Event, t string) []Event {
	if t == "" {
		return out
	}
	p.textBuf.WriteString(t)
	if p.textBuf.Len() >= deltaFlushBytes || strings.ContainsRune(t, '\n') {
		return p.flush(out)
	}
	return out
}

// flush emits whatever narration has accumulated as one verbatim EventText — no
// trimming and no added newline, which would eat the spaces between tokens.
func (p *claudeParser) flush(out []Event) []Event {
	if p.textBuf.Len() == 0 {
		return out
	}
	text := p.textBuf.String()
	p.textBuf.Reset()
	return append(out, Event{Kind: EventText, Text: text})
}

// Flush drains any buffered narration; see Flusher.
func (p *claudeParser) Flush() []Event { return p.flush(nil) }

func (p *claudeParser) Final() (string, bool) { return p.finalResult, p.finalIsError }

// decodeToolResult flattens a tool_result content field, which is either a JSON
// string or an array of {type:"text", text:"…"} blocks, into plain text.
func decodeToolResult(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var b strings.Builder
		for _, bl := range blocks {
			if bl.Text != "" {
				b.WriteString(bl.Text)
			}
		}
		return b.String()
	}
	return string(raw)
}

// ---------------------------------------------------------------------------
// jsonl
// ---------------------------------------------------------------------------

// jsonlParser reads a generic "one JSON object per line" stream BEST-EFFORT: it
// recognises the field names CLIs commonly use for narration and tool calls and
// ignores everything it doesn't understand, rather than guessing. A provider
// whose shape isn't recognised still runs — it just narrates less — and the
// mapping can be sharpened here without touching any caller.
type jsonlParser struct {
	text    strings.Builder // accumulated narration, the Final fallback
	final   string
	haveFin bool
	isError bool
}

// jsonl field-name candidates, in probe order.
var (
	jsonlTextKeys   = []string{"text", "message", "content", "delta", "response", "output", "chunk"}
	jsonlToolKeys   = []string{"tool_name", "tool", "function", "command", "name"}
	jsonlIDKeys     = []string{"tool_use_id", "call_id", "tool_call_id", "id"}
	jsonlInputKeys  = []string{"input", "arguments", "args", "params", "parameters"}
	jsonlResultKeys = []string{"result", "output", "stdout", "content", "text"}
	jsonlNestKeys   = []string{"item", "msg", "event", "data", "payload"}
)

func (p *jsonlParser) ParseLine(line string) []Event {
	line = strings.TrimSpace(line)
	if line == "" || line[0] != '{' {
		return nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(line), &obj) != nil {
		return nil
	}
	return p.parseObject(obj, 0)
}

func (p *jsonlParser) parseObject(obj map[string]json.RawMessage, depth int) []Event {
	typ := strings.ToLower(rawString(obj["type"]))
	if isErr, ok := rawBool(obj["is_error"]); ok && isErr {
		p.isError = true
	}
	if _, ok := obj["error"]; ok && rawString(obj["error"]) != "" {
		p.isError = true
	}

	// A tool result answers an earlier call; check it before the generic tool
	// probe, since both carry a tool name.
	if strings.Contains(typ, "tool_result") || strings.Contains(typ, "tool.result") || strings.Contains(typ, "function_result") {
		isErr, _ := rawBool(obj["is_error"])
		return []Event{{
			Kind:       EventToolResult,
			ToolID:     firstString(obj, jsonlIDKeys),
			ToolName:   firstString(obj, jsonlToolKeys),
			ToolResult: firstText(obj, jsonlResultKeys),
			IsError:    isErr,
		}}
	}

	if name := firstString(obj, jsonlToolKeys); name != "" && looksLikeToolCall(typ, obj) {
		var input json.RawMessage
		for _, k := range jsonlInputKeys {
			if v, ok := obj[k]; ok && len(v) > 0 && v[0] == '{' {
				input = v
				break
			}
		}
		return []Event{{Kind: EventToolCall, ToolID: firstString(obj, jsonlIDKeys), ToolName: name, ToolInput: input}}
	}

	if text := firstText(obj, jsonlTextKeys); text != "" {
		terminal := strings.Contains(typ, "result") || strings.Contains(typ, "final") ||
			strings.Contains(typ, "complete") || strings.Contains(typ, "done")
		if terminal {
			p.final, p.haveFin = text, true
			return []Event{{Kind: EventFinal, Text: text, IsError: p.isError}}
		}
		p.text.WriteString(text)
		if !strings.HasSuffix(text, "\n") {
			p.text.WriteString("\n")
		}
		return []Event{{Kind: EventText, Text: text}}
	}

	// Nothing at this level — some CLIs wrap the interesting object one deep
	// (e.g. {"type":"item.completed","item":{…}}). Descend once.
	if depth == 0 {
		for _, k := range jsonlNestKeys {
			v, ok := obj[k]
			if !ok || len(v) == 0 || v[0] != '{' {
				continue
			}
			var nested map[string]json.RawMessage
			if json.Unmarshal(v, &nested) != nil {
				continue
			}
			if evs := p.parseObject(nested, depth+1); len(evs) > 0 {
				return evs
			}
		}
	}
	return nil
}

// looksLikeToolCall keeps the generic "name" key from turning every object into
// a tool call: either the event type says so, or the object carries arguments.
func looksLikeToolCall(typ string, obj map[string]json.RawMessage) bool {
	if strings.Contains(typ, "tool") || strings.Contains(typ, "function") || strings.Contains(typ, "command") {
		return true
	}
	for _, k := range jsonlInputKeys {
		if _, ok := obj[k]; ok {
			return true
		}
	}
	return false
}

func (p *jsonlParser) Final() (string, bool) {
	if p.haveFin {
		return p.final, p.isError
	}
	return strings.TrimSpace(p.text.String()), p.isError
}

// rawString decodes a JSON string, returning "" for anything else.
func rawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

func rawBool(raw json.RawMessage) (bool, bool) {
	if len(raw) == 0 {
		return false, false
	}
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b, true
	}
	return false, false
}

func firstString(obj map[string]json.RawMessage, keys []string) string {
	for _, k := range keys {
		if s := rawString(obj[k]); s != "" {
			return s
		}
	}
	return ""
}

// firstText resolves the first key that yields text, accepting a plain string or
// an array of {text:…} / plain-string blocks (the Anthropic-ish content shape).
func firstText(obj map[string]json.RawMessage, keys []string) string {
	for _, k := range keys {
		raw, ok := obj[k]
		if !ok || len(raw) == 0 {
			continue
		}
		if s := rawString(raw); s != "" {
			return s
		}
		if raw[0] == '[' {
			if s := decodeToolResult(raw); strings.TrimSpace(s) != "" && s != string(raw) {
				return s
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// text
// ---------------------------------------------------------------------------

// textParser treats every non-empty line as narration and returns the whole
// transcript as the final answer — the only safe reading of a CLI that just
// prints prose.
type textParser struct {
	acc strings.Builder
}

func (p *textParser) ParseLine(line string) []Event {
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return nil
	}
	p.acc.WriteString(line)
	p.acc.WriteString("\n")
	return []Event{{Kind: EventText, Text: line + "\n"}}
}

func (p *textParser) Final() (string, bool) { return strings.TrimSpace(p.acc.String()), false }
