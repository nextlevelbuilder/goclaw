package agent

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// deterministicAnswerProvider returns a fixed final answer with no tool calls, so
// a real Loop.Run reaches its success tail in exactly one iteration. It is used to
// prove the outer-delivery fence (Phase 7 closure item 1) at the real Loop.Run
// seam rather than through the ownsSession() unit predicate alone.
type deterministicAnswerProvider struct{ answer string }

func (p deterministicAnswerProvider) Chat(context.Context, providers.ChatRequest) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{Content: p.answer, FinishReason: "stop"}, nil
}

func (p deterministicAnswerProvider) ChatStream(_ context.Context, _ providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{Content: p.answer, FinishReason: "stop"}, nil
}

func (p deterministicAnswerProvider) DefaultModel() string { return "test-model" }
func (p deterministicAnswerProvider) Name() string         { return "test-provider" }

// recordingSessionStore is a thread-safe in-memory SessionStore that records the
// history-commit calls the production pipeline makes (AddMessage/Save), so a test
// can assert whether a run committed to the session.
type recordingSessionStore struct {
	mu       sync.Mutex
	messages map[string][]providers.Message
	saves    int
}

func newRecordingSessionStore() *recordingSessionStore {
	return &recordingSessionStore{messages: make(map[string][]providers.Message)}
}

func (s *recordingSessionStore) assistantCount(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.messages[key] {
		if m.Role == "assistant" {
			n++
		}
	}
	return n
}

func (s *recordingSessionStore) AddMessage(_ context.Context, key string, msg providers.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[key] = append(s.messages[key], msg)
}
func (s *recordingSessionStore) GetHistory(_ context.Context, key string) []providers.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]providers.Message, len(s.messages[key]))
	copy(out, s.messages[key])
	return out
}
func (s *recordingSessionStore) GetOrCreate(_ context.Context, key string) *store.SessionData {
	return &store.SessionData{Key: key}
}
func (s *recordingSessionStore) Get(context.Context, string) *store.SessionData          { return nil }
func (s *recordingSessionStore) GetSummary(context.Context, string) string               { return "" }
func (s *recordingSessionStore) SetSummary(context.Context, string, string)              {}
func (s *recordingSessionStore) GetLabel(context.Context, string) string                 { return "titled" }
func (s *recordingSessionStore) SetLabel(context.Context, string, string)                {}
func (s *recordingSessionStore) SetAgentInfo(context.Context, string, uuid.UUID, string) {}
func (s *recordingSessionStore) TruncateHistory(context.Context, string, int)            {}
func (s *recordingSessionStore) SetHistory(_ context.Context, key string, msgs []providers.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[key] = append([]providers.Message(nil), msgs...)
}
func (s *recordingSessionStore) Reset(context.Context, string)        {}
func (s *recordingSessionStore) Delete(context.Context, string) error { return nil }
func (s *recordingSessionStore) Save(_ context.Context, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saves++
	return nil
}
func (s *recordingSessionStore) UpdateMetadata(context.Context, string, string, string, string) {}
func (s *recordingSessionStore) AccumulateTokens(context.Context, string, int64, int64)         {}
func (s *recordingSessionStore) IncrementCompaction(context.Context, string)                    {}
func (s *recordingSessionStore) GetCompactionCount(context.Context, string) int                 { return 0 }
func (s *recordingSessionStore) GetMemoryFlushCompactionCount(context.Context, string) int      { return 0 }
func (s *recordingSessionStore) SetMemoryFlushDone(context.Context, string)                     {}
func (s *recordingSessionStore) GetSessionMetadata(context.Context, string) map[string]string {
	return nil
}
func (s *recordingSessionStore) SetSessionMetadata(context.Context, string, map[string]string) {}
func (s *recordingSessionStore) SetSpawnInfo(context.Context, string, string, int)             {}
func (s *recordingSessionStore) SetContextWindow(context.Context, string, int)                 {}
func (s *recordingSessionStore) GetContextWindow(context.Context, string) int                  { return 0 }
func (s *recordingSessionStore) SetLastPromptTokens(context.Context, string, int, int)         {}
func (s *recordingSessionStore) GetLastPromptTokens(context.Context, string) (int, int)        { return 0, 0 }
func (s *recordingSessionStore) List(context.Context, string) []store.SessionInfo              { return nil }
func (s *recordingSessionStore) ListPaged(context.Context, store.SessionListOpts) store.SessionListResult {
	return store.SessionListResult{}
}
func (s *recordingSessionStore) ListPagedRich(context.Context, store.SessionListOpts) store.SessionListRichResult {
	return store.SessionListRichResult{}
}
func (s *recordingSessionStore) LastUsedChannel(context.Context, string) (string, string) {
	return "", ""
}

