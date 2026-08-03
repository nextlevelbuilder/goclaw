// Package workflow turns an authored canvas graph into the primitives that
// actually run things.
//
// The split is deliberate (see migration 000083): the graph is authoring truth,
// the compiled primitives are execution truth. Nothing in the runtime reads a
// graph — the compiler translates it into cron jobs, and those are what fire. So
// a half-written graph can never stop an existing schedule, and an unarmed
// workflow is inert no matter what it contains.
package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Graph is the canvas document. Field names match what @xyflow/react serialises,
// so the client can post its state without a translation layer — and any node
// property the compiler does not understand rides along in Data untouched,
// because the canvas will grow fields faster than this compiler learns them.
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

type Node struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type Edge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
}

// TriggerData is the `data` of a node with type "trigger".
//
// Only cron kinds exist today. Channel and webhook triggers are named in the
// product plan but deliberately absent here: the compiler must refuse a trigger
// it cannot honour rather than arm a workflow that silently never fires.
type TriggerData struct {
	Kind string `json:"kind"` // "cron" | "every" | "at"
	Expr string `json:"expr"` // cron expression, for kind "cron"
	// EveryMS / AtMS mirror store.CronSchedule for the other two kinds.
	EveryMS *int64 `json:"everyMs,omitempty"`
	AtMS    *int64 `json:"atMs,omitempty"`
	TZ      string `json:"tz,omitempty"`
}

// AgentData is the `data` of a node with type "agent" as it appears in a
// workflow graph: which agent, and what to say to it when the trigger fires.
type AgentData struct {
	AgentID string `json:"agentId"`
	Prompt  string `json:"prompt"`
	// Deliver routes the result to a channel, mirroring cron's own delivery
	// fields. Absent = the run happens and its output stays in the run log.
	Deliver        bool   `json:"deliver,omitempty"`
	DeliverChannel string `json:"deliverChannel,omitempty"`
	DeliverTo      string `json:"deliverTo,omitempty"`
}

// OutputData is the `data` of a node with type "output": where a step's result
// goes when it finishes.
//
// Only channel delivery exists today, because that is the only thing cron can do
// with a result (see cmd/gateway_cron.go). Writing to a Sheet or a Doc is a TOOL
// the agent calls, not a delivery target, so it belongs in the instruction rather
// than here — modelling it as an output node would imply the platform routes it,
// which it does not.
type OutputData struct {
	Kind    string `json:"kind"`    // "channel"
	Channel string `json:"channel"` // channel instance name, e.g. "telegram-mybot"
	To      string `json:"to"`      // chat id within that channel
}

// Step is one compiled unit: "on this schedule, send this prompt to this agent".
type Step struct {
	TriggerNodeID string
	AgentNodeID   string
	Kind          string
	Expr          string
	EveryMS       *int64
	AtMS          *int64
	TZ            string
	AgentID       string
	Prompt        string
	// Handoffs are the agents this step passes work to, in graph order.
	//
	// MODEL-DRIVEN, not a deterministic pipeline. goclaw's delegation is a TOOL the
	// agent chooses to call, so a chain compiles to (a) a link permitting the
	// handoff and (b) an instruction telling the agent to make it. The sequence is
	// something the model is asked to do, not something the runtime enforces — a
	// real sequencing runtime can replace this behind the same graph shape, which is
	// why the chain is modelled explicitly here instead of buried in the prompt.
	Handoffs    []Handoff
	Deliver     bool
	DeliverChan string
	DeliverTo   string
}

// Handoff is one link in a chain: who to pass to, and what to ask for.
type Handoff struct {
	NodeID  string
	AgentID string
	Prompt  string
}

// ValidationError names the node or edge at fault so the canvas can highlight it
// instead of showing a message the user has to map back to a shape themselves.
type ValidationError struct {
	NodeID string
	Msg    string
}

func (e ValidationError) Error() string {
	if e.NodeID == "" {
		return e.Msg
	}
	return fmt.Sprintf("%s (node %s)", e.Msg, e.NodeID)
}

// ParseGraph decodes an authored graph. A nil/empty blob is an empty graph, not
// an error: that is what a workflow looks like the moment it is created.
func ParseGraph(raw json.RawMessage) (*Graph, error) {
	if len(raw) == 0 {
		return &Graph{}, nil
	}
	var g Graph
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, fmt.Errorf("graph is not readable: %w", err)
	}
	return &g, nil
}

