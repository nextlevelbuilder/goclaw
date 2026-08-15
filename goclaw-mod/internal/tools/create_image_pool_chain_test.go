package tools

// Integration tests for pool-failover-before-chain-fallthrough semantics in create_image.
// These tests exercise the full stack: ExecuteWithChain + wrapPoolProvider +
// CreateImageTool.callProvider, wired through a real ChatGPTOAuthRouter backed
// by mock HTTP servers. They prove the contract from issue #1008 is correct:
// pool member failover happens INSIDE the router before the outer chain advances.
//
// Not duplicated here (already covered at unit level):
//   - internal/providers/chatgpt_oauth_router_image_test.go — router failover semantics
//   - internal/tools/media_provider_chain_pool_test.go      — wrapPoolProvider decisions

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/providers/providertest"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// poolImageSSE returns a minimal SSE body that parseNativeImageSSE accepts.
func poolImageSSE(b64data string) string {
	return `data: {"type":"response.output_item.done","item":{"type":"image_generation_call","result":"` +
		b64data + `","output_format":"png"}}` + "\n\ndata: [DONE]\n"
}

// poolSSEServer starts a test server that returns a successful image SSE on each request.
func poolSSEServer(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(poolImageSSE("aW1hZ2VkYXRh"))) // "imagedata" base64
	}))
	t.Cleanup(s.Close)
	return s
}

// pool429Server starts a test server that always returns HTTP 429 (retryable).
func pool429Server(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	t.Cleanup(s.Close)
	return s
}

// buildPoolChainRegistry creates a registry and registers each CodexProvider under
// both the master tenant (so ExecuteWithChain's Get resolves it) and the given
// tenantID (so ChatGPTOAuthRouter's GetForTenant resolves pool members).
func buildPoolChainRegistry(tenantID uuid.UUID, members ...*providers.CodexProvider) *providers.Registry {
	reg := providers.NewRegistry(nil)
	for _, p := range members {
		reg.Register(p)                    // master tenant — found by ExecuteWithChain
		reg.RegisterForTenant(tenantID, p) // tenant scope — found by router's GetForTenant
	}
	return reg
}

// poolBaseChainEntry returns a chain entry pointing at baseProvider.
// Prompt injected via Params so callProvider can build NativeImageRequest.
func poolBaseChainEntry(baseProvider string) MediaProviderEntry {
	return MediaProviderEntry{
		Provider:   baseProvider,
		Model:      "gpt-image-2",
		Enabled:    true,
		Timeout:    10,
		MaxRetries: 1,
		Params: map[string]any{
			"prompt":       "integration test image",
			"aspect_ratio": "1:1",
		},
	}
}

// fakeFallbackEntry returns a chain entry for a nativeImageProvider-backed fake
// (already defined in create_image_native_path_test.go in this package).
// Using a native fake avoids credential requirements for the fallback slot.
func fakeFallbackEntry(name string) MediaProviderEntry {
	return MediaProviderEntry{
		Provider:   name,
		Model:      "fake-model",
		Enabled:    true,
		Timeout:    10,
		MaxRetries: 1,
		Params: map[string]any{
			"prompt":       "integration test image",
			"aspect_ratio": "1:1",
		},
	}
}

