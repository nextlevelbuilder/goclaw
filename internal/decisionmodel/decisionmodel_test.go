package decisionmodel

import "testing"

func TestDecisionAccept(t *testing.T) {
	offered := map[string]string{"coder": "x", "writer": "y", SelfOption: "z"}
	cases := []struct {
		name           string
		dec            Decision
		fallbackToSelf bool
		want           string
	}{
		{"confident member", Decision{Option: "coder", Confidence: 0.9}, true, "coder"},
		{"model chose self", Decision{Option: SelfOption, Confidence: 0.99}, false, ""},
		{"invented option", Decision{Option: "ghost", Confidence: 0.99}, false, ""},
		{"empty option", Decision{Confidence: 0.99}, false, ""},
		// The two low-confidence branches must differ; a flag that changes
		// nothing is the defect this test exists to catch.
		{"low confidence, fallback on", Decision{Option: "coder", Confidence: 0.3}, true, ""},
		{"low confidence, fallback off", Decision{Option: "coder", Confidence: 0.3}, false, "coder"},
		{"exactly at threshold", Decision{Option: "writer", Confidence: 0.65}, true, "writer"},
	}
	for _, tc := range cases {
		if got := tc.dec.Accept(offered, 0.65, tc.fallbackToSelf); got != tc.want {
			t.Errorf("%s: Accept = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 3, "hel"},
		{"日本語テキスト", 3, "日本語"},
		{"", 3, ""},
	}
	for _, tc := range cases {
		if got := truncateRunes(tc.in, tc.n); got != tc.want {
			t.Errorf("truncateRunes(%q,%d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}

func TestWithSelfDoesNotMutateInput(t *testing.T) {
	in := map[string]string{"coder": "x"}
	out := withSelf(in)
	if _, ok := in[SelfOption]; ok {
		t.Fatal("withSelf mutated caller map")
	}
	if len(out) != 2 || out[SelfOption] == "" {
		t.Fatalf("out = %+v", out)
	}
	custom := withSelf(map[string]string{SelfOption: "my rubric"})
	if custom[SelfOption] != "my rubric" {
		t.Fatal("caller-supplied self rubric overwritten")
	}
}
