package providers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

// imageSSEResponse builds a minimal SSE body that parseNativeImageSSE will accept.
// b64data is base64-encoded image bytes (any non-empty string works for routing tests).
func imageSSEResponse(b64data string) string {
	return `data: {"type":"response.output_item.done","item":{"type":"image_generation_call","result":"` +
		b64data + `","output_format":"png"}}` + "\n\ndata: [DONE]\n"
}

// imageTestServer returns a test HTTP server that responds with a successful image SSE body.
// body is called on each request so callers can count hits via a closure.
func imageTestServer(t *testing.T, body func() string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body()))
	}))
	t.Cleanup(s.Close)
	return s
}

// retryableImageTestServer returns a test HTTP server that always responds HTTP 429.
func retryableImageTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	t.Cleanup(s.Close)
	return s
}

// badRequestImageTestServer returns a test HTTP server that always responds HTTP 400 (non-retryable).
func badRequestImageTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	t.Cleanup(s.Close)
	return s
}

// newImageCodexProvider creates a *CodexProvider with retries disabled, pointing at apiBase.
func newImageCodexProvider(name, apiBase string) *CodexProvider {
	p := NewCodexProvider(name, &staticTokenSource{token: "token-" + name}, apiBase, "gpt-5.4")
	p.retryConfig.Attempts = 1 // disable internal retries so router failover logic is exercised
	return p
}

// imageReq is a minimal valid NativeImageRequest used across image router tests.
var imageReq = NativeImageRequest{Prompt: "a cat", ImageModel: "gpt-image-2"}

// b64img is a non-empty base64 string used as placeholder image data in test SSE responses.
const b64img = "aW1hZ2VkYXRh" // "imagedata" — not a valid PNG, but parseNativeImageSSE accepts any non-empty b64

// TestChatGPTOAuthRouterImage_ChatBurstDoesNotPerturbImageOrder is the regression
// test for issue #1018. It uses the issue's exact worked example: chat bursts
// interleaved between image calls on a 3-account pool.
//
// On the OLD shared-counter implementation the image hit sequence was skewed:
//
//	5 chat (counter 0→2) │ image1 start=2 → C, counter→0
//	4 chat (counter 0→1) │ image2 start=1 → B, counter→2
//	3 chat (counter 2→2) │ image3 start=2 → C, counter→0
//	                         ─ hits: A=0, B=1, C=2 (skew)
//
// On the fixed per-modality implementation the image counter is untouched by
// chat bursts and advances 0→1→2, giving exact sequence A, B, C (even).
//
// Critically, this scenario discriminates buggy vs. fixed code — unlike a test
// that runs N-consecutive image calls on an N-member pool, which always hits
// every member once regardless of the starting offset and cannot detect the bug.
func TestChatGPTOAuthRouterImage_ChatBurstDoesNotPerturbImageOrder(t *testing.T) {
	tenantID := uuid.New()
	registry := NewRegistry(nil)

	// Dual-purpose test servers: CodexProvider routes both chat and image to
	// `{apiBase}/codex/responses` — the request body distinguishes them (image
	// requests carry a `"type":"image_generation"` tool entry). Each server
	// counts chat vs image hits based on body inspection.
	var chatHits, imgHits [3]int
	mkServer := func(i int) *httptest.Server {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if bytes.Contains(body, []byte(`"image_generation"`)) {
				imgHits[i]++
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(imageSSEResponse(b64img)))
				return
			}
			chatHits[i]++
			writeSSEDone(w)
		}))
		t.Cleanup(s.Close)
		return s
	}
	srvA, srvB, srvC := mkServer(0), mkServer(1), mkServer(2)

	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-a", srvA.URL))
	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-b", srvB.URL))
	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-c", srvC.URL))

	router := NewChatGPTOAuthRouter(tenantID, registry, "acct-a", "round_robin", []string{"acct-b", "acct-c"})

	// Interleaved pattern from issue #1018: 5 chat, 1 image, 4 chat, 1 image, 3 chat, 1 image.
	pattern := []struct {
		chatBurst int
		image     bool
	}{
		{5, true},
		{4, true},
		{3, true},
	}
	imageOrder := make([]int, 0, 3) // records which server index served each image call
	for _, step := range pattern {
		for i := 0; i < step.chatBurst; i++ {
			if _, err := router.Chat(context.Background(), ChatRequest{
				Messages: []Message{{Role: "user", Content: "chat"}},
			}); err != nil {
				t.Fatalf("chat call failed: %v", err)
			}
		}
		if step.image {
			before := imgHits
			if _, err := router.GenerateImage(context.Background(), imageReq); err != nil {
				t.Fatalf("image call failed: %v", err)
			}
			for i := range imgHits {
				if imgHits[i] > before[i] {
					imageOrder = append(imageOrder, i)
					break
				}
			}
		}
	}

	// Post-fix expectation: image counter is independent, so image order is [A, B, C].
	// Pre-fix (buggy) expectation with this input would be [C, B, C] — NOT all distinct.
	wantOrder := []int{0, 1, 2}
	if len(imageOrder) != 3 {
		t.Fatalf("imageOrder length = %d, want 3 (hits=%v)", len(imageOrder), imgHits)
	}
	for i, got := range imageOrder {
		if got != wantOrder[i] {
			t.Fatalf("image call %d hit acct-%c, want acct-%c (order=%v, hits=%v)",
				i, 'a'+byte(got), 'a'+byte(wantOrder[i]), imageOrder, imgHits)
		}
	}

	// And each image server MUST have been hit exactly once (acceptance criterion from #1018).
	for i, n := range imgHits {
		if n != 1 {
			t.Fatalf("image server acct-%c hit %d times, want 1 (hits=%v)", 'a'+byte(i), n, imgHits)
		}
	}
}

