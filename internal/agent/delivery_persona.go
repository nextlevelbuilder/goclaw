package agent

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
)

// BuildDeliveryPersonaBrief extracts only compact style cues suitable for
// delivery-only messages. It intentionally omits raw context file names and
// unrelated persona sections.
func BuildDeliveryPersonaBrief(files []bootstrap.ContextFile) string {
	var soulContent string
	for _, f := range files {
		if filepath.Base(f.Path) == bootstrap.SoulFile {
			soulContent = f.Content
			break
		}
	}
	if strings.TrimSpace(soulContent) == "" {
		return ""
	}

	parts := make([]string, 0, 2)
	if style := compactPersonaCue(extractMarkdownSection(soulContent, "Style")); style != "" {
		parts = append(parts, "Style: "+style)
	}
	if vibe := compactPersonaCue(extractMarkdownSection(soulContent, "Vibe")); vibe != "" {
		parts = append(parts, "Vibe: "+vibe)
	}
	return strings.Join(parts, " | ")
}

// DeliveryPersonaBrief returns the active compact persona for channel delivery.
// It uses the same context resolution path as the main prompt so per-user
// overrides apply when present.
func (l *Loop) DeliveryPersonaBrief(ctx context.Context, userID string) string {
	if l == nil {
		return ""
	}
	if strings.TrimSpace(userID) != "" {
		return BuildDeliveryPersonaBrief(l.resolveContextFiles(ctx, userID))
	}
	return BuildDeliveryPersonaBrief(l.contextFiles)
}

func compactPersonaCue(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}
