package agent

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// withSecretVault installs this run's vault runtime in ctx and its redactor in
// req. It always overrides any runtime inherited from a parent context, so a
// delegate or child run never reuses its parent's secrets: it only sees what
// is granted to its own agent. Without a store, tenant or agent identity the
// run simply has no runtime, and vault-aware tools reject placeholders.
//
// The redactor is preloaded with every secret granted to the agent, not just
// the ones used so far, so a value sent to an allowed host in one run cannot be
// read back unredacted in the next.
func (l *Loop) withSecretVault(ctx context.Context, req *RunRequest) context.Context {
	if l.secretVault == nil || l.agentUUID == uuid.Nil || store.TenantIDFromContext(ctx) == uuid.Nil {
		return secretvault.WithRuntime(ctx, nil)
	}
	rt := secretvault.NewRuntime(l.secretVault, l.agentUUID, req.UserID)
	if err := rt.Preload(ctx); err != nil {
		// Without a preloaded redactor the run must not resolve secrets.
		slog.Warn("secretvault.preload_failed", "agent", l.id, "error", err)
		return secretvault.WithRuntime(ctx, nil)
	}
	req.SecretRedactor = rt.Redactor.Func()
	return secretvault.WithRuntime(ctx, rt)
}

// redactToolResult removes secret values from every text field of a tool
// result using the run's context redactor. It runs before the result reaches
// the LLM, events, session history or logs.
func redactToolResult(ctx context.Context, r *tools.Result) *tools.Result {
	if r == nil {
		return r
	}
	r.ForLLM = tracing.RedactText(ctx, r.ForLLM)
	r.ForUser = tracing.RedactText(ctx, r.ForUser)
	r.Deliverable = tracing.RedactText(ctx, r.Deliverable)
	if r.Err != nil {
		if msg := r.Err.Error(); tracing.RedactText(ctx, msg) != msg {
			r.Err = errors.New(tracing.RedactText(ctx, msg))
		}
	}
	return r
}
