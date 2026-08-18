// Package contract defines the first-class multi-agent collaboration
// primitives: handoff contracts, jury/competition contracts, and negotiation
// contracts. It is a pure domain package (mirroring internal/artifact): no
// store or provider imports, so any surface (tools, RPC methods, workflow
// steps) can build and validate a Contract without touching the database.
package contract

import (
	"errors"
	"math"
	"time"
)

// ContractKind enumerates the collaboration modes a contract can represent.
type ContractKind string

const (
	// ContractHandoff hands a task from one agent to another with acceptance
	// criteria and a deadline.
	ContractHandoff ContractKind = "handoff"
	// ContractJury asks a panel of judges to evaluate a produced outcome.
	ContractJury ContractKind = "jury"
	// ContractCompetition fans the same task out to multiple contenders and
	// selects the best result by scoring criteria.
	ContractCompetition ContractKind = "competition"
	// ContractNegotiation runs a bounded proposal/counter-proposal round model
	// between agents until consensus or exhaustion.
	ContractNegotiation ContractKind = "negotiation"
)

// ValidContractKind reports whether k is a known contract kind.
func ValidContractKind(k ContractKind) bool {
	switch k {
	case ContractHandoff, ContractJury, ContractCompetition, ContractNegotiation:
		return true
	}
	return false
}

// ContractBudget bounds a contract's execution. Each field is optional; a nil
// field means the limit is not enforced for that axis.
type ContractBudget struct {
	MaxCost      *float64 `json:"max_cost,omitempty"`
	MaxDuration  *string  `json:"max_duration,omitempty"` // human duration, e.g. "15m"
	MaxToolCalls *int     `json:"max_tool_calls,omitempty"`
}

// Verdict is a judge's (or a negotiation voter's) evaluation of a contender.
// Decision is one of "approve", "reject", or "revise". Votes is the number of
// panel members that produced this verdict when aggregated from a jury.
type Verdict struct {
	ContenderID string  `json:"contender_id"`
	Decision    string  `json:"decision"` // approve|reject|revise
	Score       float64 `json:"score"`
	Reason      string  `json:"reason"`
	Votes       int     `json:"votes"`
	JudgeAgent  string  `json:"judge_agent,omitempty"`
}

// Contract describes a single multi-agent collaboration. Verdicts is excluded
// from JSON serialization: it is runtime aggregation state, persisted
// separately by the store layer.
type Contract struct {
	ID          string          `json:"id"`
	Kind        ContractKind    `json:"kind"`
	Task        string          `json:"task"`
	Context     string          `json:"context,omitempty"`
	Constraints []string        `json:"constraints,omitempty"`
	Artifacts   []string        `json:"artifacts,omitempty"` // relative paths or artifact IDs
	Acceptance  []string        `json:"acceptance_criteria,omitempty"`
	Deadline    *time.Time      `json:"deadline,omitempty"`
	Budget      *ContractBudget `json:"budget,omitempty"`
	AuthorAgent string          `json:"author_agent,omitempty"`
	Verdicts    []Verdict       `json:"-"`
}

// ErrInvalidKind reports an empty or unknown contract kind.
var ErrInvalidKind = errors.New("contract: invalid or missing kind")

// ErrEmptyTask reports a contract with no task description.
var ErrEmptyTask = errors.New("contract: task must not be empty")

// Validate verifies the contract's required fields: a known Kind and a
// non-empty Task. Optional fields (context, constraints, budget, deadline)
// are not validated here.
func (c *Contract) Validate() error {
	if c == nil {
		return errors.New("contract: nil contract")
	}
	if !ValidContractKind(c.Kind) {
		return ErrInvalidKind
	}
	if c.Task == "" {
		return ErrEmptyTask
	}
	return nil
}

// AddVerdict appends a single verdict to the contract's aggregation state.
func (c *Contract) AddVerdict(v Verdict) {
	if c == nil {
		return
	}
	c.Verdicts = append(c.Verdicts, v)
}

// Consensus reports whether at least matchFraction of the recorded verdicts
// share the same Decision for a single contender. When a majority exists it
// returns the majority verdict; otherwise it returns (false, zero Verdict).
// A contract with no verdicts never reaches consensus. matchFraction must be
// in (0, 1]; values outside that range default to a strict majority (0.5).
func (c *Contract) Consensus(matchFraction float64) (bool, Verdict) {
	if c == nil || len(c.Verdicts) == 0 {
		return false, Verdict{}
	}
	f := matchFraction
	if f <= 0 || f > 1 {
		f = 0.5
	}
	// Required number of agreeing verdicts, rounded up so the agreeing
	// fraction is at least matchFraction (e.g. 2/3 of 3 votes needs 2, of 2
	// votes needs 2). The epsilon guards floating-point representation error
	// (e.g. 2.0/3.0*3 = 1.9999999999999998) without flipping exact integers.
	ratio := float64(len(c.Verdicts)) * f
	required := int(math.Ceil(ratio - 1e-9))
	if required < 1 {
		required = 1
	}
	counts := make(map[string]*verdictAccum, 4)
	for _, v := range c.Verdicts {
		key := v.ContenderID + "\x00" + v.Decision
		acc := counts[key]
		if acc == nil {
			acc = &verdictAccum{contenderID: v.ContenderID, decision: v.Decision}
			counts[key] = acc
		}
		acc.votes++
		acc.totalScore += v.Score
		acc.lastReason = v.Reason
		acc.lastJudge = v.JudgeAgent
		if acc.votes >= required {
			// Average the panel scores; the majority decision wins.
			return true, Verdict{
				ContenderID: acc.contenderID,
				Decision:    acc.decision,
				Score:       acc.totalScore / float64(acc.votes),
				Reason:      acc.lastReason,
				Votes:       acc.votes,
				JudgeAgent:  acc.lastJudge,
			}
		}
	}
	return false, Verdict{}
}

// verdictAccum accumulates votes toward a consensus during a single scan.
type verdictAccum struct {
	contenderID string
	decision    string
	votes       int
	totalScore  float64
	lastReason  string
	lastJudge   string
}
