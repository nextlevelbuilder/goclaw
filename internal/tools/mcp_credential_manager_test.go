package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// mockMCPServerStore implements store.MCPServerStore with in-memory state for tests.
type mockMCPServerStore struct {
	mu          sync.Mutex
	servers     map[string]*store.MCPServerData // keyed by name
	serverIDs   map[uuid.UUID]string            // id -> name
	credentials map[credKey]*store.MCPUserCredentials
	accessible  []store.MCPAccessInfo
}

type credKey struct {
	serverID uuid.UUID
	userID   string
}

func newMockMCPServerStore() *mockMCPServerStore {
	return &mockMCPServerStore{
		servers:     make(map[string]*store.MCPServerData),
		serverIDs:   make(map[uuid.UUID]string),
		credentials: make(map[credKey]*store.MCPUserCredentials),
	}
}

func (m *mockMCPServerStore) addServer(srv *store.MCPServerData) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers[srv.Name] = srv
	m.serverIDs[srv.ID] = srv.Name
}

func (m *mockMCPServerStore) setCredentials(serverID uuid.UUID, userID string, creds *store.MCPUserCredentials) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if creds == nil {
		delete(m.credentials, credKey{serverID: serverID, userID: userID})
	} else {
		m.credentials[credKey{serverID: serverID, userID: userID}] = creds
	}
}

// MCPServerStore interface methods used by the tool:

func (m *mockMCPServerStore) ListAccessible(_ context.Context, _ uuid.UUID, _ string) ([]store.MCPAccessInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.accessible, nil
}

func (m *mockMCPServerStore) GetServerByName(_ context.Context, name string) (*store.MCPServerData, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	srv, ok := m.servers[name]
	if !ok {
		return nil, fmt.Errorf("server %q not found", name)
	}
	return srv, nil
}

func (m *mockMCPServerStore) GetUserCredentials(_ context.Context, serverID uuid.UUID, userID string) (*store.MCPUserCredentials, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	creds, ok := m.credentials[credKey{serverID: serverID, userID: userID}]
	if !ok {
		return nil, nil
	}
	return creds, nil
}

func (m *mockMCPServerStore) SetUserCredentials(_ context.Context, serverID uuid.UUID, userID string, creds store.MCPUserCredentials) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.credentials[credKey{serverID: serverID, userID: userID}] = &store.MCPUserCredentials{
		APIKey:  creds.APIKey,
		Headers: creds.Headers,
		Env:     creds.Env,
	}
	return nil
}

func (m *mockMCPServerStore) DeleteUserCredentials(_ context.Context, serverID uuid.UUID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.credentials, credKey{serverID: serverID, userID: userID})
	return nil
}

// Stubs for unused MCPServerStore interface methods:

func (m *mockMCPServerStore) CreateServer(_ context.Context, _ *store.MCPServerData) error {
	return fmt.Errorf("not implemented")
}
func (m *mockMCPServerStore) GetServer(_ context.Context, _ uuid.UUID) (*store.MCPServerData, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockMCPServerStore) ListServers(_ context.Context) ([]store.MCPServerData, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockMCPServerStore) UpdateServer(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return fmt.Errorf("not implemented")
}
func (m *mockMCPServerStore) DeleteServer(_ context.Context, _ uuid.UUID) error {
	return fmt.Errorf("not implemented")
}
func (m *mockMCPServerStore) GrantToAgent(_ context.Context, _ *store.MCPAgentGrant) error {
	return fmt.Errorf("not implemented")
}
func (m *mockMCPServerStore) RevokeFromAgent(_ context.Context, _, _ uuid.UUID) error {
	return fmt.Errorf("not implemented")
}
func (m *mockMCPServerStore) ListAgentGrants(_ context.Context, _ uuid.UUID) ([]store.MCPAgentGrant, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockMCPServerStore) ListServerGrants(_ context.Context, _ uuid.UUID) ([]store.MCPAgentGrant, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockMCPServerStore) GrantToUser(_ context.Context, _ *store.MCPUserGrant) error {
	return fmt.Errorf("not implemented")
}
func (m *mockMCPServerStore) RevokeFromUser(_ context.Context, _ uuid.UUID, _ string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockMCPServerStore) CountAgentGrantsByServer(_ context.Context) (map[uuid.UUID]int, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockMCPServerStore) CreateRequest(_ context.Context, _ *store.MCPAccessRequest) error {
	return fmt.Errorf("not implemented")
}
func (m *mockMCPServerStore) ListPendingRequests(_ context.Context) ([]store.MCPAccessRequest, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockMCPServerStore) ReviewRequest(_ context.Context, _ uuid.UUID, _ bool, _, _ string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockMCPServerStore) CacheToolDescriptions(_ context.Context, _ uuid.UUID, _ map[string]store.CachedToolInfo) error {
	return fmt.Errorf("not implemented")
}

