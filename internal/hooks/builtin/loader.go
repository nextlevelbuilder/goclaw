// Package builtin discovers, parses, and seeds builtin hook rows.
// Builtins are shipped with the binary via //go:embed and are the only hooks
// allowed to mutate event input (source-tier gate in dispatcher).
//
// Lifecycle: cmd/gateway_managed.go calls Load() once at startup (before
// dispatcher wiring), then Seed(ctx, hookStore, cfg.Hooks) after migrations.
// Seed is idempotent via stable UUIDv5 IDs derived from the builtin's kebab
// name; safe to run on every boot.
package builtin

import (
	"embed"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// fs carries the registry YAML and one .js per builtin. A `//go:embed *.js`
// directive errors at build time if zero .js files exist, so Phase 04 ships a
// tiny `_placeholder.js` alongside an empty `builtins:` list. Phase 05 adds
// pii-redactor.js and removes the placeholder.
//
//go:embed *.yaml *.js
var fs embed.FS

// BuiltinNamespace is the stable UUIDv5 namespace for all builtin hook IDs.
// Precomputed via uuid.NewSHA1(uuid.NameSpaceDNS, []byte("goclaw.hooks.builtin"))
// so every GoClaw build produces the same hash and the seeded rows persist
// across restarts / migrations.
var BuiltinNamespace = uuid.MustParse("082ab084-a25f-52b4-a4a4-eb8a816bd9a8")

// BuiltinID returns the stable UUIDv5 derived from a builtin's kebab name.
// Used by Seed + dispatcher AllowlistFor lookups.
func BuiltinID(name string) uuid.UUID {
	return uuid.NewSHA1(BuiltinNamespace, []byte(name))
}

// BuiltinEventID returns the stable UUIDv5 for a single (builtin, event)
// pair. Each YAML spec lists N events, each materialized as one DB row.
func BuiltinEventID(name, event string) uuid.UUID {
	return uuid.NewSHA1(BuiltinNamespace, []byte(name+"/"+event))
}

// Spec mirrors one entry in builtins.yaml. All fields optional except id,
// events, source_file; loader validates at parse time.
//
// DefaultDisabled flips the initial enabled value for fresh installs (true →
// row created with enabled=false). Reverse-named so the zero value preserves
// the old "default on" behaviour for any builtin that omits the field. Only
// affects INSERT — existing DB rows retain their user-set enabled toggle
// across version bumps.
type Spec struct {
	ID              string   `yaml:"id"`
	Version         int      `yaml:"version"`
	Events          []string `yaml:"events"`
	Scope           string   `yaml:"scope"`
	Matcher         string   `yaml:"matcher"`
	IfExpr          string   `yaml:"if_expr"`
	TimeoutMS       int      `yaml:"timeout_ms"`
	OnTimeout       string   `yaml:"on_timeout"`
	Priority        int      `yaml:"priority"`
	MutableFields   []string `yaml:"mutable_fields"`
	SourceFile      string   `yaml:"source_file"`
	Description     string   `yaml:"description"`
	DefaultDisabled bool     `yaml:"default_disabled"`
}

type catalog struct {
	Builtins []Spec `yaml:"builtins"`
}

// Registry holds the parsed view of builtins.yaml plus fast allowlist lookup.
// Populated once at Load(); read concurrently afterwards under regMu.
var (
	regMu       sync.RWMutex
	specs       []Spec               // ordered list for deterministic seeding
	eventIDSpec map[uuid.UUID]*Spec  // per-event UUID → owning Spec
	sourceCache map[string][]byte    // source_file → file bytes
)

// Load parses builtins.yaml and caches the registry in memory. Call once at
// startup before Seed/AllowlistFor. Idempotent — subsequent calls replace the
// cache so tests can reload after fixture edits.
func Load() error {
	raw, err := fs.ReadFile("builtins.yaml")
	if err != nil {
		return fmt.Errorf("builtin: read yaml: %w", err)
	}
	var cat catalog
	if err := yaml.Unmarshal(raw, &cat); err != nil {
		return fmt.Errorf("builtin: parse yaml: %w", err)
	}

	idx := make(map[uuid.UUID]*Spec, len(cat.Builtins)*2)
	srcs := make(map[string][]byte, len(cat.Builtins))
	for i := range cat.Builtins {
		s := &cat.Builtins[i]
		if s.ID == "" || s.SourceFile == "" || len(s.Events) == 0 {
			return fmt.Errorf("builtin: incomplete spec: %+v", *s)
		}
		src, err := fs.ReadFile(s.SourceFile)
		if err != nil {
			return fmt.Errorf("builtin: missing source file %q: %w", s.SourceFile, err)
		}
		srcs[s.SourceFile] = src
		for _, ev := range s.Events {
			idx[BuiltinEventID(s.ID, ev)] = s
		}
	}

	regMu.Lock()
	specs = cat.Builtins
	eventIDSpec = idx
	sourceCache = srcs
	regMu.Unlock()
	return nil
}

// RegisteredSpecs returns a copy of the loaded specs for testing / callers
// that need to iterate independently of Load()'s internal state.
func RegisteredSpecs() []Spec {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Spec, len(specs))
	copy(out, specs)
	return out
}

// source looks up the embedded JS bytes for a SourceFile key.
func source(file string) ([]byte, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	b, ok := sourceCache[file]
	return b, ok
}

// Source returns the raw bytes of an embedded builtin source file without
// going through Load()/the in-memory cache. Primarily for handler tests that
// want to exercise the real embedded JS in isolation. Returns the usual
// fs.ErrNotExist when the file is missing.
func Source(name string) ([]byte, error) {
	return fs.ReadFile(name)
}
