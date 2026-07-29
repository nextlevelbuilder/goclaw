package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/cliagent"
)

// delegateStreamer parses Claude Code's --output-format stream-json output line
// by line as the run progresses, emits tool.call/tool.result events so the
// website renders each of the connected agent's actions as a live nested chip
// under this delegation's card, and captures the final "result" event as the
// delegation's answer. All routing reuses the subagent live-progress wire shape
// (parent_tool_call_id + subagent_id), so no new website rendering is needed.
type delegateStreamer struct {
	emit       ToolEventEmitter
	parentID   string            // this delegate_external call's tool_call.id
	subagentID string            // stable grouping id for the chips
	label      string            // e.g. "Claude Code"
	pending    map[string]string // tool_use id → tool name (to label results)

	finalResult  string
	finalIsError bool
	eventCount   int

	// partialActive flips true once token-level stream_event deltas start
	// arriving (--include-partial-messages). While set, the whole-block text
	// on the assistant event is suppressed — it would duplicate what the
	// deltas already streamed word-by-word.
	partialActive bool
	// textBuf coalesces successive token deltas so we emit a handful of
	// subagent.chunk events per second instead of one per token — real-time
	// feel without flooding the socket / re-rendering the UI every character.
	textBuf strings.Builder
}

// deltaFlushBytes is the coalescing threshold: buffered narration is flushed
// once it crosses this many bytes (or at a content-block/newline boundary).
const deltaFlushBytes = 48

func newDelegateStreamer(ctx context.Context, label string) *delegateStreamer {
	return &delegateStreamer{
		emit:       ToolEventEmitterFromCtx(ctx),
		parentID:   ParentToolCallIDFromCtx(ctx),
		subagentID: "delegate:" + label,
		label:      label,
		pending:    map[string]string{},
	}
}

// streamEvent is the subset of Claude Code's stream-json events we consume.
type streamEvent struct {
	Type    string `json:"type"`
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
	Message struct {
		Content []streamContent `json:"content"`
	} `json:"message"`
	// Event carries the wrapped Anthropic streaming event when Type is
	// "stream_event" (--include-partial-messages). Pointer so it's nil for the
	// message-level event types (assistant/user/result).
	Event *innerStreamEvent `json:"event"`
}

// innerStreamEvent is the Anthropic SSE event nested inside a stream_event —
// content_block_delta (text_delta) is the one we stream token-by-token.
type innerStreamEvent struct {
	Type  string `json:"type"` // content_block_delta | content_block_stop | message_stop | …
	Delta struct {
		Type string `json:"type"` // text_delta | input_json_delta | thinking_delta
		Text string `json:"text"`
	} `json:"delta"`
}

