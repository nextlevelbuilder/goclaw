package builtin

import (
	"testing"

	"github.com/google/uuid"
)

// TestLoad_ShippedRegistry verifies the baked-in builtins parse clean at
// build time. Phase 05 ships pii-redactor (2 events), so we assert the
// registry contains at least that spec and that its embedded source file
// loaded successfully.
func TestLoad_ShippedRegistry(t *testing.T) {
	if err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	specs := RegisteredSpecs()
	var pii *Spec
	for i := range specs {
		if specs[i].ID == "pii-redactor" {
			pii = &specs[i]
			break
		}
	}
	if pii == nil {
		t.Fatalf("pii-redactor not in registry; have %v", specIDs(specs))
	}
	if pii.SourceFile != "pii-redactor.js" {
		t.Errorf("pii-redactor source_file=%q, want pii-redactor.js", pii.SourceFile)
	}
	if len(pii.Events) != 2 {
		t.Errorf("pii-redactor events=%v, want 2", pii.Events)
	}
	if src, ok := source("pii-redactor.js"); !ok || len(src) == 0 {
		t.Error("pii-redactor.js bytes not cached after Load")
	}
}

func specIDs(ss []Spec) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.ID)
	}
	return out
}

// TestBuiltinNamespace_Stable guards against a silent namespace change that
// would orphan every already-seeded row at the next boot.
func TestBuiltinNamespace_Stable(t *testing.T) {
	want := uuid.MustParse("082ab084-a25f-52b4-a4a4-eb8a816bd9a8")
	if BuiltinNamespace != want {
		t.Fatalf("namespace drifted: have %s, want %s", BuiltinNamespace, want)
	}
}

// TestBuiltinID_Deterministic locks in the per-id hash so that a renamed id
// gets caught in review — downgrade-safety depends on stable IDs.
func TestBuiltinID_Deterministic(t *testing.T) {
	a := BuiltinID("pii-redactor")
	b := BuiltinID("pii-redactor")
	if a != b {
		t.Fatalf("BuiltinID non-deterministic: %s vs %s", a, b)
	}
	c := BuiltinID("sql-guard")
	if a == c {
		t.Fatal("distinct ids produced equal UUID — namespace collision")
	}
}

// TestBuiltinEventID_Distinct ensures each event of a multi-event spec gets
// its own row id so per-event enabled toggles don't clobber siblings.
func TestBuiltinEventID_Distinct(t *testing.T) {
	a := BuiltinEventID("pii-redactor", "user_prompt_submit")
	b := BuiltinEventID("pii-redactor", "pre_tool_use")
	if a == b {
		t.Fatal("event UUIDs collided for different events")
	}
	if a == BuiltinID("pii-redactor") {
		t.Fatal("per-event UUID must differ from bare spec id")
	}
}

// TestAllowlistFor_Unknown returns nil for ids that aren't registered.
// Dispatcher treats nil as strip → defense-in-depth against scripts with
// forged source='builtin' on a random UUID.
func TestAllowlistFor_Unknown(t *testing.T) {
	if err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := AllowlistFor(uuid.New()); got != nil {
		t.Errorf("unknown id → allowlist=%v, want nil", got)
	}
}

func TestIsBuiltin_Unknown(t *testing.T) {
	if err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if IsBuiltin(uuid.New()) {
		t.Error("random UUID reported as builtin")
	}
}

// TestMetadataVersion covers the JSONB float64 decode path plus int fast path.
func TestMetadataVersion(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]any
		want int
	}{
		{"nil", nil, 0},
		{"missing", map[string]any{}, 0},
		{"int", map[string]any{"version": 3}, 3},
		{"int64", map[string]any{"version": int64(4)}, 4},
		{"float64", map[string]any{"version": float64(5)}, 5},
		{"string-ignored", map[string]any{"version": "oops"}, 0},
	}
	for _, tc := range cases {
		if got := metadataVersion(tc.meta); got != tc.want {
			t.Errorf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}
}
