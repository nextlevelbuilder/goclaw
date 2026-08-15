package consolidation

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// resolvedDreamingConfig is the fully-populated view of dreaming settings used
// inside Handle(). All fields are concrete (no pointers) so the worker can
// operate without further nil-checks.
type resolvedDreamingConfig struct {
	Enabled    bool
	Debounce   time.Duration
	Threshold  int
	VerboseLog bool
}

// defaultDreamingConfig returns hardcoded defaults that match the worker's
// legacy behaviour before per-agent overrides were added. These values are
// mirrored into the struct defaults in workers.go.
func defaultDreamingConfig() resolvedDreamingConfig {
	return resolvedDreamingConfig{
		Enabled:    true,
		Debounce:   dreamingDefaultDebounce,
		Threshold:  dreamingDefaultThreshold,
		VerboseLog: false,
	}
}

// mergeDreamingConfig applies an agent-provided override onto the defaults.
// Nil pointer fields fall through to defaults; zero integers are treated as
// "unset" so a legacy empty JSONB still resolves to defaults. Negative values
// are clamped to defaults to defend against malformed config.
func mergeDreamingConfig(base resolvedDreamingConfig, override *config.DreamingConfig) resolvedDreamingConfig {
	if override == nil {
		return base
	}
	if override.Enabled != nil {
		base.Enabled = *override.Enabled
	}
	if override.DebounceMs > 0 {
		base.Debounce = time.Duration(override.DebounceMs) * time.Millisecond
	}
	if override.Threshold > 0 {
		base.Threshold = override.Threshold
	}
	if override.VerboseLog != nil {
		base.VerboseLog = *override.VerboseLog
	}
	return base
}

// DreamingConfigResolver fetches per-agent dreaming config at event handling
// time. Implementations should be fast and non-blocking; the consolidation
// worker calls this on every event that passes the agent-ID parse.
type DreamingConfigResolver func(ctx context.Context, agentID string) *config.DreamingConfig

// newAgentStoreResolver builds a DreamingConfigResolver that reads from the
// given AgentStore and extracts MemoryConfig.Dreaming. Returns nil if the
// store is nil so the worker can fall back to defaults.
func newAgentStoreResolver(agents store.AgentCRUDStore) DreamingConfigResolver {
	if agents == nil {
		return nil
	}
	return func(ctx context.Context, agentID string) *config.DreamingConfig {
		id, err := uuid.Parse(agentID)
		if err != nil {
			return nil
		}
		ag, err := agents.GetByIDUnscoped(ctx, id)
		if err != nil || ag == nil {
			return nil
		}
		mc := ag.ParseMemoryConfig()
		if mc == nil {
			return nil
		}
		return mc.Dreaming
	}
}
