package tools

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeSearchProvider implements SearchProvider for chain-resolution tests.
type fakeSearchProvider struct{ name string }

func (f *fakeSearchProvider) Name() string { return f.name }
func (f *fakeSearchProvider) Search(_ context.Context, _ searchParams) ([]searchResult, error) {
	return nil, nil
}

// chainNames extracts provider names in order for terse assertions.
func chainNames(chain []SearchProvider) []string {
	out := make([]string, 0, len(chain))
	for _, p := range chain {
		out = append(out, p.Name())
	}
	return out
}

// fakeSecretsStore implements store.ConfigSecretsStore for unit tests.
type fakeSecretsStore struct {
	data map[uuid.UUID]map[string]string // tenantID → key → value
}

func newFakeSecretsStore() *fakeSecretsStore {
	return &fakeSecretsStore{data: make(map[uuid.UUID]map[string]string)}
}

func (f *fakeSecretsStore) Get(ctx context.Context, key string) (string, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		tid = store.MasterTenantID
	}
	m, ok := f.data[tid]
	if !ok {
		return "", sql.ErrNoRows
	}
	v, ok := m[key]
	if !ok {
		return "", sql.ErrNoRows
	}
	return v, nil
}

func (f *fakeSecretsStore) Set(ctx context.Context, key, value string) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		tid = store.MasterTenantID
	}
	if _, ok := f.data[tid]; !ok {
		f.data[tid] = make(map[string]string)
	}
	f.data[tid][key] = value
	return nil
}

func (f *fakeSecretsStore) Delete(ctx context.Context, key string) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		tid = store.MasterTenantID
	}
	m, ok := f.data[tid]
	if ok {
		delete(m, key)
	}
	return nil
}

func (f *fakeSecretsStore) GetAll(ctx context.Context) (map[string]string, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		tid = store.MasterTenantID
	}
	m, ok := f.data[tid]
	if !ok {
		return make(map[string]string), nil
	}
	return m, nil
}

// --- JSON unmarshal tests ---

func TestWebSearchChainOverride_UnmarshalJSON_ProviderOrder(t *testing.T) {
	data := []byte(`{"provider_order":["brave","exa"],"brave":{"enabled":false}}`)
	var o WebSearchChainOverride
	if err := o.UnmarshalJSON(data); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(o.ProviderOrder) != 2 || o.ProviderOrder[0] != "brave" {
		t.Errorf("ProviderOrder: got %v", o.ProviderOrder)
	}
	po, ok := o.Providers["brave"]
	if !ok || po.Enabled == nil || *po.Enabled != false {
		t.Errorf("brave override: got %+v", po)
	}
}

func TestWebSearchChainOverride_UnmarshalJSON_Empty(t *testing.T) {
	var o WebSearchChainOverride
	if err := o.UnmarshalJSON([]byte(`{}`)); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(o.ProviderOrder) != 0 || len(o.Providers) != 0 {
		t.Errorf("expected empty override: %+v", o)
	}
}

// --- Chain resolution tests (scenarios 1-6 from phase file) ---

func TestBuildChainFromStorage(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  uuid.UUID
		override  string // JSON string for builtin_tool_tenant_configs.settings web_search field
		secrets   map[string]string
		wantNames []string
		wantLen   int
	}{
		{
			name:      "scenario 1: no secrets, no override → DDG only",
			tenantID:  uuid.New(),
			override:  "",
			secrets:   map[string]string{},
			wantNames: []string{"duckduckgo"},
			wantLen:   1,
		},
		{
			name:     "scenario 2: override brave+exa, brave key present → [brave, exa, duckduckgo]",
			tenantID: uuid.New(),
			override: `{"provider_order":["brave","exa"]}`,
			secrets: map[string]string{
				"tools.web.brave.api_key": "test-key-brave-123",
				"tools.web.exa.api_key":   "test-key-exa-456",
			},
			wantNames: []string{"brave", "exa", "duckduckgo"},
			wantLen:   3,
		},
		{
			name:     "scenario 3: override brave only, no brave key → [duckduckgo] (DDG always present)",
			tenantID: uuid.New(),
			override: `{"provider_order":["brave"]}`,
			secrets:  map[string]string{},
			wantNames: []string{"duckduckgo"},
			wantLen:   1,
		},
		{
			name:     "scenario 4: override with unknown provider name → skipped, DDG present",
			tenantID: uuid.New(),
			override: `{"provider_order":["unknown_provider","brave"]}`,
			secrets: map[string]string{
				"tools.web.brave.api_key": "test-key-brave-789",
			},
			wantNames: []string{"brave", "duckduckgo"},
			wantLen:   2,
		},
		{
			name:     "scenario 5: DDG explicitly disabled → still present (force-enabled)",
			tenantID: uuid.New(),
			override: `{"duckduckgo":{"enabled":false}}`,
			secrets:  map[string]string{},
			wantNames: []string{"duckduckgo"},
			wantLen:   1,
		},
		{
			name:     "scenario 6: Brave explicitly disabled, key present → skipped, DDG present",
			tenantID: uuid.New(),
			override: `{"brave":{"enabled":false}}`,
			secrets: map[string]string{
				"tools.web.brave.api_key": "test-key-brave-disabled",
			},
			wantNames: []string{"duckduckgo"},
			wantLen:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = store.WithTenantID(ctx, tt.tenantID)

			// Set up fake secrets
			fake := newFakeSecretsStore()
			for k, v := range tt.secrets {
				fake.Set(ctx, k, v)
			}

			// Set up override in context (tenant-layer settings)
			if tt.override != "" {
				settingsMap := BuiltinToolSettings{
					"web_search": []byte(tt.override),
				}
				ctx = WithTenantToolSettings(ctx, settingsMap)
			}

			// Resolve chain
			chain := BuildChainFromStorage(ctx, fake)

			if len(chain) != tt.wantLen {
				t.Errorf("chain length: got %d, want %d", len(chain), tt.wantLen)
			}

			names := chainNames(chain)
			if len(names) != len(tt.wantNames) {
				t.Errorf("provider names: got %v, want %v", names, tt.wantNames)
				return
			}

			for i, want := range tt.wantNames {
				if names[i] != want {
					t.Errorf("provider %d: got %s, want %s", i, names[i], want)
				}
			}
		})
	}
}

// --- Cache tests (see tenant_chain_cache_test.go for comprehensive cache tests) ---

func TestTenantChainCache_SetGet(t *testing.T) {
	c := newTenantChainCache()

	tid := uuid.New()
	providers := []SearchProvider{&fakeSearchProvider{"ddg"}}
	c.Set(tid, providers)

	got, ok := c.Get(tid)
	if !ok || len(got) != 1 || got[0].Name() != "ddg" {
		t.Errorf("cache Get: ok=%v got=%v", ok, chainNames(got))
	}
}
