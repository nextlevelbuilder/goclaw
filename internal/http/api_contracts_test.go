package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func TestMemoryHandler_ResolvesAgentKeyBeforeStore(t *testing.T) {
	setupTestToken(t, "")
	setupTestNoAuthFallback(t, true)
	agentID := uuid.New()
	mem := &recordingMemoryStore{}
	h := &MemoryHandler{
		store:  mem,
		agents: &memoryAgentResolver{id: agentID},
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/agents/goclaw/memory/documents", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if mem.listAllAgentID != agentID.String() {
		t.Fatalf("agentID passed to store = %q, want %q", mem.listAllAgentID, agentID.String())
	}
}

func TestMemoryHandler_InvalidAgentIDReturnsStructured400(t *testing.T) {
	setupTestToken(t, "")
	setupTestNoAuthFallback(t, true)
	h := &MemoryHandler{store: &recordingMemoryStore{}}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/agents/not-a-uuid/memory/documents", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json error envelope: %v", err)
	}
	if body["error"].Code != protocol.ErrInvalidRequest {
		t.Fatalf("error code = %q, want %q", body["error"].Code, protocol.ErrInvalidRequest)
	}
}

func TestSessionsHandler_AdminAPIContextCanListWithoutUserHeader(t *testing.T) {
	setupTestToken(t, "")
	setupTestNoAuthFallback(t, true)
	sessions := &recordingSessionStore{
		result: store.SessionListRichResult{
			Sessions: []store.SessionInfoRich{{SessionInfo: store.SessionInfo{Key: "agent:goclaw:ws:abc"}}},
			Total:    1,
		},
	}
	mux := http.NewServeMux()
	NewSessionsHandler(sessions, nil).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions?limit=3&offset=1&agentId=goclaw", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if sessions.opts.UserID != "" {
		t.Fatalf("UserID filter = %q, want empty for admin context", sessions.opts.UserID)
	}
	if sessions.opts.AgentID != "goclaw" || sessions.opts.Limit != 3 || sessions.opts.Offset != 1 {
		t.Fatalf("opts = %+v", sessions.opts)
	}
}

func TestSessionsHandler_ReadAPIKeyWithoutUserHeaderIsRejected(t *testing.T) {
	token := "read-key"
	setupTestToken(t, "")
	setupTestNoAuthFallback(t, false)
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {ID: uuid.New(), Scopes: []string{"operator.read"}},
	})
	mux := http.NewServeMux()
	NewSessionsHandler(&recordingSessionStore{}, nil).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRegisterAPINotFoundRoute_UsesStructuredError(t *testing.T) {
	mux := http.NewServeMux()
	RegisterAPINotFoundRoute(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/tools/custom", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json error envelope: %v", err)
	}
	if body["error"].Code != protocol.ErrNotFound {
		t.Fatalf("error code = %q, want %q", body["error"].Code, protocol.ErrNotFound)
	}
}

type memoryAgentResolver struct {
	id uuid.UUID
}

func (r *memoryAgentResolver) GetByKey(context.Context, string) (*store.AgentData, error) {
	return &store.AgentData{BaseModel: store.BaseModel{ID: r.id}, AgentKey: "goclaw"}, nil
}

type recordingMemoryStore struct {
	listAllAgentID string
}

