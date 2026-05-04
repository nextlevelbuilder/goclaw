package methods

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// guardStubStore embeds the interface so unexpected calls panic.
// GetByKey enforces tenant scope to model the production query.
type guardStubStore struct {
	store.AgentStore

	mu             sync.Mutex
	agent          *store.AgentData
	agentTenant    uuid.UUID
	updateCalls    []map[string]any
	setFileCalls   []setFileCall
	getFilesReturn []store.AgentContextFileData
}

type setFileCall struct {
	AgentID  uuid.UUID
	FileName string
	Content  string
}

var errAgentNotFound = errors.New("agent not found")

func (s *guardStubStore) GetByKey(ctx context.Context, _ string) (*store.AgentData, error) {
	if s.agentTenant != uuid.Nil {
		if got := store.TenantIDFromContext(ctx); got != s.agentTenant {
			return nil, errAgentNotFound
		}
	}
	return s.agent, nil
}

func (s *guardStubStore) Update(_ context.Context, _ uuid.UUID, updates map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateCalls = append(s.updateCalls, updates)
	return nil
}

func (s *guardStubStore) SetAgentContextFile(_ context.Context, agentID uuid.UUID, name, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setFileCalls = append(s.setFileCalls, setFileCall{AgentID: agentID, FileName: name, Content: content})
	return nil
}

func (s *guardStubStore) GetAgentContextFiles(_ context.Context, _ uuid.UUID) ([]store.AgentContextFileData, error) {
	return s.getFilesReturn, nil
}

func (s *guardStubStore) PropagateContextFile(_ context.Context, _ uuid.UUID, _ string) (int, error) {
	return 0, nil
}

func (s *guardStubStore) updateRecorded() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.updateCalls)
}

func (s *guardStubStore) setFileRecorded() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.setFileCalls)
}

func newGuardMethods(t *testing.T, stub store.AgentStore, ownerIDs ...string) *AgentsMethods {
	t.Helper()
	cfg := &config.Config{}
	cfg.Gateway.OwnerIDs = ownerIDs
	return &AgentsMethods{
		agents:     agent.NewRouter(),
		cfg:        cfg,
		workspace:  t.TempDir(),
		agentStore: stub,
	}
}

// guardCtx mirrors what router.handleRequest injects in production: locale,
// tenantID, tenantSlug, role — but NOT userID. Handlers that need userID
// must read it from client.UserID(); see authorizePredefinedAgentEdit.
func guardCtx(client *gateway.Client) context.Context {
	ctx := context.Background()
	if tid := client.TenantID(); tid != uuid.Nil {
		ctx = store.WithTenantID(ctx, tid)
	}
	if role := client.Role(); role != "" {
		ctx = store.WithRole(ctx, string(role))
	}
	return ctx
}

func captureSlog(t *testing.T) func() string {
	t.Helper()
	prev := slog.Default()
	buf := &bytes.Buffer{}
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func() string { return buf.String() }
}

func buildRequest(t *testing.T, id, method string, params map[string]any) *protocol.RequestFrame {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return &protocol.RequestFrame{ID: id, Method: method, Params: raw}
}

func newPredefinedAgent(tenantID uuid.UUID, ownerID string) *store.AgentData {
	return &store.AgentData{
		BaseModel: store.BaseModel{ID: uuid.New()},
		TenantID:  tenantID,
		AgentKey:  "test-agent",
		AgentType: store.AgentTypePredefined,
		OwnerID:   ownerID,
	}
}

func guardClient(role permissions.Role, tenantID uuid.UUID, userID string) *gateway.Client {
	return gateway.NewTestClient(role, tenantID, userID)
}
