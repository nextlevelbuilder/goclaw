package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providerresolve"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ChannelFactory creates a Channel from DB instance data.
// name: channel name (registered in Manager, used in session keys).
// creds: decrypted credentials JSON (token, API keys, etc.).
// cfg: non-secret config JSONB (dm_policy, dm_stream, group_stream, etc.).
type ChannelFactory func(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (Channel, error)

// InstanceLoader loads channel instances from the database and registers them with the Manager.
// Follows a load-all-at-startup pattern with cache invalidation for reload.
type InstanceLoader struct {
	store             store.ChannelInstanceStore
	agentStore        store.AgentStore
	providerReg       *providers.Registry
	pendingCompactCfg *config.PendingCompactionConfig
	factories         map[string]ChannelFactory
	manager           *Manager
	msgBus            *bus.MessageBus
	pairingSvc        store.PairingStore
	mu                sync.Mutex
	loaded            map[string]struct{} // channel names managed by this loader
}

// NewInstanceLoader creates a new InstanceLoader.
func NewInstanceLoader(
	s store.ChannelInstanceStore,
	agentStore store.AgentStore,
	mgr *Manager,
	msgBus *bus.MessageBus,
	pairingSvc store.PairingStore,
) *InstanceLoader {
	return &InstanceLoader{
		store:      s,
		agentStore: agentStore,
		factories:  make(map[string]ChannelFactory),
		manager:    mgr,
		msgBus:     msgBus,
		pairingSvc: pairingSvc,
		loaded:     make(map[string]struct{}),
	}
}

// SetProviderRegistry sets the provider registry for pending message compaction.
// Must be called before LoadAll/Reload.
func (l *InstanceLoader) SetProviderRegistry(reg *providers.Registry) {
	l.providerReg = reg
}

// SetPendingCompactionConfig sets the global pending message compaction thresholds.
// Must be called before LoadAll/Reload.
func (l *InstanceLoader) SetPendingCompactionConfig(cfg *config.PendingCompactionConfig) {
	l.pendingCompactCfg = cfg
}

// RegisterFactory registers a factory for a channel type (e.g., "telegram", "discord").
func (l *InstanceLoader) RegisterFactory(channelType string, factory ChannelFactory) {
	l.factories[channelType] = factory
}

// LoadAll loads all enabled channel instances from the database, creates channels, and registers them.
func (l *InstanceLoader) LoadAll(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	instances, err := l.store.ListAllEnabled(ctx)
	if err != nil {
		return err
	}

	registered := 0
	for _, inst := range instances {
		// Don't start channels here — StartAll() will start them after all channels are registered.
		if err := l.loadInstance(ctx, inst, false); err != nil {
			slog.Error("failed to load channel instance",
				"name", inst.Name, "type", inst.ChannelType, "error", err)
			continue
		}
		registered++
	}

	if registered > 0 {
		slog.Info("channel instances loaded from DB", "count", registered)
	}
	return nil
}

// Reload stops all managed channels, reloads from DB, and starts new ones.
// Called on cache invalidation events.
func (l *InstanceLoader) Reload(ctx context.Context) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Stop and unregister old channels
	for name := range l.loaded {
		if ch, ok := l.manager.GetChannel(name); ok {
			if err := ch.Stop(ctx); err != nil {
				slog.Warn("failed to stop channel instance on reload", "name", name, "error", err)
			}
		}
		l.manager.UnregisterChannel(name)
	}
	l.loaded = make(map[string]struct{})

	// Brief pause to let external APIs (e.g., Telegram getUpdates) release polling locks.
	time.Sleep(500 * time.Millisecond)

	// Reload from DB (all tenants — server-internal)
	instances, err := l.store.ListAllEnabled(ctx)
	if err != nil {
		slog.Error("failed to reload channel instances", "error", err)
		return
	}

	registered := 0
	for _, inst := range instances {
		// Reload must start channels immediately (StartAll was called at boot, not again).
		if err := l.loadInstance(ctx, inst, true); err != nil {
			slog.Error("failed to reload channel instance",
				"name", inst.Name, "type", inst.ChannelType, "error", err)
			continue
		}
		registered++
	}

	slog.Info("channel instances reloaded", "count", registered)
}

// Stop stops all managed channels.
func (l *InstanceLoader) Stop(ctx context.Context) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for name := range l.loaded {
		if ch, ok := l.manager.GetChannel(name); ok {
			if err := ch.Stop(ctx); err != nil {
				slog.Warn("failed to stop channel instance", "name", name, "error", err)
			}
		}
		l.manager.UnregisterChannel(name)
	}
	l.loaded = make(map[string]struct{})
}

// coerceStringBools converts string "true"/"false" values to JSON booleans
// in a raw config blob. Older UI versions saved select-based bool fields as strings.
func coerceStringBools(data json.RawMessage) json.RawMessage {
	if len(data) == 0 {
		return data
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return data
	}
	changed := false
	for k, v := range m {
		if s, ok := v.(string); ok {
			switch s {
			case "true":
				m[k] = true
				changed = true
			case "false":
				m[k] = false
				changed = true
			}
		}
	}
	if !changed {
		return data
	}
	out, _ := json.Marshal(m)
	return out
}

// LoadedNames returns the set of channel names managed by the loader.
func (l *InstanceLoader) LoadedNames() map[string]struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()

	result := make(map[string]struct{}, len(l.loaded))
	maps.Copy(result, l.loaded)
	return result
}

