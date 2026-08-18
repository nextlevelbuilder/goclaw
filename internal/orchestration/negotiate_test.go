package orchestration

import (
	"errors"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/contract"
)

func mustContract(t *testing.T, kind contract.ContractKind) *contract.Contract {
	t.Helper()
	c := &contract.Contract{ID: "c-negotiate", Kind: kind, Task: "agree on retry policy"}
	if err := c.Validate(); err != nil {
		t.Fatalf("contract.Validate: %v", err)
	}
	return c
}

func TestNewNegotiation_Errors(t *testing.T) {
	if _, err := NewNegotiation(nil, 5); err != ErrNilContract {
		t.Errorf("nil contract: got %v, want ErrNilContract", err)
	}
	if _, err := NewNegotiation(mustContract(t, contract.ContractNegotiation), -1); err != ErrInvalidMaxRounds {
		t.Errorf("negative max rounds: got %v, want ErrInvalidMaxRounds", err)
	}
}

func TestNewNegotiation_DefaultMaxRounds(t *testing.T) {
	n, err := NewNegotiation(mustContract(t, contract.ContractNegotiation), 0)
	if err != nil {
		t.Fatalf("NewNegotiation: %v", err)
	}
	if n.MaxRounds != defaultMaxRounds {
		t.Errorf("MaxRounds = %d, want %d", n.MaxRounds, defaultMaxRounds)
	}
}

func TestSubmitProposal_AdvancesRounds(t *testing.T) {
	n, err := NewNegotiation(mustContract(t, contract.ContractNegotiation), 3)
	if err != nil {
		t.Fatalf("NewNegotiation: %v", err)
	}
	if err := n.SubmitProposal("a", "proposal 1"); err != nil {
		t.Fatalf("SubmitProposal: %v", err)
	}
	if err := n.SubmitProposal("b", "proposal 2"); err != nil {
		t.Fatalf("SubmitProposal: %v", err)
	}
	if n.Rounds != 2 || len(n.Proposals) != 2 {
		t.Fatalf("rounds=%d proposals=%d, want 2/2", n.Rounds, len(n.Proposals))
	}
	if n.Proposals[0].Author != "a" || n.Proposals[0].Round != 1 {
		t.Errorf("proposal[0] = %+v", n.Proposals[0])
	}
	if n.Proposals[1].Author != "b" || n.Proposals[1].Round != 2 {
		t.Errorf("proposal[1] = %+v", n.Proposals[1])
	}
}

func TestSubmitProposal_Errors(t *testing.T) {
	n, _ := NewNegotiation(mustContract(t, contract.ContractNegotiation), 2)
	if err := n.SubmitProposal("", "content"); err != ErrEmptyProposal {
		t.Errorf("empty author: got %v, want ErrEmptyProposal", err)
	}
	if err := n.SubmitProposal("a", ""); err != ErrEmptyProposal {
		t.Errorf("empty content: got %v, want ErrEmptyProposal", err)
	}
	if err := n.SubmitProposal("a", "ok"); err != nil {
		t.Fatalf("SubmitProposal: %v", err)
	}
	if err := n.SubmitProposal("b", "ok2"); err != nil {
		t.Fatalf("SubmitProposal: %v", err)
	}
	if err := n.SubmitProposal("c", "over budget"); err != ErrNegotiationExhausted {
		t.Errorf("over budget: got %v, want ErrNegotiationExhausted", err)
	}
	if !n.IsExhausted() {
		t.Error("IsExhausted = false, want true at round bound")
	}
}

func TestVote_AccumulatesAndDeterministicConsensus(t *testing.T) {
	n, _ := NewNegotiation(mustContract(t, contract.ContractNegotiation), 5)
	n.Vote(contract.Verdict{ContenderID: "x", Decision: "approve", Score: 8})
	n.Vote(contract.Verdict{ContenderID: "x", Decision: "approve", Score: 9})
	if len(n.Contract.Verdicts) != 2 {
		t.Fatalf("verdicts = %d, want 2", len(n.Contract.Verdicts))
	}
	ok, v := n.ReachedConsensus(2.0 / 3.0)
	if !ok {
		t.Fatal("ReachedConsensus(2/3): expected true, both agree")
	}
	if v == nil || v.ContenderID != "x" || v.Decision != "approve" {
		t.Errorf("consensus = %+v", v)
	}
	if !n.IsExhausted() {
		t.Error("IsExhausted = false, want true after consensus")
	}
}

