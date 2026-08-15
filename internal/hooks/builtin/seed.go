package builtin

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Seed UPSERTs every registered builtin into hookStore following the
// version-reconciliation rules in phase-04:
//
//   - DB row missing → INSERT one row per event with source='builtin'.
//   - DB version < embed version → UPDATE content (preserve enabled toggle).
//   - DB version == embed version → no-op.
//   - DB version > embed version → WARN + keep DB (no rollback).
//
// Runs AFTER migrations and BEFORE dispatcher handler registration. Uses
// store.RoleOwner + hooks.WithSeedBypass to traverse the tenant-scope guard
// and the builtin-readonly Update protection; both markers are process-local.
func Seed(ctx context.Context, hookStore hooks.HookStore, cfg config.HooksConfig) error {
	regMu.RLock()
	all := make([]Spec, len(specs))
	copy(all, specs)
	regMu.RUnlock()

	disabled := make(map[string]struct{}, len(cfg.BuiltinDisable))
	for _, d := range cfg.BuiltinDisable {
		disabled[d] = struct{}{}
	}

	ctx = store.WithRole(ctx, store.RoleOwner)
	ctx = hooks.WithSeedBypass(ctx)

	for i := range all {
		s := all[i]
		_, forceDisabled := disabled[s.ID]
		if err := seedOne(ctx, hookStore, s, forceDisabled); err != nil {
			slog.Warn("hooks.builtin_seed_failed", "id", s.ID, "err", err)
		}
	}
	return nil
}

// seedOne reconciles one spec (N events → N rows) against the store.
func seedOne(ctx context.Context, hs hooks.HookStore, s Spec, forceDisabled bool) error {
	src, ok := source(s.SourceFile)
	if !ok {
		return fmt.Errorf("source not cached: %s", s.SourceFile)
	}

	cfgMap := map[string]any{"source": string(src)}
	metadata := map[string]any{
		"builtin":     true,
		"version":     s.Version,
		"description": s.Description,
	}

	for _, ev := range s.Events {
		id := BuiltinEventID(s.ID, ev)
		if err := reconcile(ctx, hs, id, s, ev, cfgMap, metadata, forceDisabled); err != nil {
			return fmt.Errorf("%s/%s: %w", s.ID, ev, err)
		}
	}
	return nil
}

// reconcile handles insert/update/noop for a single builtin-event row.
func reconcile(
	ctx context.Context,
	hs hooks.HookStore,
	id uuid.UUID,
	s Spec,
	event string,
	cfgMap, metadata map[string]any,
	forceDisabled bool,
) error {
	current, err := hs.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get: %w", err)
	}

	if current == nil {
		// Fresh insert: honor both the operator force-disable list AND the
		// spec's default_disabled flag. Spec-default off keeps high-impact
		// builtins (e.g. PII redactor) opt-in so users opt them in
		// deliberately rather than having mutation enabled silently.
		enabled := !forceDisabled && !s.DefaultDisabled
		cfg := hooks.HookConfig{
			ID:          id,
			TenantID:    hooks.SentinelTenantID,
			Name:        s.ID,
			Event:       hooks.HookEvent(event),
			HandlerType: hooks.HandlerScript,
			Scope:       hooks.Scope(s.Scope),
			Config:      cfgMap,
			Matcher:     s.Matcher,
			IfExpr:      s.IfExpr,
			TimeoutMS:   s.TimeoutMS,
			OnTimeout:   hooks.Decision(s.OnTimeout),
			Priority:    s.Priority,
			Enabled:     enabled,
			Source:      hooks.SourceBuiltin,
			Metadata:    metadata,
		}
		if _, err := hs.Create(ctx, cfg); err != nil {
			return fmt.Errorf("create: %w", err)
		}
		slog.Info("hooks.builtin_seeded", "id", s.ID, "event", event, "version", s.Version)
		return nil
	}

	dbVersion := metadataVersion(current.Metadata)
	switch {
	case s.Version > dbVersion:
		patch := map[string]any{
			"config":     cfgMap,
			"matcher":    s.Matcher,
			"if_expr":    s.IfExpr,
			"timeout_ms": s.TimeoutMS,
			"on_timeout": string(hooks.Decision(s.OnTimeout)),
			"priority":   s.Priority,
			"metadata":   metadata,
		}
		// One-shot: when a version bump introduces default_disabled=true on
		// a row that was previously enabled, flip it off too. Lets us roll
		// out the "input mutation is opt-in" policy retroactively without
		// stomping on a user who deliberately re-enables afterwards (the
		// next boot finds dbVersion >= s.Version, skips this branch).
		if s.DefaultDisabled && current.Enabled {
			patch["enabled"] = false
			slog.Info("hooks.builtin_default_off_applied",
				"id", s.ID, "event", event,
				"reason", "version bump introduced default_disabled=true")
		}
		if err := hs.Update(ctx, id, patch); err != nil {
			return fmt.Errorf("update: %w", err)
		}
		slog.Info("hooks.builtin_updated", "id", s.ID, "event", event,
			"from_version", dbVersion, "to_version", s.Version)
	case s.Version < dbVersion:
		slog.Warn("hooks.builtin_downgrade_detected",
			"id", s.ID, "event", event,
			"db_version", dbVersion, "embed_version", s.Version,
			"note", "DB content kept; no rollback performed")
	}

	if forceDisabled && current.Enabled {
		if err := hs.Update(ctx, id, map[string]any{"enabled": false}); err != nil {
			return fmt.Errorf("force-disable: %w", err)
		}
		slog.Info("hooks.builtin_disabled_by_config", "id", s.ID, "event", event)
	}
	return nil
}

// metadataVersion reads metadata["version"] tolerating JSONB float64 decoding.
func metadataVersion(m map[string]any) int {
	if m == nil {
		return 0
	}
	switch v := m["version"].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
