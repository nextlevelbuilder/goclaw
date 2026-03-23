package agent

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// hasReadImageProvider checks if the read_image builtin tool has a dedicated provider configured.
// When true, images should NOT be attached inline to the main LLM — instead the agent
// uses the read_image tool which routes to the configured vision provider.
// Supports both legacy flat format {"provider":"X"} and chain format {"providers":[...]}.
func (l *Loop) hasReadImageProvider() bool {
	if l.builtinToolSettings == nil {
		return false
	}
	raw, ok := l.builtinToolSettings["read_image"]
	if !ok || len(raw) == 0 {
		return false
	}

	// Try chain format first: {"providers":[{"provider":"X",...}]}
	var chain struct {
		Providers []struct {
			Provider string `json:"provider"`
			Enabled  *bool  `json:"enabled,omitempty"`
		} `json:"providers"`
	}
	if json.Unmarshal(raw, &chain) == nil && len(chain.Providers) > 0 {
		for _, p := range chain.Providers {
			if p.Provider != "" && (p.Enabled == nil || *p.Enabled) {
				return true
			}
		}
	}

	// Fallback: legacy flat format {"provider":"X"}
	var legacy struct {
		Provider string `json:"provider"`
	}
	if json.Unmarshal(raw, &legacy) == nil && legacy.Provider != "" {
		return true
	}

	return false
}

// loadHistoricalImagesForTool collects image MediaRefs from historical messages
// and loads them into context for the read_image tool. Merges with any images already
// in context (from current turn). Limited to last maxMediaReloadMessages messages with image refs.
func (l *Loop) loadHistoricalImagesForTool(ctx context.Context, currentRefs []providers.MediaRef, messages []providers.Message) context.Context {
	// Start with current-turn images already in context.
	existing := tools.MediaImagesFromCtx(ctx)

	// Collect image paths from historical messages (scan backwards, limit count).
	var histPaths []bus.MediaFile
	count := 0
	for i := len(messages) - 1; i >= 0 && count < maxMediaReloadMessages; i-- {
		if len(messages[i].MediaRefs) == 0 {
			continue
		}
		hasImage := false
		for _, ref := range messages[i].MediaRefs {
			if ref.Kind != "image" {
				continue
			}
			hasImage = true
			p := ref.Path
			if p == "" && l.mediaStore != nil {
				p, _ = l.mediaStore.LoadPath(ref.ID)
			}
			if p == "" {
				continue
			}
			histPaths = append(histPaths, bus.MediaFile{Path: p, MimeType: ref.MimeType})
		}
		if hasImage {
			count++
		}
	}

	if len(histPaths) == 0 {
		return ctx
	}

	histImages := loadImages(histPaths)
	if len(histImages) == 0 {
		return ctx
	}

	// Merge: existing (current turn) + historical.
	merged := make([]providers.ImageContent, 0, len(existing)+len(histImages))
	merged = append(merged, existing...)
	merged = append(merged, histImages...)
	slog.Debug("vision: loaded historical images for read_image tool", "current", len(existing), "historical", len(histImages))
	return tools.WithMediaImages(ctx, merged)
}