// --- Scenario 1 ---
// Chain: [Pool(A retryable, B success), Fallback]
// Expected: result from B; Fallback NOT called.
// Proves issue #1008 fix: pool failover is internal to the router, never leaks to outer chain.
func TestCreateImagePoolChain_PoolMemberFailover_FallbackNotCalled(t *testing.T) {
	tenantID := uuid.New()

	var hitsA, hitsB atomic.Int32
	serverA := pool429Server(t, &hitsA)
	serverB := poolSSEServer(t, &hitsB)

	baseA := providertest.NewCodexProviderFast("pool-a", serverA.URL)
	memberB := providertest.NewCodexProviderFast("pool-b", serverB.URL)
	baseA.WithRoutingDefaults("round_robin", []string{"pool-b"})

	reg := buildPoolChainRegistry(tenantID, baseA, memberB)

	// Fallback fake — should NOT be called.
	fallback := &nativeImageProvider{
		name:       "fallback-fake",
		model:      "fake-model",
		returnData: []byte("fallback-bytes"),
	}
	reg.Register(fallback)

	ctx := store.WithTenantID(t.Context(), tenantID)
	ctx = WithToolWorkspace(ctx, t.TempDir())

	chain := []MediaProviderEntry{
		poolBaseChainEntry("pool-a"),
		fakeFallbackEntry("fallback-fake"),
	}

	tool := NewCreateImageTool(reg)
	result, err := ExecuteWithChain(ctx, chain, reg, tool.callProvider)
	if err != nil {
		t.Fatalf("ExecuteWithChain failed: %v", err)
	}
	if len(result.Data) == 0 {
		t.Error("result.Data empty — expected image bytes from pool member B")
	}
	if hitsA.Load() == 0 {
		t.Error("pool member A was never hit (expected 429)")
	}
	if hitsB.Load() == 0 {
		t.Error("pool member B was never hit (expected success)")
	}
	// Core assertion from issue #1008: fallback must NOT have been called.
	if fallback.calledWith != nil {
		t.Errorf("fallback provider was called — pool failover to B should prevent chain fallthrough (issue #1008 regression)")
	}
}

// --- Scenario 2 ---
// Chain: [Pool(A fail, B fail), Fallback success]
// Expected: result from Fallback.
func TestCreateImagePoolChain_PoolExhausted_FallsThroughToFallback(t *testing.T) {
	tenantID := uuid.New()

	var hitsA, hitsB atomic.Int32
	serverA := pool429Server(t, &hitsA)
	serverB := pool429Server(t, &hitsB)

	baseA := providertest.NewCodexProviderFast("pool-a2", serverA.URL)
	memberB := providertest.NewCodexProviderFast("pool-b2", serverB.URL)
	baseA.WithRoutingDefaults("round_robin", []string{"pool-b2"})

	reg := buildPoolChainRegistry(tenantID, baseA, memberB)

	fallback := &nativeImageProvider{
		name:       "fallback-fake2",
		model:      "fake-model",
		returnData: []byte("fallback-image-bytes"),
	}
	reg.Register(fallback)

	ctx := store.WithTenantID(t.Context(), tenantID)
	ctx = WithToolWorkspace(ctx, t.TempDir())

	chain := []MediaProviderEntry{
		poolBaseChainEntry("pool-a2"),
		fakeFallbackEntry("fallback-fake2"),
	}

	tool := NewCreateImageTool(reg)
	result, err := ExecuteWithChain(ctx, chain, reg, tool.callProvider)
	if err != nil {
		t.Fatalf("ExecuteWithChain failed: %v — expected fallback to succeed", err)
	}
	if len(result.Data) == 0 {
		t.Error("result.Data empty — expected bytes from fallback")
	}
	if hitsA.Load() == 0 {
		t.Error("pool member A was not attempted")
	}
	if hitsB.Load() == 0 {
		t.Error("pool member B was not attempted")
	}
	if fallback.calledWith == nil {
		t.Error("fallback was not called — expected chain fallthrough after pool exhausted")
	}
}

// --- Scenario 3 ---
// Chain: [Pool(A,B) exhausted, Fallback also fails]
// Expected: error surfaces; no panic.
func TestCreateImagePoolChain_AllFail_ErrorSurfaces(t *testing.T) {
	tenantID := uuid.New()

	var hitsA, hitsB atomic.Int32
	serverA := pool429Server(t, &hitsA)
	serverB := pool429Server(t, &hitsB)

	baseA := providertest.NewCodexProviderFast("pool-a3", serverA.URL)
	memberB := providertest.NewCodexProviderFast("pool-b3", serverB.URL)
	baseA.WithRoutingDefaults("round_robin", []string{"pool-b3"})

	reg := buildPoolChainRegistry(tenantID, baseA, memberB)

	// Fallback fake that returns an error.
	fallbackErr := &nativeImageProvider{
		name:        "fallback-fake3",
		model:       "fake-model",
		returnError: errPoolTestFailure,
	}
	reg.Register(fallbackErr)

	ctx := store.WithTenantID(t.Context(), tenantID)
	ctx = WithToolWorkspace(ctx, t.TempDir())

	chain := []MediaProviderEntry{
		poolBaseChainEntry("pool-a3"),
		fakeFallbackEntry("fallback-fake3"),
	}

	tool := NewCreateImageTool(reg)
	_, err := ExecuteWithChain(ctx, chain, reg, tool.callProvider)
	if err == nil {
		t.Fatal("expected error when all providers fail, got nil")
	}
}

