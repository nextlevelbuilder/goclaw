package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// --- Red Team F3: subagent ExecTool must inherit secureCLIStore wiring ---

type stubSecureCLIStoreCmd struct{}

func (s *stubSecureCLIStoreCmd) Create(ctx context.Context, b *store.SecureCLIBinary) error {
	return nil
}
func (s *stubSecureCLIStoreCmd) Get(ctx context.Context, id uuid.UUID) (*store.SecureCLIBinary, error) {
	return nil, nil
}
func (s *stubSecureCLIStoreCmd) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	return nil
}
func (s *stubSecureCLIStoreCmd) Delete(ctx context.Context, id uuid.UUID) error { return nil }
func (s *stubSecureCLIStoreCmd) List(ctx context.Context) ([]store.SecureCLIBinary, error) {
	return nil, nil
}
func (s *stubSecureCLIStoreCmd) ListEnabled(ctx context.Context) ([]store.SecureCLIBinary, error) {
	return nil, nil
}
func (s *stubSecureCLIStoreCmd) ListForAgent(ctx context.Context, agentID uuid.UUID) ([]store.SecureCLIBinary, error) {
	return nil, nil
}
func (s *stubSecureCLIStoreCmd) IsRegisteredBinary(ctx context.Context, binaryName string) (bool, error) {
	return false, nil
}
func (s *stubSecureCLIStoreCmd) LookupByBinary(ctx context.Context, binaryName string, agentID *uuid.UUID, userID string) (*store.SecureCLIBinary, error) {
	return nil, nil
}
func (s *stubSecureCLIStoreCmd) GetUserCredentials(ctx context.Context, binaryID uuid.UUID, userID string) (*store.SecureCLIUserCredential, error) {
	return nil, nil
}
func (s *stubSecureCLIStoreCmd) SetUserCredentials(ctx context.Context, binaryID uuid.UUID, userID string, encryptedEnv []byte) error {
	return nil
}
func (s *stubSecureCLIStoreCmd) SetUserCredentialsTyped(ctx context.Context, binaryID uuid.UUID, userID string, encryptedEnv []byte, credentialType, hostScope *string) error {
	return nil
}
func (s *stubSecureCLIStoreCmd) DeleteUserCredentials(ctx context.Context, binaryID uuid.UUID, userID string) error {
	return nil
}
func (s *stubSecureCLIStoreCmd) ListUserCredentials(ctx context.Context, binaryID uuid.UUID) ([]store.SecureCLIUserCredential, error) {
	return nil, nil
}

// TestSubagentExecTool_StoreWired ensures the subagent tool factory wires the
// SecureCLIStore into the subagent's ExecTool, so the gate enforces on
// spawned-subagent exec (Red Team F3). A missing wiring would let a parent
// agent bypass the gate by delegating the exec to a subagent.
func TestSubagentExecTool_StoreWired(t *testing.T) {
	parent := tools.NewRegistry()
	stub := &stubSecureCLIStoreCmd{}

	_, execTool := buildSubagentToolsRegistry(parent, t.TempDir(), false, nil, stub)
	if execTool == nil {
		t.Fatal("expected non-nil exec tool from factory")
	}
	if !execTool.HasSecureCLIStore() {
		t.Fatalf("expected subagent ExecTool to have SecureCLIStore wired (Red Team F3)")
	}
}

// TestSubagentExecTool_NilStoreIsSafe ensures the factory does not panic when
// the store is unavailable (Lite edition / no encryption key). The exec tool
// simply lacks the gate — same as today's Lite behavior.
func TestSubagentExecTool_NilStoreIsSafe(t *testing.T) {
	parent := tools.NewRegistry()
	_, execTool := buildSubagentToolsRegistry(parent, t.TempDir(), false, nil, nil)
	if execTool == nil {
		t.Fatal("expected non-nil exec tool from factory")
	}
	if execTool.HasSecureCLIStore() {
		t.Fatalf("expected no SecureCLIStore when passed nil (Lite path)")
	}
}