// Plan validates a graph and returns the steps it compiles to.
//
// Refuses rather than guesses. Every failure here is something the user can see
// and fix on the canvas, and arming a workflow that cannot fire — or that fires
// something other than what is drawn — is worse than refusing to arm it.
func (g *Graph) Plan() ([]Step, error) {
	byID := make(map[string]Node, len(g.Nodes))
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}

	var steps []Step
	for _, e := range g.Edges {
		src, okS := byID[e.Source]
		dst, okD := byID[e.Target]
		if !okS || !okD {
			// A dangling edge means the client sent a graph it had already broken.
			// Ignoring it would compile a workflow that is not the one on screen.
			return nil, ValidationError{Msg: "an edge points at a node that is not in the graph"}
		}
		if src.Type != "trigger" {
			// Non-trigger edges (agent→agent chains, capability grants) are not
			// compiled YET. Skipped rather than rejected so a graph can carry shapes
			// this compiler will learn later without becoming unsavable.
			continue
		}
		if dst.Type != "agent" {
			return nil, ValidationError{NodeID: e.Target, Msg: "a trigger must connect to an agent"}
		}

		var td TriggerData
		if len(src.Data) > 0 {
			if err := json.Unmarshal(src.Data, &td); err != nil {
				return nil, ValidationError{NodeID: src.ID, Msg: "trigger settings are not readable"}
			}
		}
		var ad AgentData
		if len(dst.Data) > 0 {
			if err := json.Unmarshal(dst.Data, &ad); err != nil {
				return nil, ValidationError{NodeID: dst.ID, Msg: "agent settings are not readable"}
			}
		}

		step := Step{
			TriggerNodeID: src.ID,
			AgentNodeID:   dst.ID,
			Kind:          strings.TrimSpace(td.Kind),
			Expr:          strings.TrimSpace(td.Expr),
			EveryMS:       td.EveryMS,
			AtMS:          td.AtMS,
			TZ:            strings.TrimSpace(td.TZ),
			AgentID:       strings.TrimSpace(ad.AgentID),
			Prompt:        strings.TrimSpace(ad.Prompt),
			Deliver:       ad.Deliver,
			DeliverChan:   strings.TrimSpace(ad.DeliverChannel),
			DeliverTo:     strings.TrimSpace(ad.DeliverTo),
		}
		// Follow the chain of agent→agent edges from this step.
		if err := g.attachHandoffs(&step, byID); err != nil {
			return nil, err
		}
		// Delivery, if the LAST agent in the chain is wired to an output node.
		if err := g.attachOutput(&step, byID); err != nil {
			return nil, err
		}
		if err := step.validate(); err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}

	if len(steps) == 0 {
		// Arming a workflow that compiles to nothing would report success and then
		// never fire — the most confusing possible outcome.
		return nil, ValidationError{Msg: "this workflow has no trigger connected to an agent, so there is nothing to run"}
	}
	return steps, nil
}

// maxChainLength bounds a handoff chain.
//
// Each hop is an agent run, so a long chain is expensive and slow — and a runaway
// one is hard to stop once armed. Eight is far beyond any workflow drawn so far.
const maxChainLength = 8

// attachHandoffs walks agent→agent edges from the step's agent and records the
// chain, moving step.AgentNodeID to the LAST agent so an output node attaches to
// the END of the chain rather than the start.
//
// Refuses a fork: an agent with two outgoing agent edges has no defined order, and
// picking one silently would run a workflow other than the one drawn.
func (g *Graph) attachHandoffs(step *Step, byID map[string]Node) error {
	seen := map[string]bool{step.AgentNodeID: true}
	current := step.AgentNodeID

	for {
		var next Node
		var found bool
		for _, e := range g.Edges {
			if e.Source != current {
				continue
			}
			dst, ok := byID[e.Target]
			if !ok || dst.Type != "agent" {
				continue
			}
			if found {
				return ValidationError{NodeID: current, Msg: "this agent hands off to more than one agent, so the order is unclear"}
			}
			next, found = dst, true
		}
		if !found {
			break
		}
		if seen[next.ID] {
			// A cycle would compile to a chain that instructs itself forever.
			return ValidationError{NodeID: next.ID, Msg: "these agents hand off to each other in a loop"}
		}
		if len(step.Handoffs)+1 >= maxChainLength {
			return ValidationError{NodeID: next.ID, Msg: fmt.Sprintf("a workflow can chain at most %d agents", maxChainLength)}
		}

		var ad AgentData
		if len(next.Data) > 0 {
			if err := json.Unmarshal(next.Data, &ad); err != nil {
				return ValidationError{NodeID: next.ID, Msg: "agent settings are not readable"}
			}
		}
		agentID := strings.TrimSpace(ad.AgentID)
		prompt := strings.TrimSpace(ad.Prompt)
		if agentID == "" {
			return ValidationError{NodeID: next.ID, Msg: "choose which agent should run"}
		}
		if _, err := uuid.Parse(agentID); err != nil {
			return ValidationError{NodeID: next.ID, Msg: "the agent must be identified by its id, not its name"}
		}
		if prompt == "" {
			return ValidationError{NodeID: next.ID, Msg: "say what this agent should do"}
		}

		step.Handoffs = append(step.Handoffs, Handoff{NodeID: next.ID, AgentID: agentID, Prompt: prompt})
		seen[next.ID] = true
		current = next.ID
	}

	if len(step.Handoffs) > 0 {
		// Delivery belongs to the END of the chain: the last agent produces the result
		// a user sees, so an output wired to the first would send the intermediate one.
		step.AgentNodeID = current
	}
	return nil
}

