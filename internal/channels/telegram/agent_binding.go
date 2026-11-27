package telegram

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// boundAgentNegativeTTL bounds how long an invalid or transiently-unresolvable
// binding is cached. Valid bindings stay cached for the channel lifetime (a
// config edit rebuilds the channel), but a DB blip on the first message or an
// agent created afterwards must not pin the group to the channel default until
// restart.
const boundAgentNegativeTTL = 30 * time.Second

// boundAgentRef is the resolved form of a per-group/per-topic agent_id binding
// (or of the channel default agent when no binding applies).
type boundAgentRef struct {
	key string    // normalized agent_key used for routing/session keys; "" = none
	id  uuid.UUID // agent UUID when resolvable; zero for config-trust channels
}

// boundAgentCacheEntry is one boundAgents value: the resolved reference plus
// the time it was cached (used to expire negative results).
type boundAgentCacheEntry struct {
	ref boundAgentRef
	at  time.Time
}

// resolveBoundAgent returns the resolved binding for a raw configured agent_id
// (agent_key or UUID). An empty or unknown binding resolves to the zero value
// so callers fall back to the channel default agent.
//
// Resolution is lazy and cached per channel: the first inbound message for a
// distinct binding performs one tenant-scoped agent lookup. Valid results are
// cached for the channel lifetime; invalid/transient ones expire after
// boundAgentNegativeTTL so the next message retries. This keeps the per-message
// hot path free of DB queries (repo rule: DB query reuse) without pinning a
// group to the wrong agent after a store blip.
func (c *Channel) resolveBoundAgent(ctx context.Context, agentID string) boundAgentRef {
	if agentID == "" {
		return boundAgentRef{}
	}
	if v, ok := c.boundAgents.Load(agentID); ok {
		entry := v.(boundAgentCacheEntry)
		if entry.ref.key != "" || time.Since(entry.at) < boundAgentNegativeTTL {
			return entry.ref
		}
	}
	resolved := c.validateBoundAgent(ctx, agentID)
	c.boundAgents.Store(agentID, boundAgentCacheEntry{ref: resolved, at: time.Now()})
	return resolved
}

// resolveBoundAgentID returns the normalized agent_key to route inbound
// messages to for a per-group/per-topic agent_id binding. It returns "" when
// the binding should be ignored (empty input or unknown agent) so the caller
// falls back to the channel default agent.
func (c *Channel) resolveBoundAgentID(ctx context.Context, agentID string) string {
	return c.resolveBoundAgent(ctx, agentID).key
}

// effectiveInboundAgent returns the agent that handles this chat/topic: the
// per-group/per-topic binding (when set and valid), otherwise the channel
// default agent. The default carries no UUID; callers that need one resolve it
// via effectiveAgentUUID.
func (c *Channel) effectiveInboundAgent(ctx context.Context, chatIDStr string, messageThreadID int, isGroup bool) boundAgentRef {
	if isGroup {
		if tc := resolveTopicConfig(c.config, chatIDStr, messageThreadID); tc.agentID != "" {
			if ref := c.resolveBoundAgent(ctx, tc.agentID); ref.key != "" {
				return ref
			}
		}
	}
	return boundAgentRef{key: c.AgentID()}
}

// effectiveInboundAgentID returns the agent_key that should process inbound
// messages for this chat/topic. Used by the /stop, /stopall and /reset command
// paths, which must target the same agent (and thus the same session key) that
// the group's conversation runs under.
func (c *Channel) effectiveInboundAgentID(ctx context.Context, chatIDStr string, messageThreadID int, isGroup bool) string {
	return c.effectiveInboundAgent(ctx, chatIDStr, messageThreadID, isGroup).key
}

// effectiveAgentUUID returns the UUID of the agent that handles this chat/topic
// (the bound agent when set and valid, otherwise the channel default). The
// permission-gated and agent-scoped auxiliary commands (/reset writer gate,
// /tasks, /subagents, /writers, /addcron) and per-agent wake words use it so
// they consult the same agent their session targets.
func (c *Channel) effectiveAgentUUID(ctx context.Context, chatIDStr string, messageThreadID int, isGroup bool) (uuid.UUID, error) {
	ref := c.effectiveInboundAgent(ctx, chatIDStr, messageThreadID, isGroup)
	if ref.id != uuid.Nil {
		return ref.id, nil
	}
	return c.resolveAgentUUIDForKey(ctx, ref.key)
}

// validateBoundAgent performs a tenant-scoped agent lookup for a configured
// agent_id (agent_key or UUID) and returns the normalized reference, or the
// zero value when the agent does not exist or cannot be verified.
func (c *Channel) validateBoundAgent(ctx context.Context, agentID string) boundAgentRef {
	if c.agentStore == nil {
		// Config-based channels have no agent store wired in; trust the config.
		// The inbound consumer fails closed with a warn log if the agent is unknown.
		slog.Debug("telegram: bound agent not validated (no agent store)",
			"agent_id", agentID, "channel", c.Name())
		ref := boundAgentRef{key: agentID}
		if id, err := uuid.Parse(agentID); err == nil && id != uuid.Nil {
			ref.id = id // UUID literals stay usable for permission lookups
		}
		return ref
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
		return boundAgentRef{}
	}
	// Normalize to agent_key: routing and session keys use agent_key, not UUID
	// (same convention as InstanceLoader's SetAgentID(ag.AgentKey)).
	key := agentID
	if ag.AgentKey != "" {
		key = ag.AgentKey
	}
	return boundAgentRef{key: key, id: ag.ID}
}
