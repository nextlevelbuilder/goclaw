package teamconfig

import (
	"encoding/json"
	"testing"
)

func TestParseRoutingAbsentBlock(t *testing.T) {
	got := ParseRouting(json.RawMessage(`{"member_requests":{"enabled":true}}`))
	if got.Enabled != nil || len(got.Rules) != 0 {
		t.Fatalf("empty Routing expected, got %+v", got)
	}
	if r := ParseRouting(nil); r.Enabled != nil {
		t.Fatalf("nil settings must yield zero Routing, got %+v", r)
	}
	if r := ParseRouting(json.RawMessage(`not json`)); len(r.Rules) != 0 {
		t.Fatalf("malformed settings must not be fatal, got %+v", r)
	}
}

func TestParseRoutingRoundTrip(t *testing.T) {
	in := `{"routing":{"enabled":true,"fallback_to_self":false,"confidence_threshold":0.8,
		"rules":[{"agent_key":"coder-agent","keywords":["golang","build"],"file_patterns":[".go","dockerfile"],"criteria":"go code","enabled":true}]}}`
	r := ParseRouting(json.RawMessage(in))
	if r.Enabled == nil || !*r.Enabled {
		t.Fatalf("Enabled not parsed: %+v", r)
	}
	if r.FallbackToSelf == nil || *r.FallbackToSelf != false {
		t.Fatalf("FallbackToSelf not parsed: %+v", r)
	}
	if r.ConfidenceThreshold == nil || *r.ConfidenceThreshold != 0.8 {
		t.Fatalf("ConfidenceThreshold not parsed: %+v", r)
	}
	if len(r.Rules) != 1 || r.Rules[0].AgentKey != "coder-agent" || len(r.Rules[0].Keywords) != 2 {
		t.Fatalf("Rules not parsed: %+v", r.Rules)
	}
}

func TestParseMemberRequest(t *testing.T) {
	if got := ParseMemberRequest(json.RawMessage(`{}`)); got.Enabled || got.AutoDispatch {
		t.Fatalf("absent block must be disabled, got %+v", got)
	}
	got := ParseMemberRequest(json.RawMessage(`{"member_requests":{"enabled":true,"auto_dispatch":true}}`))
	if !got.Enabled || !got.AutoDispatch {
		t.Fatalf("member_requests not parsed: %+v", got)
	}
	// Defaults (nil) are off, matching legacy behavior.
	got = ParseMemberRequest(json.RawMessage(`{"member_requests":{}}`))
	if got.Enabled || got.AutoDispatch {
		t.Fatalf("nil booleans must map to false, got %+v", got)
	}
}

func TestEffectiveRulesDropsDisabledAndStale(t *testing.T) {
	enabled := true
	disabled := false
	r := Routing{Rules: []RoutingRule{
		{AgentKey: "coder-agent", Keywords: []string{"go"}, Enabled: &enabled},
		{AgentKey: "gone-agent", Keywords: []string{"gone"}, Enabled: &enabled},
		{AgentKey: "designer-agent", Keywords: []string{"figma"}, Enabled: &disabled},
		{AgentKey: "writer-agent", Keywords: []string{"blog"}}, // nil = enabled
	}}
	got := EffectiveRules(r, []string{"coder-agent", "designer-agent", "writer-agent"})
	if len(got) != 2 {
		t.Fatalf("want 2 rules (coder+writer), got %+v", got)
	}
	if got[0].AgentKey != "coder-agent" || got[1].AgentKey != "writer-agent" {
		t.Fatalf("wrong rules kept: %+v", got)
	}
}

func TestRuleMatchesMessageWordBoundary(t *testing.T) {
	rule := RoutingRule{AgentKey: "coder-agent", Keywords: []string{"go"}, FilePatterns: []string{".go"}}
	if !RuleMatchesMessage(rule, "refactor internal/foo.go please") {
		t.Fatal(".go pattern must match foo.go")
	}
	if RuleMatchesMessage(rule, "explain google to me") {
		t.Fatal("keyword go must not match google")
	}
	// Keyword "go" legitimately matches "go.mod" (word boundary at '.'), so
	// the .go-pattern exclusion is asserted on a rule without that keyword.
	extOnly := RoutingRule{AgentKey: "coder-agent", FilePatterns: []string{".go"}}
	if RuleMatchesMessage(extOnly, "check go.mod") {
		t.Fatal(".go pattern must not match go.mod")
	}
	rule2 := RoutingRule{AgentKey: "a", FilePatterns: []string{"go.mod"}}
	if !RuleMatchesMessage(rule2, "check go.mod") {
		t.Fatal("explicit go.mod pattern must match go.mod")
	}

	globRule := RoutingRule{AgentKey: "coder-agent", FilePatterns: []string{"*.ts"}}
	if !RuleMatchesMessage(globRule, "fix the bug in app.ts please") {
		t.Fatal("*.ts pattern must match app.ts")
	}
	if RuleMatchesMessage(globRule, "check app.tsx") {
		t.Fatal("*.ts pattern must not match app.tsx")
	}
}

func TestEffectiveRulesHonorsRoutingEnabledFalse(t *testing.T) {
	off := false
	r := Routing{Enabled: &off, Rules: []RoutingRule{{AgentKey: "coder-agent", Keywords: []string{"go"}}}}
	if got := EffectiveRules(r, []string{"coder-agent"}); len(got) != 0 {
		t.Fatalf("Routing.Enabled=false must disable all rules, got %+v", got)
	}
}