// --- Test setup helpers ---

var (
	mcpCredTestAgentID = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	mcpCredTestUserID  = "test-user-42"
	mcpCredTestTenantID = uuid.MustParse("22222222-2222-2222-2222-222222222222")

	mcpCredServerGitHub = &store.MCPServerData{
		BaseModel:  store.BaseModel{ID: uuid.MustParse("33333333-3333-3333-3333-333333333333")},
		Name:       "github",
		DisplayName: "GitHub API",
		Transport:  "streamable-http",
		URL:        "https://mcp.github.io",
		Enabled:    true,
	}
	mcpCredServerPostgres = &store.MCPServerData{
		BaseModel:  store.BaseModel{ID: uuid.MustParse("44444444-4444-4444-4444-444444444444")},
		Name:       "postgres",
		DisplayName: "",
		Transport:  "stdio",
		Command:    "npx",
		Args:       json.RawMessage(`["@mcp/postgres"]`),
		Enabled:    true,
	}
	mcpCredServerSlack = &store.MCPServerData{
		BaseModel:             store.BaseModel{ID: uuid.MustParse("55555555-5555-5555-5555-555555555555")},
		Name:                  "slack",
		DisplayName:           "Slack",
		Transport:             "streamable-http",
		URL:                   "https://mcp.slack.com",
		Enabled:               true,
		RequireUserCredentials: true,
	}
	mcpCredServerVault = &store.MCPServerData{
		BaseModel: store.BaseModel{ID: uuid.MustParse("66666666-6666-6666-6666-666666666666")},
		Name:      "vault",
		Transport: "streamable-http",
		URL:       "https://vault.example.com",
		Enabled:   true,
		Settings:  json.RawMessage(`{"require_user_credentials":true}`),
	}
)

func mcpCredCtx() context.Context {
	ctx := context.Background()
	ctx = store.WithAgentID(ctx, mcpCredTestAgentID)
	ctx = store.WithUserID(ctx, mcpCredTestUserID)
	ctx = store.WithTenantID(ctx, mcpCredTestTenantID)
	return ctx
}

func newTestMCPCredTool() (*mockMCPServerStore, *MCPCredentialManagerTool) {
	mock := newMockMCPServerStore()
	tool := NewMCPCredentialManagerTool()
	tool.SetMCPServerStore(mock)
	return mock, tool
}

// --- Tests ---