// errPoolTestFailure is a sentinel error used for fallback failure simulation.
var errPoolTestFailure = &poolChainTestError{msg: "pool integration test: simulated failure"}

// poolChainTestError is a simple non-retryable error for testing.
type poolChainTestError struct{ msg string }

func (e *poolChainTestError) Error() string { return e.msg }

// --- Scenario 4 ---
// Chain: [Pool(A)] — single-member pool, no routing defaults.
// wrapPoolProvider must NOT wrap; callProvider routes directly to A via native path.
func TestCreateImagePoolChain_SingleMemberPool_NoWrapOverhead(t *testing.T) {
	tenantID := uuid.New()

	var hitsA atomic.Int32
	serverA := poolSSEServer(t, &hitsA)

	// Solo provider — no WithRoutingDefaults → wrapPoolProvider returns it unchanged.
	soloA := providertest.NewCodexProviderFast("solo-a", serverA.URL)

	reg := buildPoolChainRegistry(tenantID, soloA)

	ctx := store.WithTenantID(t.Context(), tenantID)
	ctx = WithToolWorkspace(ctx, t.TempDir())

	chain := []MediaProviderEntry{poolBaseChainEntry("solo-a")}

	tool := NewCreateImageTool(reg)
	result, err := ExecuteWithChain(ctx, chain, reg, tool.callProvider)
	if err != nil {
		t.Fatalf("single-member pool failed: %v", err)
	}
	if len(result.Data) == 0 {
		t.Error("result.Data empty")
	}
	if hitsA.Load() == 0 {
		t.Error("solo member A was not called")
	}
}

// --- Scenario 5 ---
// Chain: [Pool(A,B) round_robin] — 2 calls must hit different members.
// Verifies RR counter advances once per GenerateImage call (not per member tried).
func TestCreateImagePoolChain_RoundRobin_RotatesAcrossTwoCalls(t *testing.T) {
	tenantID := uuid.New()

	var hitsA, hitsB atomic.Int32
	serverA := poolSSEServer(t, &hitsA)
	serverB := poolSSEServer(t, &hitsB)

	baseA := providertest.NewCodexProviderFast("rr-a", serverA.URL)
	memberB := providertest.NewCodexProviderFast("rr-b", serverB.URL)
	baseA.WithRoutingDefaults("round_robin", []string{"rr-b"})

	reg := buildPoolChainRegistry(tenantID, baseA, memberB)

	ctx := store.WithTenantID(t.Context(), tenantID)
	ctx = WithToolWorkspace(ctx, t.TempDir())

	chain := []MediaProviderEntry{poolBaseChainEntry("rr-a")}
	tool := NewCreateImageTool(reg)

	for i := range 2 {
		result, err := ExecuteWithChain(ctx, chain, reg, tool.callProvider)
		if err != nil {
			t.Fatalf("call %d: ExecuteWithChain failed: %v", i+1, err)
		}
		if len(result.Data) == 0 {
			t.Errorf("call %d: result.Data empty", i+1)
		}
	}

	// With round_robin and 2 members, 2 successful calls must each hit a different member.
	if hitsA.Load() != 1 {
		t.Errorf("hitsA = %d, want 1 (round-robin should spread 2 calls across 2 members)", hitsA.Load())
	}
	if hitsB.Load() != 1 {
		t.Errorf("hitsB = %d, want 1 (round-robin should spread 2 calls across 2 members)", hitsB.Load())
	}
}
