package teamworkclassify

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/decisionmodel"
	"github.com/nextlevelbuilder/goclaw/internal/teamconfig"
)

type fakeDecider struct {
	dec   decisionmodel.Decision
	err   error
	calls int
	state decisionmodel.State
}

func (d *fakeDecider) Decide(_ context.Context, s decisionmodel.State) (decisionmodel.Decision, error) {
	d.calls++
	d.state = s
	return d.dec, d.err
}

// deciderInput is ambiguous evidence, lead role, routing enabled, two members.
func deciderInput(d decisionmodel.Decider) Input {
	in := ambiguousInput()
	in.TeamRole = "lead"
	in.CanAssignTeamTasks = true
	in.CurrentAgent.Key = "lead-agent"
	in.Members = []Profile{
		{Kind: "team_member", Name: "Lead", Key: "lead-agent", Text: "chiến lược"},
		{Kind: "team_member", Name: "Coder", Key: "coder-agent", Text: "chiến lược code"},
		{Kind: "team_member", Name: "Writer", Key: "writer-agent", Text: "chiến lược content"},
	}
	in.RoutingRules = []teamconfig.RoutingRule{{AgentKey: "writer-agent", Criteria: "Writes marketing copy"}}
	in.RoutingEnabled = true
	in.DecisionThreshold = 0.65
	in.DecisionFallbackToSelf = true
	in.Decider = d
	return in
}

func TestDeciderReplacesArbiterOnAmbiguousEvidence(t *testing.T) {
	d := &fakeDecider{dec: decisionmodel.Decision{Option: "coder-agent", Confidence: 0.9, Model: "jev-1.13.0"}}
	arbiter := &fakeArbiterProvider{content: `{"decision":"self","mode":"team"}`}
	got := ClassifyWithLLM(context.Background(), deciderInput(d), arbiter, "lead-model", nil)

	if arbiter.calls != 0 {
		t.Fatal("LLM arbiter must not run when the decision model is enabled")
	}
	if d.calls != 1 || got.Decision != DecisionTeam || got.TargetAgentKey != "coder-agent" || got.RequiredTool != "team_tasks" {
		t.Fatalf("got %+v (calls=%d)", got, d.calls)
	}
	if !strings.Contains(got.WorkflowHint, `assignee="coder-agent"`) || !strings.Contains(got.WorkflowHint, "search") {
		t.Fatalf("hint must be search-first and name the member: %q", got.WorkflowHint)
	}
	if got.Source() != "decision_model" {
		t.Fatalf("Source = %q", got.Source())
	}
	// Options: other members (rule criteria wins over profile text) + self; never the sender.
	opts := d.state.Options
	if _, ok := opts["lead-agent"]; ok {
		t.Fatal("sender offered as its own delegate")
	}
	if opts["writer-agent"] != "Writes marketing copy" || opts["coder-agent"] == "" {
		t.Fatalf("options = %+v", opts)
	}
}

func TestDeciderNonRoutingAnswersKeepEvidenceVerdict(t *testing.T) {
	cases := map[string]*fakeDecider{
		"unavailable":     {err: decisionmodel.ErrUnavailable},
		"chose self":      {dec: decisionmodel.Decision{Option: decisionmodel.SelfOption, Confidence: 0.99}},
		"invented member": {dec: decisionmodel.Decision{Option: "ghost-agent", Confidence: 0.99}},
		"sender itself":   {dec: decisionmodel.Decision{Option: "lead-agent", Confidence: 0.99}},
		"low confidence":  {dec: decisionmodel.Decision{Option: "coder-agent", Confidence: 0.2}},
	}
	for name, d := range cases {
		arbiter := &fakeArbiterProvider{content: `{"decision":"team","mode":"team"}`}
		got := ClassifyWithLLM(context.Background(), deciderInput(d), arbiter, "lead-model", nil)
		if got.Decision != DecisionSelf || got.RequiredTool != "" || got.TargetAgentKey != "" {
			t.Errorf("%s: got %+v, want evidence self verdict", name, got)
		}
		if arbiter.calls != 0 {
			t.Errorf("%s: decision-model failure fell through to the LLM arbiter", name)
		}
		if got.Source() != "decision_model" {
			t.Errorf("%s: Source = %q", name, got.Source())
		}
	}
	// Transport failure must be distinguishable from a "self" answer in logs.
	got := ClassifyWithLLM(context.Background(), deciderInput(cases["unavailable"]), nil, "", nil)
	if !strings.HasPrefix(got.Reason, "decision_model_unavailable") {
		t.Fatalf("reason = %q", got.Reason)
	}
}

func TestDeciderFallbackOffTrustsLowConfidence(t *testing.T) {
	d := &fakeDecider{dec: decisionmodel.Decision{Option: "coder-agent", Confidence: 0.2}}
	in := deciderInput(d)
	in.DecisionFallbackToSelf = false
	if got := ClassifyWithLLM(context.Background(), in, nil, "", nil); got.TargetAgentKey != "coder-agent" {
		t.Fatalf("fallback_to_self=false must route on the choice, got %+v", got)
	}
}

func TestDeciderNotConsulted(t *testing.T) {
	type tc struct {
		name string
		mod  func(*Input)
	}
	for _, c := range []tc{
		{"routing disabled for team", func(in *Input) { in.RoutingEnabled = false }},
		{"delegate mode", func(in *Input) { in.Mode = ModeDelegate }},
		{"decisive evidence", func(in *Input) {
			dec := decisiveSelfInput()
			in.Message, in.Embedder, in.CurrentAgent.Text = dec.Message, dec.Embedder, dec.CurrentAgent.Text
		}},
		{"rule match", func(in *Input) {
			in.RoutingRules = []teamconfig.RoutingRule{{AgentKey: "writer-agent", Keywords: []string{"chiến"}}}
		}},
	} {
		d := &fakeDecider{dec: decisionmodel.Decision{Option: "coder-agent", Confidence: 0.99}}
		in := deciderInput(d)
		c.mod(&in)
		_ = ClassifyWithLLM(context.Background(), in, nil, "", nil)
		if d.calls != 0 {
			t.Errorf("%s: decision model called (egress) when it must not be", c.name)
		}
	}
}

func TestDeciderChoiceStillPassesMemberPermissionGate(t *testing.T) {
	d := &fakeDecider{dec: decisionmodel.Decision{Option: "coder-agent", Confidence: 0.95}}
	in := deciderInput(d)
	in.TeamRole = "member"
	in.CanAssignTeamTasks = false
	in.MemberRequestsEnabled = false
	got := ClassifyWithLLM(context.Background(), in, nil, "", nil)
	if got.Decision != DecisionSelf || got.RequiredTool != "" || !strings.Contains(got.Reason, "member lacks team request permission") {
		t.Fatalf("got %+v", got)
	}
}
