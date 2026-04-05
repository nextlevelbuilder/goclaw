package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ============================================================
// mockSessionStore — minimal mock implementing store.SessionStore
// ============================================================

type mockSessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*store.SessionData
}

func newMockSessionStore() *mockSessionStore {
	return &mockSessionStore{sessions: make(map[string]*store.SessionData)}
}

func (m *mockSessionStore) seed(key string, msgs []providers.Message, label string) {
	now := time.Now()
	m.sessions[key] = &store.SessionData{
		Key:      key,
		Messages: msgs,
		Label:    label,
		Created:  now,
		Updated:  now,
	}
}

func (m *mockSessionStore) GetOrCreate(_ context.Context, key string) *store.SessionData {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.sessions[key]; ok {
		return d
	}
	now := time.Now()
	d := &store.SessionData{Key: key, Messages: []providers.Message{}, Created: now, Updated: now}
	m.sessions[key] = d
	return d
}

func (m *mockSessionStore) Get(_ context.Context, key string) *store.SessionData {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[key]
}

func (m *mockSessionStore) AddMessage(_ context.Context, key string, msg providers.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.sessions[key]; ok {
		d.Messages = append(d.Messages, msg)
	}
}

func (m *mockSessionStore) GetHistory(_ context.Context, key string) []providers.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if d, ok := m.sessions[key]; ok {
		cp := make([]providers.Message, len(d.Messages))
		copy(cp, d.Messages)
		return cp
	}
	return nil
}

func (m *mockSessionStore) GetSummary(_ context.Context, key string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if d, ok := m.sessions[key]; ok {
		return d.Summary
	}
	return ""
}

func (m *mockSessionStore) SetSummary(_ context.Context, key, summary string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.sessions[key]; ok {
		d.Summary = summary
	}
}

func (m *mockSessionStore) GetLabel(_ context.Context, key string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if d, ok := m.sessions[key]; ok {
		return d.Label
	}
	return ""
}

func (m *mockSessionStore) SetLabel(_ context.Context, key, label string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.sessions[key]; ok {
		d.Label = label
	}
}

func (m *mockSessionStore) SetAgentInfo(context.Context, string, uuid.UUID, string) {}
func (m *mockSessionStore) TruncateHistory(context.Context, string, int)            {}
func (m *mockSessionStore) SetHistory(context.Context, string, []providers.Message) {}
func (m *mockSessionStore) Reset(context.Context, string)                           {}
func (m *mockSessionStore) Delete(context.Context, string) error                    { return nil }
func (m *mockSessionStore) Save(context.Context, string) error                      { return nil }

func (m *mockSessionStore) UpdateMetadata(context.Context, string, string, string, string) {}
func (m *mockSessionStore) AccumulateTokens(context.Context, string, int64, int64)         {}
func (m *mockSessionStore) IncrementCompaction(context.Context, string)                    {}
func (m *mockSessionStore) GetCompactionCount(context.Context, string) int                 { return 0 }
func (m *mockSessionStore) GetMemoryFlushCompactionCount(context.Context, string) int      { return 0 }
func (m *mockSessionStore) SetMemoryFlushDone(context.Context, string)                     {}
func (m *mockSessionStore) GetSessionMetadata(context.Context, string) map[string]string   { return nil }
func (m *mockSessionStore) SetSessionMetadata(context.Context, string, map[string]string)  {}
func (m *mockSessionStore) SetSpawnInfo(context.Context, string, string, int)              {}
func (m *mockSessionStore) SetContextWindow(context.Context, string, int)                  {}
func (m *mockSessionStore) GetContextWindow(context.Context, string) int                   { return 0 }
func (m *mockSessionStore) SetLastPromptTokens(context.Context, string, int, int)          {}
func (m *mockSessionStore) GetLastPromptTokens(context.Context, string) (int, int)         { return 0, 0 }

func (m *mockSessionStore) List(_ context.Context, agentID string) []store.SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []store.SessionInfo
	prefix := "agent:" + agentID + ":"
	for _, d := range m.sessions {
		if agentID == "" || strings.HasPrefix(d.Key, prefix) {
			result = append(result, store.SessionInfo{
				Key:          d.Key,
				MessageCount: len(d.Messages),
				Created:      d.Created,
				Updated:      d.Updated,
				Label:        d.Label,
			})
		}
	}
	return result
}

