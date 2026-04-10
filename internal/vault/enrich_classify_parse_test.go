package vault

import (
	"fmt"
	"strings"
	"testing"
)

// ============================================================================
// JSON Parsing Tests
// ============================================================================

// TestParseClassifyResponse_Valid parses standard JSON array.
func TestParseClassifyResponse_Valid(t *testing.T) {
	raw := `[
		{"idx":1,"type":"reference","ctx":"mentions config"},
		{"idx":2,"type":"depends_on","ctx":"requires authentication"}
	]`

	results, err := parseClassifyResponse(raw, 2)
	if err != nil {
		t.Fatalf("parseClassifyResponse failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	if results[0].Idx != 1 || results[0].Type != "reference" {
		t.Errorf("Result 0: got idx=%d type=%q, want idx=1 type=reference", results[0].Idx, results[0].Type)
	}

	if results[1].Idx != 2 || results[1].Type != "depends_on" {
		t.Errorf("Result 1: got idx=%d type=%q, want idx=2 type=depends_on", results[1].Idx, results[1].Type)
	}
}

// TestParseClassifyResponse_WithCodeFence strips ```json fences.
func TestParseClassifyResponse_WithCodeFence(t *testing.T) {
	raw := "```json\n[{\"idx\":1,\"type\":\"reference\",\"ctx\":\"test\"}]\n```"

	results, err := parseClassifyResponse(raw, 1)
	if err != nil {
		t.Fatalf("parseClassifyResponse with code fence failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	if results[0].Type != "reference" {
		t.Errorf("Expected type reference, got %q", results[0].Type)
	}
}

// TestParseClassifyResponse_InvalidJSON returns error on unmarshal failure.
func TestParseClassifyResponse_InvalidJSON(t *testing.T) {
	raw := `{invalid json}`

	_, err := parseClassifyResponse(raw, 1)
	if err == nil {
		t.Fatalf("parseClassifyResponse should return error for invalid JSON")
	}

	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("Error should mention unmarshal: %v", err)
	}
}

// TestParseClassifyResponse_OutOfRangeIdx filters silently, keeps valid entries.
func TestParseClassifyResponse_OutOfRangeIdx(t *testing.T) {
	raw := `[
		{"idx":1,"type":"reference","ctx":"ok"},
		{"idx":99,"type":"related","ctx":"invalid idx"},
		{"idx":2,"type":"extends","ctx":"ok"}
	]`

	results, err := parseClassifyResponse(raw, 2)
	if err != nil {
		t.Fatalf("parseClassifyResponse failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 results (out-of-range filtered), got %d", len(results))
	}

	if results[0].Idx != 1 || results[1].Idx != 2 {
		t.Errorf("Out-of-range entry not filtered properly")
	}
}

// TestParseClassifyResponse_UnknownType filters silently.
func TestParseClassifyResponse_UnknownType(t *testing.T) {
	raw := `[
		{"idx":1,"type":"reference","ctx":"ok"},
		{"idx":2,"type":"unknown_type","ctx":"invalid"},
		{"idx":3,"type":"related","ctx":"ok"}
	]`

	results, err := parseClassifyResponse(raw, 3)
	if err != nil {
		t.Fatalf("parseClassifyResponse failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 results (unknown type filtered), got %d", len(results))
	}

	if results[0].Type != "reference" || results[1].Type != "related" {
		t.Errorf("Unknown type not filtered properly")
	}
}

// TestParseClassifyResponse_SKIPType preserves SKIP in output.
func TestParseClassifyResponse_SKIPType(t *testing.T) {
	raw := `[
		{"idx":1,"type":"reference","ctx":"valid"},
		{"idx":2,"type":"SKIP","ctx":"no relationship"}
	]`

	results, err := parseClassifyResponse(raw, 2)
	if err != nil {
		t.Fatalf("parseClassifyResponse failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Expected 2 results including SKIP, got %d", len(results))
	}

	skipFound := false
	for _, r := range results {
		if r.Type == "SKIP" {
			skipFound = true
			break
		}
	}

	if !skipFound {
		t.Errorf("SKIP type should be preserved in results")
	}
}

// TestParseClassifyResponse_EmptyArray returns valid empty slice.
func TestParseClassifyResponse_EmptyArray(t *testing.T) {
	raw := `[]`

	results, err := parseClassifyResponse(raw, 5)
	if err != nil {
		t.Fatalf("parseClassifyResponse failed on empty array: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf("Expected 0 results, got %d", len(results))
	}
}

// TestParseClassifyResponse_CtxTruncation truncates context over 256 chars.
func TestParseClassifyResponse_CtxTruncation(t *testing.T) {
	longCtx := strings.Repeat("x", 300)
	raw := fmt.Sprintf(`[{"idx":1,"type":"reference","ctx":"%s"}]`, longCtx)

	results, err := parseClassifyResponse(raw, 1)
	if err != nil {
		t.Fatalf("parseClassifyResponse failed: %v", err)
	}

	if len([]rune(results[0].Ctx)) > classifyCtxMaxLen {
		t.Errorf("Context not truncated: got %d runes, want ≤%d",
			len([]rune(results[0].Ctx)), classifyCtxMaxLen)
	}
}

// TestParseClassifyResponse_ZeroIdx filters silently (idx must be >= 1).
func TestParseClassifyResponse_ZeroIdx(t *testing.T) {
	raw := `[
		{"idx":0,"type":"reference","ctx":"invalid"},
		{"idx":1,"type":"reference","ctx":"valid"}
	]`

	results, err := parseClassifyResponse(raw, 1)
	if err != nil {
		t.Fatalf("parseClassifyResponse failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result (idx 0 filtered), got %d", len(results))
	}

	if results[0].Idx != 1 {
		t.Errorf("Zero idx should be filtered")
	}
}