// newDeliveryFenceLoop builds a real production Loop wired to a deterministic
// provider and a recording session store, with tracing/memory/tools left minimal
// so Loop.Run reaches its success tail through the real pipeline.
func newDeliveryFenceLoop(t *testing.T, sessions *recordingSessionStore) *Loop {
	t.Helper()
	return NewLoop(LoopConfig{
		ID:         "fence-agent",
		Provider:   deterministicAnswerProvider{answer: "the-answer"},
		Model:      "test-model",
		Sessions:   sessions,
		Workspace:  t.TempDir(),
		Tools:      tools.NewRegistry(),
		ToolPolicy: tools.NewPolicyEngine(&config.ToolsConfig{}),
	})
}

// TestLoopRun_OwnerReceivesSuccessResult proves the success tail is unchanged for
// a run that still owns its session: it returns (result, nil), emits run.completed,
// and commits the assistant answer to history (Phase 7 closure item 1, owner path).
func TestLoopRun_OwnerReceivesSuccessResult(t *testing.T) {
	sessions := newRecordingSessionStore()
	loop := newDeliveryFenceLoop(t, sessions)
	col := &eventCollector{}
	loop.onEvent = col.onEvent

	sessionKey := "sess-owner"
	result, err := loop.Run(context.Background(), RunRequest{
		RunID:          "run-owner",
		SessionKey:     sessionKey,
		Message:        "hello",
		IsCurrentOwner: func() bool { return true },
	})
	if err != nil {
		t.Fatalf("owner run returned error: %v", err)
	}
	if result == nil || result.Content != "the-answer" {
		t.Fatalf("owner run result = %+v, want the-answer", result)
	}
	if got := len(col.filter(protocol.AgentEventRunCompleted)); got != 1 {
		t.Fatalf("run.completed events = %d, want exactly one for the owner run", got)
	}
	if got := sessions.assistantCount(sessionKey); got != 1 {
		t.Fatalf("assistant history entries = %d, want 1 (owner committed its answer)", got)
	}
}

// TestLoopRun_LostOwnershipAtTailReturnsTypedError proves the outer-delivery fence:
// a run that completed its work but lost session ownership before the success tail
// returns (nil, ErrRunOwnershipLost), emits NO run.completed, and commits nothing
// to history (Phase 7 closure item 1, zombie path). The typed error is what
// WS/inbound callers key on to suppress stale delivery.
func TestLoopRun_LostOwnershipAtTailReturnsTypedError(t *testing.T) {
	sessions := newRecordingSessionStore()
	loop := newDeliveryFenceLoop(t, sessions)
	col := &eventCollector{}
	loop.onEvent = col.onEvent

	sessionKey := "sess-zombie"
	result, err := loop.Run(context.Background(), RunRequest{
		RunID:          "run-zombie",
		SessionKey:     sessionKey,
		Message:        "hello",
		IsCurrentOwner: func() bool { return false }, // superseded before the tail
	})
	if !errors.Is(err, ErrRunOwnershipLost) {
		t.Fatalf("zombie run error = %v, want ErrRunOwnershipLost", err)
	}
	if result != nil {
		t.Fatalf("zombie run result = %+v, want nil (delivery suppressed)", result)
	}
	if got := len(col.filter(protocol.AgentEventRunCompleted)); got != 0 {
		t.Fatalf("run.completed events = %d, want 0 for a zombie run", got)
	}
	if got := sessions.assistantCount(sessionKey); got != 0 {
		t.Fatalf("assistant history entries = %d, want 0 (zombie must not commit)", got)
	}
}