func (m *mockSessionStore) ListPaged(context.Context, store.SessionListOpts) store.SessionListResult {
	return store.SessionListResult{}
}
func (m *mockSessionStore) ListPagedRich(context.Context, store.SessionListOpts) store.SessionListRichResult {
	return store.SessionListRichResult{}
}
func (m *mockSessionStore) LastUsedChannel(context.Context, string) (string, string) {
	return "", ""
}

// ============================================================
// test helpers
// ============================================================

func agentCtx(agentID string) context.Context {
	ctx := context.Background()
	uid, err := uuid.Parse(agentID)
	if err != nil {
		return ctx
	}
	ctx = store.WithAgentID(ctx, uid)
	// Session keys use agent_key, not UUID.  In production WithToolAgentKey
	// is set by the agent loop; tests must mirror this.
	return WithToolAgentKey(ctx, agentID)
}

func agentCtxWithSandbox(agentID, sandboxKey string) context.Context {
	ctx := agentCtx(agentID)
	return WithToolSandboxKey(ctx, sandboxKey)
}

const sessTestAgentID = "11111111-1111-1111-1111-111111111111"
const sessTestAgentID2 = "22222222-2222-2222-2222-222222222222"

// ============================================================
// sessions_list tests
// ============================================================

func TestSessionsList_ReturnsAgentSessions(t *testing.T) {
	ms := newMockSessionStore()
	ms.seed("agent:"+sessTestAgentID+":ws:direct:1", nil, "")
	ms.seed("agent:"+sessTestAgentID+":ws:direct:2", nil, "")
	ms.seed("agent:"+sessTestAgentID2+":ws:direct:1", nil, "")

	tool := NewSessionsListTool()
	tool.SetSessionStore(ms)

	res := tool.Execute(agentCtx(sessTestAgentID), map[string]any{})

	var out map[string]any
	json.Unmarshal([]byte(res.ForLLM), &out)

	count := int(out["count"].(float64))
	if count != 2 {
		t.Fatalf("expected 2 sessions for agent, got %d", count)
	}
}

func TestSessionsList_ActiveMinutesFilter(t *testing.T) {
	ms := newMockSessionStore()
	ms.seed("agent:"+sessTestAgentID+":ws:direct:1", nil, "")
	// Make one session old
	ms.sessions["agent:"+sessTestAgentID+":ws:direct:1"].Updated = time.Now().Add(-2 * time.Hour)
	ms.seed("agent:"+sessTestAgentID+":ws:direct:2", nil, "")

	tool := NewSessionsListTool()
	tool.SetSessionStore(ms)

	res := tool.Execute(agentCtx(sessTestAgentID), map[string]any{
		"active_minutes": float64(30),
	})

	var out map[string]any
	json.Unmarshal([]byte(res.ForLLM), &out)

	count := int(out["count"].(float64))
	if count != 1 {
		t.Fatalf("expected 1 active session, got %d", count)
	}
}

func TestSessionsList_LimitCapsResults(t *testing.T) {
	ms := newMockSessionStore()
	for i := range 5 {
		ms.seed("agent:"+sessTestAgentID+":ws:direct:"+string(rune('a'+i)), nil, "")
	}

	tool := NewSessionsListTool()
	tool.SetSessionStore(ms)

	res := tool.Execute(agentCtx(sessTestAgentID), map[string]any{
		"limit": float64(2),
	})

	var out map[string]any
	json.Unmarshal([]byte(res.ForLLM), &out)

	count := int(out["count"].(float64))
	if count != 2 {
		t.Fatalf("expected 2 sessions (limited), got %d", count)
	}
}

// ============================================================
// session_status tests
// ============================================================

