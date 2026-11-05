package providers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers/acp"
)

func progressUpdate(t *testing.T, raw string) *acp.SessionUpdate {
	t.Helper()
	var u acp.SessionUpdate
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	return &u
}

func TestACPProgressThinking(t *testing.T) {
	seen := map[string]string{}

	thought := progressUpdate(t, `{"sessionId":"s","update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"checking build"}}}`)
	if got := acpProgressThinking(thought, seen); got != "checking build" {
		t.Fatalf("thought: got %q", got)
	}

	start := progressUpdate(t, `{"sessionId":"s","update":{"sessionUpdate":"tool_call","toolCallId":"t1","title":"bash go build","kind":"execute","status":"pending"}}`)
	if got := acpProgressThinking(start, seen); got != "\n🔧 bash go build [pending]\n" {
		t.Fatalf("tool_call: got %q", got)
	}
	// Same status again is deduped; a new status is surfaced.
	if got := acpProgressThinking(start, seen); got != "" {
		t.Fatalf("duplicate status should be suppressed, got %q", got)
	}
	done := progressUpdate(t, `{"sessionId":"s","update":{"sessionUpdate":"tool_call_update","toolCallId":"t1","status":"completed"}}`)
	if got := acpProgressThinking(done, seen); got != "\n🔧 t1 [completed]\n" {
		t.Fatalf("tool_call_update: got %q", got)
	}

	plan := progressUpdate(t, `{"sessionId":"s","update":{"sessionUpdate":"plan","entries":[{"content":"build","priority":"high","status":"in_progress"}]}}`)
	if got := acpProgressThinking(plan, seen); got != "\n📋 Plan:\n- [in_progress] build\n" {
		t.Fatalf("plan: got %q", got)
	}
	// Plan sent again with no changes is deduped.
	if got := acpProgressThinking(plan, seen); got != "" {
		t.Fatalf("identical plan should be deduped, got %q", got)
	}

	// Thought chunk with stripThinking=true is suppressed.
	if got := acpProgressThinking(thought, seen, true); got != "" {
		t.Fatalf("thought chunk with stripThinking should be empty, got %q", got)
	}

	// Missing toolCallId and empty label is suppressed.
	unlabelled := progressUpdate(t, `{"sessionId":"s","update":{"sessionUpdate":"tool_call_update","status":"completed"}}`)
	if got := acpProgressThinking(unlabelled, seen); got != "" {
		t.Fatalf("unlabelled tool update should be suppressed, got %q", got)
	}

	// Message chunks are handled by the Content path, not surfaced as thinking.
	msg := progressUpdate(t, `{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"}}}`)
	if got := acpProgressThinking(msg, seen); got != "" {
		t.Fatalf("message chunk: got %q", got)
	}
}

func TestSessionUpdateContentTextArray(t *testing.T) {
	u := progressUpdate(t, `{"update":{"content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}}`)
	if got := u.ContentText(); got != "ab" {
		t.Fatalf("got %q", got)
	}

	empty := progressUpdate(t, `{"update":{}}`)
	if got := empty.ContentText(); got != "" {
		t.Fatalf("empty content should yield empty string, got %q", got)
	}
}

func TestACPProgressPlanCapping(t *testing.T) {
	seen := map[string]string{}
	// Plan with 15 entries should be capped at 10 plus count note
	var entries []map[string]string
	for i := 1; i <= 15; i++ {
		entries = append(entries, map[string]string{
			"content":  "task",
			"status":   "in_progress",
			"priority": "normal",
		})
	}
	raw, _ := json.Marshal(map[string]any{
		"sessionId": "s",
		"update": map[string]any{
			"sessionUpdate": "plan",
			"entries":       entries,
		},
	})
	plan := progressUpdate(t, string(raw))
	got := acpProgressThinking(plan, seen)
	if got == "" {
		t.Fatal("expected rendered plan")
	}
	if !strings.Contains(got, "(+5 more)") {
		t.Fatalf("expected plan capping note (+5 more), got %q", got)
	}
}
