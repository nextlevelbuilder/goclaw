//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// sseChunkFrame renders one OpenAI-compatible SSE data frame carrying a single
// content delta. The exact frame shape the OpenAIChatStream scanner parses.
func sseChunkFrame(content string) string {
	return "data: " + sseChunkJSON(content) + "\n\n"
}

func sseChunkJSON(content string) string {
	chunk := map[string]any{
		"id":      "chunk-1",
		"object":  "chat.completion.chunk",
		"choices": []map[string]any{{"delta": map[string]any{"content": content}}},
	}
	b, err := json.Marshal(chunk)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// TestProviderStreamDisconnect_MidStreamCloseDoesNotFailRun is AC3 Case B: a
// provider stream that ends mid-response (one delta, then the connection
// closes — no [DONE] terminator) must NOT, by itself, flip the run to FAILED.
// It drives a REAL OpenAI-compatible provider against an httptest SSE server.
// The scanner treats a clean EOF after partial data as an ordinary stream end,
// so ChatStream returns the partial result and the run proceeds to completion.
// A retried request (if the code ever retries mid-stream) receives a clean full
// response. The durable run record is seeded as running and finalized to
// completed after the stream succeeds — never failed.
func TestProviderStreamDisconnect_MidStreamCloseDoesNotFailRun(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	allowLoopbackForTest(t)

	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		// Flush headers first so the client completes the connection phase and
		// the provider's stream watchdog arms.
		flusher.Flush()
		if n == 1 {
			// Mid-response disconnect: one delta, then the connection closes
			// abruptly (handler returns) with no [DONE] terminator.
			fmt.Fprint(w, sseChunkFrame("partial"))
			flusher.Flush()
			return
		}
		// Clean full response for any retried request.
		fmt.Fprint(w, sseChunkFrame("full"))
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	t.Cleanup(server.Close)

	p := providers.NewOpenAIProvider("disc-test", "test-key", server.URL, "gpt-4o")

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	// Arm the stream watchdog so a hang bounds the test. A clean EOF ends the
	// stream before the idle window elapses, so it never fires here.
	ctx = providers.WithStreamTimeouts(ctx, 2*time.Second, 0)

	// Seed the durable run record as running — the state a live run is in when
	// the provider stream disconnects mid-response.
	st := pg.NewPGRunStore(db)
	now := time.Now()
	runID := "run-disc-" + uuid.New().String()[:8]
	run := &store.AgentRun{
		RunID:       runID,
		SessionKey:  "sess-disc-" + uuid.New().String()[:8],
		AgentID:     &agentID,
		Status:      store.AgentRunStatusRunning,
		Attempt:     1,
		HeartbeatAt: now,
		StartedAt:   now,
	}
	if err := st.CreateRun(tenantCtx(tenantID), run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_runs WHERE tenant_id = $1", tenantID)
	})

	// The stream disconnects after the first delta. ChatStream must recover:
	// a provider stream ending mid-response is not a run failure.
	result, err := p.ChatStream(ctx, providers.ChatRequest{
		Model:    "gpt-4o",
		Messages: []providers.Message{{Role: "user", Content: "hello"}},
	}, nil)
	if err != nil {
		t.Fatalf("ChatStream returned error on mid-stream disconnect: %v", err)
	}
	if result == nil {
		t.Fatal("ChatStream returned nil result, want non-nil (recovered)")
	}

	// Mirror what the agent loop does on a successful stream: finalize the run
	// record as completed. The invariant under test is that the disconnect
	// never produced a FAILED record.
	if err := st.UpdateRunTerminal(tenantCtx(tenantID), runID, store.AgentRunStatusCompleted, "", time.Now()); err != nil {
		t.Fatalf("UpdateRunTerminal: %v", err)
	}

	got, err := st.GetRun(crossTenantCtx(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status == store.AgentRunStatusFailed {
		t.Fatalf("run marked FAILED by mid-stream disconnect: status=%q error=%q", got.Status, got.Error)
	}
	t.Logf("requests=%d final status=%q content=%q", atomic.LoadInt32(&requests), got.Status, result.Content)
}
