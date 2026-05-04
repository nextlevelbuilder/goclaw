package memory

import (
	"strings"
	"testing"
)

func TestChunkMarkdown_NoHeadings_FallsBackToParagraph(t *testing.T) {
	body := strings.Repeat("para\nline\n\n", 200) // long, no headings
	got := ChunkMarkdown(body, 500, 50)
	if len(got) < 2 {
		t.Errorf("expected multiple chunks for long body, got %d", len(got))
	}
}

func TestChunkMarkdown_SplitsAtHeadings(t *testing.T) {
	// Each section padded well past the merge threshold (maxChunkLen/4 = 75 here).
	body := `# Project

Top-level intro paragraph that runs a bit long to push past the threshold easily.
More text to make sure we don't accidentally get merged into a sibling section.

## Section A

` + strings.Repeat("Content A line that is reasonably long. ", 8) + `

## Section B

` + strings.Repeat("Content B line that is reasonably long. ", 8) + `

## Section C

` + strings.Repeat("Content C line that is reasonably long. ", 8)
	got := ChunkMarkdown(body, 300, 50)
	if len(got) < 3 {
		t.Fatalf("expected ≥3 chunks (one per H2), got %d:\n%v", len(got), got)
	}
	// Confirm each major section's heading appears in exactly one chunk.
	for _, h := range []string{"## Section A", "## Section B", "## Section C"} {
		matches := 0
		for _, c := range got {
			if strings.Contains(c.Text, h) {
				matches++
			}
		}
		if matches != 1 {
			t.Errorf("heading %q appears in %d chunks, want 1", h, matches)
		}
	}
}

func TestChunkMarkdown_MergesShortSections(t *testing.T) {
	// Three tiny sections; total < maxChunkLen. They should collapse
	// to a single chunk.
	body := `## A
short

## B
also short

## C
tiny`
	got := ChunkMarkdown(body, 1000, 0)
	if len(got) != 1 {
		t.Errorf("expected 1 merged chunk for tiny sections, got %d:\n%v", len(got), got)
	}
}

func TestChunkMarkdown_IgnoresHeadingsInCodeBlock(t *testing.T) {
	body := "## Real Heading\n\nIntro.\n\n```\n# Not A Heading\nmore code\n```\n\n## Real Other Heading\n\ncontent"
	got := ChunkMarkdown(body, 1000, 0)
	for _, c := range got {
		if strings.Contains(c.Text, "# Not A Heading") && !strings.Contains(c.Text, "## Real Heading") {
			t.Errorf("code-block heading split out of its parent section: %q", c.Text)
		}
	}
}

func TestChunkMarkdown_LongSectionGetsSubChunked(t *testing.T) {
	// One H2 with a giant body of multiple paragraphs — should sub-chunk
	// via ChunkText (which splits on paragraph boundaries within a section).
	long := strings.Repeat("Paragraph of many words and ideas, going on for a while.\n\n", 100) // ~5800 chars across many paragraph boundaries
	body := "## Big\n\n" + long
	got := ChunkMarkdown(body, 500, 50)
	if len(got) < 2 {
		t.Errorf("expected long section to sub-chunk, got %d", len(got))
	}
}
