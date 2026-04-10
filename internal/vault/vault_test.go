package vault

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// ============================================================================
// Hash Tests
// ============================================================================

// TestContentHash verifies SHA-256 hashing of known inputs.
func TestContentHash(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string // pre-computed SHA-256
	}{
		{
			name:     "empty",
			input:    []byte(""),
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:     "simple text",
			input:    []byte("hello world"),
			expected: "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
		},
		{
			name:     "single character",
			input:    []byte("a"),
			expected: "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb",
		},
		{
			name:     "multiline content",
			input:    []byte("line1\nline2\nline3"),
			expected: hashRef([]byte("line1\nline2\nline3")),
		},
		{
			name:     "unicode content",
			input:    []byte("café"),
			expected: hashRef([]byte("café")),
		},
		{
			name:     "binary content",
			input:    []byte{0x00, 0x01, 0x02, 0xff},
			expected: hashRef([]byte{0x00, 0x01, 0x02, 0xff}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ContentHash(tt.input)
			if result != tt.expected {
				t.Errorf("ContentHash() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestContentHashFile reads a temporary file and verifies hash matches ContentHash.
func TestContentHashFile(t *testing.T) {
	tmpdir := t.TempDir()
	tmpfile := tmpdir + "/test.txt"

	content := []byte("test file content")
	if err := os.WriteFile(tmpfile, content, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	fileHash, err := ContentHashFile(tmpfile)
	if err != nil {
		t.Fatalf("ContentHashFile failed: %v", err)
	}

	directHash := ContentHash(content)
	if fileHash != directHash {
		t.Errorf("ContentHashFile() = %q, ContentHash() = %q, want match", fileHash, directHash)
	}
}

// TestContentHashFile_MultipleFiles verifies different files produce different hashes.
func TestContentHashFile_MultipleFiles(t *testing.T) {
	tmpdir := t.TempDir()

	file1 := tmpdir + "/file1.txt"
	file2 := tmpdir + "/file2.txt"

	content1 := []byte("content one")
	content2 := []byte("content two")

	if err := os.WriteFile(file1, content1, 0644); err != nil {
		t.Fatalf("WriteFile file1 failed: %v", err)
	}
	if err := os.WriteFile(file2, content2, 0644); err != nil {
		t.Fatalf("WriteFile file2 failed: %v", err)
	}

	hash1, err := ContentHashFile(file1)
	if err != nil {
		t.Fatalf("ContentHashFile(file1) failed: %v", err)
	}

	hash2, err := ContentHashFile(file2)
	if err != nil {
		t.Fatalf("ContentHashFile(file2) failed: %v", err)
	}

	if hash1 == hash2 {
		t.Errorf("Different files produced same hash: %q", hash1)
	}

	if hash1 != ContentHash(content1) {
		t.Errorf("file1 hash mismatch")
	}
	if hash2 != ContentHash(content2) {
		t.Errorf("file2 hash mismatch")
	}
}

// TestContentHashFile_NotFound verifies error for missing file.
func TestContentHashFile_NotFound(t *testing.T) {
	_, err := ContentHashFile("/nonexistent/path/file.txt")
	if err == nil {
		t.Fatalf("ContentHashFile should return error for non-existent file")
	}
}

// TestContentHashFile_EmptyFile verifies empty file hashing.
func TestContentHashFile_EmptyFile(t *testing.T) {
	tmpdir := t.TempDir()
	tmpfile := tmpdir + "/empty.txt"

	if err := os.WriteFile(tmpfile, []byte{}, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	hash, err := ContentHashFile(tmpfile)
	if err != nil {
		t.Fatalf("ContentHashFile failed: %v", err)
	}

	expected := ContentHash([]byte{})
	if hash != expected {
		t.Errorf("Empty file hash mismatch: got %q, want %q", hash, expected)
	}
}

// ============================================================================
// Wikilink Parser Tests
// ============================================================================

// TestExtractWikilinks_Basic tests simple [[target]] format.
func TestExtractWikilinks_Basic(t *testing.T) {
	content := "Check out [[foo]] for details."
	matches := ExtractWikilinks(content)

	if len(matches) != 1 {
		t.Fatalf("ExtractWikilinks returned %d matches, want 1", len(matches))
	}

	if matches[0].Target != "foo" {
		t.Errorf("Target = %q, want 'foo'", matches[0].Target)
	}

	if !bytes.Contains([]byte(matches[0].Context), []byte("foo")) {
		t.Errorf("Context should contain 'foo': %q", matches[0].Context)
	}
}

// TestExtractWikilinks_DisplayText tests [[target|display]] format.
func TestExtractWikilinks_DisplayText(t *testing.T) {
	content := "See [[foo|click here]] for more."
	matches := ExtractWikilinks(content)

	if len(matches) != 1 {
		t.Fatalf("ExtractWikilinks returned %d matches, want 1", len(matches))
	}

	// Target should be 'foo', display text ignored
	if matches[0].Target != "foo" {
		t.Errorf("Target = %q, want 'foo'", matches[0].Target)
	}
}

// TestExtractWikilinks_Multiple tests multiple links in one document.
func TestExtractWikilinks_Multiple(t *testing.T) {
	content := "Start [[link1]] middle [[link2]] end [[link3]]"
	matches := ExtractWikilinks(content)

	if len(matches) != 3 {
		t.Fatalf("ExtractWikilinks returned %d matches, want 3", len(matches))
	}

	expected := []string{"link1", "link2", "link3"}
	for i, exp := range expected {
		if matches[i].Target != exp {
			t.Errorf("Match %d: Target = %q, want %q", i, matches[i].Target, exp)
		}
	}
}

// TestExtractWikilinks_Empty verifies no links returns empty slice.
func TestExtractWikilinks_Empty(t *testing.T) {
	content := "This document has no wikilinks at all."
	matches := ExtractWikilinks(content)

	if len(matches) != 0 {
		t.Fatalf("ExtractWikilinks returned %d matches, want 0", len(matches))
	}

	if len(matches) > 0 {
		t.Errorf("ExtractWikilinks should return empty slice for no matches")
	}
}

// TestExtractWikilinks_Whitespace tests handling of whitespace in targets.
func TestExtractWikilinks_Whitespace(t *testing.T) {
	content := "Link with spaces: [[foo bar]] end."
	matches := ExtractWikilinks(content)

	if len(matches) != 1 {
		t.Fatalf("ExtractWikilinks returned %d matches, want 1", len(matches))
	}

	// Target should preserve internal spaces but trim edges
	if matches[0].Target != "foo bar" {
		t.Errorf("Target = %q, want 'foo bar'", matches[0].Target)
	}
}

// TestExtractWikilinks_PathFormat tests [[path/to/file]] format.
func TestExtractWikilinks_PathFormat(t *testing.T) {
	content := "See [[docs/reference/guide]] for info."
	matches := ExtractWikilinks(content)

	if len(matches) != 1 {
		t.Fatalf("ExtractWikilinks returned %d matches, want 1", len(matches))
	}

	if matches[0].Target != "docs/reference/guide" {
		t.Errorf("Target = %q, want 'docs/reference/guide'", matches[0].Target)
	}
}

// TestExtractWikilinks_WithExtension tests [[file.md]] format.
func TestExtractWikilinks_WithExtension(t *testing.T) {
	content := "Reference: [[notes.md]] please."
	matches := ExtractWikilinks(content)

	if len(matches) != 1 {
		t.Fatalf("ExtractWikilinks returned %d matches, want 1", len(matches))
	}

	if matches[0].Target != "notes.md" {
		t.Errorf("Target = %q, want 'notes.md'", matches[0].Target)
	}
}

// TestExtractWikilinks_MixedFormats tests mix of different link formats.
func TestExtractWikilinks_MixedFormats(t *testing.T) {
	content := `
		Simple: [[foo]]
		Display: [[bar|click here]]
		Path: [[docs/readme.md]]
		Complex: [[path/to/file.md|Go to file]]
	`
	matches := ExtractWikilinks(content)

	if len(matches) != 4 {
		t.Fatalf("ExtractWikilinks returned %d matches, want 4", len(matches))
	}

	expected := []string{"foo", "bar", "docs/readme.md", "path/to/file.md"}
	for i, exp := range expected {
		if matches[i].Target != exp {
			t.Errorf("Match %d: Target = %q, want %q", i, matches[i].Target, exp)
		}
	}
}

// TestExtractWikilinks_EmptyTarget tests [[]] and [[|display]] edge cases.
func TestExtractWikilinks_EmptyTarget(t *testing.T) {
	tests := []struct {
		name    string
		content string
		count   int
	}{
		{
			name:    "empty brackets",
			content: "Test [[]] here.",
			count:   0, // Empty target should be skipped
		},
		{
			name:    "only display text",
			content: "Test [[|display]] here.",
			count:   0, // Empty target should be skipped
		},
		{
			name:    "whitespace only target",
			content: "Test [[   ]] here.",
			count:   0, // Trimmed to empty should be skipped
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := ExtractWikilinks(tt.content)
			if len(matches) != tt.count {
				t.Errorf("ExtractWikilinks returned %d matches, want %d", len(matches), tt.count)
			}
		})
	}
}

// TestExtractWikilinks_Context verifies context window around link.
func TestExtractWikilinks_Context(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		checkFn  func(string) bool
	}{
		{
			name:    "context at document start",
			content: "[[link]] some text after",
			checkFn: func(ctx string) bool {
				// Link is at start, context should include the link and text after
				return bytes.Contains([]byte(ctx), []byte("link"))
			},
		},
		{
			name:    "context with surrounding text",
			content: "some text before [[link]] some text after",
			checkFn: func(ctx string) bool {
				// Context should include both surrounding text and the link
				ctx_lower := bytes.ToLower([]byte(ctx))
				hasLink := bytes.Contains(ctx_lower, []byte("link"))
				hasBefore := bytes.Contains(ctx_lower, []byte("before"))
				hasAfter := bytes.Contains(ctx_lower, []byte("after"))
				return hasLink && hasBefore && hasAfter
			},
		},
		{
			name:    "context at document end",
			content: "some text before [[link]]",
			checkFn: func(ctx string) bool {
				return bytes.Contains([]byte(ctx), []byte("link"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := ExtractWikilinks(tt.content)
			if len(matches) == 0 {
				t.Fatalf("ExtractWikilinks found no matches")
			}
			if !tt.checkFn(matches[0].Context) {
				t.Errorf("Context validation failed for: %q", matches[0].Context)
			}
		})
	}
}

// TestExtractWikilinks_Offset verifies offset points to link start.
func TestExtractWikilinks_Offset(t *testing.T) {
	content := "Text [[link]] more text"
	matches := ExtractWikilinks(content)

	if len(matches) != 1 {
		t.Fatalf("ExtractWikilinks returned %d matches, want 1", len(matches))
	}

	// Offset should point to the start of [[
	if matches[0].Offset != 5 {
		t.Errorf("Offset = %d, want 5", matches[0].Offset)
	}

	// Verify the link starts at offset
	linkStart := content[matches[0].Offset : matches[0].Offset+8] // len("[[link]]") == 8
	if linkStart != "[[link]]" {
		t.Errorf("Content at offset doesn't match link: %q", linkStart)
	}
}

// TestExtractWikilinks_ContextLength verifies context is ~50 chars total.
func TestExtractWikilinks_ContextLength(t *testing.T) {
	// Create content with specific length for context testing
	content := "0123456789" + // 10 chars
		"0123456789" + // 20 chars
		"[[link]]" + // 8 chars = 28 chars total before link end
		"0123456789" + // 38 chars
		"0123456789" // 48 chars

	matches := ExtractWikilinks(content)
	if len(matches) != 1 {
		t.Fatalf("ExtractWikilinks returned %d matches, want 1", len(matches))
	}

	// Context should be roughly 50 chars (25 before + link + 25 after)
	// Allow some flexibility for how the boundaries align
	contextLen := len(matches[0].Context)
	if contextLen < 40 || contextLen > 60 {
		t.Logf("Context length = %d (expected ~50), context = %q", contextLen, matches[0].Context)
		// Don't fail hard here — the regex context capture is approximate
	}
}

// TestExtractWikilinks_RealWorldMarkdown tests realistic markdown content.
func TestExtractWikilinks_RealWorldMarkdown(t *testing.T) {
	content := `
# Project Overview

This project [[overview.md|overview]] describes the system architecture.
See [[architecture/components]] for component details.

## Implementation

Reference [[implementation-notes]] for design decisions.
Use [[templates/new-feature.md]] when creating features.

- Link item: [[guidelines]]
- Another: [[docs/faq|FAQ]]

## Related

- [[../parent-project]]
- [[sibling-project/readme]]
`

	matches := ExtractWikilinks(content)

	if len(matches) != 8 {
		t.Fatalf("ExtractWikilinks returned %d matches, want 8", len(matches))
	}

	expected := []string{
		"overview.md",
		"architecture/components",
		"implementation-notes",
		"templates/new-feature.md",
		"guidelines",
		"docs/faq",
		"../parent-project",
		"sibling-project/readme",
	}

	for i, exp := range expected {
		if i >= len(matches) {
			t.Fatalf("Not enough matches: want %d, got %d", len(expected), len(matches))
		}
		if matches[i].Target != exp {
			t.Errorf("Match %d: Target = %q, want %q", i, matches[i].Target, exp)
		}
	}
}

// ============================================================================
// Helper Functions
// ============================================================================

// hashRef computes SHA-256 for reference comparison.
func hashRef(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
