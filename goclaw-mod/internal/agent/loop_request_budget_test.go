package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
)

func newBudgetTestLoop(window, maxTokens int) *Loop {
	return &Loop{
		id:            "budget-test",
		contextWindow: window,
		maxTokens:     maxTokens,
		budgetCounter: tokencount.NewBudgetCounter(),
	}
}

func budgetMessageAtLeast(t *testing.T, counter tokencount.BudgetCounter, target int) providers.Message {
	t.Helper()
	low, high := 1, target*8
	for low < high {
		mid := low + (high-low)/2
		msg := providers.Message{Role: "user", Content: strings.Repeat("word ", mid)}
		count, err := counter.CountMessages([]providers.Message{msg})
		if err != nil {
			t.Fatal(err)
		}
		if count < target {
			low = mid + 1
		} else {
			high = mid
		}
	}
	return providers.Message{Role: "user", Content: strings.Repeat("word ", low)}
}

func TestGuardCompleteModelRequest_AgentWindowOnly(t *testing.T) {
	counter := tokencount.NewBudgetCounter()
	msg := budgetMessageAtLeast(t, counter, 130_000)
	req := providers.ChatRequest{Model: "model-a", Messages: []providers.Message{msg}}

	loop128 := newBudgetTestLoop(128_000, 8_192)
	err := loop128.guardCompleteModelRequest(req, "provider-a", "model-a", "initial")
	if err == nil {
		t.Fatal("128k agent: expected request to be blocked")
	}
	var budgetErr *RequestBudgetExceededError
	if !errors.As(err, &budgetErr) {
		t.Fatalf("expected RequestBudgetExceededError, got %T: %v", err, err)
	}

	loop200 := newBudgetTestLoop(200_000, 8_192)
	if err := loop200.guardCompleteModelRequest(req, "provider-a", "model-a", "initial"); err != nil {
		t.Fatalf("200k agent: expected request to fit, got %v", err)
	}
}

func TestGuardCompleteModelRequest_ModelProviderIndependent(t *testing.T) {
	loop := newBudgetTestLoop(128_000, 8_192)
	base := providers.ChatRequest{
		Model: "model-a",
		Messages: []providers.Message{{
			Role:     "assistant",
			Content:  "same content",
			Thinking: "same thinking",
			ToolCalls: []providers.ToolCall{{
				ID:        "call-1",
				Name:      "read_file",
				Arguments: map[string]any{"path": "/tmp/a"},
				Metadata:  map[string]string{"thought_signature": "sig"},
			}},
		}},
	}
	other := base
	other.Model = "totally-different-model"

	first, err := loop.budgetCounter.CountRequest(base)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loop.budgetCounter.CountRequest(other)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("completeInput changed with model: %d != %d", first, second)
	}
	if errA, errB := loop.guardCompleteModelRequest(base, "provider-a", base.Model, "a"), loop.guardCompleteModelRequest(other, "provider-b", other.Model, "b"); (errA == nil) != (errB == nil) {
		t.Fatalf("allow/abort changed with model/provider: %v vs %v", errA, errB)
	}
}

func TestGuardCompleteModelRequest_MaxTokensChangesHardCapExactly(t *testing.T) {
	counter := tokencount.NewBudgetCounter()
	msg := budgetMessageAtLeast(t, counter, 7_500)
	req := providers.ChatRequest{Messages: []providers.Message{msg}}
	input, err := counter.CountRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	window := input + 2_500

	allow := newBudgetTestLoop(window, 2_500)
	if err := allow.guardCompleteModelRequest(req, "p", "m", "allow"); err != nil {
		t.Fatalf("input + 2500 should fit exactly: input=%d window=%d err=%v", input, window, err)
	}
	block := newBudgetTestLoop(window, 2_501)
	if err := block.guardCompleteModelRequest(req, "p", "m", "block"); err == nil {
		t.Fatalf("input + 2501 must exceed by one: input=%d window=%d", input, window)
	}
}

func TestGuardCompleteModelRequest_FailsClosedOnUnresolvedAgentWindow(t *testing.T) {
	loop := newBudgetTestLoop(0, 8_192)
	req := providers.ChatRequest{Messages: []providers.Message{{Role: "user", Content: "hi"}}}
	if err := loop.guardCompleteModelRequest(req, "provider", "model", "initial"); err == nil {
		t.Fatal("expected wiring failure without configured agent window")
	}
}