// attachOutput finds the output node an agent is wired to and folds it into the
// step.
//
// Refuses an incomplete output rather than passing it through. Cron only delivers
// when Deliver AND channel AND chat id are all present — otherwise it logs
// "output discarded" and the user sees nothing at all, which is the worst
// available outcome: the workflow reports success and silently goes nowhere.
func (g *Graph) attachOutput(step *Step, byID map[string]Node) error {
	var found *OutputData
	var foundID string
	for _, e := range g.Edges {
		if e.Source != step.AgentNodeID {
			continue
		}
		dst, ok := byID[e.Target]
		if !ok || dst.Type != "output" {
			continue
		}
		var od OutputData
		if len(dst.Data) > 0 {
			if err := json.Unmarshal(dst.Data, &od); err != nil {
				return ValidationError{NodeID: dst.ID, Msg: "output settings are not readable"}
			}
		}
		if found != nil {
			// cron carries ONE delivery target per job. Two would mean silently
			// dropping one, so the graph has to say which.
			return ValidationError{NodeID: dst.ID, Msg: "an agent can only send its result to one place"}
		}
		found = &od
		foundID = dst.ID
	}
	if found == nil {
		return nil // no output wired: the result stays in the run log
	}

	kind := strings.TrimSpace(found.Kind)
	if kind == "" {
		kind = "channel"
	}
	if kind != "channel" {
		return ValidationError{NodeID: foundID, Msg: fmt.Sprintf("results cannot be sent to %q yet", kind)}
	}
	channel := strings.TrimSpace(found.Channel)
	to := strings.TrimSpace(found.To)
	if channel == "" {
		return ValidationError{NodeID: foundID, Msg: "choose where to send the result"}
	}
	if to == "" {
		// Named separately from the channel: cron discards the output when either is
		// missing, and "which chat" is the half people forget.
		return ValidationError{NodeID: foundID, Msg: "set which chat to send it to"}
	}
	step.Deliver = true
	step.DeliverChan = channel
	step.DeliverTo = to
	return nil
}

func (s Step) validate() error {
	if s.AgentID == "" {
		return ValidationError{NodeID: s.AgentNodeID, Msg: "choose which agent should run"}
	}
	// The agent id must be the row UUID, not the agent_key.
	//
	// This is enforced here because the cron store PARSES it as a UUID and
	// silently drops anything else (see PGCronStore.AddJob) — a graph carrying a
	// key would arm successfully and then run with no agent attached, i.e. the
	// wrong agent, with nothing anywhere saying so. Caught by the real-store
	// integration test; the fake accepted the key happily.
	if _, err := uuid.Parse(s.AgentID); err != nil {
		return ValidationError{NodeID: s.AgentNodeID, Msg: "the agent must be identified by its id, not its name"}
	}
	if s.Prompt == "" {
		// A cron job with an empty message would start a run with nothing to do.
		return ValidationError{NodeID: s.AgentNodeID, Msg: "say what the agent should do"}
	}
	switch s.Kind {
	case "cron":
		if s.Expr == "" {
			return ValidationError{NodeID: s.TriggerNodeID, Msg: "set a schedule"}
		}
	case "every":
		if s.EveryMS == nil || *s.EveryMS <= 0 {
			return ValidationError{NodeID: s.TriggerNodeID, Msg: "set how often this should repeat"}
		}
		// A sub-minute repeat is almost always a mistyped unit, and it would start
		// an agent run every few seconds — expensive and hard to stop.
		if *s.EveryMS < int64(time.Minute/time.Millisecond) {
			return ValidationError{NodeID: s.TriggerNodeID, Msg: "repeat interval must be at least one minute"}
		}
	case "at":
		if s.AtMS == nil || *s.AtMS <= 0 {
			return ValidationError{NodeID: s.TriggerNodeID, Msg: "set when this should run"}
		}
	case "":
		return ValidationError{NodeID: s.TriggerNodeID, Msg: "choose a trigger type"}
	default:
		// Named explicitly so a channel/webhook trigger drawn before the compiler
		// supports it fails loudly at arm time instead of silently never firing.
		return ValidationError{NodeID: s.TriggerNodeID, Msg: fmt.Sprintf("trigger type %q cannot be scheduled yet", s.Kind)}
	}
	return nil
}