type streamContent struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`          // tool_use id
	Name      string          `json:"name"`        // tool name
	Input     json.RawMessage `json:"input"`       // tool_use input
	ToolUseID string          `json:"tool_use_id"` // tool_result → the use it answers
	IsError   bool            `json:"is_error"`
	Text      string          `json:"text"`    // assistant text block
	Content   json.RawMessage `json:"content"` // tool_result content (string or [{text}])
}

// lineHandler returns the per-line consumer appropriate to the provider's output
// format, plus a finaliser to call once the exec has ended.
//
// claude_stream_json deliberately keeps the ORIGINAL onLine implementation rather
// than routing through cliagent's port of it. That path is production-tested and
// carries hard-won behaviour (token-delta coalescing, suppressing the duplicate
// whole-block text, never reading usage so BYOK tokens aren't metered); swapping
// it for an untried reimplementation would risk the one provider that works today
// for no benefit. Other formats — which previously streamed NOTHING here, because
// onLine drops any line not starting with '{' — go through the normalised parser.
func (s *delegateStreamer) lineHandler(format cliagent.OutputFormat) (onLine func(string), finish func()) {
	if format == "" || format == cliagent.OutputClaudeStreamJSON {
		return s.onLine, s.flushDelta
	}

	p := cliagent.NewParser(format)
	return func(line string) {
			for _, ev := range p.ParseLine(line) {
				s.eventCount++
				s.consume(ev)
			}
		}, func() {
			if f, ok := p.(cliagent.Flusher); ok {
				for _, ev := range f.Flush() {
					s.consume(ev)
				}
			}
			// Formats without an explicit terminal event (plain text) report their
			// answer only via Final(); without this the run would look like it produced
			// nothing and the caller would report "finished but returned no result".
			if s.finalResult == "" {
				if text, isErr := p.Final(); strings.TrimSpace(text) != "" {
					s.finalResult, s.finalIsError = text, isErr
				}
			}
		}
}

// consume maps one normalised adapter event onto the live-progress emitters, so
// every provider renders through the same UI as Claude Code.
func (s *delegateStreamer) consume(ev cliagent.Event) {
	switch ev.Kind {
	case cliagent.EventText:
		s.emitTextRaw(ev.Text)
	case cliagent.EventToolCall:
		if ev.ToolName != "" {
			s.pending[ev.ToolID] = ev.ToolName
		}
		s.emitCall(ev.ToolID, ev.ToolName, ev.ToolInput)
	case cliagent.EventToolResult:
		s.emitResult(ev.ToolID, ev.IsError, ev.ToolResult)
	case cliagent.EventFinal:
		// Final() is the authoritative source; EventFinal is the same content and
		// rendering both would duplicate the answer.
		if s.finalResult == "" {
			s.finalResult, s.finalIsError = ev.Text, ev.IsError
		}
	}
}

// onLine is invoked once per stdout line by the sandbox as output is produced.
func (s *delegateStreamer) onLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] != '{' {
		return
	}
	var ev streamEvent
	if json.Unmarshal([]byte(line), &ev) != nil {
		return
	}
	s.eventCount++
	switch ev.Type {
	case "stream_event":
		// Token-level delta (--include-partial-messages). Stream narration text
		// word-by-word for a real-time feel; ignore tool-input json deltas (the
		// complete tool_use arrives on the assistant event with full args).
		if e := ev.Event; e != nil && e.Type == "content_block_delta" && e.Delta.Type == "text_delta" {
			s.partialActive = true
			s.appendDelta(e.Delta.Text)
		} else if e != nil && (e.Type == "content_block_stop" || e.Type == "message_stop") {
			s.flushDelta()
		}
		return
	case "assistant":
		s.flushDelta() // land any buffered narration before this turn's tool chips
		for _, c := range ev.Message.Content {
			switch c.Type {
			case "tool_use":
				s.pending[c.ID] = c.Name
				s.emitCall(c.ID, c.Name, c.Input)
			case "text":
				// When token deltas are streaming (partialActive), the whole-block
				// text here is a duplicate of what already streamed — skip it.
				// Otherwise (older CLI without partial messages) emit the block.
				if !s.partialActive {
					s.emitText(c.Text)
				}
			}
		}
	case "user":
		s.flushDelta()
		for _, c := range ev.Message.Content {
			if c.Type == "tool_result" {
				s.emitResult(c.ToolUseID, c.IsError, decodeToolResult(c.Content))
			}
		}
	case "result":
		s.flushDelta()
		// NOTE: the stream-json "result" event also carries Claude Code's own
		// token usage/cost. We deliberately do NOT read or report it: Claude Code
		// runs on the USER'S own Anthropic credential (BYOK), so those tokens are
		// on the user's Anthropic account and must NEVER be metered as AOS usage.
		// delegate_external leaves Result.Usage unset for the same reason — do not
		// add usage reporting here.
		s.finalResult = ev.Result
		s.finalIsError = ev.IsError
	}
}

// emitText streams a block of the connected agent's narration to the user as a
// subagent.chunk under this delegation's card (the website appends it to the
// live progress text). A trailing newline keeps successive turns readable.
func (s *delegateStreamer) emitText(text string) {
	if s.emit == nil || s.parentID == "" {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	s.emit("subagent.chunk", map[string]any{
		"content":             text + "\n",
		"parent_tool_call_id": s.parentID,
		"subagent_id":         s.subagentID,
		"subagent_label":      s.label,
	})
}

// appendDelta buffers a token-level text delta and flushes once the buffer
// crosses deltaFlushBytes or hits a line break — coalescing per-token events
// into a smooth handful-per-second stream without re-rendering every character.
func (s *delegateStreamer) appendDelta(t string) {
	if t == "" {
		return
	}
	s.textBuf.WriteString(t)
	if s.textBuf.Len() >= deltaFlushBytes || strings.ContainsRune(t, '\n') {
		s.flushDelta()
	}
}

// flushDelta emits whatever narration has accumulated as one subagent.chunk.
func (s *delegateStreamer) flushDelta() {
	if s.textBuf.Len() == 0 {
		return
	}
	s.emitTextRaw(s.textBuf.String())
	s.textBuf.Reset()
}

// emitTextRaw streams narration verbatim (no trim / no added newline) — used
// for the token-delta path where trimming would eat the spaces between words.
func (s *delegateStreamer) emitTextRaw(text string) {
	if s.emit == nil || s.parentID == "" || text == "" {
		return
	}
	s.emit("subagent.chunk", map[string]any{
		"content":             text,
		"parent_tool_call_id": s.parentID,
		"subagent_id":         s.subagentID,
		"subagent_label":      s.label,
	})
}

func (s *delegateStreamer) emitCall(id, name string, input json.RawMessage) {
	if s.emit == nil || s.parentID == "" {
		return
	}
	var args map[string]any
	if json.Unmarshal(input, &args) != nil || args == nil {
		args = map[string]any{}
	}
	s.emit("tool.call", map[string]any{
		"name":                name,
		"id":                  id,
		"arguments":           args,
		"parent_tool_call_id": s.parentID,
		"subagent_id":         s.subagentID,
		"subagent_label":      s.label,
	})
}

func (s *delegateStreamer) emitResult(id string, isError bool, text string) {
	if s.emit == nil || s.parentID == "" {
		return
	}
	s.emit("tool.result", map[string]any{
		"name":                s.pending[id],
		"id":                  id,
		"is_error":            isError,
		"result":              truncateStrSub(text, 800),
		"parent_tool_call_id": s.parentID,
		"subagent_id":         s.subagentID,
		"subagent_label":      s.label,
	})
	delete(s.pending, id)
}

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
