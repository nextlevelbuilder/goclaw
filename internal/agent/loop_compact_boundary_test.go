package agent

import (
	"context"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TestToolChainSplitIndex verifies the shared boundary walk used by mid-loop
// compaction and the background summarizer: the kept tail (messages[idx:])
// must never start with a tool result or split an assistant(tool_calls) →
// tool chain, because providers reject orphaned role:"tool" messages.
func TestToolChainSplitIndex(t *testing.T) {
	user := providers.Message{Role: "user", Content: "u"}
	assistant := providers.Message{Role: "assistant", Content: "a"}
	assistantTC := providers.Message{
		Role:      "assistant",
		Content:   "calling tool",
		ToolCalls: []providers.ToolCall{{ID: "tc-1"}},
	}
	tool := providers.Message{Role: "tool", Content: "result", ToolCallID: "tc-1"}

	tests := []struct {
		name     string
		messages []providers.Message
		splitIdx int
		want     int
	}{
		{
			name:     "clean boundary on user message stays put",
			messages: []providers.Message{user, assistant, user, assistant},
			splitIdx: 2,
			want:     2,
		},
		{
			name:     "split on tool result walks back before assistant tool_calls",
			messages: []providers.Message{user, assistant, assistantTC, tool, user},
			splitIdx: 3,
			want:     1,
		},
		{
			name:     "split on assistant with tool_calls walks back",
			messages: []providers.Message{user, assistant, assistantTC, tool, user},
			splitIdx: 2,
			want:     1,
		},
		{
			name:     "consecutive tool results walk back through whole chain",
			messages: []providers.Message{user, assistantTC, tool, tool, user},
			splitIdx: 3,
			want:     0,
		},
		{
			name:     "split at zero unchanged",
			messages: []providers.Message{user, assistant},
			splitIdx: 0,
			want:     0,
		},
		{
			name:     "split at len keeps empty tail unchanged",
			messages: []providers.Message{user, assistantTC, tool},
			splitIdx: 3,
			want:     3,
		},
		{
			name:     "split beyond len clamps to len",
			messages: []providers.Message{user, assistant},
			splitIdx: 5,
			want:     2,
		},
		{
			name:     "chain reaching history start returns zero",
			messages: []providers.Message{assistantTC, tool, user},
			splitIdx: 1,
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolChainSplitIndex(tt.messages, tt.splitIdx)
			if got != tt.want {
				t.Fatalf("toolChainSplitIndex() = %d, want %d", got, tt.want)
			}
			// Invariant: the kept tail never starts mid tool-chain.
			if got > 0 && got < len(tt.messages) {
				m := tt.messages[got]
				if m.Role == "tool" {
					t.Errorf("kept tail starts with role=tool at index %d", got)
				}
				if m.Role == "assistant" && len(m.ToolCalls) > 0 {
					t.Errorf("kept tail starts on assistant(tool_calls) at index %d", got)
				}
			}
		})
	}
}

// TestMakeCompactMessages_SanitizesOrphanTool verifies the compaction callback
// repairs pairing on the compacted slice: an orphaned tool message surviving
// inside the kept tail must be dropped before the slice replaces run history.
func TestMakeCompactMessages_SanitizesOrphanTool(t *testing.T) {
	cap := &capturingProvider{response: "Summary of conversation."}
	loop := &Loop{
		provider: cap,
		model:    "claude-3-5-sonnet",
	}

	// 10 messages → keepCount=4, splitIdx=6. messages[6] is a plain user
	// message (clean boundary), so the orphan tool at index 7 lands in the
	// kept tail and must be repaired by the sanitize pass.
	msgs := []providers.Message{
		{Role: "user", Content: "u0"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a3"},
		{Role: "user", Content: "u4"},
		{Role: "assistant", Content: "a5"},
		{Role: "user", Content: "u6"},
		{Role: "tool", Content: "orphaned result", ToolCallID: "orphan-1"},
		{Role: "user", Content: "u8"},
		{Role: "assistant", Content: "a9"},
	}

	compact := loop.makeCompactMessages(nil)
	result, err := compact(context.Background(), msgs, loop.model)
	if err != nil {
		t.Fatalf("compact callback error: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("compact callback returned empty messages")
	}
	if len(cap.captured) != 1 {
		t.Fatalf("provider.Chat called %d time(s), want 1 (compaction must have run)", len(cap.captured))
	}
	for i, m := range result {
		if m.Role == "tool" {
			t.Errorf("orphaned tool message survived at index %d (tool_call_id=%s)", i, m.ToolCallID)
		}
	}
}

// truncCapturingStore records the keepLast argument of TruncateHistory calls.
type truncCapturingStore struct {
	nopSessionStore
	truncated chan int
}

func (s *truncCapturingStore) TruncateHistory(_ context.Context, _ string, keepLast int) {
	select {
	case s.truncated <- keepLast:
	default:
	}
}

// TestMaybeSummarize_ToolChainBoundary verifies the background summarizer
// widens the kept tail when the raw split (len-keepLast) lands mid tool-chain,
// so truncation never persists a history starting with an orphaned tool message.
func TestMaybeSummarize_ToolChainBoundary(t *testing.T) {
	const contextWindow = 10000

	// threshold = 10000 * 0.85 = 8500 tokens; long content pushes the
	// estimate over it so summarization triggers.
	longContent := makeLongString(9000)
	history := []providers.Message{
		{Role: "user", Content: longContent},      // 0
		{Role: "assistant", Content: longContent}, // 1
		{Role: "user", Content: longContent},      // 2
		{Role: "assistant", Content: longContent}, // 3
		{Role: "assistant", Content: longContent, // 4
			ToolCalls: []providers.ToolCall{{ID: "tc-a"}, {ID: "tc-b"}}},
		{Role: "tool", Content: "result a", ToolCallID: "tc-a"}, // 5
		{Role: "tool", Content: "result b", ToolCallID: "tc-b"}, // 6
		{Role: "user", Content: longContent},                    // 7
		{Role: "assistant", Content: longContent},               // 8
		{Role: "user", Content: longContent},                    // 9
	}
	// keepLast=4 → raw split at index 6 (a tool result). The boundary walk
	// must retreat to index 3, keeping the last 7 messages.
	const wantKeep = 7

	store := &truncCapturingStore{
		nopSessionStore: nopSessionStore{history: history},
		truncated:       make(chan int, 1),
	}
	loop := &Loop{
		provider:      &capturingProvider{response: "summary"},
		model:         "claude-3-5-sonnet",
		contextWindow: contextWindow,
		sessions:      store,
	}

	loop.maybeSummarize(context.Background(), "test-session-key")

	select {
	case got := <-store.truncated:
		if got != wantKeep {
			t.Fatalf("TruncateHistory keepLast = %d, want %d (kept tail must not start mid tool-chain)", got, wantKeep)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for maybeSummarize to truncate history")
	}
}
