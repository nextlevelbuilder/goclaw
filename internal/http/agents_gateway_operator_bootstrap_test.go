package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type gatewayOperatorSecureCLIStore struct {
	binaries []store.SecureCLIBinary
	created  *store.SecureCLIBinary
	updated  map[uuid.UUID]map[string]any
}

func (s *gatewayOperatorSecureCLIStore) Create(_ context.Context, b *store.SecureCLIBinary) error {
	cp := *b
	if cp.ID == uuid.Nil {
		cp.ID = store.GenNewID()
		b.ID = cp.ID
	}
	s.created = &cp
	s.binaries = append(s.binaries, cp)
	return nil
}

func (s *gatewayOperatorSecureCLIStore) Get(context.Context, uuid.UUID) (*store.SecureCLIBinary, error) {
	return nil, sql.ErrNoRows
}

func (s *gatewayOperatorSecureCLIStore) Update(_ context.Context, id uuid.UUID, updates map[string]any) error {
	if s.updated == nil {
		s.updated = map[uuid.UUID]map[string]any{}
	}
	cp := map[string]any{}
	for k, v := range updates {
		cp[k] = v
	}
	s.updated[id] = cp
	for i := range s.binaries {
		if s.binaries[i].ID != id {
			continue
		}
		for k, v := range updates {
			switch k {
			case "binary_path":
				if path, ok := v.(string); ok {
					s.binaries[i].BinaryPath = &path
				}
			case "description":
				if description, ok := v.(string); ok {
					s.binaries[i].Description = description
				}
			case "deny_args":
				if raw, ok := v.(json.RawMessage); ok {
					s.binaries[i].DenyArgs = raw
				}
			case "deny_verbose":
				if raw, ok := v.(json.RawMessage); ok {
					s.binaries[i].DenyVerbose = raw
				}
			case "timeout_seconds":
				if timeoutSeconds, ok := v.(int); ok {
					s.binaries[i].TimeoutSeconds = timeoutSeconds
				}
			case "tips":
				if tips, ok := v.(string); ok {
					s.binaries[i].Tips = tips
				}
			case "is_global":
				if isGlobal, ok := v.(bool); ok {
					s.binaries[i].IsGlobal = isGlobal
				}
			case "enabled":
				if enabled, ok := v.(bool); ok {
					s.binaries[i].Enabled = enabled
				}
			case "adapter_name":
				if adapterName, ok := v.(*string); ok {
					s.binaries[i].AdapterName = adapterName
				} else {
					s.binaries[i].AdapterName = nil
				}
			}
		}
		return nil
	}
	return nil
}

func (s *gatewayOperatorSecureCLIStore) Delete(context.Context, uuid.UUID) error { return nil }

func (s *gatewayOperatorSecureCLIStore) List(context.Context) ([]store.SecureCLIBinary, error) {
	out := make([]store.SecureCLIBinary, len(s.binaries))
	copy(out, s.binaries)
	return out, nil
}

func (s *gatewayOperatorSecureCLIStore) LookupByBinary(context.Context, string, *uuid.UUID, string) (*store.SecureCLIBinary, error) {
	return nil, sql.ErrNoRows
}

func (s *gatewayOperatorSecureCLIStore) ListEnabled(context.Context) ([]store.SecureCLIBinary, error) {
	return nil, nil
}

func (s *gatewayOperatorSecureCLIStore) ListForAgent(context.Context, uuid.UUID) ([]store.SecureCLIBinary, error) {
	return nil, nil
}

func (s *gatewayOperatorSecureCLIStore) IsRegisteredBinary(context.Context, string) (bool, error) {
	return false, nil
}

func (s *gatewayOperatorSecureCLIStore) GetUserCredentials(context.Context, uuid.UUID, string) (*store.SecureCLIUserCredential, error) {
	return nil, sql.ErrNoRows
}

func (s *gatewayOperatorSecureCLIStore) SetUserCredentials(context.Context, uuid.UUID, string, []byte) error {
	return nil
}

func (s *gatewayOperatorSecureCLIStore) SetUserCredentialsTyped(context.Context, uuid.UUID, string, []byte, *string, *string) error {
	return nil
}

func (s *gatewayOperatorSecureCLIStore) DeleteUserCredentials(context.Context, uuid.UUID, string) error {
	return nil
}

func (s *gatewayOperatorSecureCLIStore) ListUserCredentials(context.Context, uuid.UUID) ([]store.SecureCLIUserCredential, error) {
	return nil, nil
}

