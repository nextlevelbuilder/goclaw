package decisionmodel

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/security"
)

// Per-tenant config keys. Non-secret keys live in system_configs; the API key
// lives ONLY in config_secrets (AES-GCM). KeyPrefix is reserved: the generic
// system-configs endpoint refuses it so every write goes through validation.
const (
	KeyPrefix        = "decision_model."
	KeyEnabled       = KeyPrefix + "enabled"
	KeyEndpoint      = KeyPrefix + "endpoint"
	KeyModel         = KeyPrefix + "model"
	KeyTimeoutMS     = KeyPrefix + "timeout_ms"
	KeyMaxStateChars = KeyPrefix + "max_state_chars"
	KeyAPIKey        = KeyPrefix + "api_key" // config_secrets only

	MinTimeoutMS, MaxTimeoutMS         = 100, 5000
	MinMaxStateChars, MaxMaxStateChars = 200, 20000
)

// Getter is satisfied by store.SystemConfigStore and store.ConfigSecretsStore.
// Keeping it narrow keeps this package a leaf.
type Getter interface {
	Get(ctx context.Context, key string) (string, error)
}

func get(ctx context.Context, g Getter, key string) string {
	if g == nil {
		return ""
	}
	v, err := g.Get(ctx, key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

// LoadConfig reads the tenant's non-secret settings (tenant from ctx), filling
// defaults for absent or out-of-range values. APIKey is left empty.
func LoadConfig(ctx context.Context, configs Getter) Config {
	cfg := Config{
		Endpoint:      get(ctx, configs, KeyEndpoint),
		Model:         get(ctx, configs, KeyModel),
		Timeout:       DefaultTimeout,
		MaxStateChars: DefaultMaxStateChars,
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultEndpoint
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if n, err := strconv.Atoi(get(ctx, configs, KeyTimeoutMS)); err == nil && n >= MinTimeoutMS && n <= MaxTimeoutMS {
		cfg.Timeout = time.Duration(n) * time.Millisecond
	}
	if n, err := strconv.Atoi(get(ctx, configs, KeyMaxStateChars)); err == nil && n >= MinMaxStateChars && n <= MaxMaxStateChars {
		cfg.MaxStateChars = n
	}
	return cfg
}

// ValidateEndpoint is the config-write check: format (https, no credentials)
// plus SSRF resolution. Decide re-checks per call, so this is defense in depth
// that gives the operator an immediate error instead of a silent no-op.
func ValidateEndpoint(raw string) error {
	if _, err := validateEndpoint(raw); err != nil {
		return err
	}
	if _, _, err := security.Validate(raw); err != nil {
		return errors.New("decision model endpoint resolves to a blocked or unreachable address")
	}
	return nil
}

// AllowHTTPForTest lets other packages' tests point a client at an httptest
// server. Production code MUST never call it.
func AllowHTTPForTest(allow bool) { requireHTTPS = !allow }

// Resolver builds per-tenant Deciders on demand.
type Resolver struct {
	configs Getter
	secrets Getter
	// ponytail: entries are never evicted; one per distinct config an admin
	// has ever saved. Bounded by edits, not traffic. Add LRU if that changes.
	cache sync.Map // fingerprint -> Decider
}

// NewResolver returns nil when either store is missing, which every caller
// treats as "decision model unavailable".
func NewResolver(configs, secrets Getter) *Resolver {
	if configs == nil || secrets == nil {
		return nil
	}
	return &Resolver{configs: configs, secrets: secrets}
}

// Resolve returns the Decider for the tenant in ctx, or nil when not enabled.
// Enablement is a two-key gate: KeyEnabled must be exactly "true" AND an API
// key must be stored. Configuring an endpoint never starts egress by itself.
// Safe on a nil receiver.
func (r *Resolver) Resolve(ctx context.Context) Decider {
	if r == nil || get(ctx, r.configs, KeyEnabled) != "true" {
		return nil
	}
	apiKey := get(ctx, r.secrets, KeyAPIKey)
	if apiKey == "" {
		return nil
	}
	cfg := LoadConfig(ctx, r.configs)
	cfg.APIKey = apiKey
	// Reuse the client (and its pooled TLS connections) while config is unchanged.
	fp := strings.Join([]string{cfg.Endpoint, cfg.Model, cfg.Timeout.String(), strconv.Itoa(cfg.MaxStateChars), apiKey}, "\x00")
	if d, ok := r.cache.Load(fp); ok {
		return d.(Decider)
	}
	d, err := NewJev(cfg)
	if err != nil {
		return nil
	}
	actual, _ := r.cache.LoadOrStore(fp, d)
	return actual.(Decider)
}
