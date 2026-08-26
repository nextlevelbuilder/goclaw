package http

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ---- helpers ----
//
// Media tests point at an httptest.Server on 127.0.0.1, which the SSRF policy
// blocks. The bypass is a process-global atomic.Bool, so these tests must not
// call t.Parallel() — under -race a parallel test flipping it reports a data
// race that reads like a bug in the feature. Set and reset it per test, never
// at TestMain level, so the one test that asserts loopback IS refused stays
// honest.
func allowLoopbackMedia(t *testing.T) {
	t.Helper()
	security.SetAllowLoopbackForTest(true)
	t.Cleanup(func() { security.SetAllowLoopbackForTest(false) })
}

// isolateTempDir points os.CreateTemp at this test's own directory.
//
// Both this package and internal/webhooks glob os.TempDir() for goclaw_webhook_*
// to prove a failed request left nothing behind, and `go test ./...` runs
// packages in parallel — without isolation each would see the other's files and
// the before/after deltas would flake. TMPDIR covers unix, TMP/TEMP cover
// Windows, so the isolation holds however the suite is run.
func isolateTempDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	t.Setenv("TMP", dir)
	t.Setenv("TEMP", dir)
}

func mediaTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 9, G: 9, B: 9, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// servePNG starts a server returning a valid 1x1 PNG.
func servePNG(t *testing.T) *httptest.Server {
	t.Helper()
	body := mediaTestPNG(t)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(body)
	}))
	t.Cleanup(s.Close)
	return s
}

// serveOversize starts a server streaming past the 25 MB cap.
func serveOversize(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		io.CopyN(w, zeroSource{}, 25*1024*1024+1024)
	}))
	t.Cleanup(s.Close)
	return s
}

// serveBadMIME starts a server returning a type outside the allowlist.
func serveBadMIME(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>no</html>"))
	}))
	t.Cleanup(s.Close)
	return s
}

func countMediaTemps(t *testing.T) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "goclaw_webhook_*"))
	if err != nil {
		t.Fatalf("glob temp: %v", err)
	}
	return len(matches)
}

type zeroSource struct{}

func (zeroSource) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// mediaTestEnv wires a handler whose stub agent records the RunRequest it saw.
type mediaTestEnv struct {
	h         *WebhookLLMHandler
	wh        *store.WebhookData
	callStore *llmCallStore
	// seen is the RunRequest the agent was invoked with, nil if never invoked.
	seen *agent.RunRequest
}

func newMediaTestEnv(t *testing.T, lane *scheduler.Lane) *mediaTestEnv {
	t.Helper()
	agentUUID := uuid.New()
	env := &mediaTestEnv{
		callStore: &llmCallStore{},
		wh: &store.WebhookData{
			ID:       uuid.New(),
			TenantID: uuid.New(),
			AgentID:  &agentUUID,
			Kind:     "llm",
		},
	}
	ag := &stubLLMAgent{
		id:      agentUUID.String(),
		agentID: agentUUID,
		runFn: func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
			cp := req
			env.seen = &cp
			return &agent.RunResult{Content: "ok", RunID: "run-media"}, nil
		},
	}
	env.h = newTestLLMHandler(env.callStore, &msgWebhookStore{}, lane)
	env.h.agentRouter = stubRouterFor(agentUUID, ag)
	return env
}

func (e *mediaTestEnv) post(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	r := injectWebhook(buildLLMReq(t, body), e.wh)
	w := httptest.NewRecorder()
	e.h.handle(w, r)
	return w
}

func mediaItem(url string, filename string) map[string]any {
	item := map[string]any{"url": url}
	if filename != "" {
		item["filename"] = filename
	}
	return item
}

// ---- the core test ----