type gatewayOperatorGrantStore struct {
	grants map[uuid.UUID]*store.SecureCLIAgentGrant
	env    map[uuid.UUID][]byte
}

func (s *gatewayOperatorGrantStore) BinaryExists(context.Context, uuid.UUID) (bool, error) {
	return true, nil
}

func (s *gatewayOperatorGrantStore) AgentExists(context.Context, uuid.UUID) (bool, error) {
	return true, nil
}

func (s *gatewayOperatorGrantStore) Create(_ context.Context, g *store.SecureCLIAgentGrant) error {
	if s.grants == nil {
		s.grants = map[uuid.UUID]*store.SecureCLIAgentGrant{}
	}
	if g.ID == uuid.Nil {
		g.ID = store.GenNewID()
	}
	cp := *g
	s.grants[g.ID] = &cp
	return nil
}

func (s *gatewayOperatorGrantStore) Get(_ context.Context, id uuid.UUID) (*store.SecureCLIAgentGrant, error) {
	if g := s.grants[id]; g != nil {
		cp := *g
		return &cp, nil
	}
	return nil, sql.ErrNoRows
}

func (s *gatewayOperatorGrantStore) Update(_ context.Context, id uuid.UUID, updates map[string]any) error {
	g := s.grants[id]
	if g == nil {
		return sql.ErrNoRows
	}
	if enabled, ok := updates["enabled"].(bool); ok {
		g.Enabled = enabled
	}
	return nil
}

func (s *gatewayOperatorGrantStore) Delete(context.Context, uuid.UUID) error { return nil }

func (s *gatewayOperatorGrantStore) ListByBinary(_ context.Context, binaryID uuid.UUID) ([]store.SecureCLIAgentGrant, error) {
	out := []store.SecureCLIAgentGrant{}
	for _, g := range s.grants {
		if g.BinaryID == binaryID {
			out = append(out, *g)
		}
	}
	return out, nil
}

func (s *gatewayOperatorGrantStore) ListByAgent(_ context.Context, agentID uuid.UUID) ([]store.SecureCLIAgentGrant, error) {
	out := []store.SecureCLIAgentGrant{}
	for _, g := range s.grants {
		if g.AgentID == agentID {
			out = append(out, *g)
		}
	}
	return out, nil
}

func (s *gatewayOperatorGrantStore) UpdateGrantEnv(_ context.Context, grantID uuid.UUID, plaintextEnv []byte) error {
	if s.env == nil {
		s.env = map[uuid.UUID][]byte{}
	}
	if s.grants[grantID] == nil {
		return sql.ErrNoRows
	}
	cp := append([]byte(nil), plaintextEnv...)
	s.env[grantID] = cp
	s.grants[grantID].EncryptedEnv = cp
	return nil
}

type gatewayOperatorAgentCredentialStore struct {
	env       map[uuid.UUID][]byte
	createdBy map[uuid.UUID]string
}

func (s *gatewayOperatorAgentCredentialStore) BinaryExists(context.Context, uuid.UUID) (bool, error) {
	return true, nil
}

func (s *gatewayOperatorAgentCredentialStore) AgentExists(context.Context, uuid.UUID) (bool, error) {
	return true, nil
}

func (s *gatewayOperatorAgentCredentialStore) GetAgentCredentials(_ context.Context, _ uuid.UUID, agentID uuid.UUID) (*store.SecureCLIAgentCredential, error) {
	if s.env == nil || len(s.env[agentID]) == 0 {
		return nil, nil
	}
	return &store.SecureCLIAgentCredential{
		ID:           store.GenNewID(),
		AgentID:      agentID,
		EncryptedEnv: append([]byte(nil), s.env[agentID]...),
		CreatedBy:    s.createdBy[agentID],
	}, nil
}

func (s *gatewayOperatorAgentCredentialStore) SetAgentCredentials(_ context.Context, _ uuid.UUID, agentID uuid.UUID, encryptedEnv []byte, createdBy string) error {
	if s.env == nil {
		s.env = map[uuid.UUID][]byte{}
	}
	if s.createdBy == nil {
		s.createdBy = map[uuid.UUID]string{}
	}
	s.env[agentID] = append([]byte(nil), encryptedEnv...)
	s.createdBy[agentID] = createdBy
	return nil
}

