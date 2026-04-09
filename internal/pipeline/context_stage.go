package pipeline

import (
	"context"
	"fmt"

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
	if s.deps.AutoInject != nil && state.Input.Message != "" {
		section, err := s.deps.AutoInject(ctx, state.Input.Message, state.Input.UserID)
		if err == nil && section != "" {
			state.Context.MemorySection = section
			sys := state.Messages.System()
			sys.Content += "\n\n" + section
			state.Messages.SetSystem(sys)
		}
	}

	return nil
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

