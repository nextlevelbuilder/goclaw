package audio

import (
	"context"
	"testing"
)

// fakeDescribableProvider is a TTSProvider that also implements DescribableProvider.
type fakeDescribableProvider struct {
	name string
	caps ProviderCapabilities
}

func (f *fakeDescribableProvider) Name() string { return f.name }
func (f *fakeDescribableProvider) Synthesize(_ context.Context, _ string, _ TTSOptions) (*SynthResult, error) {
	return nil, nil
}
func (f *fakeDescribableProvider) Capabilities() ProviderCapabilities { return f.caps }

// fakePlainProvider is a TTSProvider only (no DescribableProvider).
type fakePlainProvider struct{ name string }

func (f *fakePlainProvider) Name() string { return f.name }
func (f *fakePlainProvider) Synthesize(_ context.Context, _ string, _ TTSOptions) (*SynthResult, error) {
	return nil, nil
}

// TestListCapabilities_AggregatesProviders verifies that ListCapabilities returns
// one entry per registered provider: describable ones return full schema, plain
// ones return a minimal stub.
func TestListCapabilities_AggregatesProviders(t *testing.T) {
	mgr := NewManager(ManagerConfig{Primary: "describable"})

	describable := &fakeDescribableProvider{
		name: "describable",
		caps: ProviderCapabilities{
			Provider:       "describable",
			DisplayName:    "Describable TTS",
			RequiresAPIKey: true,
			Models:         []string{"model-a"},
		},
	}
	plain := &fakePlainProvider{name: "plain"}

	mgr.RegisterTTS(describable)
	mgr.RegisterTTS(plain)

	caps := mgr.ListCapabilities()
	if len(caps) != 2 {
		t.Fatalf("ListCapabilities: got %d entries, want 2", len(caps))
	}

	// Build lookup map for order-independent checks.
	byName := make(map[string]ProviderCapabilities)
	for _, c := range caps {
		byName[c.Provider] = c
	}

	// Describable entry — should have full schema.
	dc, ok := byName["describable"]
	if !ok {
		t.Fatal("missing 'describable' entry")
	}
	if dc.DisplayName != "Describable TTS" {
		t.Errorf("DisplayName: got %q want %q", dc.DisplayName, "Describable TTS")
	}
	if !dc.RequiresAPIKey {
		t.Error("RequiresAPIKey: expected true")
	}
	if len(dc.Models) != 1 || dc.Models[0] != "model-a" {
		t.Errorf("Models: got %v", dc.Models)
	}

	// Plain entry — stub: Provider+DisplayName set, everything else zero.
	pc, ok := byName["plain"]
	if !ok {
		t.Fatal("missing 'plain' entry")
	}
	if pc.Provider != "plain" {
		t.Errorf("stub Provider: got %q want %q", pc.Provider, "plain")
	}
	if pc.DisplayName != "plain" {
		t.Errorf("stub DisplayName: got %q want %q", pc.DisplayName, "plain")
	}
	if pc.RequiresAPIKey {
		t.Error("stub RequiresAPIKey: expected false")
	}
}

// TestListCapabilities_EmptyManager verifies that ListCapabilities returns an empty
// non-nil slice when no providers are registered.
func TestListCapabilities_EmptyManager(t *testing.T) {
	mgr := NewManager(ManagerConfig{})
	caps := mgr.ListCapabilities()
	if caps == nil {
		t.Error("ListCapabilities on empty manager: got nil, want empty slice")
	}
	if len(caps) != 0 {
		t.Errorf("ListCapabilities on empty manager: got %d entries, want 0", len(caps))
	}
}
