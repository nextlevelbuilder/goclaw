package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/contract"
	orchestration "github.com/nextlevelbuilder/goclaw/internal/orchestration"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// defaultNegotiationMaxRounds bounds a negotiation when the caller does not
// supply one, matching the orchestration package default.
const defaultNegotiationMaxRounds = 5

// consensusMatch is the fraction of agreeing votes required to lock a
// negotiation outcome.
const consensusMatch = 2.0 / 3.0

// NegotiateTool runs a bounded proposal/counter-proposal round over a
// contract. Proposals are submitted round by round; after each round the
// recorded votes are checked for 2/3 consensus. The outcome is persisted as a
// durable negotiation record that is closed on consensus or on exhaustion.
type NegotiateTool struct {
	contracts store.ContractStore // durable record persistence; nil-safe
	maxRounds int                 // default 5
}

// NewNegotiateTool creates a negotiation tool. The store is optional: when nil
// the tool still runs the round model and returns the outcome, it simply skips
// persistence.
func NewNegotiateTool(contracts store.ContractStore) *NegotiateTool {
	return &NegotiateTool{contracts: contracts, maxRounds: defaultNegotiationMaxRounds}
}

// SetMaxRounds overrides the round bound for subsequent executions.
func (t *NegotiateTool) SetMaxRounds(n int) {
	if n > 0 {
		t.maxRounds = n
	}
}

// Name returns the tool name.
func (t *NegotiateTool) Name() string { return "negotiate" }

// Description explains the tool's purpose to the model.
func (t *NegotiateTool) Description() string {
	return "Run a bounded negotiation over a task: submit proposals and votes " +
		"round by round until 2/3 consensus is reached or the round limit is " +
		"exhausted. Returns the consensus verdict or the final round state."
}

// Parameters declares the tool's JSON schema.
func (t *NegotiateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "The subject being negotiated",
			},
			"acceptance": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Acceptance criteria the negotiated outcome must meet",
			},
			"proposals": map[string]any{
				"type":        "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"author":  map[string]any{"type": "string"},
						"content": map[string]any{"type": "string"},
					},
				},
				"description": "Initial proposal list; each item is {author, content}",
			},
			"votes": map[string]any{
				"type":        "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"contender_id": map[string]any{"type": "string"},
						"decision":     map[string]any{"type": "string"},
						"score":        map[string]any{"type": "number"},
						"reason":       map[string]any{"type": "string"},
					},
				},
				"description": "Votes attached to the contract to reach consensus",
			},
			"max_rounds": map[string]any{
				"type":        "integer",
				"description": "Maximum proposal rounds (default 5)",
			},
		},
		"required": []string{"task"},
	}
}

