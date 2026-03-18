package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
)

// makeSchedulerRunFunc creates the RunFunc for the scheduler.
// It extracts the agentID from the session key and routes to the correct agent loop.
func makeSchedulerRunFunc(agents *agent.Router, cfg *config.Config) scheduler.RunFunc {
	return func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		// Extract agentID from session key.
		// Supported formats:
		//   agent:{agentId}:{rest}
		//   delegate:{sourceUUID8}:{targetAgentKey}:{delegationId}  (legacy, kept for existing sessions)
		agentID := cfg.ResolveDefaultAgentID()
		if parts := strings.SplitN(req.SessionKey, ":", 4); len(parts) >= 2 {
			switch parts[0] {
			case "agent":
				agentID = parts[1]
			case "delegate":
				if len(parts) >= 3 {
					agentID = parts[2]
				}
			}
		}

		loop, err := agents.Get(agentID)
		if err != nil {
			return nil, fmt.Errorf("agent %s not found: %w", agentID, err)
		}

		// Register run with the agent router so IsSessionBusy() and AbortRunsForSession()
		// work correctly for inbound channel runs (Telegram DM intent classifier, /stop, etc.).
		// The ctx from the scheduler is already cancellable; we create a child so the router's
		// cancel func is independent from the scheduler's cancel func. Calling cancel twice is safe.
		runCtx, cancel := context.WithCancel(ctx)
		injectCh := agents.RegisterRun(req.RunID, req.SessionKey, agentID, cancel)
		defer agents.UnregisterRun(req.RunID)
		defer cancel()

		req.InjectCh = injectCh
		return loop.Run(runCtx, req)
	}
}
