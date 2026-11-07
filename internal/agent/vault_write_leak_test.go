package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-rod/rod/lib/launcher"
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/browser"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// leakVaultStore is an in-memory vault with the agent-write rules of the real
// stores. It is the intended destination of the value, so it is not scanned;
// its metadata and audit rows are.
type leakVaultStore struct {
	store.SecretVaultStore
	mu      sync.Mutex
	entries map[string]store.SecretVaultEntry
	values  map[string]store.SecretVaultPayload
	grants  map[uuid.UUID][]uuid.UUID
	audit   []store.SecretVaultAuditEntry
}

func newLeakVaultStore() *leakVaultStore {
	return &leakVaultStore{
		entries: map[string]store.SecretVaultEntry{},
		values:  map[string]store.SecretVaultPayload{},
		grants:  map[uuid.UUID][]uuid.UUID{},
	}
}

func (s *leakVaultStore) Create(_ context.Context, e *store.SecretVaultEntry, p store.SecretVaultPayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[e.Name]; ok {
		return store.ErrSecretVaultNameTaken
	}
	e.ID = uuid.New()
	if e.CreatedByAgent != nil {
		e.Status, e.CreatedVia = store.SecretVaultStatusPending, store.SecretVaultCreatedViaAgent
		s.audit = append(s.audit, store.SecretVaultAuditEntry{EntryID: e.ID, EntryName: e.Name, AgentID: e.CreatedByAgent,
			Action: store.SecretAuditCreated, Outcome: store.SecretAuditOutcomeAgentPending})
	}
	s.entries[e.Name] = *e
	s.values[e.Name] = p
	return nil
}

func (s *leakVaultStore) SetGrants(_ context.Context, id uuid.UUID, agents []uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grants[id] = agents
	return nil
}

func (s *leakVaultStore) AppendAudit(_ context.Context, a *store.SecretVaultAuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audit = append(s.audit, *a)
	return nil
}

func (s *leakVaultStore) List(context.Context) ([]store.SecretVaultEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.SecretVaultEntry
	for _, e := range s.entries {
		out = append(out, e)
	}
	return out, nil
}

func (s *leakVaultStore) ListGrantedSecrets(context.Context, uuid.UUID) ([]store.SecretVaultResolved, error) {
	return nil, nil
}

func (s *leakVaultStore) stored(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[name].Secret
}

// leakSessionStore records what the run persists to session history.
type leakSessionStore struct {
	store.SessionStore
	mu   sync.Mutex
	msgs []providers.Message
}

func (s *leakSessionStore) AddMessage(_ context.Context, _ string, msg providers.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, msg)
}

// fakeJWT returns a random JWT-shaped value of exactly n characters with
// three base64url segments, none of which appears anywhere by chance.
func fakeJWT(t *testing.T, n int) string {
	t.Helper()
	seg := func(chars int) string {
		b := make([]byte, chars)
		if _, err := rand.Read(b); err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)[:chars]
	}
	header, sig := seg(36), seg(43)
	return header + "." + seg(n-len(header)-len(sig)-2) + "." + sig
}

// leakNeedles lists every form of value the sinks must not contain: raw,
// JSON-escaped, base64 (all four alphabets) and each dot-separated segment.
func leakNeedles(value string) map[string]string {
	out := map[string]string{"raw": value}
	if b, err := json.Marshal(value); err == nil {
		out["json"] = string(b[1 : len(b)-1])
	}
	raw := []byte(value)
	out["base64 std"] = base64.StdEncoding.EncodeToString(raw)
	out["base64 url"] = base64.URLEncoding.EncodeToString(raw)
	out["base64 raw std"] = base64.RawStdEncoding.EncodeToString(raw)
	out["base64 raw url"] = base64.RawURLEncoding.EncodeToString(raw)
	for i, part := range strings.Split(value, ".") {
		out[fmt.Sprintf("segment %d", i)] = part
	}
	return out
}

// assertNoLeak fails (without printing the value) when any sink holds any
// form of the value.
func assertNoLeak(t *testing.T, value string, sinks map[string]string) {
	t.Helper()
	for sink, text := range sinks {
		for form, needle := range leakNeedles(value) {
			if needle != "" && strings.Contains(text, needle) {
				t.Errorf("sink %q contains the %s form of the secret", sink, form)
			}
		}
	}
}

