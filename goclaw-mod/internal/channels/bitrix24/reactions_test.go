package bitrix24

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type reactionCall struct {
	op       string // "add" | "delete"
	reaction string
}

// reactionRecorder serves the v2 reaction endpoints and records each call.
func reactionRecorder(t *testing.T) (*httptest.Server, func() []reactionCall) {
	t.Helper()
	var mu sync.Mutex
	var calls []reactionCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		op := ""
		switch {
		case strings.HasSuffix(r.URL.Path, "/rest/imbot.v2.Chat.Message.Reaction.add.json"):
			op = "add"
		case strings.HasSuffix(r.URL.Path, "/rest/imbot.v2.Chat.Message.Reaction.delete.json"):
			op = "delete"
		default:
			t.Errorf("unexpected reaction path: %q", r.URL.Path)
		}
		mu.Lock()
		calls = append(calls, reactionCall{op: op, reaction: r.Form.Get("reaction")})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"result": true})
	}))
	return srv, func() []reactionCall {
		mu.Lock()
		defer mu.Unlock()
		out := make([]reactionCall, len(calls))
		copy(out, calls)
		return out
	}
}

func TestOnReactionEvent_FullFlow_AddThenReplaceViaV2(t *testing.T) {
	srv, snapshot := reactionRecorder(t)
	defer srv.Close()
	bc := newChannelWithBoundPortal(t, srv)
	bc.cfg.ReactionLevel = "full"
	ctx := context.Background()

	// thinking → add thinkingFace
	_ = bc.OnReactionEvent(ctx, "chat100", "5001", "thinking")
	// done (terminal, bypasses debounce) → delete thinkingFace + add whiteHeavyCheckMark
	_ = bc.OnReactionEvent(ctx, "chat100", "5001", "done")

	calls := snapshot()
	if len(calls) != 3 {
		t.Fatalf("expected 3 reaction calls (add, delete, add), got %d: %+v", len(calls), calls)
	}
	if calls[0].op != "add" || calls[0].reaction != "thinkingFace" {
		t.Errorf("call[0] = %+v; want add thinkingFace", calls[0])
	}
	if calls[1].op != "delete" || calls[1].reaction != "thinkingFace" {
		t.Errorf("call[1] = %+v; want delete thinkingFace", calls[1])
	}
	if calls[2].op != "add" || calls[2].reaction != "whiteHeavyCheckMark" {
		t.Errorf("call[2] = %+v; want add whiteHeavyCheckMark", calls[2])
	}
}

func TestOnReactionEvent_MinimalSkipsIntermediate(t *testing.T) {
	srv, snapshot := reactionRecorder(t)
	defer srv.Close()
	bc := newChannelWithBoundPortal(t, srv)
	bc.cfg.ReactionLevel = "minimal"
	ctx := context.Background()

	_ = bc.OnReactionEvent(ctx, "chat100", "5001", "thinking") // skipped in minimal
	_ = bc.OnReactionEvent(ctx, "chat100", "5001", "error")    // terminal → shown

	calls := snapshot()
	if len(calls) != 1 || calls[0].op != "add" || calls[0].reaction != "crossMark" {
		t.Fatalf("minimal should send only the terminal reaction; got %+v", calls)
	}
}

func TestOnReactionEvent_OffAndInvalidAreNoOp(t *testing.T) {
	srv, snapshot := reactionRecorder(t)
	defer srv.Close()

	// off level
	bc := newChannelWithBoundPortal(t, srv)
	bc.cfg.ReactionLevel = "off"
	_ = bc.OnReactionEvent(context.Background(), "chat100", "5001", "done")

	// full level but non-numeric message id (connector synthetic id)
	bc2 := newChannelWithBoundPortal(t, srv)
	bc2.cfg.ReactionLevel = "full"
	_ = bc2.OnReactionEvent(context.Background(), "chat100", "not-a-number", "done")

	// full level, unknown status
	bc2.cfg.ReactionLevel = "full"
	_ = bc2.OnReactionEvent(context.Background(), "chat100", "5001", "bogus-status")

	if calls := snapshot(); len(calls) != 0 {
		t.Fatalf("expected no reaction calls for off/invalid/unknown, got %+v", calls)
	}
}

func TestClearReaction_RemovesCurrent(t *testing.T) {
	srv, snapshot := reactionRecorder(t)
	defer srv.Close()
	bc := newChannelWithBoundPortal(t, srv)
	bc.cfg.ReactionLevel = "full"
	ctx := context.Background()

	_ = bc.OnReactionEvent(ctx, "chat100", "5001", "thinking") // add thinkingFace
	_ = bc.ClearReaction(ctx, "chat100", "5001")               // delete thinkingFace

	calls := snapshot()
	if len(calls) != 2 || calls[1].op != "delete" || calls[1].reaction != "thinkingFace" {
		t.Fatalf("expected add then delete thinkingFace; got %+v", calls)
	}
}
