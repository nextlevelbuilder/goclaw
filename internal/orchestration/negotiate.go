package orchestration

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/contract"
)

// Participant limits binding: a negotiation stops accepting proposals beyond
// MaxRounds, regardless of consensus status.
const defaultMaxRounds = 5

// ErrNilContract reports a negotiation constructed with no contract.
var ErrNilContract = errors.New("orchestration: negotiation: nil contract")

// ErrInvalidMaxRounds reports a negative round bound.
var ErrInvalidMaxRounds = errors.New("orchestration: negotiation: max rounds must be >= 1")

// ErrNegotiationExhausted reports proposal submission after the round bound.
var ErrNegotiationExhausted = errors.New("orchestration: negotiation: max rounds reached")

// ErrEmptyProposal reports a proposal with no author or no content.
var ErrEmptyProposal = errors.New("orchestration: negotiation: proposal author and content required")

// Proposal is a single message in a negotiation round.
type Proposal struct {
	Round   int
	Author  string
	Content string
}

// Negotiation is a bounded proposal/counter-proposal vote model. Participants
// submit proposals round by round; votes attach verdicts to the underlying
// contract. A negotiation is exhausted when MaxRounds proposals have been
// submitted or the proposal stream has been closed by consensus.
type Negotiation struct {
	// Contract is the negotiation subject; its Verdicts accumulate votes.
	Contract *contract.Contract
	// Proposals records every submitted proposal in order.
	Proposals []Proposal
	// Rounds counts accepted proposal rounds so far.
	Rounds int
	// MaxRounds is the upper bound on accepted rounds.
	MaxRounds int
	// consensusReached snapshots the negotiated outcome once one exists.
	consensusReached bool
}

// NewNegotiation returns a live negotiation over the given contract. A nil
// contract is an error; a negative round bound is invalid; zero defaults to
// defaultMaxRounds.
func NewNegotiation(c *contract.Contract, maxRounds int) (*Negotiation, error) {
	if c == nil {
		return nil, ErrNilContract
	}
	if maxRounds < 0 {
		return nil, ErrInvalidMaxRounds
	}
	if maxRounds == 0 {
		maxRounds = defaultMaxRounds
	}
	return &Negotiation{Contract: c, MaxRounds: maxRounds}, nil
}

// IsExhausted reports whether no further proposals or votes can be accepted:
// the round bound is reached or consensus was already reached.
func (n *Negotiation) IsExhausted() bool {
	if n == nil {
		return true
	}
	return n.consensusReached || n.Rounds >= n.MaxRounds
}

// SubmitProposal records a proposal for the next round. It fails when the
// negotiation is exhausted (round bound reached or consensus already closed)
// or when the proposal lacks an author or content. Each accepted proposal
// advances Rounds by one, so exceeding MaxRounds closes the negotiation.
func (n *Negotiation) SubmitProposal(author, content string) error {
	if n == nil {
		return errors.New("orchestration: negotiation: nil negotiation")
	}
	if author == "" || content == "" {
		return ErrEmptyProposal
	}
	if n.IsExhausted() {
		return ErrNegotiationExhausted
	}
	n.Rounds++
	n.Proposals = append(n.Proposals, Proposal{Round: n.Rounds, Author: author, Content: content})
	slog.Debug("orchestration.negotiation.proposal",
		"author", author, "round", n.Rounds, "max_rounds", n.MaxRounds)
	return nil
}

// Vote attaches a verdict to the contract. The MaxRounds bound governs
// proposals, not votes: a vote is recorded as long as no consensus has been
// locked yet. Once consensus closes the negotiation, further votes are
// ignored so the recorded outcome stays stable.
func (n *Negotiation) Vote(verdict contract.Verdict) {
	if n == nil || n.Contract == nil {
		return
	}
	if n.consensusReached {
		slog.Warn("orchestration.negotiation.vote_ignored", "reason", "consensus already reached")
		return
	}
	n.Contract.AddVerdict(verdict)
	n.maybeLockConsensus()
}

// maybeLockConsensus checks the contract's verdicts under the default 2/3
// match and caches the outcome so subsequent IsExhausted/ReachedConsensus
// calls are deterministic. A single vote is not a panel, so at least two
// votes are required before a negotiation can lock consensus by itself
// (explicit ReachedConsensus calls may still agree on a one-vote contract).
func (n *Negotiation) maybeLockConsensus() {
	if n.consensusReached {
		return
	}
	if len(n.Contract.Verdicts) < 2 {
		return
	}
	if ok, _ := n.Contract.Consensus(consensusMatch); ok {
		n.consensusReached = true
	}
}

// consensusMatch is the negotiation approval fraction (2/3).
const consensusMatch = 2.0 / 3.0

// ReachedConsensus reports whether the negotiated votes reached the match
// fraction. It returns the winning verdict when a consensus exists.
func (n *Negotiation) ReachedConsensus(match float64) (bool, *contract.Verdict) {
	if n == nil || n.Contract == nil {
		return false, nil
	}
	if match <= 0 || match > 1 {
		match = consensusMatch
	}
	ok, v := n.Contract.Consensus(match)
	if ok {
		n.consensusReached = true
	}
	if !ok {
		return false, nil
	}
	return true, &v
}

// String summarizes the negotiation state for logs.
func (n *Negotiation) String() string {
	if n == nil {
		return "negotiation<nil>"
	}
	return fmt.Sprintf("negotiation{rounds=%d/%d, proposals=%d, consensus=%t}",
		n.Rounds, n.MaxRounds, len(n.Proposals), n.consensusReached)
}