// TestChatGPTOAuthRouter_IntrospectionDoesNotAdvanceCounter guards the invariant
// that Name(), DefaultModel(), and HasAvailableProviders() use advance=false when
// calling orderedProviders — i.e. calling them never rotates live traffic offsets.
// If someone flips those to advance=true in a refactor, this test fails fast.
func TestChatGPTOAuthRouter_IntrospectionDoesNotAdvanceCounter(t *testing.T) {
	tenantID := uuid.New()
	registry := NewRegistry(nil)

	var hitsA, hitsB, hitsC int
	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hitsA++
		writeSSEDone(w)
	}))
	t.Cleanup(serverA.Close)
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hitsB++
		writeSSEDone(w)
	}))
	t.Cleanup(serverB.Close)
	serverC := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hitsC++
		writeSSEDone(w)
	}))
	t.Cleanup(serverC.Close)

	pa := newImageCodexProvider("acct-a", serverA.URL)
	pb := newImageCodexProvider("acct-b", serverB.URL)
	pc := newImageCodexProvider("acct-c", serverC.URL)
	registry.RegisterForTenant(tenantID, pa)
	registry.RegisterForTenant(tenantID, pb)
	registry.RegisterForTenant(tenantID, pc)

	router := NewChatGPTOAuthRouter(tenantID, registry, "acct-a", "round_robin", []string{"acct-b", "acct-c"})

	// Call introspection 20 times — counter must stay at 0.
	for range 20 {
		_ = router.Name()
		_ = router.DefaultModel()
		_ = router.HasAvailableProviders()
	}

	// One real chat call — must hit acct-a (counter was never advanced).
	if _, err := router.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	}); err != nil {
		t.Fatalf("chat failed: %v", err)
	}
	if hitsA != 1 || hitsB != 0 || hitsC != 0 {
		t.Fatalf("introspection leaked counter advance: hitsA=%d hitsB=%d hitsC=%d, want 1/0/0",
			hitsA, hitsB, hitsC)
	}
}

// TestChatGPTOAuthRouterImage_RoundRobin_RotatesAcrossCalls verifies that 2 successive
// GenerateImage calls each hit a different pool member (round-robin distribution).
func TestChatGPTOAuthRouterImage_RoundRobin_RotatesAcrossCalls(t *testing.T) {
	tenantID := uuid.New()
	registry := NewRegistry(nil)

	var hitsA, hitsB int
	serverA := imageTestServer(t, func() string { hitsA++; return imageSSEResponse(b64img) })
	serverB := imageTestServer(t, func() string { hitsB++; return imageSSEResponse(b64img) })

	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-a", serverA.URL))
	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-b", serverB.URL))

	router := NewChatGPTOAuthRouter(tenantID, registry, "acct-a", "round_robin", []string{"acct-b"})

	for i := range 2 {
		if _, err := router.GenerateImage(context.Background(), imageReq); err != nil {
			t.Fatalf("call %d: GenerateImage failed: %v", i, err)
		}
	}
	if hitsA != 1 {
		t.Fatalf("hitsA = %d, want 1", hitsA)
	}
	if hitsB != 1 {
		t.Fatalf("hitsB = %d, want 1", hitsB)
	}
}