// loadInstance creates and registers a single channel from a DB instance (caller must hold lock).
// If autoStart is true, the channel is started immediately (used by Reload).
// If false, the caller is responsible for starting (used by LoadAll, where StartAll handles it).
func (l *InstanceLoader) loadInstance(ctx context.Context, inst store.ChannelInstanceData, autoStart bool) error {
	l.loaded[inst.Name] = struct{}{}

	factory, ok := l.factories[inst.ChannelType]
	if !ok {
		l.manager.RecordHealth(inst.Name, NewChannelHealthForType(
			inst.ChannelType,
			ChannelHealthStateFailed,
			"Unsupported channel type",
			fmt.Sprintf("No channel factory is registered for %q", inst.ChannelType),
			ChannelFailureKindConfig,
			false,
		))
		slog.Warn("no factory for channel type", "type", inst.ChannelType, "name", inst.Name)
		return nil
	}

	// Normalize config: convert string "true"/"false" to JSON booleans.
	// Older UI versions saved select-based bool fields as strings.
	cfg := coerceStringBools(inst.Config)

	ch, err := factory(inst.Name, inst.Credentials, cfg, l.msgBus, l.pairingSvc)
	if err != nil {
		l.manager.RecordFailureForType(inst.Name, inst.ChannelType, "", err)
		return err
	}
	if ch == nil {
		l.manager.RecordHealth(inst.Name, NewChannelHealthForType(
			inst.ChannelType,
			ChannelHealthStateFailed,
			"Missing credentials",
			"Channel instance is enabled but required credentials are incomplete.",
			ChannelFailureKindConfig,
			false,
		))
		slog.Info("channel instance not ready (missing credentials)", "name", inst.Name, "type", inst.ChannelType)
		return nil
	}

	// Resolve agent_key from UUID — the routing system (Router, session keys) uses agent_key, not UUID.
	// Use the instance's tenant_id to scope the agent lookup.
	instCtx := store.WithTenantID(ctx, inst.TenantID)
	var ag *store.AgentData
	if base, ok := ch.(interface{ SetAgentID(string) }); ok {
		var err error
		ag, err = l.agentStore.GetByID(instCtx, inst.AgentID)
		if err != nil {
			l.manager.RecordFailureForType(inst.Name, inst.ChannelType, "", fmt.Errorf("agent %s not found for channel %s: %w", inst.AgentID, inst.Name, err))
			return fmt.Errorf("agent %s not found for channel %s: %w", inst.AgentID, inst.Name, err)
		}
		base.SetAgentID(ag.AgentKey)
	}
	// Set the platform type on the channel so Manager.ChannelTypeForName can read it.
	if base, ok := ch.(interface{ SetType(string) }); ok {
		base.SetType(inst.ChannelType)
	}
	// Propagate tenant_id from DB instance to channel for tenant-scoped message handling.
	if base, ok := ch.(interface{ SetTenantID(uuid.UUID) }); ok {
		base.SetTenantID(inst.TenantID)
	}
	// Propagate tenant_id to pending history for compaction/sweep DB operations.
	// Factory creates PendingHistory before SetTenantID is called, so tenantID is uuid.Nil at construction.
	if ph, ok := ch.(interface{ SetPendingHistoryTenantID(uuid.UUID) }); ok {
		ph.SetPendingHistoryTenantID(inst.TenantID)
	}

	// Wire pending message auto-compaction.
	// Priority: config provider/model > agent's provider/model > fallback.
	if pc, ok := ch.(PendingCompactable); ok && l.providerReg != nil {
		var p providers.Provider
		var model string

		// Try config-level provider/model first.
		tctx := store.WithTenantID(ctx, inst.TenantID)
		if l.pendingCompactCfg != nil && l.pendingCompactCfg.Provider != "" {
			if cp, err := l.providerReg.Get(tctx, l.pendingCompactCfg.Provider); err == nil {
				p = cp
				model = l.pendingCompactCfg.Model
				if model == "" {
					model = cp.DefaultModel()
				}
			}
		}
		// Fallback: agent's provider/model.
		if p == nil && ag != nil && ag.Provider != "" {
			if ap, err := providerresolve.ResolveConfiguredProvider(l.providerReg, ag); err == nil {
				p = ap
				model = ag.Model
				if model == "" {
					model = ap.DefaultModel()
				}
			}
		}

		if p != nil && model != "" {
			cc := &CompactionConfig{
				Provider: p,
				Model:    model,
			}
			if l.pendingCompactCfg != nil {
				cc.Threshold = l.pendingCompactCfg.Threshold
				cc.KeepRecent = l.pendingCompactCfg.KeepRecent
				cc.MaxTokens = l.pendingCompactCfg.MaxTokens
			}
			pc.SetPendingCompaction(cc)
			slog.Debug("pending compaction configured", "channel", inst.Name, "provider", p.Name(), "model", model,
				"threshold", cc.Threshold, "keep_recent", cc.KeepRecent, "max_tokens", cc.MaxTokens)
		} else {
			attemptedProvider := ""
			if l.pendingCompactCfg != nil {
				attemptedProvider = l.pendingCompactCfg.Provider
			}
			if attemptedProvider == "" && ag != nil {
				attemptedProvider = ag.Provider
			}
			slog.Warn("pending compaction not configured: provider/model unavailable",
				"channel", inst.Name, "agent_id", inst.AgentID, "attempted_provider", attemptedProvider)
		}
	}
	l.manager.RegisterChannel(inst.Name, ch)

	// Start the channel if requested (Reload path). LoadAll defers to StartAll.
	if autoStart {
		if err := ch.Start(ctx); err != nil {
			l.manager.recordChannelStartFailure(inst.Name, ch, "", err)
			slog.Error("channel instance start failed", "name", inst.Name, "error", err)
			// Still registered — will show as not running.
		} else {
			l.manager.RecordHealth(inst.Name, snapshotChannelHealth(ch))
		}
	}

	slog.Info("channel instance loaded",
		"name", inst.Name, "type", inst.ChannelType, "agent_id", inst.AgentID)
	return nil
}
