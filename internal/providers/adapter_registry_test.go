package providers

import (
	"errors"
	"net/http"
	"sync"
	"testing"
)

// TestAdapterRegistry_RegisterAndGet verifies basic registry store/lookup.
func TestAdapterRegistry_RegisterAndGet(t *testing.T) {
	r := NewAdapterRegistry()
	if r == nil {
		t.Fatal("NewAdapterRegistry returned nil")
	}

	// Register a fake factory returning a stub adapter.
	r.Register("stub", func(cfg ProviderConfig) (ProviderAdapter, error) {
		return &stubAdapter{name: "stub"}, nil
	})

	got, err := r.Get("stub", ProviderConfig{Name: "stub"})
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got.Name() != "stub" {
		t.Errorf("Get Name() = %q, want %q", got.Name(), "stub")
	}
}

// TestAdapterRegistry_GetUnknown verifies lookup of an unregistered name
// surfaces an error without panicking.
func TestAdapterRegistry_GetUnknown(t *testing.T) {
	r := NewAdapterRegistry()
	_, err := r.Get("missing", ProviderConfig{})
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
}

// TestAdapterRegistry_RegisterOverwrite verifies re-registering replaces the factory.
func TestAdapterRegistry_RegisterOverwrite(t *testing.T) {
	r := NewAdapterRegistry()
	r.Register("x", func(cfg ProviderConfig) (ProviderAdapter, error) {
		return &stubAdapter{name: "v1"}, nil
	})
	r.Register("x", func(cfg ProviderConfig) (ProviderAdapter, error) {
		return &stubAdapter{name: "v2"}, nil
	})
	got, _ := r.Get("x", ProviderConfig{})
	if got.Name() != "v2" {
		t.Errorf("Get Name() = %q, want v2 (latest registration wins)", got.Name())
	}
}

// TestAdapterRegistry_FactoryErrorSurfaces verifies factory errors propagate.
func TestAdapterRegistry_FactoryErrorSurfaces(t *testing.T) {
	r := NewAdapterRegistry()
	want := errors.New("factory boom")
	r.Register("boom", func(cfg ProviderConfig) (ProviderAdapter, error) {
		return nil, want
	})
	_, err := r.Get("boom", ProviderConfig{})
	if !errors.Is(err, want) {
		t.Errorf("Get err = %v, want %v", err, want)
	}
}

// TestAdapterRegistry_ConcurrentRegister verifies the registry is safe under
// concurrent read+write access (RWMutex contract).
func TestAdapterRegistry_ConcurrentRegister(t *testing.T) {
	r := NewAdapterRegistry()
	// Pre-register baseline so Get path exists during concurrent writes.
	r.Register("base", func(cfg ProviderConfig) (ProviderAdapter, error) {
		return &stubAdapter{name: "base"}, nil
	})

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(2)
		go func(id int) {
			defer wg.Done()
			r.Register("base", func(cfg ProviderConfig) (ProviderAdapter, error) {
				return &stubAdapter{name: "base"}, nil
			})
		}(i)
		go func() {
			defer wg.Done()
			_, _ = r.Get("base", ProviderConfig{})
		}()
	}
	wg.Wait()

	got, err := r.Get("base", ProviderConfig{})
	if err != nil {
		t.Fatalf("post-concurrent Get error: %v", err)
	}
	if got.Name() != "base" {
		t.Errorf("final Name() = %q, want base", got.Name())
	}
}

// TestDefaultAdapterRegistry verifies the default registry contains all
// built-in adapters and each resolves without error.
func TestDefaultAdapterRegistry(t *testing.T) {
	r := DefaultAdapterRegistry()
	if r == nil {
		t.Fatal("DefaultAdapterRegistry returned nil")
	}

	// Codex adapter doesn't need API key (token comes from TokenSource).
	cases := []struct {
		name string
		cfg  ProviderConfig
	}{
		{"anthropic", ProviderConfig{APIKey: "fake"}},
		{"openai", ProviderConfig{APIKey: "fake"}},
		{"dashscope", ProviderConfig{APIKey: "fake"}},
		{"codex", ProviderConfig{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := r.Get(tc.name, tc.cfg)
			if err != nil {
				t.Fatalf("Get(%q) returned error: %v", tc.name, err)
			}
			if a == nil {
				t.Fatalf("Get(%q) returned nil adapter", tc.name)
			}
			if a.Name() == "" {
				t.Errorf("Get(%q) Name() is empty", tc.name)
			}
		})
	}
}

// stubAdapter is a minimal ProviderAdapter used by registry tests.
type stubAdapter struct {
	name string
}

func (s *stubAdapter) Name() string                       { return s.name }
func (s *stubAdapter) Capabilities() ProviderCapabilities { return ProviderCapabilities{} }
func (s *stubAdapter) ToRequest(ChatRequest) ([]byte, http.Header, error) {
	return nil, nil, nil
}
func (s *stubAdapter) FromResponse([]byte) (*ChatResponse, error)   { return nil, nil }
func (s *stubAdapter) FromStreamChunk([]byte) (*StreamChunk, error) { return nil, nil }