// TestWebhookLLM_MediaURL_PopulatesRunRequest asserts BOTH halves of the
// contract. A test that checked only len(Media) > 0 would pass while file-ref
// vision mode is silently broken: with a dedicated read_image provider the
// images are never attached to the main LLM, so the <media:image> tag is the
// only signal the model gets, and enrichImageIDs rewrites an existing tag
// rather than inserting a missing one.
func TestWebhookLLM_MediaURL_PopulatesRunRequest(t *testing.T) {
	allowLoopbackMedia(t)
	srv := servePNG(t)
	env := newMediaTestEnv(t, nil)

	w := env.post(t, map[string]any{
		"input": "What is in this screenshot?",
		"media": []map[string]any{mediaItem(srv.URL+"/shot.png", "shot.png")},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if env.seen == nil {
		t.Fatal("agent was never invoked")
	}
	if len(env.seen.Media) != 1 {
		t.Fatalf("RunRequest.Media = %d, want 1", len(env.seen.Media))
	}
	if env.seen.Media[0].MimeType != "image/png" {
		t.Errorf("MimeType = %q, want image/png", env.seen.Media[0].MimeType)
	}
	if !strings.Contains(env.seen.Message, "<media:image") {
		t.Fatalf("Message has no <media:image tag; file-ref vision mode would silently ignore the image.\nMessage = %q", env.seen.Message)
	}
	if !strings.Contains(env.seen.Message, "What is in this screenshot?") {
		t.Errorf("caller text lost from Message = %q", env.seen.Message)
	}
}

func TestWebhookLLM_MediaOnly_NoInput(t *testing.T) {
	allowLoopbackMedia(t)
	srv := servePNG(t)
	env := newMediaTestEnv(t, nil)

	// An image with no caption is a legitimate request.
	w := env.post(t, map[string]any{
		"media": []map[string]any{mediaItem(srv.URL+"/shot.png", "shot.png")},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if env.seen == nil {
		t.Fatal("agent was never invoked")
	}
	if !strings.Contains(env.seen.Message, "<media:image") {
		t.Errorf("Message = %q, want a media tag", env.seen.Message)
	}
}

func TestWebhookLLM_NoInputNoMedia_Returns400(t *testing.T) {
	env := newMediaTestEnv(t, nil)

	// Relaxing the input guard must not let a genuinely empty request through.
	for _, body := range []map[string]any{
		{},
		{"media": []map[string]any{}},
		{"input": ""},
	} {
		w := env.post(t, body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %+v: expected 400, got %d: %s", body, w.Code, w.Body.String())
		}
	}
	if env.seen != nil {
		t.Error("agent invoked for an empty request")
	}
}

// ---- failure mapping ----

func TestWebhookLLM_MediaTooLarge_Returns413(t *testing.T) {
	allowLoopbackMedia(t)
	srv := serveOversize(t)
	env := newMediaTestEnv(t, nil)

	w := env.post(t, map[string]any{
		"input": "hi",
		"media": []map[string]any{mediaItem(srv.URL+"/big.png", "big.png")},
	})

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", w.Code, w.Body.String())
	}
	if env.seen != nil {
		t.Error("agent invoked despite a failed media item")
	}
}

func TestWebhookLLM_MediaMimeDenied_Returns415(t *testing.T) {
	allowLoopbackMedia(t)
	srv := serveBadMIME(t)
	env := newMediaTestEnv(t, nil)

	w := env.post(t, map[string]any{
		"input": "hi",
		"media": []map[string]any{mediaItem(srv.URL+"/page.html", "page.html")},
	})

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWebhookLLM_MediaSSRF_Returns400(t *testing.T) {
	// No loopback bypass: this asserts a private-range URL is refused.
	srv := servePNG(t)
	env := newMediaTestEnv(t, nil)

	w := env.post(t, map[string]any{
		"input": "hi",
		"media": []map[string]any{mediaItem(srv.URL+"/shot.png", "shot.png")},
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if env.seen != nil {
		t.Error("agent invoked for an SSRF-blocked URL")
	}
}

func TestWebhookLLM_MediaAnyFailure_FailsRequest(t *testing.T) {
	isolateTempDir(t)
	allowLoopbackMedia(t)
	before := countMediaTemps(t)

	good := servePNG(t)
	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer missing.Close()

	env := newMediaTestEnv(t, nil)
	w := env.post(t, map[string]any{
		"input": "hi",
		"media": []map[string]any{
			mediaItem(good.URL+"/a.png", "a.png"),
			mediaItem(missing.URL+"/b.png", "b.png"),
		},
	})

	if w.Code == http.StatusOK {
		t.Fatalf("expected an error status, got 200: %s", w.Body.String())
	}
	if env.seen != nil {
		t.Error("agent invoked despite one item failing — all-or-nothing violated")
	}
	if after := countMediaTemps(t); after != before {
		t.Errorf("temp files leaked: before=%d after=%d", before, after)
	}
}

// TestWebhookLLM_StatusIndependentOfArrayOrder pins the deterministic mapping:
// the status comes from the highest-severity failure across all items, not from
// whichever slot failed first.
func TestWebhookLLM_StatusIndependentOfArrayOrder(t *testing.T) {
	allowLoopbackMedia(t)
	oversize := serveOversize(t)
	badMIME := serveBadMIME(t)

	orders := [][]map[string]any{
		{mediaItem(oversize.URL+"/big.png", "big.png"), mediaItem(badMIME.URL+"/p.html", "p.html")},
		{mediaItem(badMIME.URL+"/p.html", "p.html"), mediaItem(oversize.URL+"/big.png", "big.png")},
	}
	for i, media := range orders {
		env := newMediaTestEnv(t, nil)
		w := env.post(t, map[string]any{"input": "hi", "media": media})
		// mime_denied outranks too_large, so both orders must yield 415.
		if w.Code != http.StatusUnsupportedMediaType {
			t.Errorf("order %d: expected 415, got %d: %s", i, w.Code, w.Body.String())
		}
	}
}

// TestWebhookLLM_FailureBodyLeaksNothing pins the closed-enum contract at the
// HTTP boundary: a caller must not be able to use this endpoint to learn
// whether an internal host exists, what it resolved to, or why a connection
// failed.
func TestWebhookLLM_FailureBodyLeaksNothing(t *testing.T) {
	env := newMediaTestEnv(t, nil)

	const metadataHost = "169.254.169.254"
	w := env.post(t, map[string]any{
		"input": "hi",
		"media": []map[string]any{mediaItem("http://"+metadataHost+"/latest/meta-data/", "x.png")},
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, leak := range []string{metadataHost, "169.254", "ssrf:", "meta-data", "dial", "connection refused"} {
		if strings.Contains(body, leak) {
			t.Errorf("response body leaks %q: %s", leak, body)
		}
	}
}

// ---- cleanup ownership ----

// TestWebhookLLM_CleanupSurvivesClientDisconnect is the test that catches a
// handler-level defer.
//
// lane.Submit runs its closure in a DETACHED goroutine. handleSync then selects
// on outCh or laneCtx.Done(), and laneCtx derives from the request context
// while the run itself uses context.WithoutCancel. A deferred cleanup in the
// handler therefore fires on client disconnect while ag.Run is still executing,
// and the agent spends the rest of its run answering about a <media:image> tag
// with no file behind it. With cleanup inside the closure, the file outlives
// the handler.
func TestWebhookLLM_CleanupSurvivesClientDisconnect(t *testing.T) {
	allowLoopbackMedia(t)
	srv := servePNG(t)

	agentUUID := uuid.New()
	wh := &store.WebhookData{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		AgentID:  &agentUUID,
		Kind:     "llm",
	}

	entered := make(chan string, 1) // media path, sent as the run starts
	proceed := make(chan struct{})  // closed once the handler has returned
	existed := make(chan bool, 1)   // whether the file survived
	ag := &stubLLMAgent{
		id:      agentUUID.String(),
		agentID: agentUUID,
		runFn: func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
			if len(req.Media) != 1 {
				entered <- ""
				return &agent.RunResult{Content: "ok"}, nil
			}
			entered <- req.Media[0].Path
			<-proceed
			_, err := os.Stat(req.Media[0].Path)
			existed <- err == nil
			return &agent.RunResult{Content: "ok"}, nil
		},
	}

	h := newTestLLMHandler(&llmCallStore{}, &msgWebhookStore{}, nil)
	h.agentRouter = stubRouterFor(agentUUID, ag)
	h.syncTimeout = 30 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	r := injectWebhook(buildLLMReq(t, map[string]any{
		"input": "hi",
		"media": []map[string]any{mediaItem(srv.URL+"/shot.png", "shot.png")},
	}), wh).WithContext(ctx)
	// injectWebhook built its context off the original request, so re-inject
	// onto the cancellable one.
	r = injectWebhook(r, wh)

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.handle(httptest.NewRecorder(), r)
	}()

	path := <-entered
	if path == "" {
		t.Fatal("run started with no media attached")
	}

	// Client goes away mid-run.
	cancel()
	<-done

	// The handler is gone; the run is not. The file must still be there.
	close(proceed)
	if !<-existed {
		t.Fatalf("media file %s was deleted while the agent run was still executing", path)
	}
}

// TestWebhookLLM_CleanupOnLaneSaturated covers the branch where the closure
// never runs, so its deferred cleanup never runs either.
func TestWebhookLLM_CleanupOnLaneSaturated(t *testing.T) {
	isolateTempDir(t)
	allowLoopbackMedia(t)
	before := countMediaTemps(t)
	srv := servePNG(t)

	// Occupy the lane's only slot with a job that never finishes within the
	// test, so Submit blocks until laneCtx's deadline fires and returns an
	// error. lane.Stop() would be racy here: Submit selects over both the
	// shutdown channel and an available token, and Go picks a ready case at
	// random.
	lane := scheduler.NewLane("saturated", 1)
	blocked := make(chan struct{})
	defer close(blocked)
	if err := lane.Submit(context.Background(), func() { <-blocked }); err != nil {
		t.Fatalf("occupying the lane: %v", err)
	}

	env := newMediaTestEnv(t, lane)
	env.h.syncTimeout = 50 * time.Millisecond

	w := env.post(t, map[string]any{
		"input": "hi",
		"media": []map[string]any{mediaItem(srv.URL+"/shot.png", "shot.png")},
	})

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
	if after := countMediaTemps(t); after != before {
		t.Errorf("temp files leaked on the lane-saturated path: before=%d after=%d", before, after)
	}
}

// TestWebhookLLM_CleanupOnIdempotencyReplay covers the third and last cleanup
// path. reserveIdempotentCall can return handled=true and write the response
// itself, so handleSync returns before lane.Submit — the closure that normally
// owns the temp files never runs.
func TestWebhookLLM_CleanupOnIdempotencyReplay(t *testing.T) {
	isolateTempDir(t)
	allowLoopbackMedia(t)
	before := countMediaTemps(t)
	srv := servePNG(t)

	env := newMediaTestEnv(t, nil)
	env.callStore.createErr = errors.New("idempotency reserve failed")

	r := injectWebhook(buildLLMReq(t, map[string]any{
		"input": "hi",
		"media": []map[string]any{mediaItem(srv.URL+"/shot.png", "shot.png")},
	}), env.wh)
	r.Header.Set("Idempotency-Key", "replayed-key")

	w := httptest.NewRecorder()
	env.h.handle(w, r)

	if w.Code == http.StatusOK {
		t.Fatalf("expected the reserve failure to short-circuit, got 200: %s", w.Body.String())
	}
	if env.seen != nil {
		t.Error("agent invoked despite the early return")
	}
	if after := countMediaTemps(t); after != before {
		t.Errorf("temp files stranded by the idempotency early return: before=%d after=%d", before, after)
	}
}

// TestWebhookLLM_MediaTagCarriesNoSignature is the end-to-end half of the
// redaction contract: what the agent actually receives must not contain the
// caller's presigned signature, because RunRequest.Message goes to the LLM
// provider and into session history.
func TestWebhookLLM_MediaTagCarriesNoSignature(t *testing.T) {
	allowLoopbackMedia(t)
	srv := servePNG(t)
	env := newMediaTestEnv(t, nil)

	w := env.post(t, map[string]any{
		"input": "what is this",
		"media": []map[string]any{mediaItem(srv.URL+"/shot.png?X-Amz-Signature=deadbeefcafe", "shot.png")},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if env.seen == nil {
		t.Fatal("agent was never invoked")
	}
	if strings.Contains(env.seen.Message, "deadbeefcafe") {
		t.Errorf("prompt carries the URL signature: %q", env.seen.Message)
	}
}

// ---- regressions ----

func TestWebhookLLM_NoMediaField_Unchanged(t *testing.T) {
	env := newMediaTestEnv(t, nil)

	w := env.post(t, map[string]any{"input": "plain text only"})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if env.seen == nil {
		t.Fatal("agent was never invoked")
	}
	if len(env.seen.Media) != 0 {
		t.Errorf("RunRequest.Media = %d, want 0", len(env.seen.Media))
	}
	if env.seen.Message != "plain text only" {
		t.Errorf("Message = %q, want the caller text verbatim with no tag prefix", env.seen.Message)
	}
}

// TestWebhookLLM_AsyncWithMedia_Returns400 — the second assertion is the one
// that matters. An enqueued row that can never succeed is exactly the failure
// mode this guard exists to prevent.
func TestWebhookLLM_AsyncWithMedia_Returns400(t *testing.T) {
	isolateTempDir(t)
	allowLoopbackMedia(t)
	srv := servePNG(t)
	before := countMediaTemps(t)
	env := newMediaTestEnv(t, nil)

	w := env.post(t, map[string]any{
		"input":        "hi",
		"mode":         "async",
		"callback_url": "https://example.com/cb",
		"media":        []map[string]any{mediaItem(srv.URL+"/shot.png", "shot.png")},
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if len(env.callStore.created) != 0 {
		t.Errorf("enqueued %d rows for a call that could never succeed", len(env.callStore.created))
	}
	if after := countMediaTemps(t); after != before {
		t.Errorf("media was downloaded for a rejected async request: before=%d after=%d", before, after)
	}
}

func TestWebhookLLM_AsyncWithoutMedia_StillEnqueues(t *testing.T) {
	env := newMediaTestEnv(t, nil)

	w := env.post(t, map[string]any{
		"input":        "hi",
		"mode":         "async",
		"callback_url": "https://example.com/cb",
	})

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	if len(env.callStore.created) != 1 {
		t.Errorf("created %d rows, want 1", len(env.callStore.created))
	}
}

// TestWebhookLLM_AuditPayloadRedactsMediaURL pins that a presigned URL's
// signature never lands in an audit row. request_payload is surfaced verbatim
// by GET /v1/webhooks/{id}/calls/{callId} and retained for 30 days, and a
// presigned URL is a bearer credential.
func TestWebhookLLM_AuditPayloadRedactsMediaURL(t *testing.T) {
	allowLoopbackMedia(t)
	srv := servePNG(t)
	env := newMediaTestEnv(t, nil)

	const secret = "X-Amz-Signature=deadbeefcafe"
	w := env.post(t, map[string]any{
		"input": "hi",
		"media": []map[string]any{mediaItem(srv.URL+"/shot.png?"+secret, "shot.png")},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(env.callStore.created) != 1 {
		t.Fatalf("created %d audit rows, want 1", len(env.callStore.created))
	}
	payload := string(env.callStore.created[0].RequestPayload)
	if strings.Contains(payload, "deadbeefcafe") {
		t.Errorf("audit payload retains the URL signature: %s", payload)
	}
	if !strings.Contains(payload, "shot.png") {
		t.Errorf("audit payload lost the URL path entirely: %s", payload)
	}
}
