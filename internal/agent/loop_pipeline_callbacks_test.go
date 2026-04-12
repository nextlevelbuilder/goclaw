package agent

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

type callbackMockSessionStore struct {
	history []providers.Message

	addedMessages []providers.Message
	savedKeys     []string

	agentSessionKey string
	agentUUID       uuid.UUID
	agentUserID     string

	metadataSessionKey string
	metadataModel      string
	metadataProvider   string
	metadataChannel    string
}

func (m *callbackMockSessionStore) GetOrCreate(context.Context, string) *store.SessionData {
	return nil
}
func (m *callbackMockSessionStore) Get(context.Context, string) *store.SessionData { return nil }
func (m *callbackMockSessionStore) AddMessage(_ context.Context, _ string, msg providers.Message) {
	m.addedMessages = append(m.addedMessages, msg)
}
func (m *callbackMockSessionStore) GetHistory(context.Context, string) []providers.Message {
	out := make([]providers.Message, len(m.history))
	copy(out, m.history)
	return out
}
func (m *callbackMockSessionStore) GetSummary(context.Context, string) string  { return "" }
func (m *callbackMockSessionStore) SetSummary(context.Context, string, string) {}
func (m *callbackMockSessionStore) GetLabel(context.Context, string) string    { return "" }
func (m *callbackMockSessionStore) SetLabel(context.Context, string, string)   {}
func (m *callbackMockSessionStore) SetAgentInfo(_ context.Context, key string, agentUUID uuid.UUID, userID string) {
	m.agentSessionKey = key
	m.agentUUID = agentUUID
	m.agentUserID = userID
}
func (m *callbackMockSessionStore) TruncateHistory(context.Context, string, int) {}
func (m *callbackMockSessionStore) SetHistory(context.Context, string, []providers.Message) {
}
func (m *callbackMockSessionStore) Reset(context.Context, string)        {}
func (m *callbackMockSessionStore) Delete(context.Context, string) error { return nil }
func (m *callbackMockSessionStore) Save(_ context.Context, key string) error {
	m.savedKeys = append(m.savedKeys, key)
	return nil
}
func (m *callbackMockSessionStore) UpdateMetadata(_ context.Context, key, model, provider, channel string) {
	m.metadataSessionKey = key
	m.metadataModel = model
	m.metadataProvider = provider
	m.metadataChannel = channel
}
func (m *callbackMockSessionStore) AccumulateTokens(context.Context, string, int64, int64) {}
func (m *callbackMockSessionStore) IncrementCompaction(context.Context, string)            {}
func (m *callbackMockSessionStore) GetCompactionCount(context.Context, string) int         { return 0 }
func (m *callbackMockSessionStore) GetMemoryFlushCompactionCount(context.Context, string) int {
	return 0
}
func (m *callbackMockSessionStore) SetMemoryFlushDone(context.Context, string) {}
func (m *callbackMockSessionStore) GetSessionMetadata(context.Context, string) map[string]string {
	return nil
}
func (m *callbackMockSessionStore) SetSessionMetadata(context.Context, string, map[string]string) {}
func (m *callbackMockSessionStore) SetSpawnInfo(context.Context, string, string, int)             {}
func (m *callbackMockSessionStore) SetContextWindow(context.Context, string, int)                 {}
func (m *callbackMockSessionStore) GetContextWindow(context.Context, string) int                  { return 0 }
func (m *callbackMockSessionStore) SetLastPromptTokens(context.Context, string, int, int)         {}
func (m *callbackMockSessionStore) GetLastPromptTokens(context.Context, string) (int, int) {
	return 0, 0
}
func (m *callbackMockSessionStore) List(context.Context, string) []store.SessionInfo { return nil }
func (m *callbackMockSessionStore) ListPaged(context.Context, store.SessionListOpts) store.SessionListResult {
	return store.SessionListResult{}
}
func (m *callbackMockSessionStore) ListPagedRich(context.Context, store.SessionListOpts) store.SessionListRichResult {
	return store.SessionListRichResult{}
}
func (m *callbackMockSessionStore) LastUsedChannel(context.Context, string) (string, string) {
	return "", ""
}

type callbackMockProvider struct{}

func (callbackMockProvider) Chat(context.Context, providers.ChatRequest) (*providers.ChatResponse, error) {
	return nil, nil
}

func (callbackMockProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return nil, nil
}

func (callbackMockProvider) DefaultModel() string { return "mock-model" }
func (callbackMockProvider) Name() string         { return "mock-provider" }

func TestMakeBuildMessagesPersistsRunInputSnapshotOnce(t *testing.T) {
	t.Parallel()

	sessionStore := &callbackMockSessionStore{
		history: []providers.Message{{Role: "assistant", Content: "existing"}},
	}
	agentID := uuid.New()
	loop := &Loop{
		id:            "coder",
		displayName:   "Coder",
		agentUUID:     agentID,
		agentType:     store.AgentTypeOpen,
		model:         "gpt-test",
		provider:      callbackMockProvider{},
		contextWindow: 200_000,
		sessions:      sessionStore,
		tools:         tools.NewRegistry(),
	}

	buildMessages := loop.makeBuildMessages()
	input := &pipeline.RunInput{
		SessionKey: "agent:coder:ws:direct:test-chat",
		Message:    "persist me",
		UserID:     "user-1",
		Channel:    "ws",
		PeerKind:   "direct",
	}

	msgs, err := buildMessages(context.Background(), input, sessionStore.history, "")
	if err != nil {
		t.Fatalf("buildMessages() error = %v", err)
	}
	if len(msgs) == 0 || msgs[len(msgs)-1].Role != "user" || msgs[len(msgs)-1].Content != "persist me" {
		t.Fatalf("current user message missing from built prompt: %#v", msgs)
	}

	_, err = buildMessages(context.Background(), input, sessionStore.history, "")
	if err != nil {
		t.Fatalf("second buildMessages() error = %v", err)
	}

	if len(sessionStore.addedMessages) != 1 {
		t.Fatalf("persisted messages = %d, want 1", len(sessionStore.addedMessages))
	}
	if sessionStore.addedMessages[0].Role != "user" || sessionStore.addedMessages[0].Content != "persist me" {
		t.Fatalf("persisted message = %#v", sessionStore.addedMessages[0])
	}
	if sessionStore.agentSessionKey != input.SessionKey || sessionStore.agentUUID != agentID || sessionStore.agentUserID != input.UserID {
		t.Fatalf("agent info not persisted correctly: key=%q agent=%q user=%q", sessionStore.agentSessionKey, sessionStore.agentUUID, sessionStore.agentUserID)
	}
	if sessionStore.metadataSessionKey != input.SessionKey || sessionStore.metadataModel != "gpt-test" || sessionStore.metadataProvider != "mock-provider" || sessionStore.metadataChannel != "ws" {
		t.Fatalf("metadata not persisted correctly: key=%q model=%q provider=%q channel=%q", sessionStore.metadataSessionKey, sessionStore.metadataModel, sessionStore.metadataProvider, sessionStore.metadataChannel)
	}
	if len(sessionStore.savedKeys) != 1 || sessionStore.savedKeys[0] != input.SessionKey {
		t.Fatalf("save calls = %#v, want [%q]", sessionStore.savedKeys, input.SessionKey)
	}
}
