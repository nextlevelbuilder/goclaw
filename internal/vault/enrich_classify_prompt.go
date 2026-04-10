package vault

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// classifyResult is a single LLM classification output for a candidate doc.
type classifyResult struct {
	Idx  int    `json:"idx"`
	Type string `json:"type"`
	Ctx  string `json:"ctx"`
}

const classifySystemPrompt = `You classify relationships between documents in a knowledge vault.

## Link Types
- reference: A cites, mentions, or quotes B
- depends_on: A requires B to function (config, data dependency, prerequisite)
- extends: A expands, details, or elaborates on B
- related: A and B share the same topic or domain (use only when no stronger type fits)
- supersedes: A is a newer version that replaces or updates B
- contradicts: A conflicts with or opposes B's content

## Rules
- Output ONLY a valid JSON array, no other text
- Use SKIP when documents are similar but have no meaningful relationship
- Prefer specific types (reference, depends_on) over generic (related)
- Each candidate classified independently
- Keep ctx descriptions under 60 words

## Output Format
[{"idx":1,"type":"reference","ctx":"mentions OAuth config for setup"},{"idx":2,"type":"SKIP"}]`

// buildClassifyPrompt formats the system and user prompts for classify LLM call.
func buildClassifyPrompt(source classifyDoc, candidates []classifyDoc) (system, user string) {
	var b strings.Builder
	b.WriteString("## Source Document\n")
	fmt.Fprintf(&b, "Title: %s\nPath: %s\nSummary: %s\n\n", source.Title, source.Path, source.Summary)
	b.WriteString("## Candidates\n")
	for i, c := range candidates {
		fmt.Fprintf(&b, "%d. Title: %s | Path: %s | Summary: %s\n", i+1, c.Title, c.Path, c.Summary)
	}
	return classifySystemPrompt, b.String()
}

// parseClassifyResponse parses LLM JSON output into classify results.
// Uses partial success model: invalid entries filtered silently, error only on total unmarshal failure.
func parseClassifyResponse(raw string, count int) ([]classifyResult, error) {
	raw = strings.TrimSpace(raw)
	// Strip code fences.
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var results []classifyResult
	if err := json.Unmarshal([]byte(raw), &results); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	// Filter invalid entries (partial success).
	valid := results[:0]
	for _, r := range results {
		if r.Idx < 1 || r.Idx > count {
			slog.Debug("vault.classify: idx out of range", "idx", r.Idx, "count", count)
			continue
		}
		if r.Type != "SKIP" && !validClassifyTypes[r.Type] {
			continue
		}
		// Truncate context.
		if len(r.Ctx) > classifyCtxMaxLen {
			r.Ctx = string([]rune(r.Ctx)[:classifyCtxMaxLen])
		}
		valid = append(valid, r)
	}
	return valid, nil
}

// truncateSummary caps summary at classifySummaryMaxChars with UTF-8 safe rune truncation.
func truncateSummary(s string) string {
	runes := []rune(s)
	if len(runes) <= classifySummaryMaxChars {
		return s
	}
	return string(runes[:classifySummaryMaxChars]) + "..."
}
