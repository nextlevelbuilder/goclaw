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
	result.ForLLM = stripArtifactMediaLines(result.ForLLM)
}

func stripArtifactMediaLines(content string) string {
	if !strings.Contains(content, "MEDIA:") {
		return content
	}
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[[audio_as_voice]]") {
			continue
		}
		cleaned := strings.TrimRight(embeddedMediaPattern.ReplaceAllString(line, ""), " \t")
		if strings.TrimSpace(cleaned) != "" {
			kept = append(kept, cleaned)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}