func (s *recordingMemoryStore) GetDocument(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (s *recordingMemoryStore) PutDocument(context.Context, string, string, string, string) error {
	return nil
}
func (s *recordingMemoryStore) DeleteDocument(context.Context, string, string, string) error {
	return nil
}
func (s *recordingMemoryStore) ListDocuments(context.Context, string, string) ([]store.DocumentInfo, error) {
	return nil, nil
}
func (s *recordingMemoryStore) ListAllDocumentsGlobal(context.Context) ([]store.DocumentInfo, error) {
	return nil, nil
}
func (s *recordingMemoryStore) ListAllDocuments(_ context.Context, agentID string) ([]store.DocumentInfo, error) {
	s.listAllAgentID = agentID
	return []store.DocumentInfo{}, nil
}
func (s *recordingMemoryStore) GetDocumentDetail(context.Context, string, string, string) (*store.DocumentDetail, error) {
	return nil, nil
}
func (s *recordingMemoryStore) ListChunks(context.Context, string, string, string) ([]store.ChunkInfo, error) {
	return nil, nil
}
func (s *recordingMemoryStore) Search(context.Context, string, string, string, store.MemorySearchOptions) ([]store.MemorySearchResult, error) {
	return nil, nil
}
func (s *recordingMemoryStore) IndexDocument(context.Context, string, string, string) error {
	return nil
}
func (s *recordingMemoryStore) IndexAll(context.Context, string, string) error { return nil }
func (s *recordingMemoryStore) SetEmbeddingProvider(store.EmbeddingProvider)   {}
func (s *recordingMemoryStore) Close() error                                   { return nil }

type recordingSessionStore struct {
	opts   store.SessionListOpts
	result store.SessionListRichResult
}

func (s *recordingSessionStore) GetOrCreate(context.Context, string) *store.SessionData {
	return nil
}
func (s *recordingSessionStore) Get(context.Context, string) *store.SessionData        { return nil }
func (s *recordingSessionStore) AddMessage(context.Context, string, providers.Message) {}
func (s *recordingSessionStore) GetHistory(context.Context, string) []providers.Message {
	return nil
}
func (s *recordingSessionStore) GetSummary(context.Context, string) string               { return "" }
func (s *recordingSessionStore) SetSummary(context.Context, string, string)              {}
func (s *recordingSessionStore) GetLabel(context.Context, string) string                 { return "" }
func (s *recordingSessionStore) SetLabel(context.Context, string, string)                {}
func (s *recordingSessionStore) SetAgentInfo(context.Context, string, uuid.UUID, string) {}
func (s *recordingSessionStore) TruncateHistory(context.Context, string, int)            {}
func (s *recordingSessionStore) SetHistory(context.Context, string, []providers.Message) {}
func (s *recordingSessionStore) BranchSession(context.Context, string, store.SessionBranchOpts) (*store.SessionData, int, error) {
	return nil, 0, store.ErrSessionNotFound
}
func (s *recordingSessionStore) Reset(context.Context, string)        {}
func (s *recordingSessionStore) Delete(context.Context, string) error { return nil }
func (s *recordingSessionStore) Save(context.Context, string) error   { return nil }
func (s *recordingSessionStore) UpdateMetadata(context.Context, string, string, string, string) {
}
func (s *recordingSessionStore) AccumulateTokens(context.Context, string, int64, int64) {}
func (s *recordingSessionStore) IncrementCompaction(context.Context, string)            {}
func (s *recordingSessionStore) GetCompactionCount(context.Context, string) int         { return 0 }
func (s *recordingSessionStore) GetMemoryFlushCompactionCount(context.Context, string) int {
	return 0
}
func (s *recordingSessionStore) SetMemoryFlushDone(context.Context, string) {}
func (s *recordingSessionStore) GetSessionMetadata(context.Context, string) map[string]string {
	return nil
}
func (s *recordingSessionStore) SetSessionMetadata(context.Context, string, map[string]string) {}
func (s *recordingSessionStore) SetSpawnInfo(context.Context, string, string, int)             {}
func (s *recordingSessionStore) SetContextWindow(context.Context, string, int)                 {}
func (s *recordingSessionStore) GetContextWindow(context.Context, string) int                  { return 0 }
func (s *recordingSessionStore) SetLastPromptTokens(context.Context, string, int, int)         {}
func (s *recordingSessionStore) GetLastPromptTokens(context.Context, string) (int, int) {
	return 0, 0
}
func (s *recordingSessionStore) List(context.Context, string) []store.SessionInfo { return nil }
func (s *recordingSessionStore) ListPaged(context.Context, store.SessionListOpts) store.SessionListResult {
	return store.SessionListResult{}
}
func (s *recordingSessionStore) ListPagedRich(_ context.Context, opts store.SessionListOpts) store.SessionListRichResult {
	s.opts = opts
	return s.result
}
func (s *recordingSessionStore) LastUsedChannel(context.Context, string) (string, string) {
	return "", ""
}

var _ store.MemoryStore = (*recordingMemoryStore)(nil)
var _ store.SessionStore = (*recordingSessionStore)(nil)
