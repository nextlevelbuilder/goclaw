// Markdown-aware chunking. Splits a document at H1/H2/H3 headings so
// each topic-coherent section becomes its own chunk (with overflow
// fallback to the existing paragraph-based ChunkText for long sections).
//
// Why: the default ChunkText splits on paragraph + length, which works
// fine for prose but produces topic-fragmented chunks for structured
// wiki pages — e.g. a project page with sections "Overview", "Repos",
// "Recent Activity / Q1 / Q2 / Q3" gets sliced across heading
// boundaries, hurting retrieval precision when an agent searches for
// "Recent Q2 activity on Katana". Splitting at headings keeps the
// section title and its content together as one chunk.
//
// Heading detection is regex-based (no markdown AST dependency added).
// Code-block fences (```...```) are tracked so heading-looking lines
// inside fenced code don't trigger a split.
package memory

import (
	"regexp"
	"strings"
)

// headingRE matches setext-style would be too rare in practice — match
// ATX (#) headings only at the start of a line, levels 1-3. Heading text
// after the # (and optional space) is captured for chunk metadata.
var headingRE = regexp.MustCompile(`^#{1,3}\s+\S`)

// ChunkMarkdown splits a markdown body at H1/H2/H3 boundaries. Each
// heading starts a new chunk that includes the heading line and all
// content up to the next heading (or EOF). Sections longer than
// maxChunkLen are split further via ChunkText; sections shorter than
// maxChunkLen/4 are merged with the next section to avoid chunk
// fragmentation on docs with lots of small subsections.
//
// Falls back to ChunkText (paragraph-based) when the body has no
// recognized headings.
func ChunkMarkdown(body string, maxChunkLen, overlap int) []TextChunk {
	if maxChunkLen <= 0 {
		maxChunkLen = 1000
	}
	sections := splitByHeadings(body)
	if len(sections) <= 1 {
		// No headings (or just one — body is one section). Fall back
		// to the existing paragraph chunker which handles overlap.
		return ChunkText(body, maxChunkLen, overlap)
	}

	// Merge tiny adjacent sections so we don't end up with one chunk
	// per H3 in pages that use deep nesting. Threshold: maxChunkLen/4.
	threshold := maxChunkLen / 4
	if threshold < 200 {
		threshold = 200
	}
	merged := mergeShortSections(sections, threshold, maxChunkLen)

	var chunks []TextChunk
	for _, sec := range merged {
		if len(sec.Text) <= maxChunkLen {
			chunks = append(chunks, sec)
			continue
		}
		// Section too large — sub-chunk it, preserving start line.
		sub := ChunkText(sec.Text, maxChunkLen, overlap)
		for i := range sub {
			sub[i].StartLine += sec.StartLine - 1
			sub[i].EndLine += sec.StartLine - 1
		}
		chunks = append(chunks, sub...)
	}
	return chunks
}

// splitByHeadings walks the body line-by-line, tracking fenced code
// blocks, and emits one section per H1/H2/H3 boundary. The first
// section (everything before the first heading) is included only if
// non-empty.
func splitByHeadings(body string) []TextChunk {
	lines := strings.Split(body, "\n")
	var sections []TextChunk
	var current strings.Builder
	startLine := 1
	inFence := false

	flush := func(endLine int) {
		text := strings.TrimRight(current.String(), "\n")
		if strings.TrimSpace(text) != "" {
			sections = append(sections, TextChunk{
				Text:      text,
				StartLine: startLine,
				EndLine:   endLine,
			})
		}
		current.Reset()
	}

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
		}
		if !inFence && headingRE.MatchString(line) {
			// Boundary — flush prior section, start a new one.
			if current.Len() > 0 {
				flush(lineNum - 1)
			}
			startLine = lineNum
		}
		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		flush(len(lines))
	}
	return sections
}

// mergeShortSections combines sections smaller than threshold with the
// following section, as long as the combined size stays under
// maxChunkLen. This prevents page-with-many-H3s from producing dozens
// of tiny single-paragraph chunks.
func mergeShortSections(sections []TextChunk, threshold, maxChunkLen int) []TextChunk {
	if len(sections) == 0 {
		return sections
	}
	out := make([]TextChunk, 0, len(sections))
	cur := sections[0]
	for i := 1; i < len(sections); i++ {
		next := sections[i]
		curLen := len(cur.Text)
		if curLen < threshold && curLen+len(next.Text)+1 <= maxChunkLen {
			cur.Text = cur.Text + "\n" + next.Text
			cur.EndLine = next.EndLine
			continue
		}
		out = append(out, cur)
		cur = next
	}
	out = append(out, cur)
	return out
}
