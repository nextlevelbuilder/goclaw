package decisionmodel

import (
	"context"
	"errors"
	"testing"
	"time"
)

type mapGetter map[string]string

func (m mapGetter) Get(_ context.Context, key string) (string, error) {
	v, ok := m[key]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

func TestResolveRequiresBothKeys(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		configs mapGetter
		secrets mapGetter
		want    bool
	}{
		{"enabled, no key", mapGetter{KeyEnabled: "true"}, mapGetter{}, false},
		{"key, not enabled", mapGetter{}, mapGetter{KeyAPIKey: testKey}, false},
		{"key, enabled=false", mapGetter{KeyEnabled: "false"}, mapGetter{KeyAPIKey: testKey}, false},
		// Only exactly "true" enables; anything truthy-looking is not consent.
		{"key, enabled=1", mapGetter{KeyEnabled: "1"}, mapGetter{KeyAPIKey: testKey}, false},
		{"endpoint alone", mapGetter{KeyEndpoint: DefaultEndpoint}, mapGetter{}, false},
		{"both", mapGetter{KeyEnabled: "true"}, mapGetter{KeyAPIKey: testKey}, true},
	}
	for _, tc := range cases {
		got := NewResolver(tc.configs, tc.secrets).Resolve(ctx) != nil
		if got != tc.want {
			t.Errorf("%s: enabled = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestResolveNilSafe(t *testing.T) {
	var r *Resolver
	if r.Resolve(context.Background()) != nil {
		t.Fatal("nil resolver must resolve to nil")
	}
	if NewResolver(nil, mapGetter{}) != nil {
		t.Fatal("missing store must yield nil resolver")
	}
}

func TestResolveReusesClientUntilConfigChanges(t *testing.T) {
	ctx := context.Background()
	configs := mapGetter{KeyEnabled: "true"}
	secrets := mapGetter{KeyAPIKey: testKey}
	r := NewResolver(configs, secrets)
	a, b := r.Resolve(ctx), r.Resolve(ctx)
	if a != b {
		t.Fatal("unchanged config must reuse the client")
	}
	secrets[KeyAPIKey] = "rotated-key"
	if r.Resolve(ctx) == a {
		t.Fatal("key rotation must build a new client")
	}
}

func TestLoadConfigClampsOutOfRange(t *testing.T) {
	cfg := LoadConfig(context.Background(), mapGetter{
		KeyTimeoutMS:     "50",    // below min
		KeyMaxStateChars: "99999", // above max
	})
	if cfg.Timeout != DefaultTimeout || cfg.MaxStateChars != DefaultMaxStateChars {
		t.Fatalf("out-of-range values must fall back to defaults, got %+v", cfg)
	}
	cfg = LoadConfig(context.Background(), mapGetter{KeyTimeoutMS: "2500", KeyMaxStateChars: "800", KeyModel: "jev-latest"})
	if cfg.Timeout != 2500*time.Millisecond || cfg.MaxStateChars != 800 || cfg.Model != "jev-latest" || cfg.Endpoint != DefaultEndpoint {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestValidateEndpointRejectsPrivate(t *testing.T) {
	for _, ep := range []string{"https://127.0.0.1/x", "https://169.254.169.254/", "https://10.1.2.3/", "http://api.typesafe.ai/v1/systemone"} {
		if ValidateEndpoint(ep) == nil {
			t.Errorf("ValidateEndpoint(%q) accepted", ep)
		}
	}
}
