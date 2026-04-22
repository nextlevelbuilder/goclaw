package audio

import (
	"testing"
)

func TestSetNested_FlatKey(t *testing.T) {
	m := map[string]any{}
	SetNested(m, "speed", 1.2)
	v, ok := m["speed"]
	if !ok {
		t.Fatal("expected key 'speed' to be set")
	}
	if v != 1.2 {
		t.Errorf("got %v, want 1.2", v)
	}
}

func TestSetNested_TwoLevel(t *testing.T) {
	m := map[string]any{}
	SetNested(m, "voice_settings.stability", 0.5)
	vs, ok := m["voice_settings"].(map[string]any)
	if !ok {
		t.Fatalf("voice_settings not a map: %T", m["voice_settings"])
	}
	if vs["stability"] != 0.5 {
		t.Errorf("stability: got %v, want 0.5", vs["stability"])
	}
}

func TestSetNested_MergesExisting(t *testing.T) {
	m := map[string]any{
		"voice_settings": map[string]any{"speed": 1.0},
	}
	SetNested(m, "voice_settings.stability", 0.5)
	vs := m["voice_settings"].(map[string]any)
	if vs["speed"] != 1.0 {
		t.Errorf("speed: got %v, want 1.0 (must not be overwritten)", vs["speed"])
	}
	if vs["stability"] != 0.5 {
		t.Errorf("stability: got %v, want 0.5", vs["stability"])
	}
}

func TestSetNested_OverridesScalar(t *testing.T) {
	m := map[string]any{}
	SetNested(m, "voice_settings.stability", 0.5)
	SetNested(m, "voice_settings.stability", 0.7)
	vs := m["voice_settings"].(map[string]any)
	if vs["stability"] != 0.7 {
		t.Errorf("stability: got %v, want 0.7 (override)", vs["stability"])
	}
}

func TestSetNested_ReplacesNonMapIntermediate(t *testing.T) {
	// If intermediate segment holds a scalar, it must be replaced with a map.
	m := map[string]any{"voice_settings": "not-a-map"}
	SetNested(m, "voice_settings.stability", 0.5)
	vs, ok := m["voice_settings"].(map[string]any)
	if !ok {
		t.Fatalf("voice_settings should have been replaced with a map, got %T", m["voice_settings"])
	}
	if vs["stability"] != 0.5 {
		t.Errorf("stability: got %v, want 0.5", vs["stability"])
	}
}

func TestSetNested_ThreeLevel(t *testing.T) {
	m := map[string]any{}
	SetNested(m, "a.b.c", 42)
	ab := m["a"].(map[string]any)
	abc := ab["b"].(map[string]any)
	if abc["c"] != 42 {
		t.Errorf("a.b.c: got %v, want 42", abc["c"])
	}
}

func TestGetNested_FlatKey(t *testing.T) {
	m := map[string]any{"speed": 1.5}
	v, ok := GetNested(m, "speed")
	if !ok || v != 1.5 {
		t.Errorf("GetNested flat: got (%v,%v), want (1.5,true)", v, ok)
	}
}

func TestGetNested_TwoLevel(t *testing.T) {
	m := map[string]any{
		"voice_settings": map[string]any{"stability": 0.5},
	}
	v, ok := GetNested(m, "voice_settings.stability")
	if !ok || v != 0.5 {
		t.Errorf("GetNested two-level: got (%v,%v), want (0.5,true)", v, ok)
	}
}

func TestGetNested_MissingKey(t *testing.T) {
	m := map[string]any{}
	_, ok := GetNested(m, "missing")
	if ok {
		t.Error("expected false for missing key")
	}
}

func TestGetNested_MissingIntermediate(t *testing.T) {
	m := map[string]any{}
	_, ok := GetNested(m, "a.b.c")
	if ok {
		t.Error("expected false when intermediate key absent")
	}
}

func TestGetNested_NonMapIntermediate(t *testing.T) {
	m := map[string]any{"a": "scalar"}
	_, ok := GetNested(m, "a.b")
	if ok {
		t.Error("expected false when intermediate is scalar")
	}
}

func TestSetNested_EmptyKeyIgnored(t *testing.T) {
	m := map[string]any{}
	SetNested(m, "", "value") // must not panic
	if len(m) != 0 {
		t.Error("empty key should leave map unchanged")
	}
}
