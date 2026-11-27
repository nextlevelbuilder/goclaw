package telegram

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// resolveBoundAgentID returns the normalized agent_key to route inbound
// messages to for a per-group/per-topic agent_id binding. It returns "" when
// the binding should be ignored (empty input or unknown agent) so the caller
// falls back to the channel default agent.
//
// Resolution is lazy and cached per channel: the first inbound message for a
// distinct binding performs one tenant-scoped agent lookup; all subsequent
// messages are served from boundAgentValid (raw config value → resolved
// agent_key, "" = invalid). This keeps the per-message hot path free of DB
// queries (repo rule: DB query reuse).
func (c *Channel) resolveBoundAgentID(ctx context.Context, agentID string) string {
	if agentID == "" {
		return ""
	}
	if v, ok := c.boundAgentValid.Load(agentID); ok {
		return v.(string)
	}
	resolved := c.validateBoundAgent(ctx, agentID)
	c.boundAgentValid.Store(agentID, resolved)
	return resolved
}

// effectiveInboundAgentID returns the agent_key that should process inbound
// messages for this chat/topic: the per-group/per-topic binding (when set and
// valid), otherwise the channel default agent. Used by the /stop, /stopall
// and /reset command paths, which must target the same agent (and thus the
// same session key) that the group's conversation runs under.
func (c *Channel) effectiveInboundAgentID(ctx context.Context, chatIDStr string, messageThreadID int, isGroup bool) string {
	target := c.AgentID()
	if isGroup {
		if tc := resolveTopicConfig(c.config, chatIDStr, messageThreadID); tc.agentID != "" {
			if bound := c.resolveBoundAgentID(ctx, tc.agentID); bound != "" {
				return bound
			}
		}
	}
	return target
}

// validateBoundAgent performs a tenant-scoped agent lookup for a configured
// agent_id (agent_key or UUID) and returns the normalized agent_key, or ""
// when the agent does not exist or cannot be verified.
func (c *Channel) validateBoundAgent(ctx context.Context, agentID string) string {
	if c.agentStore == nil {
		// Config-based channels have no agent store wired in; trust the config.
		// The inbound consumer fails closed with a warn log if the agent is unknown.
		slog.Debug("telegram: bound agent not validated (no agent store)",
			"agent_id", agentID, "channel", c.Name())
		return agentID
	}

	tctx := store.WithTenantID(ctx, c.TenantID())
	var (
		ag  *store.AgentData
		err error
	)
	if id, perr := uuid.Parse(agentID); perr == nil && id != uuid.Nil {
		ag, err = c.agentStore.GetByID(tctx, id)
	} else {
		ag, err = c.agentStore.GetByKey(tctx, agentID)
	}
	if err != nil || ag == nil {
		slog.Warn("telegram: bound agent not found, falling back to channel default",
			"agent_id", agentID, "channel", c.Name(), "tenant_id", c.TenantID(), "error", err)
		return ""
	}
	// Normalize to agent_key: routing and session keys use agent_key, not UUID
	// (same convention as InstanceLoader's SetAgentID(ag.AgentKey)).
	if ag.AgentKey != "" {
		return ag.AgentKey
	}
	return agentID
}