func (s *gatewayOperatorAgentCredentialStore) SetAgentCredentialsTyped(ctx context.Context, binaryID uuid.UUID, agentID uuid.UUID, encryptedEnv []byte, _ *string, _ *string, createdBy string) error {
	return s.SetAgentCredentials(ctx, binaryID, agentID, encryptedEnv, createdBy)
}

func (s *gatewayOperatorAgentCredentialStore) DeleteAgentCredentials(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (s *gatewayOperatorAgentCredentialStore) ListAgentCredentials(context.Context, uuid.UUID) ([]store.SecureCLIAgentCredential, error) {
	return nil, nil
}

func gatewayOperatorContext() context.Context {
	ctx := context.Background()
	ctx = store.WithTenantID(ctx, uuid.MustParse("0193a5b0-7000-7000-8000-000000000002"))
	ctx = store.WithUserID(ctx, "system")
	ctx = store.WithRole(ctx, store.RoleOwner)
	return ctx
}

type gatewayOperatorAgentStore struct {
	store.AgentStore
	agents []store.AgentData
	files  map[uuid.UUID]map[string]string
}

func (s *gatewayOperatorAgentStore) Create(_ context.Context, agent *store.AgentData) error {
	if agent.ID == uuid.Nil {
		agent.ID = store.GenNewID()
	}
	if agent.CreatedAt.IsZero() {
		agent.CreatedAt = time.Now().UTC()
	}
	agent.UpdatedAt = agent.CreatedAt
	cp := *agent
	s.agents = append(s.agents, cp)
	return nil
}

func (s *gatewayOperatorAgentStore) GetByKey(_ context.Context, agentKey string) (*store.AgentData, error) {
	for i := range s.agents {
		if s.agents[i].AgentKey == agentKey {
			cp := s.agents[i]
			return &cp, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (s *gatewayOperatorAgentStore) List(context.Context, string) ([]store.AgentData, error) {
	out := make([]store.AgentData, len(s.agents))
	copy(out, s.agents)
	return out, nil
}

func (s *gatewayOperatorAgentStore) GetAgentContextFiles(context.Context, uuid.UUID) ([]store.AgentContextFileData, error) {
	return nil, nil
}

func (s *gatewayOperatorAgentStore) SetAgentContextFile(_ context.Context, agentID uuid.UUID, fileName, content string) error {
	if s.files == nil {
		s.files = map[uuid.UUID]map[string]string{}
	}
	if s.files[agentID] == nil {
		s.files[agentID] = map[string]string{}
	}
	s.files[agentID][fileName] = content
	return nil
}

func TestGatewayOperatorBootstrapCreatesBinaryGrantAndSensitiveTokenEnv(t *testing.T) {
	setupTestToken(t, "test-gateway-token")
	secureCLI := &gatewayOperatorSecureCLIStore{}
	grants := &gatewayOperatorGrantStore{}
	agentCreds := &gatewayOperatorAgentCredentialStore{}
	agentID := uuid.New()
	handler := &AgentsHandler{}
	handler.SetGatewayOperatorBootstrap(secureCLI, grants, agentCreds, "http://127.0.0.1:18790")
	handler.findGatewayOperatorBinary = func() (string, error) {
		return "/usr/local/bin/goclaw", nil
	}

	result, err := handler.bootstrapGatewayOperatorAccess(gatewayOperatorContext(), agentID)
	if err != nil {
		t.Fatalf("bootstrap returned error: %v", err)
	}
	if result.BinaryID == uuid.Nil || result.GrantID == uuid.Nil {
		t.Fatalf("bootstrap result missing IDs: %#v", result)
	}
	if secureCLI.created == nil {
		t.Fatal("expected goclaw secure CLI binary to be registered")
	}
	if secureCLI.created.BinaryName != "goclaw" {
		t.Fatalf("binary name=%q, want goclaw", secureCLI.created.BinaryName)
	}
	if secureCLI.created.IsGlobal {
		t.Fatal("gateway operator binary must be non-global")
	}
	if secureCLI.created.BinaryPath == nil || *secureCLI.created.BinaryPath != "/usr/local/bin/goclaw" {
		t.Fatalf("binary path not recorded: %#v", secureCLI.created.BinaryPath)
	}
	if !strings.Contains(string(secureCLI.created.DenyArgs), "auth") {
		t.Fatalf("expected lifecycle/auth deny patterns, got %s", string(secureCLI.created.DenyArgs))
	}

	if len(grants.env) != 0 {
		t.Fatalf("gateway token must not be stored in revealable grant env: %#v", grants.env)
	}

	envJSON := agentCreds.env[agentID]
	if len(envJSON) == 0 {
		t.Fatal("expected non-revealable per-agent credential env")
	}
	entries, err := store.ParseSecureCLIEnv(envJSON)
	if err != nil {
		t.Fatalf("parse agent credential env: %v", err)
	}
	tokenEntry := entries["GOCLAW_GATEWAY_TOKEN"]
	if tokenEntry.Kind != store.SecureCLIEnvKindSensitive || tokenEntry.Value != "test-gateway-token" {
		t.Fatalf("token entry not stored as sensitive override: %#v", tokenEntry)
	}
	if entries["GOCLAW_SERVER"].Value != "http://127.0.0.1:18790" {
		t.Fatalf("GOCLAW_SERVER not injected: %#v", entries["GOCLAW_SERVER"])
	}

	safeEnv := agentCredentialResponse(store.SecureCLIAgentCredential{EncryptedEnv: envJSON}).Env
	if safeEnv["GOCLAW_GATEWAY_TOKEN"].Value != nil || !safeEnv["GOCLAW_GATEWAY_TOKEN"].Masked {
		encoded, _ := json.Marshal(safeEnv)
		t.Fatalf("gateway token not masked in agent credential response: %s", encoded)
	}
}

func TestGatewayOperatorBootstrapReusesExistingGrant(t *testing.T) {
	setupTestToken(t, "rotated-test-token")
	binaryID := uuid.New()
	grantID := uuid.New()
	agentID := uuid.New()
	secureCLI := &gatewayOperatorSecureCLIStore{
		binaries: []store.SecureCLIBinary{{
			BaseModel:  store.BaseModel{ID: binaryID},
			BinaryName: "goclaw",
			Enabled:    true,
			IsGlobal:   false,
		}},
	}
	grants := &gatewayOperatorGrantStore{
		grants: map[uuid.UUID]*store.SecureCLIAgentGrant{
			grantID: {
				BaseModel: store.BaseModel{ID: grantID},
				BinaryID:  binaryID,
				AgentID:   agentID,
				Enabled:   false,
			},
		},
	}
	agentCreds := &gatewayOperatorAgentCredentialStore{}
	handler := &AgentsHandler{}
	handler.SetGatewayOperatorBootstrap(secureCLI, grants, agentCreds, "http://127.0.0.1:18790")
	handler.findGatewayOperatorBinary = func() (string, error) {
		return "/usr/local/bin/goclaw", nil
	}

	result, err := handler.bootstrapGatewayOperatorAccess(gatewayOperatorContext(), agentID)
	if err != nil {
		t.Fatalf("bootstrap returned error: %v", err)
	}
	if result.GrantID != grantID {
		t.Fatalf("expected existing grant %s, got %s", grantID, result.GrantID)
	}
	if !grants.grants[grantID].Enabled {
		t.Fatal("existing disabled grant should be re-enabled")
	}
	if secureCLI.created != nil {
		t.Fatal("existing binary should be reused, not recreated")
	}
	if len(agentCreds.env[agentID]) == 0 {
		t.Fatal("expected agent credential env to be refreshed for existing grant")
	}
}

func TestGatewayOperatorBootstrapConvertsExistingGlobalBinaryToGrantScoped(t *testing.T) {
	setupTestToken(t, "test-gateway-token")
	binaryID := uuid.New()
	agentID := uuid.New()
	secureCLI := &gatewayOperatorSecureCLIStore{
		binaries: []store.SecureCLIBinary{{
			BaseModel:  store.BaseModel{ID: binaryID},
			BinaryName: "goclaw",
			Enabled:    true,
			IsGlobal:   true,
		}},
	}
	grants := &gatewayOperatorGrantStore{}
	agentCreds := &gatewayOperatorAgentCredentialStore{}
	handler := &AgentsHandler{}
	handler.SetGatewayOperatorBootstrap(secureCLI, grants, agentCreds, "127.0.0.1:19999")
	handler.findGatewayOperatorBinary = func() (string, error) {
		return "/opt/goclaw/current/goclaw", nil
	}

	result, err := handler.bootstrapGatewayOperatorAccess(gatewayOperatorContext(), agentID)
	if err != nil {
		t.Fatalf("bootstrap returned error: %v", err)
	}
	if result.BinaryID != binaryID {
		t.Fatalf("expected existing binary %s, got %s", binaryID, result.BinaryID)
	}
	if secureCLI.created != nil {
		t.Fatal("existing binary should be policy-updated, not duplicated")
	}
	updates := secureCLI.updated[binaryID]
	if updates == nil {
		t.Fatal("expected existing global binary to be updated")
	}
	if updates["is_global"] != false || updates["enabled"] != true {
		t.Fatalf("expected explicit non-global enabled policy, got %#v", updates)
	}
	if got := secureCLI.binaries[0].BinaryPath; got == nil || *got != "/opt/goclaw/current/goclaw" {
		t.Fatalf("binary path not updated: %#v", got)
	}
	if len(grants.grants) != 1 {
		t.Fatalf("expected one explicit agent grant, got %d", len(grants.grants))
	}
	entries, err := store.ParseSecureCLIEnv(agentCreds.env[agentID])
	if err != nil {
		t.Fatalf("parse agent credential env: %v", err)
	}
	if entries["GOCLAW_SERVER"].Value != "http://127.0.0.1:19999" {
		t.Fatalf("GOCLAW_SERVER not normalized: %#v", entries["GOCLAW_SERVER"])
	}
}

func TestGatewayOperatorBootstrapRewritesUnsafeExistingNonGlobalBinary(t *testing.T) {
	setupTestToken(t, "test-gateway-token")
	binaryID := uuid.New()
	agentID := uuid.New()
	unsafePath := "/tmp/wrapper-goclaw"
	adapterName := "git"
	secureCLI := &gatewayOperatorSecureCLIStore{
		binaries: []store.SecureCLIBinary{{
			BaseModel:      store.BaseModel{ID: binaryID},
			BinaryName:     "goclaw",
			BinaryPath:     &unsafePath,
			Enabled:        true,
			IsGlobal:       false,
			DenyArgs:       json.RawMessage(`[]`),
			DenyVerbose:    json.RawMessage(`[]`),
			TimeoutSeconds: 300,
			AdapterName:    &adapterName,
		}},
	}
	grants := &gatewayOperatorGrantStore{}
	agentCreds := &gatewayOperatorAgentCredentialStore{}
	handler := &AgentsHandler{}
	handler.SetGatewayOperatorBootstrap(secureCLI, grants, agentCreds, "http://127.0.0.1:18790")
	handler.findGatewayOperatorBinary = func() (string, error) {
		return "/opt/goclaw/current/goclaw", nil
	}

	result, err := handler.bootstrapGatewayOperatorAccess(gatewayOperatorContext(), agentID)
	if err != nil {
		t.Fatalf("bootstrap returned error: %v", err)
	}
	if result.BinaryID != binaryID {
		t.Fatalf("expected existing binary %s, got %s", binaryID, result.BinaryID)
	}
	updates := secureCLI.updated[binaryID]
	if updates == nil {
		t.Fatal("expected unsafe existing binary policy to be rewritten before grant")
	}
	if got := secureCLI.binaries[0].BinaryPath; got == nil || *got != "/opt/goclaw/current/goclaw" {
		t.Fatalf("safe binary path not enforced: %#v", got)
	}
	if !strings.Contains(string(secureCLI.binaries[0].DenyArgs), "migrate") {
		t.Fatalf("safe deny policy not enforced: %s", string(secureCLI.binaries[0].DenyArgs))
	}
	if secureCLI.binaries[0].AdapterName != nil {
		t.Fatalf("gateway operator binary must use passthrough adapter, got %q", *secureCLI.binaries[0].AdapterName)
	}
	if len(grants.grants) != 1 || len(agentCreds.env[agentID]) == 0 {
		t.Fatal("expected grant and non-revealable credential after safe policy rewrite")
	}
}

func TestAgentsCreateAddsGatewayOperatorBootstrapMetadataWhenRequested(t *testing.T) {
	setupTestToken(t, "test-gateway-token")
	agents := &gatewayOperatorAgentStore{}
	secureCLI := &gatewayOperatorSecureCLIStore{}
	grants := &gatewayOperatorGrantStore{}
	agentCreds := &gatewayOperatorAgentCredentialStore{}
	handler := &AgentsHandler{
		agents:           agents,
		defaultWorkspace: "/tmp/workspace",
	}
	handler.SetGatewayOperatorBootstrap(secureCLI, grants, agentCreds, "http://127.0.0.1:18790")
	handler.findGatewayOperatorBinary = func() (string, error) {
		return "/usr/local/bin/goclaw", nil
	}

	body := []byte(`{
		"agent_key":"assistant",
		"display_name":"Assistant",
		"provider":"anthropic",
		"model":"claude",
		"grant_gateway_operator_access":true
	}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewReader(body))
	req = req.WithContext(gatewayOperatorContext())
	rr := httptest.NewRecorder()

	handler.handleCreate(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var response struct {
		ID                       uuid.UUID                       `json:"id"`
		AgentKey                 string                          `json:"agent_key"`
		GatewayOperatorBootstrap *gatewayOperatorBootstrapResult `json:"gateway_operator_bootstrap"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rr.Body.String())
	}
	if response.ID == uuid.Nil || response.AgentKey != "assistant" {
		t.Fatalf("unexpected agent response: %#v", response)
	}
	if response.GatewayOperatorBootstrap == nil || response.GatewayOperatorBootstrap.Status != "granted" {
		t.Fatalf("expected granted bootstrap metadata, got %#v body=%s", response.GatewayOperatorBootstrap, rr.Body.String())
	}
	if len(grants.grants) != 1 {
		t.Fatalf("expected one grant, got %d", len(grants.grants))
	}
	if len(agentCreds.env[response.ID]) == 0 {
		t.Fatal("expected per-agent gateway credentials to be stored")
	}
	if strings.Contains(rr.Body.String(), "test-gateway-token") {
		t.Fatalf("gateway token leaked in create response: %s", rr.Body.String())
	}
}

func TestGatewayOperatorBootstrapFailsClosedWithoutToken(t *testing.T) {
	setupTestToken(t, "")
	secureCLI := &gatewayOperatorSecureCLIStore{}
	grants := &gatewayOperatorGrantStore{}
	agentCreds := &gatewayOperatorAgentCredentialStore{}
	handler := &AgentsHandler{}
	handler.SetGatewayOperatorBootstrap(secureCLI, grants, agentCreds, "http://127.0.0.1:18790")
	handler.findGatewayOperatorBinary = func() (string, error) {
		return "/usr/local/bin/goclaw", nil
	}

	_, err := handler.bootstrapGatewayOperatorAccess(gatewayOperatorContext(), uuid.New())
	if err == nil {
		t.Fatal("expected missing gateway token to fail closed")
	}
	if secureCLI.created != nil || len(grants.grants) > 0 || len(agentCreds.env) > 0 {
		t.Fatal("missing token must not create binary, grant, or agent credential")
	}
}

func TestGatewayOperatorBootstrapReportsBinaryDiscoveryFailure(t *testing.T) {
	setupTestToken(t, "test-gateway-token")
	handler := &AgentsHandler{}
	handler.SetGatewayOperatorBootstrap(&gatewayOperatorSecureCLIStore{}, &gatewayOperatorGrantStore{}, &gatewayOperatorAgentCredentialStore{}, "http://127.0.0.1:18790")
	handler.findGatewayOperatorBinary = func() (string, error) {
		return "", errors.New("not found")
	}

	_, err := handler.bootstrapGatewayOperatorAccess(gatewayOperatorContext(), uuid.New())
	if !errors.Is(err, errGatewayOperatorBinaryMissing) {
		t.Fatalf("expected binary discovery warning, got %v", err)
	}
}

func TestGatewayOperatorFirstAgentGateUsesDeterministicEarliestRow(t *testing.T) {
	firstID := uuid.MustParse("0193a5b0-7000-7000-8000-000000000001")
	secondID := uuid.MustParse("0193a5b0-7000-7000-8000-000000000002")
	createdAt := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	got := deterministicFirstAgentID([]store.AgentData{
		{BaseModel: store.BaseModel{ID: secondID, CreatedAt: createdAt.Add(time.Second)}},
		{BaseModel: store.BaseModel{ID: firstID, CreatedAt: createdAt}},
	})

	if got != firstID {
		t.Fatalf("deterministic first id=%s, want %s", got, firstID)
	}

	tieWinner := deterministicFirstAgentID([]store.AgentData{
		{BaseModel: store.BaseModel{ID: secondID, CreatedAt: createdAt}},
		{BaseModel: store.BaseModel{ID: firstID, CreatedAt: createdAt}},
	})
	if tieWinner != firstID {
		t.Fatalf("tie winner id=%s, want lexical uuid %s", tieWinner, firstID)
	}
}
