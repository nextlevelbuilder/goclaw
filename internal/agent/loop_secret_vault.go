package agent

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
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
	rt.Forms = secretvault.DefaultForms()
	rt.SessionKey = req.SessionKey
	rt.AllowWrite = l.secretVaultWrite
	rt.Agents = l.secretVaultAgents
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

// sensitiveArgTools lists tools whose arguments carry a secret value. The
// value is registered with the run redactor before the tool call is emitted as
// an event, traced or stored, because those sinks see the arguments before the
// tool itself runs.
var sensitiveArgTools = map[string]bool{"vault_secret_set": true}

// maskSensitiveToolCalls registers the secret values of every sensitive call
// in a model response at once, as soon as the response arrives. Parallel
// sibling calls and later sinks (the LLM span output, the stored assistant
// turn, a call dropped as truncated or malformed) then all see the mask, not
// only the call that carries the value. Calls whose arguments failed to
// parse keep only what the provider could parse; that part is registered too.
func (l *Loop) maskSensitiveToolCalls(ctx context.Context, calls []providers.ToolCall) {
	for _, tc := range calls {
		tc = l.normalizeToolCall(tc)
		maskSensitiveToolArgs(ctx, l.canonicalToolName(l.resolveToolCallName(tc.Name)), tc.Arguments)
	}
}

// maskSensitiveToolArgs registers the secret value carried in the arguments of
// a sensitive tool with the run's redactor. Call it before the tool.call event
// and the tool span are emitted. Without a vault runtime there is no redactor
// and the tool refuses to run.
func maskSensitiveToolArgs(ctx context.Context, toolName string, args map[string]any) {
	if !sensitiveArgTools[toolName] {
		return
	}
	rt := secretvault.RuntimeFrom(ctx)
	if rt == nil {
		return
	}
	value, _ := args["value"].(string)
	username, _ := args["username"].(string)
	if value != "" {
		rt.Redactor.AddPayload(store.SecretVaultPayload{Username: username, Secret: value})
	}
}

// secretVaultAgentLookup resolves an agent key to its id within the context
// tenant. A cross-tenant store lookup is rejected by comparing tenants.
func secretVaultAgentLookup(agents store.AgentStore) secretvault.AgentLookup {
	if agents == nil {
		return nil
	}
	return func(ctx context.Context, agentKey string) (uuid.UUID, error) {
		tenant := store.TenantIDFromContext(ctx)
		ag, err := agents.GetByKey(ctx, agentKey)
		if err != nil || ag == nil || tenant == uuid.Nil || ag.TenantID != tenant {
			return uuid.Nil, secretvault.ErrAgentNotFound
		}
		return ag.ID, nil
	}
}
