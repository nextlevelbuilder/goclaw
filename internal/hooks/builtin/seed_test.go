package builtin

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
)

// fakeHookStore is an in-memory HookStore just rich enough to test seed
// reconciliation. Implements only the methods the seeder touches (Create,
// GetByID, Update); others return zero values / nil.
type fakeHookStore struct {
	rows map[uuid.UUID]*hooks.HookConfig
}

func newFakeHookStore() *fakeHookStore {
	return &fakeHookStore{rows: map[uuid.UUID]*hooks.HookConfig{}}
}

func (f *fakeHookStore) Create(_ context.Context, cfg hooks.HookConfig) (uuid.UUID, error) {
	if cfg.ID == uuid.Nil {
		cfg.ID = uuid.Must(uuid.NewV7())
	}
	if _, exists := f.rows[cfg.ID]; exists {
		// Mimic DB primary-key conflict to catch non-idempotent seeds.
		return uuid.Nil, errDuplicate
	}
	c := cfg
	f.rows[cfg.ID] = &c
	return cfg.ID, nil
}

func (f *fakeHookStore) GetByID(_ context.Context, id uuid.UUID) (*hooks.HookConfig, error) {
	if r, ok := f.rows[id]; ok {
		c := *r
		return &c, nil
	}
	return nil, nil
}

func (f *fakeHookStore) List(context.Context, hooks.ListFilter) ([]hooks.HookConfig, error) {
	return nil, nil
}

func (f *fakeHookStore) Update(_ context.Context, id uuid.UUID, updates map[string]any) error {
	r, ok := f.rows[id]
	if !ok {
		return errNotFound
	}
	for k, v := range updates {
		switch k {
		case "enabled":
			if b, ok := v.(bool); ok {
				r.Enabled = b
			}
		case "metadata":
			if m, ok := v.(map[string]any); ok {
				r.Metadata = m
			}
		case "config":
			if m, ok := v.(map[string]any); ok {
				r.Config = m
			}
		case "matcher":
			if s, ok := v.(string); ok {
				r.Matcher = s
			}
		case "if_expr":
			if s, ok := v.(string); ok {
				r.IfExpr = s
			}
		case "priority":
			if n, ok := v.(int); ok {
				r.Priority = n
			}
		case "timeout_ms":
			if n, ok := v.(int); ok {
				r.TimeoutMS = n
			}
		}
	}
	r.Version++
	return nil
}

func (f *fakeHookStore) Delete(_ context.Context, id uuid.UUID) error {
	delete(f.rows, id)
	return nil
}

func (f *fakeHookStore) ResolveForEvent(context.Context, hooks.Event) ([]hooks.HookConfig, error) {
	return nil, nil
}