func mustJSONText(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type leakHarness struct {
	loop     *Loop
	req      *RunRequest
	ctx      context.Context
	events   *eventCollector
	vault    *leakVaultStore
	sessions *leakSessionStore
	stop     func()
	spans    *spanCaptureStore
	logs     *bytes.Buffer
}

// newLeakHarness wires a Loop the way the resolver does for an agent with
// vault_agent_write on: vault runtime, run redactor, tracing, events and a
// session store, around the given tools.
func newLeakHarness(t *testing.T, ts ...tools.Tool) *leakHarness {
	t.Helper()
	var logs bytes.Buffer
	var logMu sync.Mutex
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&lockedBuffer{b: &logs, mu: &logMu}, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	reg := tools.NewRegistry()
	for _, tool := range ts {
		reg.Register(tool)
	}
	h := &leakHarness{events: &eventCollector{}, vault: newLeakVaultStore(), sessions: &leakSessionStore{}, logs: &logs}
	tenant := uuid.New()
	h.loop = &Loop{
		id: "capture-agent", agentUUID: uuid.New(), tenantID: tenant,
		tools: reg, onEvent: h.events.onEvent, sessions: h.sessions,
		secretVault: h.vault, secretVaultWrite: true,
	}
	h.req = &RunRequest{RunID: "run-leak", SessionKey: "sess-leak", UserID: "user-1", Channel: "ws"}

	ctx, c, cs := newCaptureCtx(t)
	h.stop, h.spans = c.Stop, cs
	// What injectContext puts in every agent run.
	ctx = store.WithTenantID(ctx, tenant)
	ctx = store.WithUserID(ctx, h.req.UserID)
	ctx = store.WithAgentID(ctx, h.loop.agentUUID)
	ctx = store.WithAgentKey(ctx, h.loop.id)
	ctx = h.loop.withSecretVault(ctx, h.req)
	h.ctx = withDelegationArtifactTextRedactor(ctx, h.req)
	if secretvault.RuntimeFrom(h.ctx) == nil || !secretvault.RuntimeFrom(h.ctx).CanWrite() {
		t.Fatal("run must have a writable vault runtime")
	}
	return h
}

type lockedBuffer struct {
	b  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

// call runs one tool call through the real sequential tool callback (events,
// tool span, execution, result processing) and persists the assistant turn
// and the tool messages through the real session flush.
func (h *leakHarness) call(t *testing.T, id, name string, args map[string]any) []providers.Message {
	t.Helper()
	tc := providers.ToolCall{ID: id, Name: name, Arguments: args}
	msgs, err := h.loop.makeExecuteToolCall(h.req, &runState{})(h.ctx, &pipeline.RunState{RunID: h.req.RunID}, tc)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	assistant := providers.Message{Role: "assistant", ToolCalls: []providers.ToolCall{tc}}
	if err := h.loop.makeFlushMessages(h.req)(h.ctx, h.req.SessionKey, append([]providers.Message{assistant}, msgs...)); err != nil {
		t.Fatal(err)
	}
	return msgs
}

// sinks collects everything the run produced outside the vault itself.
func (h *leakHarness) sinks(t *testing.T, results []providers.Message) map[string]string {
	t.Helper()
	h.stop() // flush the trace collector

	h.events.mu.Lock()
	events := append([]AgentEvent(nil), h.events.events...)
	h.events.mu.Unlock()
	var timeline []store.RunTimelineItem
	for i, ev := range events {
		if item, ok := runTimelineItemFromEvent(ev, i+1); ok {
			timeline = append(timeline, item)
		}
	}
	h.spans.mu.Lock()
	spans, updates := mustJSONText(t, h.spans.spans), mustJSONText(t, h.spans.updates)
	h.spans.mu.Unlock()
	h.sessions.mu.Lock()
	session := mustJSONText(t, h.sessions.msgs)
	h.sessions.mu.Unlock()
	entries, _ := h.vault.List(context.Background())
	h.vault.mu.Lock()
	audit := mustJSONText(t, h.vault.audit)
	h.vault.mu.Unlock()

	return map[string]string{
		"tool results":     mustJSONText(t, results),
		"events":           mustJSONText(t, events),
		"run timeline":     mustJSONText(t, timeline),
		"trace spans":      spans,
		"trace updates":    updates,
		"session history":  session,
		"slog output":      h.logs.String(),
		"vault list (API)": mustJSONText(t, entries),
		"vault audit":      audit,
	}
}

// A 900 character JWT kept in localStorage is captured through the real
// browser tool in a real headless Chrome and must appear in no transcript,
// log, trace, event or response, in any common encoding.
func TestVaultWriteLeak_CaptureSecretKeepsAJWTOutOfEverySink(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real headless Chrome")
	}
	if _, ok := launcher.LookPath(); !ok {
		t.Skip("no Chrome or Chromium installed")
	}
	jwt := fakeJWT(t, 900)
	if len(jwt) != 900 || strings.Count(jwt, ".") != 2 {
		t.Fatalf("bad fixture: len=%d", len(jwt))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><title>App</title><script>localStorage.setItem("access_token", %q)</script><p>signed in</p>`, jwt)
	}))
	defer srv.Close()

	mgr := browser.New(browser.WithHeadless(true))
	defer mgr.Close()
	h := newLeakHarness(t, browser.NewBrowserTool(mgr), tools.NewVaultSecretSetTool())
	if err := mgr.Start(h.ctx); err != nil {
		t.Skipf("Chrome cannot be started here: %v", err)
	}

	var results []providers.Message
	opened := h.call(t, "tc-open", "browser", map[string]any{"action": "open", "targetUrl": srv.URL})
	results = append(results, opened...)
	var tab struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal([]byte(opened[0].Content), &tab); err != nil || tab.TargetID == "" {
		t.Fatalf("open failed: %s", opened[0].Content)
	}

	captured := h.call(t, "tc-capture", "browser", map[string]any{
		"action": "act", "targetId": tab.TargetID,
		"request": map[string]any{"kind": "capture_secret", "source": "localStorage", "key": "access_token", "secret_name": "app-jwt"},
	})
	results = append(results, captured...)
	// The loop's credential scrubber rewrites "secret:<name>}}" in tool
	// results, so only the status is asserted at this level.
	if captured[0].IsError || !strings.Contains(captured[0].Content, "status: pending") {
		t.Fatal("capture_secret did not report a pending entry")
	}
	if h.vault.stored("app-jwt") != jwt {
		t.Fatal("the vault did not receive the page value")
	}
	e := h.vault.entries["app-jwt"]
	if e.Status != store.SecretVaultStatusPending || len(e.AllowedDomains) != 1 || e.AllowedDomains[0] != "127.0.0.1" {
		t.Fatalf("entry status=%s domains=%v", e.Status, e.AllowedDomains)
	}

	// Ask for it again under the same name: refused, and the error is scanned too.
	again := h.call(t, "tc-again", "browser", map[string]any{
		"action": "act", "targetId": tab.TargetID,
		"request": map[string]any{"kind": "capture_secret", "source": "localStorage", "key": "access_token", "secret_name": "app-jwt"},
	})
	results = append(results, again...)
	if !again[0].IsError {
		t.Fatal("an existing name must be refused")
	}

	assertNoLeak(t, jwt, h.sinks(t, results))
}

// vault_secret_set receives the value as an argument. It is registered with
// the run redactor before the tool.call event and the tool span are emitted,
// so the trace input preview, the event, the persisted run timeline and the
// stored assistant turn hold only the mask.
func TestVaultWriteLeak_VaultSecretSetArgumentIsMaskedBeforeEmit(t *testing.T) {
	value := fakeJWT(t, 900)
	h := newLeakHarness(t, tools.NewVaultSecretSetTool())
	results := h.call(t, "tc-set", "vault_secret_set", map[string]any{
		"name": "api-jwt", "value": value, "domains": []any{"api.example.com"}, "scope": "vault",
	})
	if results[0].IsError || !strings.Contains(results[0].Content, "status: pending") {
		t.Fatal("vault_secret_set did not report a pending entry")
	}
	if h.vault.stored("api-jwt") != value {
		t.Fatal("the vault did not receive the value")
	}
	sinks := h.sinks(t, results)
	assertNoLeak(t, value, sinks)

	// The pre-execution sinks exist and carry the mask, so the scan above
	// really covered them.
	var calls int
	for _, ev := range h.events.filter(protocol.AgentEventToolCall) {
		calls++
		if !strings.Contains(mustJSONText(t, ev.Payload), secretvault.Mask) {
			t.Error("tool.call event arguments must carry the mask")
		}
		if item, ok := runTimelineItemFromEvent(ev, 1); !ok || !strings.Contains(item.Preview, secretvault.Mask) {
			t.Error("run timeline tool.call preview must carry the mask")
		}
	}
	if calls != 1 {
		t.Fatalf("tool.call events = %d", calls)
	}
	h.spans.mu.Lock()
	defer h.spans.mu.Unlock()
	var toolSpans int
	for _, s := range h.spans.spans {
		if s.SpanType == store.SpanTypeToolCall {
			toolSpans++
			if !strings.Contains(s.InputPreview, secretvault.Mask) {
				t.Error("tool span input preview must carry the mask")
			}
		}
	}
	if toolSpans != 1 {
		t.Fatalf("tool spans = %d", toolSpans)
	}
}

// toolCallsProvider answers every request with the given tool calls.
type toolCallsProvider struct {
	calls  []providers.ToolCall
	finish string
}

func (p toolCallsProvider) Chat(context.Context, providers.ChatRequest) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{ToolCalls: p.calls, FinishReason: p.finish}, nil
}

func (p toolCallsProvider) ChatStream(ctx context.Context, req providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return p.Chat(ctx, req)
}
func (toolCallsProvider) DefaultModel() string { return "stub-model" }
func (toolCallsProvider) Name() string         { return "stubprov" }

// The model can emit the value in two calls of one batch: vault_secret_set and
// a sibling that runs first (in parallel or earlier in order). The value is
// registered as soon as the response arrives, so the sibling's event and
// span, and the LLM span of the response, already carry the mask.
func TestVaultWriteLeak_ValueInASiblingCallIsMaskedBeforeItRuns(t *testing.T) {
	value := fakeJWT(t, 300)
	h := newLeakHarness(t, tools.NewVaultSecretSetTool())
	sibling := providers.ToolCall{ID: "tc-echo", Name: "web_fetch", Arguments: map[string]any{"url": "https://api.example.com/?t=" + value}}
	set := providers.ToolCall{ID: "tc-set", Name: "vault_secret_set", Arguments: map[string]any{
		"name": "api-jwt", "value": value, "domains": []any{"api.example.com"}, "scope": "vault",
	}}
	state := &pipeline.RunState{RunID: h.req.RunID, Provider: toolCallsProvider{calls: []providers.ToolCall{sibling, set}, finish: "tool_calls"}, Model: "stub-model"}
	if _, err := h.loop.makeCallLLM(h.req, func(AgentEvent) {})(h.ctx, state, providers.ChatRequest{}); err != nil {
		t.Fatal(err)
	}

	// The sibling runs before vault_secret_set, through the parallel path.
	msg, _, err := h.loop.makeExecuteToolRaw(h.req)(h.ctx, sibling)
	if err != nil {
		t.Fatal(err)
	}
	sinks := h.sinks(t, []providers.Message{msg})
	assertNoLeak(t, value, sinks)
	if !strings.Contains(sinks["events"], secretvault.Mask) || !strings.Contains(sinks["trace spans"]+sinks["trace updates"], secretvault.Mask) {
		t.Fatal("the sibling event and the spans must carry the mask")
	}
}

// A vault_secret_set call dropped as truncated never runs; whatever part of
// its value the provider parsed is still registered.
func TestVaultWriteLeak_TruncatedCallValueIsRegistered(t *testing.T) {
	value := fakeJWT(t, 300)
	h := newLeakHarness(t)
	set := providers.ToolCall{ID: "tc-set", Name: "vault_secret_set", Arguments: map[string]any{"name": "api-jwt", "value": value}}
	state := &pipeline.RunState{RunID: h.req.RunID, Provider: toolCallsProvider{calls: []providers.ToolCall{set}, finish: "length"}, Model: "stub-model"}
	if _, err := h.loop.makeCallLLM(h.req, func(AgentEvent) {})(h.ctx, state, providers.ChatRequest{}); err != nil {
		t.Fatal(err)
	}
	if got := secretvault.RuntimeFrom(h.ctx).Redactor.Redact("x " + value); strings.Contains(got, value) {
		t.Fatal("the value of a call that will not run must still be registered")
	}
	assertNoLeak(t, value, h.sinks(t, nil))
}