func TestSessionStatus_ValidSession(t *testing.T) {
	ms := newMockSessionStore()
	key := "agent:" + sessTestAgentID + ":ws:direct:1"
	ms.seed(key, []providers.Message{{Role: "user", Content: "hello"}}, "my-label")
	ms.sessions[key].Model = "claude-3"
	ms.sessions[key].InputTokens = 100
	ms.sessions[key].OutputTokens = 50

	tool := NewSessionStatusTool()
	tool.SetSessionStore(ms)

	res := tool.Execute(agentCtx(sessTestAgentID), map[string]any{
		"session_key": key,
	})

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Messages: 1") {
		t.Fatalf("expected message count, got: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Model: claude-3") {
		t.Fatalf("expected model info, got: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Label: my-label") {
		t.Fatalf("expected label, got: %s", res.ForLLM)
	}
}

func TestSessionStatus_NonExistent_NoPhantom(t *testing.T) {
	ms := newMockSessionStore()

	tool := NewSessionStatusTool()
	tool.SetSessionStore(ms)

	key := "agent:" + sessTestAgentID + ":ws:direct:999"
	res := tool.Execute(agentCtx(sessTestAgentID), map[string]any{
		"session_key": key,
	})

	if !res.IsError {
		t.Fatal("expected error for non-existent session")
	}
	if !strings.Contains(res.ForLLM, "session not found") {
		t.Fatalf("expected 'session not found', got: %s", res.ForLLM)
	}

	// Verify no phantom session was created
	if ms.Get(context.Background(), key) != nil {
		t.Fatal("phantom session was created — Get should have been used, not GetOrCreate")
	}
}

func TestSessionStatus_CrossAgent_Denied(t *testing.T) {
	ms := newMockSessionStore()
	otherKey := "agent:" + sessTestAgentID2 + ":ws:direct:1"
	ms.seed(otherKey, nil, "")

	tool := NewSessionStatusTool()
	tool.SetSessionStore(ms)

	res := tool.Execute(agentCtx(sessTestAgentID), map[string]any{
		"session_key": otherKey,
	})

	if !res.IsError || !strings.Contains(res.ForLLM, "access denied") {
		t.Fatalf("expected access denied, got: %s", res.ForLLM)
	}
}

func TestSessionStatus_EmptyAgentID_Error(t *testing.T) {
	ms := newMockSessionStore()

	tool := NewSessionStatusTool()
	tool.SetSessionStore(ms)

	// Context with no agent ID
	res := tool.Execute(context.Background(), map[string]any{
		"session_key": "agent:x:ws:direct:1",
	})

	if !res.IsError || !strings.Contains(res.ForLLM, "agent context required") {
		t.Fatalf("expected 'agent context required', got: %s", res.ForLLM)
	}
}

// ============================================================
// sessions_history tests
// ============================================================

func TestSessionsHistory_ReturnsMessages(t *testing.T) {
	ms := newMockSessionStore()
	key := "agent:" + sessTestAgentID + ":ws:direct:1"
	ms.seed(key, []providers.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}, "")

	tool := NewSessionsHistoryTool()
	tool.SetSessionStore(ms)

	res := tool.Execute(agentCtx(sessTestAgentID), map[string]any{
		"session_key": key,
	})

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}

	var out map[string]any
	json.Unmarshal([]byte(res.ForLLM), &out)
	count := int(out["count"].(float64))
	if count != 2 {
		t.Fatalf("expected 2 messages, got %d", count)
	}
}

func TestSessionsHistory_FilterToolMessages(t *testing.T) {
	ms := newMockSessionStore()
	key := "agent:" + sessTestAgentID + ":ws:direct:1"
	ms.seed(key, []providers.Message{
		{Role: "user", Content: "hello"},
		{Role: "tool", Content: "tool result"},
		{Role: "assistant", Content: "response"},
	}, "")

	tool := NewSessionsHistoryTool()
	tool.SetSessionStore(ms)

	// Without include_tools
	res := tool.Execute(agentCtx(sessTestAgentID), map[string]any{
		"session_key": key,
	})
	var out map[string]any
	json.Unmarshal([]byte(res.ForLLM), &out)
	if int(out["count"].(float64)) != 2 {
		t.Fatalf("expected 2 messages (tool filtered), got %v", out["count"])
	}

	// With include_tools
	res = tool.Execute(agentCtx(sessTestAgentID), map[string]any{
		"session_key":   key,
		"include_tools": true,
	})
	json.Unmarshal([]byte(res.ForLLM), &out)
	if int(out["count"].(float64)) != 3 {
		t.Fatalf("expected 3 messages (with tools), got %v", out["count"])
	}
}

func TestSessionsHistory_LimitFromEnd(t *testing.T) {
	ms := newMockSessionStore()
	key := "agent:" + sessTestAgentID + ":ws:direct:1"
	var msgs []providers.Message
	for range 10 {
		msgs = append(msgs, providers.Message{Role: "user", Content: "msg"})
	}
	ms.seed(key, msgs, "")

	tool := NewSessionsHistoryTool()
	tool.SetSessionStore(ms)

	res := tool.Execute(agentCtx(sessTestAgentID), map[string]any{
		"session_key": key,
		"limit":       float64(3),
	})

	var out map[string]any
	json.Unmarshal([]byte(res.ForLLM), &out)
	if int(out["count"].(float64)) != 3 {
		t.Fatalf("expected 3 messages (limited), got %v", out["count"])
	}
}