// Execute parses args, runs the bounded negotiation, and persists the record.
func (t *NegotiateTool) Execute(ctx context.Context, args map[string]any) *Result {
	task, _ := args["task"].(string)
	if task == "" {
		return ErrorResult("negotiate: task is required")
	}

	maxRounds := t.maxRounds
	if m, ok := args["max_rounds"].(float64); ok && int(m) > 0 {
		maxRounds = int(m)
	}

	neg, err := orchestration.NewNegotiation(&contract.Contract{
		Kind:       contract.ContractNegotiation,
		Task:       task,
		Acceptance: parseAcceptance(args["acceptance"]),
	}, maxRounds)
	if err != nil {
		return ErrorResult(fmt.Sprintf("negotiate: %v", err))
	}

	proposals, err := parseProposals(args["proposals"])
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Submit the initial proposals one per round. A vote is attached with each
	// round (round i consumes the i-th caller-supplied vote, when present) and
	// consensus is checked after every round so the negotiation closes as soon
	// as 2/3 agreement locks, never exceeding the round bound.
	roundVotes := parseVotes(args["votes"])
	var consensus *contract.Verdict
	for i, p := range proposals {
		if neg.IsExhausted() {
			break
		}
		if err := neg.SubmitProposal(p.author, p.content); err != nil {
			slog.Warn("negotiate.proposal_failed", "author", p.author, "error", err)
			break
		}
		if i < len(roundVotes) {
			neg.Vote(roundVotes[i])
		}
		if ok, verdict := neg.ReachedConsensus(consensusMatch); ok {
			consensus = verdict
			break
		}
	}

	// Attach any remaining votes so a caller that supplies more votes than
	// proposals still gets them recorded within the bound.
	for i := len(proposals); i < len(roundVotes); i++ {
		if neg.IsExhausted() {
			break
		}
		neg.Vote(roundVotes[i])
		ok, verdict := neg.ReachedConsensus(consensusMatch)
		if ok {
			consensus = verdict
			break
		}
	}

	// When no explicit votes were supplied and the round count is exhausted,
	// the negotiation closes in its current (no-consensus) state.
	reached := consensus != nil
	slog.Info("negotiate.outcome",
		"rounds", neg.Rounds,
		"max_rounds", neg.MaxRounds,
		"consensus", reached,
	)

	status := store.ContractRecordDraft
	if reached || neg.IsExhausted() {
		status = store.ContractRecordClosed
	}

	recordID := ""
	// Verdicts carry a json:"-" tag on Contract, so they are embedded
	// explicitly to keep the durable record complete for audit.
	body, _ := json.Marshal(map[string]any{
		"contract":  neg.Contract,
		"proposals": neg.Proposals,
		"verdicts":  neg.Contract.Verdicts,
		"verdict":   consensus,
	})
	if t.contracts != nil {
		rec := &store.ContractRecord{
			TenantID: store.TenantIDFromContext(ctx),
			Kind:     store.ContractRecordNegotiation,
			Body:     string(body),
			Status:   status,
		}
		if err := t.contracts.CreateContractRecord(ctx, rec); err != nil {
			slog.Warn("negotiate.persist_failed", "error", err)
		} else {
			recordID = rec.ID.String()
		}
	}

	payload, _ := json.Marshal(map[string]any{
		"consensus": reached,
		"verdict":   consensus,
		"rounds":    neg.Rounds,
		"status":    status,
		"record_id": recordID,
	})
	return NewResult(string(payload))
}

// parseAcceptance reads the acceptance-criteria arg into a []string.
func parseAcceptance(raw any) []string {
	if raw == nil {
		return nil
	}
	out, err := parseStringList(raw, nil)
	if err != nil {
		return nil
	}
	return out
}

// negotiationProposal is a single submitted proposal.
type negotiationProposal struct {
	author  string
	content string
}

// parseProposals reads the proposals arg into a slice. Each item is an object
// with "author" and "content" strings; both are required.
func parseProposals(raw any) ([]negotiationProposal, error) {
	if raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("proposals must be a list of {author, content} objects")
	}
	out := make([]negotiationProposal, 0, len(items))
	for i, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("proposal %d must be an object", i)
		}
		author, _ := obj["author"].(string)
		content, _ := obj["content"].(string)
		if author == "" || content == "" {
			return nil, fmt.Errorf("proposal %d requires author and content", i)
		}
		out = append(out, negotiationProposal{author: author, content: content})
	}
	return out, nil
}

// parseVotes reads the votes arg into contract verdicts. Insufficient fields
// are skipped rather than rejected so malformed votes never block a round.
func parseVotes(raw any) []contract.Verdict {
	if raw == nil {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]contract.Verdict, 0, len(items))
	for i, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		v := contract.Verdict{}
		v.ContenderID, _ = obj["contender_id"].(string)
		v.Decision, _ = obj["decision"].(string)
		if s, ok := obj["score"].(float64); ok {
			v.Score = s
		}
		v.Reason, _ = obj["reason"].(string)
		if v.ContenderID == "" {
			slog.Warn("negotiate.vote_skipped", "index", i, "reason", "missing contender_id")
			continue
		}
		out = append(out, v)
	}
	return out
}