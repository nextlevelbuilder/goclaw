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

func captureEmbeddingRequest(t *testing.T, es *store.EmbeddingSettings, responseDim int) (map[string]any, [][]float32) {
	t.Helper()

	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":` + makeEmbeddingJSON(responseDim) + `}]}`))
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
	embeddings, err := ep.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	return requestBody, embeddings
}

func makeEmbeddingJSON(dim int) string {
	values := make([]float64, dim)
	for i := range values {
		values[i] = float64(i)
	}
	encoded, _ := json.Marshal(values)
	return string(encoded)
}

func TestBuildEmbeddingProviderOmitsDimensionsByDefault(t *testing.T) {
	requestBody, embeddings := captureEmbeddingRequest(t, nil, 3072)
	if _, ok := requestBody["dimensions"]; ok {
		t.Fatalf("request body unexpectedly included dimensions: %v", requestBody)
	}
	if got := len(embeddings[0]); got != store.RequiredMemoryEmbeddingDimensions {
		t.Fatalf("embedding length = %d, want %d", got, store.RequiredMemoryEmbeddingDimensions)
	}
}

func TestBuildEmbeddingProviderOmitsDimensionsWithIncompatibleStoredValue(t *testing.T) {
	requestBody, embeddings := captureEmbeddingRequest(t, &store.EmbeddingSettings{
		Enabled:    true,
		Model:      "voyage-4-nano",
		Dimensions: 2048,
	}, 3072)
	if _, ok := requestBody["dimensions"]; ok {
		t.Fatalf("request body unexpectedly included dimensions: %v", requestBody)
	}
	if got := len(embeddings[0]); got != store.RequiredMemoryEmbeddingDimensions {
		t.Fatalf("embedding length = %d, want %d", got, store.RequiredMemoryEmbeddingDimensions)
	}
}
