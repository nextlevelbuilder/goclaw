package vault

import (
	"strings"
	"testing"
)

// ============================================================================
// Prompt Building Tests
// ============================================================================

// TestBuildClassifyPrompt verifies system prompt contains all 6 types and user prompt has numbered candidates.
func TestBuildClassifyPrompt(t *testing.T) {
	source := classifyDoc{
		DocID:   "doc1",
		Title:   "Test Document",
		Path:    "docs/test.md",
		Summary: "This is a test document.",
	}

	candidates := []classifyDoc{
		{
			DocID:   "doc2",
			Title:   "Related Doc 1",
			Path:    "docs/related1.md",
			Summary: "First related document.",
		},
		{
			DocID:   "doc3",
			Title:   "Related Doc 2",
			Path:    "docs/related2.md",
			Summary: "Second related document.",
		},
	}

	system, user := buildClassifyPrompt(source, candidates)

	// System prompt must contain all 6 relationship types
	expectedTypes := []string{"reference", "depends_on", "extends", "related", "supersedes", "contradicts"}
	for _, typ := range expectedTypes {
		if !strings.Contains(system, typ) {
			t.Errorf("System prompt missing type: %s", typ)
		}
	}

	// System prompt must contain classifySystemPrompt constant text
	if !strings.Contains(system, "Link Types") {
		t.Errorf("System prompt missing 'Link Types' header")
	}

	// User prompt must contain source doc info
	if !strings.Contains(user, "Test Document") {
		t.Errorf("User prompt missing source title")
	}
	if !strings.Contains(user, "docs/test.md") {
		t.Errorf("User prompt missing source path")
	}
	if !strings.Contains(user, "This is a test document") {
		t.Errorf("User prompt missing source summary")
	}

	// User prompt must contain candidates with numbers (1., 2.)
	if !strings.Contains(user, "1.") {
		t.Errorf("User prompt missing numbered candidate (1.)")
	}
	if !strings.Contains(user, "2.") {
		t.Errorf("User prompt missing numbered candidate (2.)")
	}

	// User prompt must contain candidate info
	if !strings.Contains(user, "Related Doc 1") {
		t.Errorf("User prompt missing candidate 1 title")
	}
	if !strings.Contains(user, "Related Doc 2") {
		t.Errorf("User prompt missing candidate 2 title")
	}
}

// ============================================================================
// Truncate Summary Tests
// ============================================================================

// TestTruncateSummary_Long truncates at classifySummaryMaxChars (300) with "..." suffix.
func TestTruncateSummary_Long(t *testing.T) {
	// Create string longer than 300 chars
	longText := strings.Repeat("a", 350)
	result := truncateSummary(longText)

	if len([]rune(result)) > classifySummaryMaxChars+3 { // +3 for "..."
		t.Errorf("truncateSummary(%d chars) returned %d runes, want ≤%d",
			len([]rune(longText)), len([]rune(result)), classifySummaryMaxChars+3)
	}

	if !strings.HasSuffix(result, "...") {
		t.Errorf("Truncated result must end with '...', got: %q", result)
	}

	// Should be exactly 303 runes (300 + "...")
	expected := len([]rune(strings.Repeat("a", classifySummaryMaxChars))) + 3
	if len([]rune(result)) != expected {
		t.Errorf("Truncated result has %d runes, want %d", len([]rune(result)), expected)
	}
}

// TestTruncateSummary_Short returns short string as-is.
func TestTruncateSummary_Short(t *testing.T) {
	short := "This is short"
	result := truncateSummary(short)

	if result != short {
		t.Errorf("truncateSummary(%q) = %q, want unchanged", short, result)
	}

	if strings.Contains(result, "...") {
		t.Errorf("Short string should not have '...' suffix: %q", result)
	}
}

// TestTruncateSummary_BoundaryAt300 tests string exactly at 300 chars.
func TestTruncateSummary_BoundaryAt300(t *testing.T) {
	exact := strings.Repeat("x", classifySummaryMaxChars)
	result := truncateSummary(exact)

	if result != exact {
		t.Errorf("String at exactly %d chars should not be truncated", classifySummaryMaxChars)
	}

	if strings.HasSuffix(result, "...") {
		t.Errorf("String at boundary should not have '...' suffix")
	}
}

// TestTruncateSummary_Unicode tests UTF-8 safe truncation with multi-byte chars.
func TestTruncateSummary_Unicode(t *testing.T) {
	// Use emoji (4 bytes each) to test multi-byte handling
	longUnicode := strings.Repeat("🎉", 400) // 400 emoji chars, very long when encoded
	result := truncateSummary(longUnicode)

	runes := []rune(result)
	if len(runes) > classifySummaryMaxChars+3 {
		t.Errorf("Unicode truncation exceeded max: got %d runes, want ≤%d",
			len(runes), classifySummaryMaxChars+3)
	}

	if !strings.HasSuffix(result, "...") {
		t.Errorf("Truncated unicode must end with '...'")
	}
}