func TestSessionsHistory_NonExistent_SafeJSON(t *testing.T) {
	ms := newMockSessionStore()

	tool := NewSessionsHistoryTool()
	tool.SetSessionStore(ms)

	// Use a key with special chars that would break naive string concat
	key := "agent:" + sessTestAgentID + `:ws:direct:"inject`

	res := tool.Execute(agentCtx(sessTestAgentID), map[string]any{
		"session_key": key,
	})

	if res.IsError {
		t.Fatalf("non-existent session should return empty, not error: %s", res.ForLLM)
	}

	// Verify it's valid JSON (no injection)
	var out map[string]any
	if err := json.Unmarshal([]byte(res.ForLLM), &out); err != nil {
		t.Fatalf("invalid JSON (possible injection): %v\nContent: %s", err, res.ForLLM)
	}

	if int(out["count"].(float64)) != 0 {
		t.Fatalf("expected 0 messages for non-existent session")
	}
}

func TestSessionsHistory_CrossAgent_Denied(t *testing.T) {
	ms := newMockSessionStore()
	otherKey := "agent:" + sessTestAgentID2 + ":ws:direct:1"
	ms.seed(otherKey, nil, "")

	tool := NewSessionsHistoryTool()
	tool.SetSessionStore(ms)

	res := tool.Execute(agentCtx(sessTestAgentID), map[string]any{
		"session_key": otherKey,
	})

	if !res.IsError || !strings.Contains(res.ForLLM, "access denied") {
		t.Fatalf("expected access denied, got: %s", res.ForLLM)
	}
}

func TestSessionsHistory_EmptyAgentID_Error(t *testing.T) {
	ms := newMockSessionStore()

	tool := NewSessionsHistoryTool()
	tool.SetSessionStore(ms)

	res := tool.Execute(context.Background(), map[string]any{
		"session_key": "agent:x:ws:direct:1",
	})

	if !res.IsError || !strings.Contains(res.ForLLM, "agent context required") {
		t.Fatalf("expected 'agent context required', got: %s", res.ForLLM)
	}
}

// ============================================================
// sessions_send tests
// ============================================================

func TestSessionsSend_ValidSend(t *testing.T) {
	ms := newMockSessionStore()
	targetKey := "agent:" + sessTestAgentID + ":ws:direct:target"
	ms.seed(targetKey, nil, "")

	msgBus := bus.New()
	tool := NewSessionsSendTool()
	tool.SetSessionStore(ms)
	tool.SetMessageBus(msgBus)

	currentSession := "agent:" + sessTestAgentID + ":ws:direct:current"
	ctx := agentCtxWithSandbox(sessTestAgentID, currentSession)

	res := tool.Execute(ctx, map[string]any{
		"session_key": targetKey,
		"message":     "hello target",
	})

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}

	// Verify valid JSON response
	var out map[string]any
	if err := json.Unmarshal([]byte(res.ForLLM), &out); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if out["status"] != "accepted" {
		t.Fatalf("expected accepted status, got: %v", out["status"])
	}

	// Verify message was published to bus
	ctx2, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	msg, ok := msgBus.ConsumeInbound(ctx2)
	if !ok {
		t.Fatal("expected inbound message on bus")
	}
	if msg.Content != "hello target" {
		t.Fatalf("bus message content mismatch: %q", msg.Content)
	}
	if msg.ChatID != targetKey {
		t.Fatalf("bus message chatID mismatch: %q", msg.ChatID)
	}
}

