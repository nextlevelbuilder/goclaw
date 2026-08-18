package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/commands/gc"
)

// fakeGCDispatcher is a scripted gc.CommandDispatcher for tests.
type fakeGCDispatcher struct {
	dispatch *gc.Dispatch
	ok       bool
	called   string
}

func (f *fakeGCDispatcher) Resolve(_ context.Context, msg string) (*gc.Dispatch, bool) {
	f.called = msg
	return f.dispatch, f.ok
}

func newTestLoopWithGCDispatcher(d gc.CommandDispatcher) *Loop {
	loop := NewLoop(LoopConfig{})
	loop.gcDispatcher = d
	return loop
}

func TestApplyGCCommandPassthroughWhenNilDispatcher(t *testing.T) {
	loop := newTestLoopWithGCDispatcher(nil)
	msg, extra, filter := loop.applyGCCommand(context.Background(), nil, "/gc:plan build a feature", "existing-prompt", []string{"keep"})
	if msg != "/gc:plan build a feature" {
		t.Fatalf("message = %q, want unchanged", msg)
	}
	if extra != "existing-prompt" {
		t.Fatalf("extraPrompt = %q, want unchanged", extra)
	}
	if len(filter) != 1 || filter[0] != "keep" {
		t.Fatalf("skillFilter = %v, want unchanged", filter)
	}
}

func TestApplyGCCommandPassthroughWhenNoMatch(t *testing.T) {
	loop := newTestLoopWithGCDispatcher(&fakeGCDispatcher{ok: false})
	msg, extra, filter := loop.applyGCCommand(context.Background(), nil, "hello there", "existing-prompt", []string{"keep"})
	if msg != "hello there" {
		t.Fatalf("message = %q, want unchanged", msg)
	}
	if extra != "existing-prompt" {
		t.Fatalf("extraPrompt = %q, want unchanged", extra)
	}
	if len(filter) != 1 || filter[0] != "keep" {
		t.Fatalf("skillFilter = %v, want unchanged", filter)
	}
}

func TestApplyGCCommandMatch(t *testing.T) {
	dispatcher := &fakeGCDispatcher{
		ok: true,
		dispatch: &gc.Dispatch{
			Kind:      gc.KindPlan,
			Skill:     "plan",
			Content:   "## Plan workflow\nUnderstand then inspect.",
			Remaining: "build a feature",
		},
	}
	loop := newTestLoopWithGCDispatcher(dispatcher)
	msg, extra, filter := loop.applyGCCommand(context.Background(), nil, "/gc:plan build a feature", "existing-prompt", nil)

	if msg != "build a feature" {
		t.Fatalf("message = %q, want remaining input", msg)
	}
	if !strings.Contains(extra, "plan") {
		t.Fatalf("extraPrompt missing skill name: %q", extra)
	}
	if !strings.Contains(extra, "## Plan workflow") {
		t.Fatalf("extraPrompt missing skill content: %q", extra)
	}
	if !strings.Contains(extra, "existing-prompt") {
		t.Fatalf("extraPrompt dropped existing prompt: %q", extra)
	}
	if len(filter) != 1 || filter[0] != "plan" {
		t.Fatalf("skillFilter = %v, want [plan]", filter)
	}
}

func TestApplyGCCommandMatchEmptyRemaining(t *testing.T) {
	dispatcher := &fakeGCDispatcher{
		ok: true,
		dispatch: &gc.Dispatch{
			Kind:      gc.KindFix,
			Skill:     "fix",
			Content:   "## Fix workflow\nReproduce then fix.",
			Remaining: "",
		},
	}
	loop := newTestLoopWithGCDispatcher(dispatcher)
	msg, _, filter := loop.applyGCCommand(context.Background(), nil, "/gc:fix", "", nil)

	if !strings.Contains(msg, "fix") {
		t.Fatalf("message = %q, want skill execution directive", msg)
	}
	if len(filter) != 1 || filter[0] != "fix" {
		t.Fatalf("skillFilter = %v, want [fix]", filter)
	}
}