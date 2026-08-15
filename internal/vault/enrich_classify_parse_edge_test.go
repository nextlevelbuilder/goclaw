package vault

import (
	"testing"
)

// ============================================================================
// JSON Parsing Edge Case Tests
// ============================================================================

// TestParseClassifyResponse_MixedValidInvalid handles mix of valid and invalid entries.
func TestParseClassifyResponse_MixedValidInvalid(t *testing.T) {
	raw := `[
		{"idx":1,"type":"reference","ctx":"valid"},
		{"idx":0,"type":"reference","ctx":"invalid idx"},
		{"idx":2,"type":"bad_type","ctx":"invalid type"},
		{"idx":3,"type":"extends","ctx":"valid"}
	]`

	results, err := parseClassifyResponse(raw, 3)
	if err != nil {
		t.Fatalf("parseClassifyResponse failed: %v", err)
	}

	// Should keep only idx 1 and 3 with valid types
	if len(results) != 2 {
		t.Fatalf("Expected 2 valid results, got %d", len(results))
	}

	if results[0].Idx != 1 || results[1].Idx != 3 {
		t.Errorf("Invalid entries not filtered correctly")
	}
}

// TestParseClassifyResponse_NegativeIdx filters silently.
func TestParseClassifyResponse_NegativeIdx(t *testing.T) {
	raw := `[
		{"idx":-1,"type":"reference","ctx":"invalid"},
		{"idx":1,"type":"reference","ctx":"valid"}
	]`

	results, err := parseClassifyResponse(raw, 1)
	if err != nil {
		t.Fatalf("parseClassifyResponse failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result (negative idx filtered), got %d", len(results))
	}

	if results[0].Idx != 1 {
		t.Errorf("Negative idx should be filtered")
	}
}

// TestParseClassifyResponse_AllSKIP preserves all SKIP entries.
func TestParseClassifyResponse_AllSKIP(t *testing.T) {
	raw := `[
		{"idx":1,"type":"SKIP","ctx":"no match"},
		{"idx":2,"type":"SKIP","ctx":"different topic"}
	]`

	results, err := parseClassifyResponse(raw, 2)
	if err != nil {
		t.Fatalf("parseClassifyResponse failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 SKIP entries, got %d", len(results))
	}

	for i, r := range results {
		if r.Type != "SKIP" {
			t.Errorf("Result %d: expected SKIP, got %q", i, r.Type)
		}
	}
}

// TestParseClassifyResponse_LargeIdx beyond count are filtered.
func TestParseClassifyResponse_LargeIdx(t *testing.T) {
	raw := `[
		{"idx":1,"type":"reference","ctx":"ok"},
		{"idx":100,"type":"reference","ctx":"too large"},
		{"idx":999,"type":"reference","ctx":"way too large"}
	]`

	results, err := parseClassifyResponse(raw, 5)
	if err != nil {
		t.Fatalf("parseClassifyResponse failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result (oversized idx filtered), got %d", len(results))
	}

	if results[0].Idx != 1 {
		t.Errorf("Large idx should be filtered")
	}
}

// TestParseClassifyResponse_MaxBoundaryIdx at exactly count is valid.
func TestParseClassifyResponse_MaxBoundaryIdx(t *testing.T) {
	raw := `[
		{"idx":1,"type":"reference","ctx":"first"},
		{"idx":5,"type":"reference","ctx":"last valid"}
	]`

	results, err := parseClassifyResponse(raw, 5)
	if err != nil {
		t.Fatalf("parseClassifyResponse failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	if results[1].Idx != 5 {
		t.Errorf("Max boundary idx (5) should be valid when count=5")
	}
}

// TestParseClassifyResponse_EmptyCtx preserves empty context string.
func TestParseClassifyResponse_EmptyCtx(t *testing.T) {
	raw := `[
		{"idx":1,"type":"reference","ctx":""},
		{"idx":2,"type":"related","ctx":"has context"}
	]`

	results, err := parseClassifyResponse(raw, 2)
	if err != nil {
		t.Fatalf("parseClassifyResponse failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	if results[0].Ctx != "" {
		t.Errorf("Empty ctx should be preserved, got %q", results[0].Ctx)
	}
}

// TestParseClassifyResponse_WhitespaceCtx preserves whitespace-only context.
func TestParseClassifyResponse_WhitespaceCtx(t *testing.T) {
	raw := `[
		{"idx":1,"type":"reference","ctx":"   "},
		{"idx":2,"type":"related","ctx":"valid"}
	]`

	results, err := parseClassifyResponse(raw, 2)
	if err != nil {
		t.Fatalf("parseClassifyResponse failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	// Whitespace should be preserved
	if results[0].Ctx != "   " {
		t.Errorf("Whitespace ctx should be preserved, got %q", results[0].Ctx)
	}
}

// TestParseClassifyResponse_CaseInsensitiveType only SKIP and validClassifyTypes are accepted.
func TestParseClassifyResponse_CaseInsensitiveType(t *testing.T) {
	// Lowercase versions of valid types should not be accepted
	raw := `[
		{"idx":1,"type":"reference","ctx":"ok"},
		{"idx":2,"type":"REFERENCE","ctx":"uppercase"}
	]`

	results, err := parseClassifyResponse(raw, 2)
	if err != nil {
		t.Fatalf("parseClassifyResponse failed: %v", err)
	}

	// Only lowercase "reference" should be valid
	if len(results) != 1 {
		t.Fatalf("Expected 1 result (uppercase REFERENCE filtered), got %d", len(results))
	}

	if results[0].Type != "reference" {
		t.Errorf("Only lowercase types accepted, got %q", results[0].Type)
	}
}
