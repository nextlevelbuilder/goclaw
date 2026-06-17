package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeChannelCapabilityMCPStore struct {
	accessible []store.MCPAccessInfo
	userCreds  map[uuid.UUID]*store.MCPUserCredentials
}

func (s *fakeChannelCapabilityMCPStore) CreateServer(context.Context, *store.MCPServerData) error {
	return nil
}
func (s *fakeChannelCapabilityMCPStore) GetServer(context.Context, uuid.UUID) (*store.MCPServerData, error) {
	return nil, sql.ErrNoRows
}
func (s *fakeChannelCapabilityMCPStore) GetServerByName(context.Context, string) (*store.MCPServerData, error) {
	return nil, sql.ErrNoRows
}
func (s *fakeChannelCapabilityMCPStore) ListServers(context.Context) ([]store.MCPServerData, error) {
	return nil, nil
}
func (s *fakeChannelCapabilityMCPStore) UpdateServer(context.Context, uuid.UUID, map[string]any) error {
	return nil
}
func (s *fakeChannelCapabilityMCPStore) DeleteServer(context.Context, uuid.UUID) error {
	return nil
}
func (s *fakeChannelCapabilityMCPStore) GrantToAgent(context.Context, *store.MCPAgentGrant) error {
	return nil
}
func (s *fakeChannelCapabilityMCPStore) RevokeFromAgent(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (s *fakeChannelCapabilityMCPStore) ListAgentGrants(context.Context, uuid.UUID) ([]store.MCPAgentGrant, error) {
	return nil, nil
}
func (s *fakeChannelCapabilityMCPStore) ListServerGrants(context.Context, uuid.UUID) ([]store.MCPAgentGrant, error) {
	return nil, nil
}
func (s *fakeChannelCapabilityMCPStore) GrantToUser(context.Context, *store.MCPUserGrant) error {
	return nil
}
func (s *fakeChannelCapabilityMCPStore) RevokeFromUser(context.Context, uuid.UUID, string) error {
	return nil
}
func (s *fakeChannelCapabilityMCPStore) CountAgentGrantsByServer(context.Context) (map[uuid.UUID]int, error) {
	return map[uuid.UUID]int{}, nil
}
func (s *fakeChannelCapabilityMCPStore) ListAccessible(context.Context, uuid.UUID, string) ([]store.MCPAccessInfo, error) {
	return s.accessible, nil
}
func (s *fakeChannelCapabilityMCPStore) CreateRequest(context.Context, *store.MCPAccessRequest) error {
	return nil
}
func (s *fakeChannelCapabilityMCPStore) ListPendingRequests(context.Context) ([]store.MCPAccessRequest, error) {
	return nil, nil
}
func (s *fakeChannelCapabilityMCPStore) ReviewRequest(context.Context, uuid.UUID, bool, string, string) error {
	return nil
}
func (s *fakeChannelCapabilityMCPStore) GetUserCredentials(_ context.Context, serverID uuid.UUID, _ string) (*store.MCPUserCredentials, error) {
	if creds := s.userCreds[serverID]; creds != nil {
		return creds, nil
	}
	return nil, sql.ErrNoRows
}
func (s *fakeChannelCapabilityMCPStore) SetUserCredentials(context.Context, uuid.UUID, string, store.MCPUserCredentials) error {
	return nil
}
func (s *fakeChannelCapabilityMCPStore) DeleteUserCredentials(context.Context, uuid.UUID, string) error {
	return nil
}

type fakeChannelCapabilityCLIStore struct {
	binaries  []store.SecureCLIBinary
	userCreds map[uuid.UUID]*store.SecureCLIUserCredential
}

func (s *fakeChannelCapabilityCLIStore) Create(context.Context, *store.SecureCLIBinary) error {
	return nil
}
func (s *fakeChannelCapabilityCLIStore) Get(context.Context, uuid.UUID) (*store.SecureCLIBinary, error) {
	return nil, sql.ErrNoRows
}
func (s *fakeChannelCapabilityCLIStore) Update(context.Context, uuid.UUID, map[string]any) error {
	return nil
}
func (s *fakeChannelCapabilityCLIStore) Delete(context.Context, uuid.UUID) error {
	return nil
}
func (s *fakeChannelCapabilityCLIStore) List(context.Context) ([]store.SecureCLIBinary, error) {
	return nil, nil
}
func (s *fakeChannelCapabilityCLIStore) LookupByBinary(context.Context, string, *uuid.UUID, string) (*store.SecureCLIBinary, error) {
	return nil, sql.ErrNoRows
}
func (s *fakeChannelCapabilityCLIStore) ListEnabled(context.Context) ([]store.SecureCLIBinary, error) {
	return nil, nil
}
func (s *fakeChannelCapabilityCLIStore) ListForAgent(context.Context, uuid.UUID) ([]store.SecureCLIBinary, error) {
	return s.binaries, nil
}
func (s *fakeChannelCapabilityCLIStore) IsRegisteredBinary(context.Context, string) (bool, error) {
	return false, nil
}
func (s *fakeChannelCapabilityCLIStore) GetUserCredentials(_ context.Context, binaryID uuid.UUID, _ string) (*store.SecureCLIUserCredential, error) {
	if creds := s.userCreds[binaryID]; creds != nil {
		return creds, nil
	}
	return nil, sql.ErrNoRows
}
func (s *fakeChannelCapabilityCLIStore) SetUserCredentials(context.Context, uuid.UUID, string, []byte) error {
	return nil
}
func (s *fakeChannelCapabilityCLIStore) SetUserCredentialsTyped(context.Context, uuid.UUID, string, []byte, *string, *string) error {
	return nil
}
func (s *fakeChannelCapabilityCLIStore) DeleteUserCredentials(context.Context, uuid.UUID, string) error {
	return nil
}
func (s *fakeChannelCapabilityCLIStore) ListUserCredentials(context.Context, uuid.UUID) ([]store.SecureCLIUserCredential, error) {
	return nil, nil
}

func TestChannelContextCapabilitiesExposeOnlyCredentialMetadata(t *testing.T) {
	token := "channel-capabilities-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {Scopes: []string{"operator.write"}, OwnerID: "tenant-user-1"},
	})

	instID := uuid.New()
	agentID := uuid.New()
	channelName := "discord-admin"
	mcpID := uuid.New()
	cliID := uuid.New()
	mcpStore := &fakeChannelCapabilityMCPStore{
		accessible: []store.MCPAccessInfo{{
			Server: store.MCPServerData{
				BaseModel:   store.BaseModel{ID: mcpID},
				Name:        "github",
				DisplayName: "GitHub",
				APIKey:      "global-value",
				Enabled:     true,
			},
			ToolAllow: []string{"issues.create"},
		}},
		userCreds: map[uuid.UUID]*store.MCPUserCredentials{
			mcpID: {APIKey: "user-value"},
		},
	}
	cliStore := &fakeChannelCapabilityCLIStore{
		binaries: []store.SecureCLIBinary{{
			BaseModel:    store.BaseModel{ID: cliID},
			BinaryName:   "gh",
			Description:  "GitHub CLI",
			EncryptedEnv: []byte(`{"VALUE":"global-value"}`),
			IsGlobal:     true,
			Enabled:      true,
		}},
		userCreds: map[uuid.UUID]*store.SecureCLIUserCredential{
			cliID: {BinaryID: cliID, UserID: "tenant-user-1", EncryptedEnv: []byte(`{"VALUE":"user-value"}`)},
		},
	}
	handler := NewChannelInstancesHandler(
		&stubChannelInstanceStore{inst: &store.ChannelInstanceData{BaseModel: store.BaseModel{ID: instID}, Name: channelName, ChannelType: "discord", AgentID: agentID}},
		nil, nil, nil, nil, nil,
	)
	handler.SetCapabilityStores(mcpStore, cliStore)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/channels/instances/"+instID.String()+"/contexts/channel/"+channelName+"/capabilities", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	bodyText := rec.Body.String()
	if strings.Contains(bodyText, "global-value") || strings.Contains(bodyText, "user-value") {
		t.Fatalf("capability response leaked credential material: %s", bodyText)
	}
	var body struct {
		MCP       []channelCapabilityDTO `json:"mcp"`
		SecureCLI []channelCapabilityDTO `json:"secure_cli"`
	}
	if err := json.Unmarshal([]byte(bodyText), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.MCP) != 1 || body.MCP[0].CredentialSource != "user" || !body.MCP[0].HasCredential {
		t.Fatalf("mcp capability = %+v", body.MCP)
	}
	if len(body.SecureCLI) != 1 || body.SecureCLI[0].Source != "global" || body.SecureCLI[0].CredentialSource != "user" || !body.SecureCLI[0].HasCredential {
		t.Fatalf("secure cli capability = %+v", body.SecureCLI)
	}
}
