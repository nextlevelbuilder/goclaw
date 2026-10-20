package tools

import (
	"context"
	"strings"
)

// ApplyDelegationArtifactResultPolicy keeps unvalidated delegate outputs inside
// the exchange. Publication is owned by DelegateTool after the child run
// succeeds and the manifest has been validated.
func ApplyDelegationArtifactResultPolicy(ctx context.Context, result *Result) {
	if result == nil || !IsDelegationArtifactRun(ctx) {
		return
	}
	result.Media = nil
	result.ForLLM = stripArtifactMediaLines(ctx, result.ForLLM)
}

// stripArtifactMediaLines removes MEDIA: references using the spans reported by
// scanMediaRefs, so quoted paths and unquoted paths containing spaces are
// removed whole. Only the span is used; the resolved path is never consumed.
func stripArtifactMediaLines(ctx context.Context, content string) string {
	if !strings.Contains(content, "MEDIA:") {
		return content
	}
	scanner := &MessageTool{}
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[[audio_as_voice]]") {
			continue
		}
		var stripped strings.Builder
		last := 0
		for _, ref := range scanner.scanMediaRefs(ctx, line) {
			stripped.WriteString(line[last:ref.start])
			last = ref.end
		}
		stripped.WriteString(line[last:])
		cleaned := strings.TrimRight(stripped.String(), " \t")
		if strings.TrimSpace(cleaned) != "" {
			kept = append(kept, cleaned)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}
