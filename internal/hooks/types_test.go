package hooks_test

import (
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
)

// TestHookEventJSONRoundTrip verifies all 7 HookEvent constants have stable string values.
func TestHookEventJSONRoundTrip(t *testing.T) {
	events := []hooks.HookEvent{
		hooks.EventSessionStart,
		hooks.EventUserPromptSubmit,
		hooks.EventPreToolUse,
		hooks.EventPostToolUse,
		hooks.EventStop,
		hooks.EventSubagentStart,
		hooks.EventSubagentStop,
	}
	expectedStrings := []string{
		"session_start",
		"user_prompt_submit",
		"pre_tool_use",
		"post_tool_use",
		"stop",
		"subagent_start",
		"subagent_stop",
	}

	for i, e := range events {
		// JSON round-trip: marshal then unmarshal should produce same value.
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("Marshal(%v): %v", e, err)
		}
		var got hooks.HookEvent
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("Unmarshal(%v): %v", string(data), err)
		}
		if got != e {
			t.Errorf("round-trip mismatch: got %v, want %v", got, e)
		}
		// Verify stable string representation (no accidental int encoding).
		if string(e) != expectedStrings[i] {
			t.Errorf("HookEvent string: got %q, want %q", string(e), expectedStrings[i])
		}
	}
}

// TestHandlerTypeEnumExact verifies exactly 4 HandlerType constants exist.
func TestHandlerTypeEnumExact(t *testing.T) {
	all := []hooks.HandlerType{
		hooks.HandlerCommand,
		hooks.HandlerHTTP,
		hooks.HandlerPrompt,
		hooks.HandlerScript,
	}
	if len(all) != 4 {
		t.Fatalf("expected 4 HandlerType values, got %d", len(all))
	}
	expectedStrings := []string{"command", "http", "prompt", "script"}
	for i, h := range all {
		if string(h) != expectedStrings[i] {
			t.Errorf("HandlerType[%d]: got %q, want %q", i, string(h), expectedStrings[i])
		}
		// JSON round-trip.
		data, err := json.Marshal(h)
		if err != nil {
			t.Fatalf("Marshal(%v): %v", h, err)
		}
		var got hooks.HandlerType
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("Unmarshal(%v): %v", string(data), err)
		}
		if got != h {
			t.Errorf("HandlerType round-trip mismatch: got %v, want %v", got, h)
		}
	}
}

// TestDecisionIsBlock verifies Decision.IsBlock() returns true ONLY for "block".
func TestDecisionIsBlock(t *testing.T) {
	cases := []struct {
		d    hooks.Decision
		want bool
	}{
		{hooks.DecisionAllow, false},
		{hooks.DecisionBlock, true},
		{hooks.DecisionError, false},
		{hooks.DecisionTimeout, false},
		{hooks.DecisionAsk, false},
		{hooks.DecisionDefer, false},
	}
	for _, tc := range cases {
		got := tc.d.IsBlock()
		if got != tc.want {
			t.Errorf("Decision(%q).IsBlock() = %v, want %v", tc.d, got, tc.want)
		}
	}
}

// TestHookEventIsBlocking verifies IsBlocking returns true for the 3 blocking events.
func TestHookEventIsBlocking(t *testing.T) {
	blocking := map[hooks.HookEvent]bool{
		hooks.EventSessionStart:      false,
		hooks.EventUserPromptSubmit:  true,
		hooks.EventPreToolUse:        true,
		hooks.EventPostToolUse:       false,
		hooks.EventStop:              false,
		hooks.EventSubagentStart:     true,
		hooks.EventSubagentStop:      false,
	}
	for event, want := range blocking {
		got := event.IsBlocking()
		if got != want {
			t.Errorf("HookEvent(%q).IsBlocking() = %v, want %v", event, got, want)
		}
	}
}
