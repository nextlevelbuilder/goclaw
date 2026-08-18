package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func negotiateTestCtx() context.Context {
	return store.WithTenantID(context.Background(), uuid.New())
}

// TestNegotiateTool_ConsensusClosesRecord verifies that reaching 2/3
// consensus on the votes closes the persisted record and reports the verdict.
func TestNegotiateTool_ConsensusClosesRecord(t *testing.T) {
	contracts := &testContractStore{}
	tool := NewNegotiateTool(contracts)

	result := tool.Execute(negotiateTestCtx(), map[string]any{
		"task":       "merge strategy",
		"acceptance": []any{"no regressions"},
		"proposals": []any{
			map[string]any{"author": "alice", "content": "squash merge"},
			map[string]any{"author": "bob", "content": "rebase merge"},
		},
		"votes": []any{
			map[string]any{"contender_id": "alice", "decision": "approve", "score": 1.0, "reason": "simple"},
			map[string]any{"contender_id": "alice", "decision": "approve", "score": 0.9, "reason": "safe"},
		},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}

	var payload struct {
		Consensus bool          `json:"consensus"`
		Verdict   *struct {
			ContenderID string  `json:"contender_id"`
			Decision    string  `json:"decision"`
			Score       float64 `json:"score"`
		} `json:"verdict"`
		Rounds   int    `json:"rounds"`
		Status   string `json:"status"`
		RecordID string `json:"record_id"`
	}
	if err := json.Unmarshal([]byte(result.ForLLM), &payload); err != nil {
		t.Fatalf("result not JSON: %v", err)
	}
	if !payload.Consensus {
		t.Fatal("expected consensus true")
	}
	if payload.Verdict == nil || payload.Verdict.ContenderID != "alice" {
		t.Errorf("expected verdict for alice, got %+v", payload.Verdict)
	}
	if payload.Verdict.Decision != "approve" {
		t.Errorf("verdict decision = %q, want approve", payload.Verdict.Decision)
	}
	if payload.Status != store.ContractRecordClosed {
		t.Errorf("status = %q, want closed", payload.Status)
	}
	if payload.RecordID == "" {
		t.Error("expected a persisted record id")
	}
	// A single approve vote on round 1 already satisfies 2/3 of the recorded
	// votes, so the negotiation closes at round 1 (the round bound is never
	// the binding constraint when consensus locks first).
	if payload.Rounds != 1 {
		t.Errorf("rounds = %d, want 1 (consensus locked on the first round)", payload.Rounds)
	}

	rec := contracts.last()
	if rec == nil {
		t.Fatal("expected a persisted negotiation record")
	}
	if rec.Kind != store.ContractRecordNegotiation {
		t.Errorf("kind = %q, want negotiation", rec.Kind)
	}
	if rec.Status != store.ContractRecordClosed {
		t.Errorf("record status = %q, want closed", rec.Status)
	}
	var body struct {
		Verdicts []struct {
			ContenderID string `json:"contender_id"`
			Decision    string `json:"decision"`
		} `json:"verdicts"`
	}
	if err := json.Unmarshal([]byte(rec.Body), &body); err != nil {
		t.Fatalf("record body not JSON: %v", err)
	}
	if len(body.Verdicts) != 1 {
		t.Errorf("expected 1 verdict in body (consensus closes early), got %d", len(body.Verdicts))
	}
}

// TestNegotiateTool_BoundedRoundsClosesExhausted verifies that submitting more
// proposals than maxRounds stops the round and closes the record as exhausted.
func TestNegotiateTool_BoundedRoundsClosesExhausted(t *testing.T) {
	contracts := &testContractStore{}
	tool := NewNegotiateTool(contracts)
	tool.SetMaxRounds(2)

	result := tool.Execute(negotiateTestCtx(), map[string]any{
		"task": "decide budget",
		"proposals": []any{
			map[string]any{"author": "a", "content": "low budget"},
			map[string]any{"author": "b", "content": "medium budget"},
			map[string]any{"author": "c", "content": "high budget"},
			map[string]any{"author": "d", "content": "extreme budget"},
		},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}

	var payload struct {
		Consensus bool `json:"consensus"`
		Rounds    int  `json:"rounds"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal([]byte(result.ForLLM), &payload); err != nil {
		t.Fatalf("result not JSON: %v", err)
	}
	if payload.Consensus {
		t.Fatal("expected no consensus with no votes")
	}
	if payload.Rounds != 2 {
		t.Errorf("rounds = %d, want 2 (bounded)", payload.Rounds)
	}
	if payload.Status != store.ContractRecordClosed {
		t.Errorf("status = %q, want closed (exhausted)", payload.Status)
	}

	rec := contracts.last()
	if rec == nil {
		t.Fatal("expected a persisted negotiation record")
	}
	if rec.Status != store.ContractRecordClosed {
		t.Errorf("record status = %q, want closed", rec.Status)
	}
	// The two bounded proposals must both be persisted in the durable body.
	var body struct {
		Proposals []struct {
			Author  string `json:"author"`
			Content string `json:"content"`
		} `json:"proposals"`
	}
	if err := json.Unmarshal([]byte(rec.Body), &body); err != nil {
		t.Fatalf("record body not JSON: %v", err)
	}
	if len(body.Proposals) != 2 {
		t.Errorf("expected 2 persisted proposals, got %d", len(body.Proposals))
	}
}

// TestNegotiateTool_NoConsensusStaysDraft verifies that a partial round with
// no votes and an unexhausted round bound persists a draft record.
func TestNegotiateTool_NoConsensusStaysDraft(t *testing.T) {
	contracts := &testContractStore{}
	tool := NewNegotiateTool(contracts)

	result := tool.Execute(negotiateTestCtx(), map[string]any{
		"task": "pick a color",
		"proposals": []any{
			map[string]any{"author": "a", "content": "blue"},
		},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}

	var payload struct {
		Consensus bool   `json:"consensus"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal([]byte(result.ForLLM), &payload); err != nil {
		t.Fatalf("result not JSON: %v", err)
	}
	if payload.Consensus {
		t.Fatal("expected no consensus without votes")
	}
	if payload.Status != store.ContractRecordDraft {
		t.Errorf("status = %q, want draft", payload.Status)
	}
	if rec := contracts.last(); rec == nil || rec.Status != store.ContractRecordDraft {
		t.Errorf("record status = %v, want draft", contracts.last().Status)
	}
}

// TestNegotiateTool_ValidationErrors verifies the required-task guard and the
// malformed-proposals error path.
func TestNegotiateTool_ValidationErrors(t *testing.T) {
	contracts := &testContractStore{}
	tool := NewNegotiateTool(contracts)

	if result := tool.Execute(negotiateTestCtx(), nil); !result.IsError {
		t.Fatal("expected error for missing task")
	}

	result := tool.Execute(negotiateTestCtx(), map[string]any{
		"task":      "negotiate",
		"proposals": "not-a-list",
	})
	if !result.IsError {
		t.Fatal("expected error for malformed proposals")
	}
	if !strings.Contains(result.ForLLM, "proposals") {
		t.Errorf("expected error about proposals, got: %s", result.ForLLM)
	}

	if contracts.last() != nil {
		t.Fatal("expected no persisted record for invalid execution")
	}
}