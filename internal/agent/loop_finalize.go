package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// finalizeRun performs post-loop processing: sanitization, media dedup, session flush,
// bootstrap cleanup, and builds the final RunResult.
func (l *Loop) finalizeRun(
	ctx context.Context,
	rs *runState,
	req *RunRequest,
	history []providers.Message,
	hadBootstrap bool,
	toolTiming ToolTimingMap,
) *RunResult {
	// 5. Full sanitization pipeline (matching TS extractAssistantText + sanitizeUserFacingText)
	rs.finalContent = SanitizeAssistantContent(rs.finalContent)

	// 6. Handle NO_REPLY: save to session for context but mark as silent.
	isSilent := IsSilentReply(rs.finalContent)

	// 5b. Skill evolution: postscript suggestion after complex tasks.
	if l.skillEvolve && l.skillNudgeInterval > 0 &&
		rs.totalToolCalls >= l.skillNudgeInterval &&
		rs.finalContent != "" && !isSilent && !rs.skillPostscriptSent {
		rs.skillPostscriptSent = true
		locale := store.LocaleFromContext(ctx)
		rs.finalContent += "\n\n---\n_" + i18n.T(locale, i18n.MsgSkillNudgePostscript) + "_"
	}

	// 7. Fallback for empty content
	if rs.finalContent == "" {
		if len(rs.asyncToolCalls) > 0 {
			rs.finalContent = "..."
		} else {
			rs.finalContent = "..."
		}
	}

	// Append content suffix (e.g. image markdown for WS) before saving to session.
	// Dedup by basename: skip suffix lines whose file already appears in the agent's text.
	if req.ContentSuffix != "" {
		rs.finalContent += deduplicateMediaSuffix(rs.finalContent, req.ContentSuffix)
	}

	// Collect forwarded media + dedup + populate sizes BEFORE saving to session,
	// so we can attach output MediaRefs to the assistant message for history reload.
	for _, mf := range req.ForwardMedia {
		ct := mf.MimeType
		if ct == "" {
			ct = mimeFromExt(filepath.Ext(mf.Path))
		}
		rs.mediaResults = append(rs.mediaResults, MediaResult{Path: mf.Path, ContentType: ct})
	}
	rs.mediaResults = deduplicateMedia(rs.mediaResults)
	for i := range rs.mediaResults {
		if rs.mediaResults[i].Size == 0 {
			if info, err := os.Stat(rs.mediaResults[i].Path); err == nil {
				rs.mediaResults[i].Size = info.Size()
			}
		}
	}

	// Build final assistant message with output media refs for history persistence.
	assistantMsg := providers.Message{
		Role:     "assistant",
		Content:  rs.finalContent,
		Thinking: rs.finalThinking,
	}
	for _, mr := range rs.mediaResults {
		kind := "document"
		if strings.HasPrefix(mr.ContentType, "image/") {
			kind = "image"
		} else if strings.HasPrefix(mr.ContentType, "audio/") {
			kind = "audio"
		} else if strings.HasPrefix(mr.ContentType, "video/") {
			kind = "video"
		}
		assistantMsg.MediaRefs = append(assistantMsg.MediaRefs, providers.MediaRef{
			ID:       filepath.Base(mr.Path),
			MimeType: mr.ContentType,
			Kind:     kind,
		})
	}
	rs.pendingMsgs = append(rs.pendingMsgs, assistantMsg)

	// Bootstrap nudge: if model didn't call write_file on turn 2+, inject reminder
	// into session history so the next turn sees it.
	if hadBootstrap && l.bootstrapCleanup != nil {
		nudgeUserTurns := 1
		for _, m := range history {
			if m.Role == "user" {
				nudgeUserTurns++
			}
		}
		if !rs.bootstrapWriteDetected && nudgeUserTurns >= 2 && nudgeUserTurns < bootstrapAutoCleanupTurns {
			rs.pendingMsgs = append(rs.pendingMsgs, providers.Message{
				Role:    "user",
				Content: "[System] You haven't completed onboarding yet. Please update USER.md with the user's details and clear BOOTSTRAP.md as instructed.",
			})
		}
	}

	// Flush all buffered messages to session atomically.
	for _, msg := range rs.pendingMsgs {
		l.sessions.AddMessage(ctx, req.SessionKey, msg)
	}

	// Persist adaptive tool timing to session metadata.
	if serialized := toolTiming.Serialize(); serialized != "" {
		l.sessions.SetSessionMetadata(ctx, req.SessionKey, map[string]string{"tool_timing": serialized})
	}

	// Write session metadata (matching TS session entry updates)
	l.sessions.UpdateMetadata(ctx, req.SessionKey, l.model, l.provider.Name(), req.Channel)
	l.sessions.AccumulateTokens(ctx, req.SessionKey, int64(rs.totalUsage.PromptTokens), int64(rs.totalUsage.CompletionTokens))

	// Calibrate token estimation: store actual prompt tokens + message count.
	if rs.totalUsage.PromptTokens > 0 {
		msgCount := len(history) + rs.checkpointFlushedMsgs + len(rs.pendingMsgs)
		l.sessions.SetLastPromptTokens(ctx, req.SessionKey, rs.totalUsage.PromptTokens, msgCount)
	}

	l.sessions.Save(ctx, req.SessionKey)

	// Bootstrap auto-cleanup: after enough conversation turns, remove BOOTSTRAP.md.
	if hadBootstrap && l.bootstrapCleanup != nil {
		userTurns := 1 // current user message
		for _, m := range history {
			if m.Role == "user" {
				userTurns++
			}
		}
		if userTurns >= bootstrapAutoCleanupTurns {
			if cleanErr := l.bootstrapCleanup(ctx, l.agentUUID, req.UserID); cleanErr != nil {
				slog.Warn("bootstrap auto-cleanup failed", "error", cleanErr, "agent", l.id, "user", req.UserID)
			} else {
				slog.Info("bootstrap auto-cleanup completed", "agent", l.id, "user", req.UserID, "turns", userTurns)
			}
		}
	}

	// 8. Metadata Stripping: Clean internal [[...]] tags for user-facing content
	rs.finalContent = StripMessageDirectives(rs.finalContent)
	if isSilent {
		slog.Info("agent loop: NO_REPLY detected, suppressing delivery",
			"agent", l.id, "session", req.SessionKey)
		rs.finalContent = ""
	}

	// 9. Maybe summarize
	l.maybeSummarize(ctx, req.SessionKey)

	return &RunResult{
		Content:        rs.finalContent,
		RunID:          req.RunID,
		Iterations:     rs.iteration,
		Usage:          &rs.totalUsage,
		Media:          rs.mediaResults,
		Deliverables:   rs.deliverables,
		BlockReplies:   rs.blockReplies,
		LastBlockReply: rs.lastBlockReply,
	}
}