// TestChatGPTOAuthRouterImage_FirstRetryable_SecondSucceeds verifies that when member A
// returns HTTP 429 (retryable), the router fails over to member B and returns its result.
func TestChatGPTOAuthRouterImage_FirstRetryable_SecondSucceeds(t *testing.T) {
	tenantID := uuid.New()
	registry := NewRegistry(nil)

	serverA := retryableImageTestServer(t)
	serverB := imageTestServer(t, func() string { return imageSSEResponse(b64img) })

	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-a", serverA.URL))
	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-b", serverB.URL))

	router := NewChatGPTOAuthRouter(tenantID, registry, "acct-a", "round_robin", []string{"acct-b"})

	result, err := router.GenerateImage(context.Background(), imageReq)
	if err != nil {
		t.Fatalf("GenerateImage failed: %v", err)
	}
	if len(result.Data) == 0 {
		t.Fatal("result.Data is empty — expected image bytes from member B")
	}
}

// TestChatGPTOAuthRouterImage_PriorityOrder_FirstFails_SecondSucceeds verifies failover
// under the priority_order strategy: A returns HTTP 429, B succeeds.
func TestChatGPTOAuthRouterImage_PriorityOrder_FirstFails_SecondSucceeds(t *testing.T) {
	tenantID := uuid.New()
	registry := NewRegistry(nil)

	serverA := retryableImageTestServer(t)
	serverB := imageTestServer(t, func() string { return imageSSEResponse(b64img) })

	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-a", serverA.URL))
	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-b", serverB.URL))

	router := NewChatGPTOAuthRouter(tenantID, registry, "acct-a", "priority_order", []string{"acct-b"})

	result, err := router.GenerateImage(context.Background(), imageReq)
	if err != nil {
		t.Fatalf("GenerateImage (priority_order) failed: %v", err)
	}
	if len(result.Data) == 0 {
		t.Fatal("result.Data is empty")
	}
}

// TestChatGPTOAuthRouterImage_NonRetryable_ReturnsImmediately verifies that HTTP 400
// (non-retryable) is returned immediately without attempting member B.
func TestChatGPTOAuthRouterImage_NonRetryable_ReturnsImmediately(t *testing.T) {
	tenantID := uuid.New()
	registry := NewRegistry(nil)

	var hitsB int
	serverA := badRequestImageTestServer(t)
	serverB := imageTestServer(t, func() string { hitsB++; return imageSSEResponse(b64img) })

	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-a", serverA.URL))
	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-b", serverB.URL))

	router := NewChatGPTOAuthRouter(tenantID, registry, "acct-a", "round_robin", []string{"acct-b"})

	_, err := router.GenerateImage(context.Background(), imageReq)
	if err == nil {
		t.Fatal("GenerateImage should have failed on non-retryable HTTP 400")
	}
	if hitsB != 0 {
		t.Fatalf("hitsB = %d, want 0 (B must not be attempted after non-retryable error)", hitsB)
	}
}

// TestChatGPTOAuthRouterImage_AllFail_AggregatedError verifies that when all 3 members
// return retryable errors, the returned error message mentions all member names.
func TestChatGPTOAuthRouterImage_AllFail_AggregatedError(t *testing.T) {
	tenantID := uuid.New()
	registry := NewRegistry(nil)

	for _, name := range []string{"acct-a", "acct-b", "acct-c"} {
		s := retryableImageTestServer(t)
		registry.RegisterForTenant(tenantID, newImageCodexProvider(name, s.URL))
	}

	router := NewChatGPTOAuthRouter(tenantID, registry, "acct-a", "priority_order", []string{"acct-b", "acct-c"})

	_, err := router.GenerateImage(context.Background(), imageReq)
	if err == nil {
		t.Fatal("GenerateImage should fail when all members fail")
	}
	errStr := err.Error()
	for _, name := range []string{"acct-a", "acct-b", "acct-c"} {
		if !strings.Contains(errStr, name) {
			t.Fatalf("error %q does not mention member %q", errStr, name)
		}
	}
}

