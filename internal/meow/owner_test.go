package meow

import "testing"

func TestOwnerGate_ClosedByDefault(t *testing.T) {
	// Zero value: no owner, not verified → everyone denied.
	var g OwnerGate
	if g.Allowed("12345") || g.Allowed("") {
		t.Fatal("zero-value gate must deny everyone")
	}

	// Owner set but NOT yet round-trip verified → still denied (incl. the owner).
	g = OwnerGate{OwnerChatID: "12345"}
	if g.Allowed("12345") {
		t.Fatal("unverified owner must be denied until round-trip verification")
	}

	// Verified owner → only the exact owner id passes.
	g = OwnerGate{OwnerChatID: "12345", Verified: true}
	if !g.Allowed("12345") {
		t.Fatal("verified owner should be allowed")
	}
	if !g.Allowed(" 12345 ") {
		t.Fatal("whitespace around a matching id should still be allowed")
	}
	if g.Allowed("99999") {
		t.Fatal("non-owner must be denied")
	}
	if g.Allowed("") {
		t.Fatal("empty sender must be denied")
	}
}
