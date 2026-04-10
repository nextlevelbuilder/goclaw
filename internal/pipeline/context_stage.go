package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// ContextStage runs once in setup. Resolves workspace, loads context files,
// builds system prompt, computes overhead tokens, enriches media, injects team reminders.
type ContextStage struct {
	deps *PipelineDeps
}

// NewContextStage creates a ContextStage with the given dependencies.
func NewContextStage(deps *PipelineDeps) *ContextStage {
	return &ContextStage{deps: deps}
}

func (s *ContextStage) Name() string { return "context" }

// Execute populates RunState with workspace, context files, system prompt, and overhead tokens.
func (s *ContextStage) Execute(ctx context.Context, state *RunState) error {
	// 0. Inject context values (agent/tenant/user/workspace scoping, input guard, truncation).
	// Wraps injectContext() for v3 pipeline — called once before all other context setup.
	if s.deps.InjectContext != nil {
		enrichedCtx, err := s.deps.InjectContext(ctx, state.Input)
		if err != nil {
			return fmt.Errorf("inject context: %w", err)
		}
		ctx = enrichedCtx
		state.Ctx = ctx
	}

	// 0.5. Resolve the effective context window for this run's provider/model.
	// Done once here so PruneStage reads a stable value on every iteration and
	// the budget can't drift if the model somehow changes mid-run. A zero
	// result from the resolver (unknown model, no registry) leaves the field
	// zero — PruneStage then falls back to Config.ContextWindow.
	if s.deps.ResolveContextWindow != nil && state.Model != "" {
		providerID := ""
		if state.Provider != nil {
			providerID = state.Provider.Name()
		}
		if cw := s.deps.ResolveContextWindow(providerID, state.Model); cw > 0 {
			state.Context.EffectiveContextWindow = cw
		}
	}

	// 1. Resolve workspace
	if s.deps.ResolveWorkspace != nil {
		ws, err := s.deps.ResolveWorkspace(ctx, state.Input)
		if err != nil {
			return fmt.Errorf("resolve workspace: %w", err)
		}
		state.Workspace = ws
	}

	// 2. Load context files (agent-level + per-user + fallback bootstrap)
	if s.deps.LoadContextFiles != nil {
		files, hadBootstrap := s.deps.LoadContextFiles(ctx, state.Input.UserID)
		state.Context.ContextFiles = toAnySlice(files)
		state.Context.HadBootstrap = hadBootstrap
	}

	// 3. Load session history + summary before BuildMessages.
	if s.deps.LoadSessionHistory != nil && state.Input.SessionKey != "" {
		history, summary := s.deps.LoadSessionHistory(ctx, state.Input.SessionKey)
		if len(history) > 0 {
			state.Messages.SetHistory(history)
		}
		state.Context.Summary = summary
	}

	// 4. Build system prompt + history via callback (wraps buildMessages)
	if s.deps.BuildMessages != nil {
		msgs, err := s.deps.BuildMessages(ctx, state.Input, state.Messages.History(), state.Context.Summary)
		if err != nil {
			return fmt.Errorf("build messages: %w", err)
		}
		if len(msgs) > 0 {
			state.Messages.SetSystem(msgs[0])
			if len(msgs) > 1 {
				state.Messages.SetHistory(msgs[1:])
			}
		}
	}

	// 5. Compute overhead tokens via TokenCounter (replaces heuristic estimateOverhead)
	if s.deps.TokenCounter != nil {
		system := state.Messages.System()
		overhead := s.deps.TokenCounter.CountMessages(state.Model, []providers.Message{system})
		state.Context.OverheadTokens = overhead
	}

	// 6. Enrich input media (resolve refs, inline descriptions).
	// Receives full RunState so it can access MessageBuffer for in-place enrichment.
	if s.deps.EnrichMedia != nil {
		if err := s.deps.EnrichMedia(ctx, state); err != nil {
			return fmt.Errorf("enrich media: %w", err)
		}
	}

	// 7. Inject team task reminders into messages
	if s.deps.InjectReminders != nil {
		updated := s.deps.InjectReminders(ctx, state.Input, state.Messages.History())
		state.Messages.SetHistory(updated)
	}

	// 8. Auto-inject L0 memory context into system prompt.
	// V3RetrievalEnabled check removed — auto-inject runs whenever AutoInject is available.
	// Phase 9: pass recent conversation context so vector search can resolve
	// pronouns and implicit references in follow-up questions.
	if s.deps.AutoInject != nil && state.Input.Message != "" {
		recentCtx := buildRecentContext(state.Messages.History())
		section, err := s.deps.AutoInject(ctx, state.Input.Message, state.Input.UserID, recentCtx)
		if err == nil && section != "" {
			state.Context.MemorySection = section
			sys := state.Messages.System()
			sys.Content += "\n\n" + section
			state.Messages.SetSystem(sys)
		}
	}

	return nil
}

// buildRecentContext concatenates the trailing user turns from the history
// buffer into a short snippet suitable for enriching a recall query. Walks
// backward from the end so we keep the most recent turns regardless of
// earlier messages being pruned. Returns "" when there's no usable context.
//
// Budget: up to 2 user turns, max ~300 runes total. Rune (not byte) cap keeps
// vi/zh locales safe — a byte-wise clip would slice multi-byte characters
// and emit invalid UTF-8 to the embedding model. Tuning knob is intentional
// here rather than config-driven — Phase 9 adds it only if operational data
// shows variance across agent types.
func buildRecentContext(history []providers.Message) string {
	const maxTurns = 2
	const maxRunes = 300
	if len(history) == 0 {
		return ""
	}
	turns := make([]string, 0, maxTurns)
	for i := len(history) - 1; i >= 0 && len(turns) < maxTurns; i-- {
		m := history[i]
		if m.Role != "user" || m.Content == "" {
			continue
		}
		turns = append([]string{m.Content}, turns...) // prepend to preserve order
	}
	if len(turns) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, t := range turns {
		if i > 0 {
			sb.WriteString(" | ")
		}
		sb.WriteString(t)
	}
	joined := sb.String()
	// Rune-safe tail clip: keep the most recent portion of the conversation
	// (closest in time to the current turn) rather than the oldest.
	runes := []rune(joined)
	if len(runes) > maxRunes {
		joined = string(runes[len(runes)-maxRunes:])
	}
	return joined
}

// toAnySlice converts []bootstrap.ContextFile to []any for ContextState.ContextFiles.
// Phase 8 will remove this when ContextState uses typed field.
func toAnySlice(files []bootstrap.ContextFile) []any {
	out := make([]any, len(files))
	for i, f := range files {
		out[i] = f
	}
	return out
}

