package contract

import (
	"encoding/json"
	"testing"
	"time"
)

func TestValidContractKind(t *testing.T) {
	for _, k := range []ContractKind{
		ContractHandoff, ContractJury, ContractCompetition, ContractNegotiation,
	} {
		if !ValidContractKind(k) {
			t.Errorf("ValidContractKind(%q) = false, want true", k)
		}
	}
	if ValidContractKind("bogus") {
		t.Error("ValidContractKind(bogus) = true, want false")
	}
	if ValidContractKind("") {
		t.Error("ValidContractKind(empty) = true, want false")
	}
}

func TestValidate_Valid(t *testing.T) {
	c := &Contract{
		ID:          "c-1",
		Kind:        ContractHandoff,
		Task:        "Write a pagination helper",
		AuthorAgent: "lead",
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidate_Nil(t *testing.T) {
	var c *Contract
	if err := c.Validate(); err == nil {
		t.Fatal("Validate(nil): expected error")
	}
}

func TestValidate_InvalidKind(t *testing.T) {
	c := &Contract{ID: "c-2", Kind: "teleport", Task: "x"}
	if err := c.Validate(); err != ErrInvalidKind {
		t.Fatalf("Validate: got %v, want ErrInvalidKind", err)
	}
}

func TestValidate_EmptyKind(t *testing.T) {
	c := &Contract{ID: "c-3", Task: "x"}
	if err := c.Validate(); err != ErrInvalidKind {
		t.Fatalf("Validate: got %v, want ErrInvalidKind", err)
	}
}

func TestValidate_EmptyTask(t *testing.T) {
	c := &Contract{ID: "c-4", Kind: ContractJury}
	if err := c.Validate(); err != ErrEmptyTask {
		t.Fatalf("Validate: got %v, want ErrEmptyTask", err)
	}
}

func TestAddVerdict(t *testing.T) {
	c := &Contract{ID: "c-5", Kind: ContractCompetition, Task: "t"}
	if len(c.Verdicts) != 0 {
		t.Fatalf("initial verdicts = %d, want 0", len(c.Verdicts))
	}
	c.AddVerdict(Verdict{ContenderID: "a", Decision: "approve", Score: 8})
	c.AddVerdict(Verdict{ContenderID: "a", Decision: "approve", Score: 9})
	if len(c.Verdicts) != 2 {
		t.Fatalf("verdicts = %d, want 2", len(c.Verdicts))
	}
	ok, v := c.Consensus(2.0 / 3.0)
	if !ok {
		t.Fatal("Consensus(2/3): expected true for 2/2 agreeing")
	}
	if v.ContenderID != "a" || v.Decision != "approve" {
		t.Errorf("consensus verdict = %+v", v)
	}
	if v.Score != 8.5 {
		t.Errorf("consensus score = %v, want 8.5 (average)", v.Score)
	}
	if v.Votes != 2 {
		t.Errorf("consensus votes = %d, want 2", v.Votes)
	}
}

func TestAddVerdict_NilReceiver(t *testing.T) {
	var c *Contract
	c.AddVerdict(Verdict{}) // must not panic
}

func TestConsensus_NoVerdicts(t *testing.T) {
	c := &Contract{ID: "c-6", Kind: ContractNegotiation, Task: "t"}
	ok, v := c.Consensus(0.66)
	if ok {
		t.Error("Consensus on empty verdicts: expected false")
	}
	if v != (Verdict{}) {
		t.Errorf("zero verdict = %+v, want zero Verdict", v)
	}
}

func TestConsensus_RequiresFraction(t *testing.T) {
	c := &Contract{ID: "c-7", Kind: ContractJury, Task: "t"}
	c.AddVerdict(Verdict{ContenderID: "x", Decision: "approve", Score: 7})
	c.AddVerdict(Verdict{ContenderID: "x", Decision: "revise", Score: 5})
	// 2/3 of 2 votes requires 2 agreeing; only 1 each, so no consensus.
	if ok, _ := c.Consensus(2.0 / 3.0); ok {
		t.Error("Consensus(2/3): expected false with split votes")
	}
	// A strict majority (1 of 2) reaches consensus.
	ok, v := c.Consensus(0.5)
	if !ok {
		t.Fatal("Consensus(0.5): expected true")
	}
	if v.Decision != "approve" || v.ContenderID != "x" {
		t.Errorf("majority verdict = %+v, want approve", v)
	}
}

func TestConsensus_InvalidFractionDefaults(t *testing.T) {
	c := &Contract{ID: "c-8", Kind: ContractJury, Task: "t"}
	c.AddVerdict(Verdict{ContenderID: "y", Decision: "approve"})
	// 0 fraction would otherwise require 0 votes; default to strict majority.
	if ok, _ := c.Consensus(0); !ok {
		t.Error("Consensus(0): expected strict-majority default to agree with 1/1")
	}
	if ok, _ := c.Consensus(1.5); !ok {
		t.Error("Consensus(1.5): expected strict-majority default to agree with 1/1")
	}
}

func TestConsensus_DifferentContendersDoNotMerge(t *testing.T) {
	c := &Contract{ID: "c-9", Kind: ContractCompetition, Task: "t"}
	c.AddVerdict(Verdict{ContenderID: "a", Decision: "approve"})
	c.AddVerdict(Verdict{ContenderID: "b", Decision: "approve"})
	// Under a 2/3 supermajority neither faction reaches 2/3; approvals for
	// different contenders must not be merged into one consensus.
	if ok, _ := c.Consensus(2.0 / 3.0); ok {
		t.Error("Consensus(2/3): expected false, contenders differ")
	}
}

func TestContract_JSONRoundtrip(t *testing.T) {
	deadline := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	maxCost := 0.5
	maxToolCalls := 10
	c := &Contract{
		ID:          "c-10",
		Kind:        ContractCompetition,
		Task:        "Design a retry policy",
		Context:     "services",
		Constraints: []string{"no db"},
		Artifacts:   []string{"plan.md"},
		Acceptance:  []string{"passes review"},
		Deadline:    &deadline,
		Budget:      &ContractBudget{MaxCost: &maxCost, MaxToolCalls: &maxToolCalls},
		AuthorAgent: "lead",
		Verdicts: []Verdict{
			{ContenderID: "a", Decision: "approve", Score: 9},
		},
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got Contract
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.ID != c.ID || got.Kind != c.Kind || got.Task != c.Task {
		t.Errorf("roundtrip head = %+v", got)
	}
	// Verdicts must be excluded from serialization (json:"-").
	if len(got.Verdicts) != 0 {
		t.Errorf("Verdicts serialized despite json:\"-\", got %d", len(got.Verdicts))
	}
	if got.Budget == nil || got.Budget.MaxCost == nil || *got.Budget.MaxCost != 0.5 {
		t.Errorf("roundtrip budget = %+v", got.Budget)
	}
	if got.Deadline == nil || !got.Deadline.Equal(deadline) {
		t.Errorf("roundtrip deadline = %v", got.Deadline)
	}
}