func TestMCPCredentialManager_NilStore(t *testing.T) {
	tool := NewMCPCredentialManagerTool() // store not set
	res := tool.Execute(mcpCredCtx(), map[string]any{"action": "list_servers"})
	if !res.IsError {
		t.Fatal("expected error for nil store")
	}
	if !strings.Contains(res.ForLLM, "MCP server store not available") {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_EmptyAction(t *testing.T) {
	_, tool := newTestMCPCredTool()
	res := tool.Execute(mcpCredCtx(), map[string]any{})
	if !res.IsError {
		t.Fatal("expected error for empty action")
	}
	if !strings.Contains(res.ForLLM, "action is required") {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_UnknownAction(t *testing.T) {
	_, tool := newTestMCPCredTool()
	res := tool.Execute(mcpCredCtx(), map[string]any{"action": "fly_to_moon"})
	if !res.IsError {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(res.ForLLM, "unsupported action") {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_ListServers_Empty(t *testing.T) {
	mock, tool := newTestMCPCredTool()
	mock.accessible = nil

	res := tool.Execute(mcpCredCtx(), map[string]any{"action": "list_servers"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "don't have any MCP servers") {
		t.Fatalf("unexpected result: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_ListServers_AllScenarios(t *testing.T) {
	mock, tool := newTestMCPCredTool()
	mock.addServer(mcpCredServerGitHub)
	mock.addServer(mcpCredServerPostgres)
	mock.addServer(mcpCredServerSlack)
	mock.addServer(mcpCredServerVault)
	mock.accessible = []store.MCPAccessInfo{
		{Server: *mcpCredServerGitHub},
		{Server: *mcpCredServerPostgres},
		{Server: *mcpCredServerSlack},
		{Server: *mcpCredServerVault},
	}
	// Pre-set credentials for Slack (requires user creds, has creds)
	mock.setCredentials(mcpCredServerSlack.ID, mcpCredTestUserID, &store.MCPUserCredentials{APIKey: "xoxb-secret-token"})

	res := tool.Execute(mcpCredCtx(), map[string]any{"action": "list_servers"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}

	// All 4 servers listed
	if !strings.Contains(res.ForLLM, "4") {
		t.Fatalf("expected 4 servers, got: %s", res.ForLLM)
	}

	// GitHub — no credentials needed, no custom creds
	if !strings.Contains(res.ForLLM, "GitHub API") || !strings.Contains(res.ForLLM, "no credentials needed") {
		t.Fatalf("expected GitHub with 'no credentials needed': %s", res.ForLLM)
	}

	// Postgres — no display name, should show raw name
	if !strings.Contains(res.ForLLM, "postgres") || !strings.Contains(res.ForLLM, "no credentials needed") {
		t.Fatalf("expected postgres with 'no credentials needed': %s", res.ForLLM)
	}

	// Slack — requires user credentials and has them
	if !strings.Contains(res.ForLLM, "credentials set") {
		t.Fatalf("expected Slack with 'credentials set': %s", res.ForLLM)
	}

	// Vault — requires user credentials (from settings) but not set
	if !strings.Contains(res.ForLLM, "credentials required") {
		t.Fatalf("expected vault with 'credentials required': %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_CredentialStatus_NoServerName(t *testing.T) {
	_, tool := newTestMCPCredTool()
	res := tool.Execute(mcpCredCtx(), map[string]any{"action": "credential_status"})
	if !res.IsError {
		t.Fatal("expected error for missing server_name")
	}
	if !strings.Contains(res.ForLLM, "server_name is required") {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_CredentialStatus_ServerNotFound(t *testing.T) {
	mock, tool := newTestMCPCredTool()
	mock.addServer(mcpCredServerGitHub)

	res := tool.Execute(mcpCredCtx(), map[string]any{
		"action":      "credential_status",
		"server_name": "nonexistent",
	})
	if !res.IsError {
		t.Fatal("expected error for nonexistent server")
	}
	if !strings.Contains(res.ForLLM, "not found") {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_CredentialStatus_NoUserID(t *testing.T) {
	_, tool := newTestMCPCredTool()
	ctx := context.Background()
	ctx = store.WithAgentID(ctx, mcpCredTestAgentID)
	// No user ID set

	res := tool.Execute(ctx, map[string]any{
		"action":      "credential_status",
		"server_name": "github",
	})
	if !res.IsError {
		t.Fatal("expected error for missing user ID")
	}
	if !strings.Contains(res.ForLLM, "no user identity") {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_CredentialStatus_NoCredsSet(t *testing.T) {
	mock, tool := newTestMCPCredTool()
	mock.addServer(mcpCredServerGitHub)
	mock.accessible = []store.MCPAccessInfo{{Server: *mcpCredServerGitHub}}

	res := tool.Execute(mcpCredCtx(), map[string]any{
		"action":      "credential_status",
		"server_name": "github",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "have not set any credentials") {
		t.Fatalf("expected 'no credentials' message: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_CredentialStatus_NeedsCredsNoCreds(t *testing.T) {
	mock, tool := newTestMCPCredTool()
	mock.addServer(mcpCredServerSlack)
	mock.accessible = []store.MCPAccessInfo{{Server: *mcpCredServerSlack}}

	res := tool.Execute(mcpCredCtx(), map[string]any{
		"action":      "credential_status",
		"server_name": "slack",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "requires per-user credentials") {
		t.Fatalf("expected 'requires credentials': %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "set_credentials") {
		t.Fatalf("expected hint about set_credentials: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_CredentialStatus_WithCreds(t *testing.T) {
	mock, tool := newTestMCPCredTool()
	mock.addServer(mcpCredServerGitHub)
	mock.accessible = []store.MCPAccessInfo{{Server: *mcpCredServerGitHub}}
	mock.setCredentials(mcpCredServerGitHub.ID, mcpCredTestUserID, &store.MCPUserCredentials{
		APIKey: "ghp_abcdef1234567890abcdef1234567890",
		Headers: map[string]string{"X-Custom": "value"},
		Env:     map[string]string{"MY_VAR": "my_value"},
	})

	res := tool.Execute(mcpCredCtx(), map[string]any{
		"action":      "credential_status",
		"server_name": "github",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "ghp_") {
		t.Fatalf("expected masked API key: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "X-Custom") {
		t.Fatalf("expected header key listed: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "MY_VAR") {
		t.Fatalf("expected env var key listed: %s", res.ForLLM)
	}
	// The actual value should NOT appear (masked)
	if strings.Contains(res.ForLLM, "abcdef1234567890abcdef1234567890") {
		t.Fatalf("API key value leaked in output: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_SetCredentials_NoServerName(t *testing.T) {
	_, tool := newTestMCPCredTool()
	res := tool.Execute(mcpCredCtx(), map[string]any{
		"action":  "set_credentials",
		"api_key": "my-key",
	})
	if !res.IsError {
		t.Fatal("expected error for missing server_name")
	}
	if !strings.Contains(res.ForLLM, "server_name is required") {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_SetCredentials_NoUserID(t *testing.T) {
	_, tool := newTestMCPCredTool()
	ctx := context.Background()
	ctx = store.WithAgentID(ctx, mcpCredTestAgentID)

	res := tool.Execute(ctx, map[string]any{
		"action":      "set_credentials",
		"server_name": "github",
		"api_key":     "my-key",
	})
	if !res.IsError {
		t.Fatal("expected error for missing user ID")
	}
	if !strings.Contains(res.ForLLM, "no user identity") {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_SetCredentials_ServerNotFound(t *testing.T) {
	mock, tool := newTestMCPCredTool()
	mock.addServer(mcpCredServerGitHub)

	res := tool.Execute(mcpCredCtx(), map[string]any{
		"action":      "set_credentials",
		"server_name": "nonexistent",
		"api_key":     "my-key",
	})
	if !res.IsError {
		t.Fatal("expected error for nonexistent server")
	}
	if !strings.Contains(res.ForLLM, "not found") {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_SetCredentials_NoValues(t *testing.T) {
	mock, tool := newTestMCPCredTool()
	mock.addServer(mcpCredServerGitHub)

	res := tool.Execute(mcpCredCtx(), map[string]any{
		"action":      "set_credentials",
		"server_name": "github",
	})
	if !res.IsError {
		t.Fatal("expected error for no values")
	}
	if !strings.Contains(res.ForLLM, "at least one of") {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_SetCredentials_Success(t *testing.T) {
	mock, tool := newTestMCPCredTool()
	mock.addServer(mcpCredServerGitHub)

	res := tool.Execute(mcpCredCtx(), map[string]any{
		"action":      "set_credentials",
		"server_name": "github",
		"api_key":     "ghp_my_secret_key",
		"headers":     map[string]any{"X-Custom": "val1"},
		"env":         map[string]any{"MY_ENV": "val2"},
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Successfully set credentials") {
		t.Fatalf("expected success: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "GitHub API") {
		t.Fatalf("expected display name: %s", res.ForLLM)
	}

	// Verify the credential was stored
	creds, err := mock.GetUserCredentials(context.Background(), mcpCredServerGitHub.ID, mcpCredTestUserID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds == nil {
		t.Fatal("credentials not stored")
	}
	if creds.APIKey != "ghp_my_secret_key" {
		t.Fatalf("expected API key 'ghp_my_secret_key', got %q", creds.APIKey)
	}
	if creds.Headers["X-Custom"] != "val1" {
		t.Fatalf("expected header X-Custom=val1, got %q", creds.Headers["X-Custom"])
	}
	if creds.Env["MY_ENV"] != "val2" {
		t.Fatalf("expected env MY_ENV=val2, got %q", creds.Env["MY_ENV"])
	}
}

func TestMCPCredentialManager_SetCredentials_SuccessNoDisplayName(t *testing.T) {
	mock, tool := newTestMCPCredTool()
	mock.addServer(mcpCredServerPostgres)

	res := tool.Execute(mcpCredCtx(), map[string]any{
		"action":      "set_credentials",
		"server_name": "postgres",
		"api_key":     "pg-key",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	// Falls back to server name when DisplayName is empty
	if !strings.Contains(res.ForLLM, "postgres") {
		t.Fatalf("expected server name fallback: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_SetBearerToken_NoServerName(t *testing.T) {
	_, tool := newTestMCPCredTool()
	res := tool.Execute(mcpCredCtx(), map[string]any{
		"action": "set_bearer_token",
		"token":  "my-token",
	})
	if !res.IsError {
		t.Fatal("expected error for missing server_name")
	}
	if !strings.Contains(res.ForLLM, "server_name is required") {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_SetBearerToken_NoToken(t *testing.T) {
	_, tool := newTestMCPCredTool()
	res := tool.Execute(mcpCredCtx(), map[string]any{
		"action":      "set_bearer_token",
		"server_name": "github",
	})
	if !res.IsError {
		t.Fatal("expected error for missing token")
	}
	if !strings.Contains(res.ForLLM, "token is required") {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_SetBearerToken_NoUserID(t *testing.T) {
	_, tool := newTestMCPCredTool()
	ctx := context.Background()
	ctx = store.WithAgentID(ctx, mcpCredTestAgentID)

	res := tool.Execute(ctx, map[string]any{
		"action":      "set_bearer_token",
		"server_name": "github",
		"token":       "my-token",
	})
	if !res.IsError {
		t.Fatal("expected error for missing user ID")
	}
	if !strings.Contains(res.ForLLM, "no user identity") {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_SetBearerToken_ServerNotFound(t *testing.T) {
	mock, tool := newTestMCPCredTool()
	mock.addServer(mcpCredServerGitHub)

	res := tool.Execute(mcpCredCtx(), map[string]any{
		"action":      "set_bearer_token",
		"server_name": "nonexistent",
		"token":       "my-token",
	})
	if !res.IsError {
		t.Fatal("expected error for nonexistent server")
	}
	if !strings.Contains(res.ForLLM, "not found") {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_SetBearerToken_Success(t *testing.T) {
	mock, tool := newTestMCPCredTool()
	mock.addServer(mcpCredServerGitHub)

	res := tool.Execute(mcpCredCtx(), map[string]any{
		"action":      "set_bearer_token",
		"server_name": "github",
		"token":       "ghp_bearer_token_value",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Successfully set Bearer token") {
		t.Fatalf("expected success: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Authorization: Bearer") {
		t.Fatalf("expected Authorization Bearer mention: %s", res.ForLLM)
	}

	// Verify the credential was stored as API key
	creds, err := mock.GetUserCredentials(context.Background(), mcpCredServerGitHub.ID, mcpCredTestUserID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds == nil {
		t.Fatal("credentials not stored")
	}
	if creds.APIKey != "ghp_bearer_token_value" {
		t.Fatalf("expected API key 'ghp_bearer_token_value', got %q", creds.APIKey)
	}
}

func TestMCPCredentialManager_SetBearerToken_OverwritesExisting(t *testing.T) {
	mock, tool := newTestMCPCredTool()
	mock.addServer(mcpCredServerSlack)
	// Pre-set existing credentials
	mock.setCredentials(mcpCredServerSlack.ID, mcpCredTestUserID, &store.MCPUserCredentials{
		APIKey: "old-token",
		Headers: map[string]string{"X-Old": "value"},
	})

	res := tool.Execute(mcpCredCtx(), map[string]any{
		"action":      "set_bearer_token",
		"server_name": "slack",
		"token":       "new-bearer-token",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}

	// Verify old credentials were replaced
	creds, err := mock.GetUserCredentials(context.Background(), mcpCredServerSlack.ID, mcpCredTestUserID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds == nil {
		t.Fatal("credentials not stored")
	}
	if creds.APIKey != "new-bearer-token" {
		t.Fatalf("expected 'new-bearer-token', got %q", creds.APIKey)
	}
	// Old headers are cleared
	if len(creds.Headers) > 0 {
		t.Fatalf("expected headers cleared, got %v", creds.Headers)
	}
}

func TestMCPCredentialManager_DeleteCredentials_NoServerName(t *testing.T) {
	_, tool := newTestMCPCredTool()
	res := tool.Execute(mcpCredCtx(), map[string]any{"action": "delete_credentials"})
	if !res.IsError {
		t.Fatal("expected error for missing server_name")
	}
	if !strings.Contains(res.ForLLM, "server_name is required") {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_DeleteCredentials_NoUserID(t *testing.T) {
	_, tool := newTestMCPCredTool()
	ctx := context.Background()
	ctx = store.WithAgentID(ctx, mcpCredTestAgentID)

	res := tool.Execute(ctx, map[string]any{
		"action":      "delete_credentials",
		"server_name": "github",
	})
	if !res.IsError {
		t.Fatal("expected error for missing user ID")
	}
	if !strings.Contains(res.ForLLM, "no user identity") {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_DeleteCredentials_ServerNotFound(t *testing.T) {
	mock, tool := newTestMCPCredTool()
	mock.addServer(mcpCredServerGitHub)

	res := tool.Execute(mcpCredCtx(), map[string]any{
		"action":      "delete_credentials",
		"server_name": "nonexistent",
	})
	if !res.IsError {
		t.Fatal("expected error for nonexistent server")
	}
	if !strings.Contains(res.ForLLM, "not found") {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_DeleteCredentials_Success(t *testing.T) {
	mock, tool := newTestMCPCredTool()
	mock.addServer(mcpCredServerGitHub)
	mock.setCredentials(mcpCredServerGitHub.ID, mcpCredTestUserID, &store.MCPUserCredentials{
		APIKey: "ghp_my_key",
	})

	// Verify it exists first
	creds, _ := mock.GetUserCredentials(context.Background(), mcpCredServerGitHub.ID, mcpCredTestUserID)
	if creds == nil {
		t.Fatal("precondition failed: credentials should exist")
	}

	res := tool.Execute(mcpCredCtx(), map[string]any{
		"action":      "delete_credentials",
		"server_name": "github",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Successfully deleted credentials") {
		t.Fatalf("expected success: %s", res.ForLLM)
	}

	// Verify credentials are gone
	creds, err := mock.GetUserCredentials(context.Background(), mcpCredServerGitHub.ID, mcpCredTestUserID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds != nil {
		t.Fatal("credentials should have been deleted")
	}
}

func TestMCPCredentialManager_DeleteCredentials_Idempotent(t *testing.T) {
	mock, tool := newTestMCPCredTool()
	mock.addServer(mcpCredServerGitHub)
	// No credentials pre-set

	res := tool.Execute(mcpCredCtx(), map[string]any{
		"action":      "delete_credentials",
		"server_name": "github",
	})
	if res.IsError {
		t.Fatalf("expected delete to be idempotent, got error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Successfully deleted credentials") {
		t.Fatalf("expected success: %s", res.ForLLM)
	}
}

func TestMCPCredentialManager_StoreAccessibleReturnsListAccessible(t *testing.T) {
	// Verify that listServers calls ListAccessible by checking the number of
	// servers returned matches the mock's accessible count.
	mock, tool := newTestMCPCredTool()
	mock.addServer(mcpCredServerGitHub)
	mock.addServer(mcpCredServerPostgres)
	mock.accessible = []store.MCPAccessInfo{
		{Server: *mcpCredServerGitHub},
		{Server: *mcpCredServerPostgres},
	}

	res := tool.Execute(mcpCredCtx(), map[string]any{"action": "list_servers"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "2") {
		t.Fatalf("expected 2 servers, got: %s", res.ForLLM)
	}
}

// Test that requireUserCredsFromSettings returns true for settings with require_user_credentials.
func TestRequireUserCredsFromSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings json.RawMessage
		want     bool
	}{
		{"nil settings", nil, false},
		{"empty settings", json.RawMessage(""), false},
		{"empty object", json.RawMessage("{}"), false},
		{"require_user_credentials true", json.RawMessage(`{"require_user_credentials":true}`), true},
		{"require_user_credentials false", json.RawMessage(`{"require_user_credentials":false}`), false},
		{"nested unrelated", json.RawMessage(`{"timeout":30}`), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requireUserCredsFromSettings(tt.settings)
			if got != tt.want {
				t.Errorf("requireUserCredsFromSettings(%s) = %v, want %v", string(tt.settings), got, tt.want)
			}
		})
	}
}

// Test that maskString does not leak full secrets.
func TestMaskString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"short", "*****"},
		{"12345678", "********"},
		{"abcdefghijklmnop", "abcd********mnop"},
		{"abcdefghij", "abcd**ghij"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := maskString(tt.input)
			if got != tt.want {
				t.Errorf("maskString(%q) = %q, want %q", tt.input, got, tt.want)
			}
			// Ensure no part of the input beyond first 4 and last 4 leaks
			if len(tt.input) > 8 {
				middle := tt.input[4 : len(tt.input)-4]
				if strings.Contains(got, middle) {
					t.Errorf("maskString(%q) leaked middle part %q in %q", tt.input, middle, got)
				}
			}
		})
	}
}