func captureEmbeddingRequest(t *testing.T, es *store.EmbeddingSettings) map[string]any {
	t.Helper()

	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{
				"embedding": make([]float32, store.RequiredMemoryEmbeddingDimensions),
				"index":     0,
			}},
		})
	}))
	defer server.Close()

	provider := &store.LLMProviderData{
		Name:         "embedding-provider",
		ProviderType: store.ProviderOpenAICompat,
		APIKey:       "test-key",
		APIBase:      server.URL,
		Enabled:      true,
	}

	ep := buildEmbeddingProvider(provider, es, nil, nil)
	if ep == nil {
		t.Fatal("buildEmbeddingProvider() = nil, want provider")
	}
	if _, err := ep.Embed(context.Background(), []string{"hello"}); err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	return requestBody
}

func TestBuildEmbeddingProviderDefaultsTo1536Dimensions(t *testing.T) {
	requestBody := captureEmbeddingRequest(t, nil)
	if got := requestBody["dimensions"]; got != float64(1536) {
		t.Fatalf("dimensions = %v, want 1536", got)
	}
}

func TestBuildEmbeddingProviderIgnoresIncompatibleStoredDimensions(t *testing.T) {
	requestBody := captureEmbeddingRequest(t, &store.EmbeddingSettings{
		Enabled:    true,
		Model:      "voyage-4-nano",
		Dimensions: 2048,
	})
	if got := requestBody["dimensions"]; got != float64(1536) {
		t.Fatalf("dimensions = %v, want fallback 1536", got)
	}
}

type embeddingProviderStoreStub struct {
	masterProviders []store.LLMProviderData
	allProviders    []store.LLMProviderData
	listTenant      uuid.UUID
	listAllCalled   bool
}

func (s *embeddingProviderStoreStub) CreateProvider(context.Context, *store.LLMProviderData) error {
	return nil
}
func (s *embeddingProviderStoreStub) GetProvider(context.Context, uuid.UUID) (*store.LLMProviderData, error) {
	return nil, nil
}
func (s *embeddingProviderStoreStub) GetProviderByName(context.Context, string) (*store.LLMProviderData, error) {
	return nil, nil
}
func (s *embeddingProviderStoreStub) ListProviders(ctx context.Context) ([]store.LLMProviderData, error) {
	s.listTenant = store.TenantIDFromContext(ctx)
	return s.masterProviders, nil
}
func (s *embeddingProviderStoreStub) ListAllProviders(context.Context) ([]store.LLMProviderData, error) {
	s.listAllCalled = true
	return s.allProviders, nil
}
func (s *embeddingProviderStoreStub) UpdateProvider(context.Context, uuid.UUID, map[string]any) error {
	return nil
}
func (s *embeddingProviderStoreStub) DeleteProvider(context.Context, uuid.UUID) error { return nil }

func TestResolveEmbeddingProviderAutoDetectUsesMasterTenantOnly(t *testing.T) {
	embeddingSettings := json.RawMessage(`{"embedding":{"enabled":true}}`)
	providerStore := &embeddingProviderStoreStub{
		masterProviders: []store.LLMProviderData{{
			TenantID:     store.MasterTenantID,
			Name:         "master-embedding",
			ProviderType: store.ProviderOpenAICompat,
			APIBase:      "http://master.invalid/v1",
			APIKey:       "master-key",
			Enabled:      true,
			Settings:     embeddingSettings,
		}},
		allProviders: []store.LLMProviderData{{
			TenantID:     uuid.New(),
			Name:         "tenant-controlled-endpoint",
			ProviderType: store.ProviderOpenAICompat,
			Enabled:      true,
			Settings:     embeddingSettings,
		}},
	}

	provider := resolveEmbeddingProvider(providerStore, nil, nil)
	if provider == nil || provider.Name() != "master-embedding" {
		t.Fatalf("resolveEmbeddingProvider() = %v, want master-embedding", provider)
	}
	if providerStore.listTenant != store.MasterTenantID {
		t.Fatalf("ListProviders tenant = %s, want master tenant", providerStore.listTenant)
	}
	if providerStore.listAllCalled {
		t.Fatal("ListAllProviders was called; cross-tenant auto-detect must stay disabled")
	}
}
