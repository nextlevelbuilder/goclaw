package pipeline

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
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

	// 1b. Skill evolution postscript (matching v2 loop_finalize.go:52-57).
	if s.deps.SkillPostscript != nil && state.Observe.FinalContent != "" {
		state.Observe.FinalContent = s.deps.SkillPostscript(ctx, state.Observe.FinalContent, state.Tool.TotalToolCalls)
	}

	// 2. NO_REPLY detection: save to session for context but mark as silent.
	// Must run BEFORE session flush so the agent message is persisted even if suppressed.
	isSilent := s.deps.IsSilentReply != nil && s.deps.IsSilentReply(state.Observe.FinalContent)

	// 2b. Fallback for empty content (matching v2: channels need non-empty content to deliver).
	if state.Observe.FinalContent == "" && !isSilent {
		state.Observe.FinalContent = "..."
	}

	// 2c. Append content suffix (e.g. image markdown for WS) with dedup.
	if state.Input.ContentSuffix != "" && s.deps.DeduplicateMediaSuffix != nil {
		state.Observe.FinalContent += s.deps.DeduplicateMediaSuffix(state.Observe.FinalContent, state.Input.ContentSuffix)
	}

	// 2d. Merge forwarded media into results (matching v2 finalizeRun).
	for _, mf := range state.Input.ForwardMedia {
		ct := mf.MimeType
		state.Tool.MediaResults = append(state.Tool.MediaResults, MediaResult{Path: mf.Path, ContentType: ct})
	}

	// 3. Deduplicate + populate media sizes
	s.processMedia(state)

	// 3b. Persist assistant-generated images (Codex image_generation_call) to disk
	// BEFORE building the assistant message so MediaRefs are included in the session store.
	// Source is state.Observe.AssistantImages, which ObserveStage accumulates across
	// every iteration — required because LastResponse holds only the final iteration's
	// response (an image emitted mid-loop alongside a tool call would otherwise be lost).
	var assistantImageRefs []providers.MediaRef
	if s.deps.PersistAssistantImages != nil && len(state.Observe.AssistantImages) > 0 {
		workspace := ""
		if state.Workspace != nil {
			workspace = state.Workspace.ActivePath
		}
		// Build a scratch message carrying only Images so PersistAssistantImages can
		// decode/hash/write them and populate MediaRefs. The caller clears Images on
		// the scratch message — we harvest MediaRefs from there.
		scratch := &providers.Message{Images: state.Observe.AssistantImages}
		s.deps.PersistAssistantImages(scratch, workspace)
		assistantImageRefs = scratch.MediaRefs
		state.Observe.AssistantImages = nil // prevent double-processing on retries
	}

	// 3c. Build final assistant message with MediaRefs for session persistence.
	assistantMsg := providers.Message{
		Role:     "assistant",
		Content:  state.Observe.FinalContent,
		Thinking: state.Observe.FinalThinking,
	}
	for _, mr := range state.Tool.MediaResults {
		kind := "document"
		switch {
		case len(mr.ContentType) > 6 && mr.ContentType[:6] == "image/":
			kind = "image"
		case len(mr.ContentType) > 6 && mr.ContentType[:6] == "audio/":
			kind = "audio"
		case len(mr.ContentType) > 6 && mr.ContentType[:6] == "video/":
			kind = "video"
		}
		assistantMsg.MediaRefs = append(assistantMsg.MediaRefs, providers.MediaRef{
			ID:       filepath.Base(mr.Path),
			MimeType: mr.ContentType,
			Kind:     kind,
			Path:     mr.Path,
			Prompt:   mr.Prompt,
		})
	}
	// Append persisted assistant image refs (Codex image_generation_call output).
	assistantMsg.MediaRefs = append(assistantMsg.MediaRefs, assistantImageRefs...)
	state.Messages.AppendPending(assistantMsg)

	// 4. Flush remaining pending messages to session store
	pending := state.Messages.FlushPending()
	if len(pending) > 0 && s.deps.FlushMessages != nil {
		if err := s.deps.FlushMessages(ctx, state.Input.SessionKey, pending); err != nil {
			slog.Warn("finalize flush failed", "err", err)
		}
	}

	// 5. Update session metadata (token usage)
	if s.deps.UpdateMetadata != nil {
		if err := s.deps.UpdateMetadata(ctx, state.Input.SessionKey, state.Think.TotalUsage); err != nil {
			slog.Warn("finalize metadata update failed", "err", err)
		}
	}

	// 6. Bootstrap auto-cleanup
	if state.Context.HadBootstrap && s.deps.BootstrapCleanup != nil {
		if err := s.deps.BootstrapCleanup(ctx, state); err != nil {
			slog.Warn("bootstrap cleanup failed", "err", err)
		}
	}

	// 7. Post-run summarization (async background)
	if s.deps.MaybeSummarize != nil {
		s.deps.MaybeSummarize(ctx, state.Input.SessionKey)
	}

	// 8. Emit session.completed for consolidation pipeline (episodic → semantic → dreaming).
	if s.deps.EmitSessionCompleted != nil {
		msgCount := state.Messages.TotalLen()
		tokensUsed := state.Think.TotalUsage.PromptTokens + state.Think.TotalUsage.CompletionTokens
		s.deps.EmitSessionCompleted(ctx, state.Input.SessionKey, msgCount, tokensUsed, state.Compact.CompactionCount)
	}

	// 9. Strip internal [[...]] tags from user-facing content (matching v2 StripMessageDirectives).
	if state.Observe.FinalContent != "" && s.deps.StripMessageDirectives != nil {
		state.Observe.FinalContent = s.deps.StripMessageDirectives(state.Observe.FinalContent)
	}

	// 10. Suppress NO_REPLY (after session flush — content is persisted for context).
	if isSilent {
		slog.Info("v3 pipeline: NO_REPLY detected, suppressing delivery",
			"session", state.Input.SessionKey)
		state.Observe.FinalContent = ""
	}

	// 11. Hook: async EventStop — fire and forget.
	// run.completed event is emitted by loop_run.go after Pipeline.Run() returns,
	// with full tracing context. No duplicate emission here.
	if s.deps.Hooks != nil {
		detached := context.WithoutCancel(ctx)
		go s.deps.FireHook(detached, hooks.Event{ //nolint:errcheck
			EventID:   uuid.NewString(),
			SessionID: state.Input.SessionKey,
			TenantID:  store.TenantIDFromContext(ctx),
			AgentID:   store.AgentIDFromContext(ctx),
			HookEvent: hooks.EventStop,
		})
	}

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