func (f *fakeHookStore) WriteExecution(context.Context, hooks.HookExecution) error { return nil }
func (f *fakeHookStore) SetHookAgents(context.Context, uuid.UUID, []uuid.UUID) error { return nil }
func (f *fakeHookStore) GetHookAgents(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

type errStr string

func (e errStr) Error() string { return string(e) }

const (
	errDuplicate errStr = "fake: duplicate key"
	errNotFound  errStr = "fake: not found"
)

// withTestRegistry installs a one-spec registry for the test and restores the
// prior state on cleanup. Serial access only — tests must not run in parallel.
func withTestRegistry(t *testing.T, s Spec, src []byte) {
	t.Helper()
	regMu.Lock()
	prevSpecs := specs
	prevIdx := eventIDSpec
	prevSrc := sourceCache
	specs = []Spec{s}
	eventIDSpec = map[uuid.UUID]*Spec{}
	for _, ev := range s.Events {
		eventIDSpec[BuiltinEventID(s.ID, ev)] = &specs[0]
	}
	sourceCache = map[string][]byte{s.SourceFile: src}
	regMu.Unlock()
	t.Cleanup(func() {
		regMu.Lock()
		specs = prevSpecs
		eventIDSpec = prevIdx
		sourceCache = prevSrc
		regMu.Unlock()
	})
}

func fixtureSpec(version int) Spec {
	return Spec{
		ID:            "test-redactor",
		Version:       version,
		Events:        []string{"user_prompt_submit", "pre_tool_use"},
		Scope:         "global",
		TimeoutMS:     500,
		OnTimeout:     "allow",
		Priority:      900,
		MutableFields: []string{"rawInput"},
		SourceFile:    "test-redactor.js",
		Description:   "test",
	}
}

// TestSeed_InsertsOneRowPerEvent verifies seed creates N rows for a spec with
// N events using stable UUIDv5s — this is the H9 idempotency check.
func TestSeed_InsertsOneRowPerEvent(t *testing.T) {
	spec := fixtureSpec(1)
	withTestRegistry(t, spec, []byte("// v1"))

	fs := newFakeHookStore()
	if err := Seed(context.Background(), fs, config.HooksConfig{}); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if got := len(fs.rows); got != len(spec.Events) {
		t.Fatalf("rows=%d, want %d", got, len(spec.Events))
	}
	for _, ev := range spec.Events {
		id := BuiltinEventID(spec.ID, ev)
		row, ok := fs.rows[id]
		if !ok {
			t.Fatalf("missing row for event %q", ev)
		}
		if row.Source != hooks.SourceBuiltin {
			t.Errorf("source=%q, want builtin", row.Source)
		}
		if !row.Enabled {
			t.Error("row disabled; expected enabled=true on fresh seed")
		}
	}
}

// TestSeed_Idempotent validates H9 — re-running seed against the same registry
// creates zero additional rows. A regression here would manifest as duplicate
// rows every boot once the unique index is dropped by Phase 03.
func TestSeed_Idempotent(t *testing.T) {
	spec := fixtureSpec(1)
	withTestRegistry(t, spec, []byte("// v1"))

	fs := newFakeHookStore()
	for i := range 3 {
		if err := Seed(context.Background(), fs, config.HooksConfig{}); err != nil {
			t.Fatalf("Seed iter=%d: %v", i, err)
		}
	}
	if got := len(fs.rows); got != len(spec.Events) {
		t.Fatalf("after 3 seeds rows=%d, want %d (duplicates!)", got, len(spec.Events))
	}
}

// TestSeed_VersionBump updates existing rows when the embed version grows.
// Preserves the user's enabled toggle.
func TestSeed_VersionBump(t *testing.T) {
	spec := fixtureSpec(1)
	withTestRegistry(t, spec, []byte("// v1"))
	fs := newFakeHookStore()
	if err := Seed(context.Background(), fs, config.HooksConfig{}); err != nil {
		t.Fatal(err)
	}
	// Simulate user disabling a row.
	id := BuiltinEventID(spec.ID, "pre_tool_use")
	fs.rows[id].Enabled = false

	// Replace registry with v2.
	withTestRegistry(t, fixtureSpec(2), []byte("// v2"))
	if err := Seed(context.Background(), fs, config.HooksConfig{}); err != nil {
		t.Fatal(err)
	}
	if ver := fs.rows[id].Metadata["version"]; ver != 2 {
		t.Errorf("version not bumped: %v", ver)
	}
	if fs.rows[id].Enabled {
		t.Error("seed overwrote user's disabled toggle")
	}
}

// TestSeed_DowngradeDetected keeps the DB row intact when the embed version
// is older than the DB version (rollback scenario).
func TestSeed_DowngradeDetected(t *testing.T) {
	withTestRegistry(t, fixtureSpec(3), []byte("// v3"))
	fs := newFakeHookStore()
	if err := Seed(context.Background(), fs, config.HooksConfig{}); err != nil {
		t.Fatal(err)
	}
	id := BuiltinEventID("test-redactor", "user_prompt_submit")
	gotV := fs.rows[id].Metadata["version"]

	// Now simulate rollback to v2 embed.
	withTestRegistry(t, fixtureSpec(2), []byte("// v2"))
	if err := Seed(context.Background(), fs, config.HooksConfig{}); err != nil {
		t.Fatal(err)
	}
	if gotV2 := fs.rows[id].Metadata["version"]; gotV2 != gotV {
		t.Errorf("downgrade mutated row: %v → %v", gotV, gotV2)
	}
}

// TestSeed_DefaultDisabledOnFreshInsert verifies a spec carrying
// default_disabled=true creates rows with enabled=false on first insert.
func TestSeed_DefaultDisabledOnFreshInsert(t *testing.T) {
	spec := fixtureSpec(1)
	spec.DefaultDisabled = true
	withTestRegistry(t, spec, []byte("// v1"))

	fs := newFakeHookStore()
	if err := Seed(context.Background(), fs, config.HooksConfig{}); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	for id, row := range fs.rows {
		if row.Enabled {
			t.Errorf("row %s enabled despite default_disabled spec", id)
		}
	}
}

// TestSeed_DefaultDisabledFlipsOnVersionBump ensures the one-shot retroactive
// disable kicks in when a version bump introduces the policy on rows that
// were previously enabled. Subsequent boots (no version bump) leave the
// user's manual re-enable alone.
func TestSeed_DefaultDisabledFlipsOnVersionBump(t *testing.T) {
	// Boot 1: spec v1, default_disabled NOT set → row created enabled.
	withTestRegistry(t, fixtureSpec(1), []byte("// v1"))
	fs := newFakeHookStore()
	if err := Seed(context.Background(), fs, config.HooksConfig{}); err != nil {
		t.Fatal(err)
	}
	for _, row := range fs.rows {
		if !row.Enabled {
			t.Fatal("baseline: row should be enabled before policy change")
		}
	}

	// Boot 2: spec v2 with default_disabled=true → flips existing enabled rows.
	v2 := fixtureSpec(2)
	v2.DefaultDisabled = true
	withTestRegistry(t, v2, []byte("// v2"))
	if err := Seed(context.Background(), fs, config.HooksConfig{}); err != nil {
		t.Fatal(err)
	}
	for id, row := range fs.rows {
		if row.Enabled {
			t.Errorf("row %s still enabled after policy version bump", id)
		}
	}

	// User re-enables one row through the UI.
	for id := range fs.rows {
		fs.rows[id].Enabled = true
		break
	}

	// Boot 3: same spec v2 (no version bump) → user's choice is preserved.
	if err := Seed(context.Background(), fs, config.HooksConfig{}); err != nil {
		t.Fatal(err)
	}
	enabledAfter := 0
	for _, row := range fs.rows {
		if row.Enabled {
			enabledAfter++
		}
	}
	if enabledAfter != 1 {
		t.Errorf("user re-enable lost: enabled=%d, want 1", enabledAfter)
	}
}

// TestSeed_BuiltinDisableForcesOff applies the operator escape-hatch list.
func TestSeed_BuiltinDisableForcesOff(t *testing.T) {
	withTestRegistry(t, fixtureSpec(1), []byte("// v1"))
	fs := newFakeHookStore()
	cfg := config.HooksConfig{BuiltinDisable: []string{"test-redactor"}}
	if err := Seed(context.Background(), fs, cfg); err != nil {
		t.Fatal(err)
	}
	for id, row := range fs.rows {
		if row.Enabled {
			t.Errorf("row %s enabled despite builtin_disable", id)
		}
	}
}