// TestChatGPTOAuthRouterImage_ContextCancel_Aborts verifies that a pre-cancelled context
// causes GenerateImage to return a context-derived error.
func TestChatGPTOAuthRouterImage_ContextCancel_Aborts(t *testing.T) {
	tenantID := uuid.New()
	registry := NewRegistry(nil)

	// retryable server so the router would attempt failover — but context cancels first
	serverA := retryableImageTestServer(t)
	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-a", serverA.URL))

	router := NewChatGPTOAuthRouter(tenantID, registry, "acct-a", "round_robin", nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call

	_, err := router.GenerateImage(ctx, imageReq)
	if err == nil {
		t.Fatal("GenerateImage should fail with cancelled context")
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected context cancellation error, got: %v", err)
	}
}

// TestChatGPTOAuthRouterImage_RoundRobinAdvancesPerCall verifies that the round-robin
// counter advances once per GenerateImage call (not per member tried), matching Chat semantics.
// With 2 members: call 1 → A, call 2 → B, call 3 → A again.
func TestChatGPTOAuthRouterImage_RoundRobinAdvancesPerCall(t *testing.T) {
	tenantID := uuid.New()
	registry := NewRegistry(nil)

	var hitsA, hitsB int
	serverA := imageTestServer(t, func() string { hitsA++; return imageSSEResponse(b64img) })
	serverB := imageTestServer(t, func() string { hitsB++; return imageSSEResponse(b64img) })

	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-a", serverA.URL))
	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-b", serverB.URL))

	router := NewChatGPTOAuthRouter(tenantID, registry, "acct-a", "round_robin", []string{"acct-b"})

	for i := range 3 {
		if _, err := router.GenerateImage(context.Background(), imageReq); err != nil {
			t.Fatalf("call %d: GenerateImage failed: %v", i, err)
		}
	}
	// A: calls 1 and 3; B: call 2
	if hitsA != 2 {
		t.Fatalf("hitsA = %d, want 2", hitsA)
	}
	if hitsB != 1 {
		t.Fatalf("hitsB = %d, want 1", hitsB)
	}
}

// TestChatGPTOAuthRouter_ChatAndImageConcurrent_CountersIndependent is the
// concurrent stress proof of issue #1018's core thesis: chat and image
// rotation counters must not corrupt each other under parallel load.
//
// On a 3-member pool, N chat calls and N image calls run concurrently;
// with independent per-modality counters both modalities MUST distribute
// ~evenly (each server ±1 of N/3 for its modality). Any shared state would
// show either a data race (caught by -race) or visible skew.
func TestChatGPTOAuthRouter_ChatAndImageConcurrent_CountersIndependent(t *testing.T) {
	tenantID := uuid.New()
	registry := NewRegistry(nil)

	var chatHits, imgHits [3]int64
	mkServer := func(i int) *httptest.Server {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if bytes.Contains(body, []byte(`"image_generation"`)) {
				atomic.AddInt64(&imgHits[i], 1)
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(imageSSEResponse(b64img)))
				return
			}
			atomic.AddInt64(&chatHits[i], 1)
			writeSSEDone(w)
		}))
		t.Cleanup(s.Close)
		return s
	}
	srvA, srvB, srvC := mkServer(0), mkServer(1), mkServer(2)

	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-a", srvA.URL))
	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-b", srvB.URL))
	registry.RegisterForTenant(tenantID, newImageCodexProvider("acct-c", srvC.URL))

	router := NewChatGPTOAuthRouter(tenantID, registry, "acct-a", "round_robin", []string{"acct-b", "acct-c"})

	const perModality = 90 // divisible by 3 so even distribution is achievable
	var wg sync.WaitGroup
	wg.Add(perModality * 2)
	for range perModality {
		go func() {
			defer wg.Done()
			if _, err := router.Chat(context.Background(), ChatRequest{
				Messages: []Message{{Role: "user", Content: "c"}},
			}); err != nil {
				t.Errorf("chat failed: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := router.GenerateImage(context.Background(), imageReq); err != nil {
				t.Errorf("image failed: %v", err)
			}
		}()
	}
	wg.Wait()

	// Total hits per modality must equal perModality.
	totalChat := chatHits[0] + chatHits[1] + chatHits[2]
	totalImg := imgHits[0] + imgHits[1] + imgHits[2]
	if totalChat != perModality {
		t.Fatalf("total chat hits = %d, want %d", totalChat, perModality)
	}
	if totalImg != perModality {
		t.Fatalf("total image hits = %d, want %d", totalImg, perModality)
	}
	// Each server should get exactly perModality/3 hits per modality for a pure
	// round-robin under no failover. Allow slack for scheduling but assert no
	// member is starved and none is over-served beyond a small tolerance.
	expected := int64(perModality / 3)
	const slack = int64(5) // generous — any real bug produces much larger skew
	for i := range 3 {
		if chatHits[i] < expected-slack || chatHits[i] > expected+slack {
			t.Errorf("chat server %d hits = %d, want ~%d (±%d); all=%v",
				i, chatHits[i], expected, slack, chatHits)
		}
		if imgHits[i] < expected-slack || imgHits[i] > expected+slack {
			t.Errorf("image server %d hits = %d, want ~%d (±%d); all=%v",
				i, imgHits[i], expected, slack, imgHits)
		}
	}
}
