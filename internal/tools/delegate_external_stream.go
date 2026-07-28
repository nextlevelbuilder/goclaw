package tools

import (
	"context"
	"encoding/json"
	"strings"
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
}

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
}

type streamContent struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`          // tool_use id
	Name      string          `json:"name"`        // tool name
	Input     json.RawMessage `json:"input"`       // tool_use input
	ToolUseID string          `json:"tool_use_id"` // tool_result → the use it answers
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"` // tool_result content (string or [{text}])
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
	case "assistant":
		for _, c := range ev.Message.Content {
			if c.Type == "tool_use" {
				s.pending[c.ID] = c.Name
				s.emitCall(c.ID, c.Name, c.Input)
			}
		}
	case "user":
		for _, c := range ev.Message.Content {
			if c.Type == "tool_result" {
				s.emitResult(c.ToolUseID, c.IsError, decodeToolResult(c.Content))
			}
		}
	case "result":
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
