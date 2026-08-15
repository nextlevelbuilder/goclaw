package tools

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var nonAlphanumRe = regexp.MustCompile(`[^a-z0-9]+`)

// mediaFileName builds a semantic filename for generated media.
// If hint is provided: {slug}_{YYYYMMDD-HHmmss}_{nano6}.{ext}
// If hint is empty:    {mediaType}_{agentKey}_{YYYYMMDD-HHmmss}_{nano6}.{ext}
func mediaFileName(ctx context.Context, mediaType, hint, ext string) string {
	now := time.Now()
	ts := now.Format("20060102-150405")
	nano := fmt.Sprintf("%06d", now.UnixNano()%1_000_000)

	var prefix string
	if slug := slugifyHint(hint, 50); slug != "" {
		prefix = slug
	} else {
		agentKey := store.AgentKeyFromContext(ctx)
		if agentKey == "" {
			agentKey = "gen"
		}
		prefix = mediaType + "_" + agentKey
	}

	return fmt.Sprintf("%s_%s_%s.%s", prefix, ts, nano, ext)
}

// slugifyHint converts a hint string to a kebab-case slug, max maxLen chars.
func slugifyHint(hint string, maxLen int) string {
	s := strings.ToLower(strings.TrimSpace(hint))
	s = nonAlphanumRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > maxLen {
		s = s[:maxLen]
		if i := strings.LastIndex(s, "-"); i > 10 {
			s = s[:i]
		}
	}
	return s
}