func TestVote_NoConsensusOnSplit(t *testing.T) {
	n, _ := NewNegotiation(mustContract(t, contract.ContractNegotiation), 5)
	n.Vote(contract.Verdict{ContenderID: "x", Decision: "approve"})
	n.Vote(contract.Verdict{ContenderID: "x", Decision: "revise"})
	if ok, _ := n.ReachedConsensus(2.0 / 3.0); ok {
		t.Error("ReachedConsensus(2/3): expected false on split vote")
	}
	if n.IsExhausted() {
		t.Error("IsExhausted = true; split votes are not consensus")
	}
}

func TestVote_AfterConsensusIsIgnored(t *testing.T) {
	n, _ := NewNegotiation(mustContract(t, contract.ContractNegotiation), 5)
	n.Vote(contract.Verdict{ContenderID: "x", Decision: "approve"})
	n.Vote(contract.Verdict{ContenderID: "x", Decision: "approve"})
	if !n.IsExhausted() {
		t.Fatal("expected consensus-exhausted")
	}
	n.Vote(contract.Verdict{ContenderID: "x", Decision: "reject"})
	if len(n.Contract.Verdicts) != 2 {
		t.Errorf("verdicts = %d, want 2 (late vote rejected)", len(n.Contract.Verdicts))
	}
}

func TestVote_StillAcceptedAtRoundBound(t *testing.T) {
	// The round bound limits proposals, not votes. A vote recorded after the
	// proposal budget is spent is still valid.
	n, _ := NewNegotiation(mustContract(t, contract.ContractNegotiation), 2)
	if err := n.SubmitProposal("a", "p1"); err != nil {
		t.Fatalf("SubmitProposal: %v", err)
	}
	if err := n.SubmitProposal("b", "p2"); err != nil {
		t.Fatalf("SubmitProposal: %v", err)
	}
	if !n.IsExhausted() {
		t.Fatal("expected exhaustion at round bound")
	}
	n.Vote(contract.Verdict{ContenderID: "x", Decision: "approve"})
	if len(n.Contract.Verdicts) != 1 {
		t.Errorf("verdicts = %d, want 1 (votes not bounded by rounds)", len(n.Contract.Verdicts))
	}
}

func TestReachedConsensus_Nil(t *testing.T) {
	var n *Negotiation
	if ok, v := n.ReachedConsensus(0.66); ok || v != nil {
		t.Errorf("nil ReachedConsensus = ok:%v v:%v, want false/nil", ok, v)
	}
}

func TestReachedConsensus_InvalidFractionDefaults(t *testing.T) {
	n, _ := NewNegotiation(mustContract(t, contract.ContractNegotiation), 5)
	n.Vote(contract.Verdict{ContenderID: "x", Decision: "approve"})
	if ok, _ := n.ReachedConsensus(0); !ok {
		t.Error("ReachedConsensus(0): expected consensus with 1/1 under default")
	}
}

func TestSubmitProposal_NilNegotiation(t *testing.T) {
	var n *Negotiation
	if err := n.SubmitProposal("a", "x"); err == nil {
		t.Error("nil SubmitProposal: expected error")
	}
}

func TestVote_StillAcceptedAfterRoundBound(t *testing.T) {
	n, _ := NewNegotiation(mustContract(t, contract.ContractNegotiation), 1)
	if err := n.SubmitProposal("a", "p1"); err != nil {
		t.Fatalf("SubmitProposal: %v", err)
	}
	n.Vote(contract.Verdict{ContenderID: "x", Decision: "approve"})
	if len(n.Contract.Verdicts) != 1 {
		t.Errorf("verdicts = %d, want 1 after round bound", len(n.Contract.Verdicts))
	}
}

func TestNegotiation_String(t *testing.T) {
	n, _ := NewNegotiation(mustContract(t, contract.ContractNegotiation), 5)
	s := n.String()
	if s == "" {
		t.Error("String() empty")
	}
	var nilN *Negotiation
	if nilN.String() == "" {
		t.Error("nil String() empty")
	}
}

func TestRunParallelError_Unwrap(t *testing.T) {
	base := errors.New("boom")
	e := &RunParallelError{ContestantID: "a", Err: base}
	if !errors.Is(e, base) {
		t.Error("errors.Is should unwrap to base")
	}
	if e.Error() == "" {
		t.Error("Error() empty")
	}
}