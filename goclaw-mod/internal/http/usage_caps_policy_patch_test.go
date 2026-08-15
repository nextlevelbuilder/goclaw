package http

import "testing"

func TestPolicyPatchFromBodyClearsTokenAndCostLimits(t *testing.T) {
	patch, err := policyPatchFromBody([]byte(`{"max_tokens":null,"max_cost_usd":null}`))
	if err != nil {
		t.Fatalf("policyPatchFromBody: %v", err)
	}
	if patch.MaxTokens == nil {
		t.Fatal("MaxTokens patch missing")
	}
	if *patch.MaxTokens != nil {
		t.Fatalf("MaxTokens = %v, want nil clear", **patch.MaxTokens)
	}
	if patch.MaxCostMicros == nil {
		t.Fatal("MaxCostMicros patch missing")
	}
	if *patch.MaxCostMicros != nil {
		t.Fatalf("MaxCostMicros = %v, want nil clear", **patch.MaxCostMicros)
	}
}

func TestPolicyPatchFromBodyPreservesProvidedCostUSD(t *testing.T) {
	patch, err := policyPatchFromBody([]byte(`{"max_cost_usd":12.5}`))
	if err != nil {
		t.Fatalf("policyPatchFromBody: %v", err)
	}
	if patch.MaxCostMicros == nil || *patch.MaxCostMicros == nil {
		t.Fatal("MaxCostMicros patch missing")
	}
	if got := **patch.MaxCostMicros; got != 12_500_000 {
		t.Fatalf("MaxCostMicros = %d, want 12500000", got)
	}
}
