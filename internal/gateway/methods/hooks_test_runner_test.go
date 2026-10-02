package methods

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	hookhandlers "github.com/nextlevelbuilder/goclaw/internal/hooks/handlers"
)

func TestParseTestEventParams_SenderAndUser(t *testing.T) {
	cfg := &hooks.HookConfig{
		TenantID: uuid.New(),
		Event:    hooks.EventPreToolUse,
	}

	t.Run("outer camelCase", func(t *testing.T) {
		raw := json.RawMessage(`{"toolName":"bash","senderId":"alice","userId":"usr-1"}`)
		ev, err := parseTestEventParams(raw, cfg)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if ev.SenderID != "alice" {
			t.Errorf("expected senderId alice, got %q", ev.SenderID)
		}
		if ev.UserID != "usr-1" {
			t.Errorf("expected userId usr-1, got %q", ev.UserID)
		}
	})

	t.Run("outer snake_case", func(t *testing.T) {
		raw := json.RawMessage(`{"toolName":"bash","sender_id":"bob","user_id":"usr-2"}`)
		ev, err := parseTestEventParams(raw, cfg)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if ev.SenderID != "bob" {
			t.Errorf("expected sender_id bob, got %q", ev.SenderID)
		}
		if ev.UserID != "usr-2" {
			t.Errorf("expected user_id usr-2, got %q", ev.UserID)
		}
	})

	t.Run("from toolInput", func(t *testing.T) {
		raw := json.RawMessage(`{"toolName":"bash","toolInput":{"command":"ls","sender_id":"charlie","user_id":"usr-3"}}`)
		ev, err := parseTestEventParams(raw, cfg)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if ev.SenderID != "charlie" {
			t.Errorf("expected sender_id charlie from toolInput, got %q", ev.SenderID)
		}
		if ev.UserID != "usr-3" {
			t.Errorf("expected user_id usr-3 from toolInput, got %q", ev.UserID)
		}
	})

	t.Run("outer takes precedence over toolInput", func(t *testing.T) {
		raw := json.RawMessage(`{"toolName":"bash","sender_id":"outer-sender","toolInput":{"sender_id":"inner-sender"}}`)
		ev, err := parseTestEventParams(raw, cfg)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if ev.SenderID != "outer-sender" {
			t.Errorf("expected outer sender_id, got %q", ev.SenderID)
		}
	})
}

func TestDispatcherTestRunner_RunTest_ScriptResultCaptured(t *testing.T) {
	scriptHandler := hookhandlers.NewScriptHandler(2, 1, 16)
	runner := NewDispatcherTestRunner(map[hooks.HandlerType]hooks.Handler{
		hooks.HandlerScript: scriptHandler,
	})

	cfg := hooks.HookConfig{
		ID:          uuid.Nil,
		Version:     0,
		HandlerType: hooks.HandlerScript,
		Event:       hooks.EventPreToolUse,
		TimeoutMS:   1000,
		Config: map[string]any{
			"source": `function handle(event) {
				console.log("hello from test script");
				return {
					decision: "allow",
					reason: "custom policy passed for " + event.senderId,
					updatedInput: { modified: true }
				};
			}`,
		},
	}

	ev := hooks.Event{
		EventID:   "test-ev-1",
		HookEvent: hooks.EventPreToolUse,
		SenderID:  "dev-user",
		UserID:    "u-123",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res := runner.RunTest(ctx, cfg, ev)

	if res.Decision != hooks.DecisionAllow {
		t.Fatalf("expected DecisionAllow, got %v (err: %s)", res.Decision, res.Error)
	}
	if res.Reason != "custom policy passed for dev-user" {
		t.Errorf("expected reason with dev-user, got %q", res.Reason)
	}
	if res.Stdout == "" {
		t.Errorf("expected non-empty stdout, got %q", res.Stdout)
	}
	if res.UpdatedInput == nil || res.UpdatedInput["modified"] != true {
		t.Errorf("expected updatedInput.modified = true, got %+v", res.UpdatedInput)
	}
}
