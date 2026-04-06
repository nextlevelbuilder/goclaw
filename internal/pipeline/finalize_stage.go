package pipeline

import (
	"context"
	"log/slog"
	"os"
)

// FinalizeStage runs once after the iteration loop exits. Sanitizes content,
// deduplicates media, flushes messages, cleans up bootstrap, triggers summarization.
// Errors are logged, not fatal (pipeline.Run uses context.WithoutCancel for finalize).
type FinalizeStage struct {
	deps *PipelineDeps
}

// NewFinalizeStage creates a FinalizeStage.
func NewFinalizeStage(deps *PipelineDeps) *FinalizeStage {
	return &FinalizeStage{deps: deps}
}

func (s *FinalizeStage) Name() string { return "finalize" }

// Execute performs all post-loop cleanup. Errors are logged, not returned.
func (s *FinalizeStage) Execute(ctx context.Context, state *RunState) error {
	// 1. Sanitize final content
	if state.Observe.FinalContent != "" && s.deps.SanitizeContent != nil {
		state.Observe.FinalContent = s.deps.SanitizeContent(state.Observe.FinalContent)
	}

	// 2. Deduplicate + populate media sizes
	s.processMedia(state)

	// 3. Flush remaining pending messages to session store
	pending := state.Messages.FlushPending()
	if len(pending) > 0 && s.deps.FlushMessages != nil {
		if err := s.deps.FlushMessages(ctx, state.Input.SessionKey, pending); err != nil {
			slog.Warn("finalize flush failed", "err", err)
		}
	}

	// 4. Update session metadata (token usage)
	if s.deps.UpdateMetadata != nil {
		if err := s.deps.UpdateMetadata(ctx, state.Input.SessionKey, state.Think.TotalUsage); err != nil {
			slog.Warn("finalize metadata update failed", "err", err)
		}
	}

	// 5. Bootstrap auto-cleanup
	if state.Context.HadBootstrap && s.deps.BootstrapCleanup != nil {
		if err := s.deps.BootstrapCleanup(ctx, state); err != nil {
			slog.Warn("bootstrap cleanup failed", "err", err)
		}
	}

	// 6. Post-run summarization (async background)
	if s.deps.MaybeSummarize != nil {
		s.deps.MaybeSummarize(ctx, state.Input.SessionKey)
	}

	// run.completed event is emitted by loop_run.go after Pipeline.Run() returns,
	// with full tracing context. No duplicate emission here.

	return nil
}

// processMedia populates file sizes and deduplicates media results.
func (s *FinalizeStage) processMedia(state *RunState) {
	media := state.Tool.MediaResults

	// Populate sizes for local files
	for i := range media {
		if media[i].Size == 0 && media[i].Path != "" {
			if fi, err := os.Stat(media[i].Path); err == nil {
				media[i].Size = fi.Size()
			}
		}
	}

	// Deduplicate by path
	seen := make(map[string]bool, len(media))
	deduped := make([]MediaResult, 0, len(media))
	for _, m := range media {
		if !seen[m.Path] {
			seen[m.Path] = true
			deduped = append(deduped, m)
		}
	}
	state.Tool.MediaResults = deduped
}
