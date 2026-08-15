package hooks_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
)

// migStore is a minimal HookStore backing the command-autodisable unit tests.
// Only List + Update are exercised by the migration; Create/Delete/etc. are
// stubbed to zero values.
type migStore struct {
	rows    []hooks.HookConfig
	listErr error
}

func (m *migStore) Create(context.Context, hooks.HookConfig) (uuid.UUID, error) {
	return uuid.Nil, nil
}
func (m *migStore) GetByID(_ context.Context, id uuid.UUID) (*hooks.HookConfig, error) {
	for i := range m.rows {
		if m.rows[i].ID == id {
			c := m.rows[i]
			return &c, nil
		}
	}
	return nil, nil
}
func (m *migStore) List(_ context.Context, f hooks.ListFilter) ([]hooks.HookConfig, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := []hooks.HookConfig{}
	for _, r := range m.rows {
		if f.Enabled != nil && r.Enabled != *f.Enabled {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}
func (m *migStore) Update(_ context.Context, id uuid.UUID, updates map[string]any) error {
	for i := range m.rows {
		if m.rows[i].ID != id {
			continue
		}
		if v, ok := updates["enabled"].(bool); ok {
			m.rows[i].Enabled = v
		}
		return nil
	}
	return errors.New("row not found")
}
func (m *migStore) Delete(context.Context, uuid.UUID) error { return nil }
func (m *migStore) ResolveForEvent(context.Context, hooks.Event) ([]hooks.HookConfig, error) {
	return nil, nil
}
func (m *migStore) WriteExecution(context.Context, hooks.HookExecution) error { return nil }
func (m *migStore) SetHookAgents(context.Context, uuid.UUID, []uuid.UUID) error { return nil }
func (m *migStore) GetHookAgents(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

// fixtureRows covers every branch of the migration:
//
//	A: enabled command + ui  → should be disabled
//	B: already-disabled command + ui → skip (not in filter result)
//	C: enabled http + ui     → skip (not command)
//	D: enabled script + builtin → skip (builtin carve-out)
//	E: enabled command + builtin → skip (defensive carve-out even though
//	   Phase 04/05 never seed command builtins)
func fixtureRows() []hooks.HookConfig {
	mk := func(ht hooks.HandlerType, src string, enabled bool) hooks.HookConfig {
		return hooks.HookConfig{
			ID:          uuid.New(),
			TenantID:    uuid.New(),
			Event:       hooks.EventPreToolUse,
			HandlerType: ht,
			Scope:       hooks.ScopeTenant,
			Source:      src,
			Enabled:     enabled,
			Version:     1,
		}
	}
	return []hooks.HookConfig{
		mk(hooks.HandlerCommand, "ui", true),
		mk(hooks.HandlerCommand, "ui", false),
		mk(hooks.HandlerHTTP, "ui", true),
		mk(hooks.HandlerScript, hooks.SourceBuiltin, true),
		mk(hooks.HandlerCommand, hooks.SourceBuiltin, true),
	}
}

func TestDisableLegacyCommandHooks_StandardDisablesOnlyCommandUI(t *testing.T) {
	s := &migStore{rows: fixtureRows()}
	n, err := hooks.DisableLegacyCommandHooks(context.Background(), s, edition.Standard)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 1 {
		t.Fatalf("disabled=%d, want 1 (only the enabled+command+ui row)", n)
	}

	var cntCommandEnabled, cntCommandBuiltin, cntHTTP, cntScriptBuiltin int
	for _, r := range s.rows {
		switch {
		case r.HandlerType == hooks.HandlerCommand && r.Source == "ui" && r.Enabled:
			cntCommandEnabled++
		case r.HandlerType == hooks.HandlerCommand && r.Source == hooks.SourceBuiltin:
			if r.Enabled {
				cntCommandBuiltin++
			}
		case r.HandlerType == hooks.HandlerHTTP && r.Enabled:
			cntHTTP++
		case r.HandlerType == hooks.HandlerScript && r.Source == hooks.SourceBuiltin && r.Enabled:
			cntScriptBuiltin++
		}
	}
	if cntCommandEnabled != 0 {
		t.Errorf("command/ui rows still enabled: %d", cntCommandEnabled)
	}
	if cntCommandBuiltin != 1 {
		t.Errorf("command/builtin row should stay enabled: got %d", cntCommandBuiltin)
	}
	if cntHTTP != 1 {
		t.Errorf("http row touched: got %d enabled", cntHTTP)
	}
	if cntScriptBuiltin != 1 {
		t.Errorf("script/builtin row touched: got %d enabled", cntScriptBuiltin)
	}
}

func TestDisableLegacyCommandHooks_Idempotent(t *testing.T) {
	s := &migStore{rows: fixtureRows()}
	if _, err := hooks.DisableLegacyCommandHooks(context.Background(), s, edition.Standard); err != nil {
		t.Fatalf("first run: %v", err)
	}
	n, err := hooks.DisableLegacyCommandHooks(context.Background(), s, edition.Standard)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if n != 0 {
		t.Fatalf("second run disabled=%d, want 0 (idempotent)", n)
	}
}

func TestDisableLegacyCommandHooks_LiteNoOp(t *testing.T) {
	s := &migStore{rows: fixtureRows()}
	n, err := hooks.DisableLegacyCommandHooks(context.Background(), s, edition.Lite)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("Lite returned disabled=%d, want 0", n)
	}
	// Nothing touched.
	for _, r := range s.rows {
		if r.HandlerType == hooks.HandlerCommand && r.Source == "ui" && !r.Enabled {
			// This row started disabled; fine.
			continue
		}
		if r.HandlerType == hooks.HandlerCommand && r.Source == "ui" && !r.Enabled {
			t.Errorf("Lite disabled a command row: %+v", r)
		}
	}
}

func TestDisableLegacyCommandHooks_NilStore(t *testing.T) {
	n, err := hooks.DisableLegacyCommandHooks(context.Background(), nil, edition.Standard)
	if err != nil || n != 0 {
		t.Fatalf("nil store: n=%d err=%v", n, err)
	}
}

func TestDisableLegacyCommandHooks_ListErrorPropagates(t *testing.T) {
	s := &migStore{listErr: errors.New("db down")}
	_, err := hooks.DisableLegacyCommandHooks(context.Background(), s, edition.Standard)
	if err == nil {
		t.Fatal("want error from List propagation")
	}
}