func TestSessionsSend_LabelResolution(t *testing.T) {
	ms := newMockSessionStore()
	key := "agent:" + sessTestAgentID + ":ws:direct:labeled"
	ms.seed(key, nil, "my-session")

	msgBus := bus.New()
	tool := NewSessionsSendTool()
	tool.SetSessionStore(ms)
	tool.SetMessageBus(msgBus)

	ctx := agentCtxWithSandbox(sessTestAgentID, "agent:"+sessTestAgentID+":ws:direct:other")

	res := tool.Execute(ctx, map[string]any{
		"label":   "my-session",
		"message": "hello",
	})

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestSessionsSend_LabelNotFound(t *testing.T) {
	ms := newMockSessionStore()

	msgBus := bus.New()
	tool := NewSessionsSendTool()
	tool.SetSessionStore(ms)
	tool.SetMessageBus(msgBus)

	res := tool.Execute(agentCtx(sessTestAgentID), map[string]any{
		"label":   "nonexistent",
		"message": "hello",
	})

	if !res.IsError || !strings.Contains(res.ForLLM, "no session found with label") {
		t.Fatalf("expected label not found error, got: %s", res.ForLLM)
	}
}

func TestSessionsSend_LabelResolution_NoPhantom(t *testing.T) {
	ms := newMockSessionStore()
	key := "agent:" + sessTestAgentID + ":ws:direct:1"
	ms.seed(key, nil, "other-label")

	msgBus := bus.New()
	tool := NewSessionsSendTool()
	tool.SetSessionStore(ms)
	tool.SetMessageBus(msgBus)

	initialCount := len(ms.sessions)

	// Search for non-existent label — should NOT create phantom sessions
	tool.Execute(agentCtx(sessTestAgentID), map[string]any{
		"label":   "nonexistent",
		"message": "hello",
	})

	if len(ms.sessions) != initialCount {
		t.Fatalf("phantom sessions created: had %d, now %d", initialCount, len(ms.sessions))
	}
}

func TestSessionsSend_CrossAgent_Denied(t *testing.T) {
	ms := newMockSessionStore()
	otherKey := "agent:" + sessTestAgentID2 + ":ws:direct:1"

	msgBus := bus.New()
	tool := NewSessionsSendTool()
	tool.SetSessionStore(ms)
	tool.SetMessageBus(msgBus)

	res := tool.Execute(agentCtx(sessTestAgentID), map[string]any{
		"session_key": otherKey,
		"message":     "hello",
	})

	if !res.IsError || !strings.Contains(res.ForLLM, "access denied") {
		t.Fatalf("expected access denied, got: %s", res.ForLLM)
	}
}

func TestSessionsSend_SelfSend_Blocked(t *testing.T) {
	ms := newMockSessionStore()
	key := "agent:" + sessTestAgentID + ":ws:direct:current"
	ms.seed(key, nil, "")

	msgBus := bus.New()
	tool := NewSessionsSendTool()
	tool.SetSessionStore(ms)
	tool.SetMessageBus(msgBus)

	// Set sandbox key to same session as target
	ctx := agentCtxWithSandbox(sessTestAgentID, key)

	res := tool.Execute(ctx, map[string]any{
		"session_key": key,
		"message":     "hello myself",
	})

	if !res.IsError || !strings.Contains(res.ForLLM, "cannot send to your own current session") {
		t.Fatalf("expected self-send block, got: %s", res.ForLLM)
	}
}

func TestSessionsSend_EmptyAgentID_Error(t *testing.T) {
	ms := newMockSessionStore()

	msgBus := bus.New()
	tool := NewSessionsSendTool()
	tool.SetSessionStore(ms)
	tool.SetMessageBus(msgBus)

	res := tool.Execute(context.Background(), map[string]any{
		"session_key": "agent:x:ws:direct:1",
		"message":     "hello",
	})

	if !res.IsError || !strings.Contains(res.ForLLM, "agent context required") {
		t.Fatalf("expected 'agent context required', got: %s", res.ForLLM)
	}
}

func TestSessionsSend_MissingMessage_Error(t *testing.T) {
	ms := newMockSessionStore()

	msgBus := bus.New()
	tool := NewSessionsSendTool()
	tool.SetSessionStore(ms)
	tool.SetMessageBus(msgBus)

	res := tool.Execute(agentCtx(sessTestAgentID), map[string]any{
		"session_key": "agent:" + sessTestAgentID + ":ws:direct:1",
	})

	if !res.IsError || !strings.Contains(res.ForLLM, "message is required") {
		t.Fatalf("expected 'message is required', got: %s", res.ForLLM)
	}
}

func TestSessionsSend_ResponseJSON_NoInjection(t *testing.T) {
	ms := newMockSessionStore()
	// Key with quotes that could break naive Sprintf
	key := `agent:` + sessTestAgentID + `:ws:direct:"quoted`
	ms.seed(key, nil, "")

	msgBus := bus.New()
	tool := NewSessionsSendTool()
	tool.SetSessionStore(ms)
	tool.SetMessageBus(msgBus)

	ctx := agentCtxWithSandbox(sessTestAgentID, "agent:"+sessTestAgentID+":ws:direct:other")

	res := tool.Execute(ctx, map[string]any{
		"session_key": key,
		"message":     "test",
	})

	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}

	// Verify valid JSON
	var out map[string]any
	if err := json.Unmarshal([]byte(res.ForLLM), &out); err != nil {
		t.Fatalf("invalid JSON (possible injection): %v\nContent: %s", err, res.ForLLM)
	}
}
