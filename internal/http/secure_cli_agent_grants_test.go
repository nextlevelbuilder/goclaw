package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeSecureCLIGrantStore struct {
	binaries map[uuid.UUID]bool
	agents   map[uuid.UUID]bool
	grants   map[uuid.UUID]*store.SecureCLIAgentGrant

	createCalled bool
	updateCalled bool
	deleteCalled bool
}

func (s *fakeSecureCLIGrantStore) BinaryExists(_ context.Context, id uuid.UUID) (bool, error) {
	return s.binaries[id], nil
}

func (s *fakeSecureCLIGrantStore) AgentExists(_ context.Context, id uuid.UUID) (bool, error) {
	return s.agents[id], nil
}

func (s *fakeSecureCLIGrantStore) Create(_ context.Context, g *store.SecureCLIAgentGrant) error {
	s.createCalled = true
	if g.ID == uuid.Nil {
		g.ID = store.GenNewID()
	}
	s.grants[g.ID] = g
	return nil
}

func (s *fakeSecureCLIGrantStore) Get(_ context.Context, id uuid.UUID) (*store.SecureCLIAgentGrant, error) {
	if g := s.grants[id]; g != nil {
		cp := *g
		return &cp, nil
	}
	return nil, sql.ErrNoRows
}

func (s *fakeSecureCLIGrantStore) Update(context.Context, uuid.UUID, map[string]any) error {
	s.updateCalled = true
	return nil
}

func (s *fakeSecureCLIGrantStore) Delete(context.Context, uuid.UUID) error {
	s.deleteCalled = true
	return nil
}

func (s *fakeSecureCLIGrantStore) ListByBinary(_ context.Context, binaryID uuid.UUID) ([]store.SecureCLIAgentGrant, error) {
	grants := make([]store.SecureCLIAgentGrant, 0, len(s.grants))
	for _, grant := range s.grants {
		if grant == nil || grant.BinaryID != binaryID {
			continue
		}
		cp := *grant
		grants = append(grants, cp)
	}
	return grants, nil
}

func (s *fakeSecureCLIGrantStore) ListByAgent(context.Context, uuid.UUID) ([]store.SecureCLIAgentGrant, error) {
	return nil, nil
}

func (s *fakeSecureCLIGrantStore) UpdateGrantEnv(context.Context, uuid.UUID, []byte) error {
	s.updateCalled = true
	return nil
}

func requestWithGrantPath(method string, body io.Reader, binaryID, grantID uuid.UUID) (*httptest.ResponseRecorder, *http.Request) {
	req := httptest.NewRequest(method, "/v1/cli-credentials/"+binaryID.String()+"/agent-grants/"+grantID.String(), body)
	req.SetPathValue("id", binaryID.String())
	req.SetPathValue("grantId", grantID.String())
	ctx := store.WithTenantID(req.Context(), uuid.MustParse("0193a5b0-7000-7000-8000-000000000002"))
	ctx = store.WithRole(ctx, store.RoleOwner)
	ctx = store.WithUserID(ctx, "admin@example.com")
	return httptest.NewRecorder(), req.WithContext(ctx)
}

func requestWithBinaryPath(body io.Reader, binaryID uuid.UUID) (*httptest.ResponseRecorder, *http.Request) {
	req := httptest.NewRequest(http.MethodPost, "/v1/cli-credentials/"+binaryID.String()+"/agent-grants", body)
	req.SetPathValue("id", binaryID.String())
	ctx := store.WithTenantID(req.Context(), uuid.MustParse("0193a5b0-7000-7000-8000-000000000002"))
	ctx = store.WithRole(ctx, store.RoleOwner)
	ctx = store.WithUserID(ctx, "admin@example.com")
	return httptest.NewRecorder(), req.WithContext(ctx)
}

func TestSecureCLIGrantNestedRoutesRejectWrongBinaryParent(t *testing.T) {
	realBinaryID := uuid.New()
	pathBinaryID := uuid.New()
	grantID := uuid.New()
	fake := &fakeSecureCLIGrantStore{
		grants: map[uuid.UUID]*store.SecureCLIAgentGrant{
			grantID: {
				BaseModel:    store.BaseModel{ID: grantID},
				BinaryID:     realBinaryID,
				AgentID:      uuid.New(),
				Enabled:      true,
				EncryptedEnv: []byte(`{"TOKEN":"value"}`),
			},
		},
	}
	h := NewSecureCLIGrantHandler(fake, nil, nil)

	tests := []struct {
		name   string
		method string
		body   string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "get", method: http.MethodGet, call: h.handleGet},
		{name: "update", method: http.MethodPut, body: `{"enabled":false}`, call: h.handleUpdate},
		{name: "delete", method: http.MethodDelete, call: h.handleDelete},
		{name: "reveal", method: http.MethodPost, call: h.handleRevealEnv},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake.updateCalled = false
			fake.deleteCalled = false
			rr, req := requestWithGrantPath(tt.method, strings.NewReader(tt.body), pathBinaryID, grantID)
			tt.call(rr, req)
			if rr.Code != http.StatusNotFound {
				t.Fatalf("expected 404 for wrong binary parent, got %d body=%s", rr.Code, rr.Body.String())
			}
			if fake.updateCalled {
				t.Fatal("wrong-parent request must not update grant or env")
			}
			if fake.deleteCalled {
				t.Fatal("wrong-parent request must not delete grant")
			}
		})
	}
}

func TestSecureCLIGrantCreateValidatesBinaryAndAgentScope(t *testing.T) {
	binaryID := uuid.New()
	agentID := uuid.New()

	tests := []struct {
		name       string
		binaryOK   bool
		agentOK    bool
		wantStatus int
		wantCreate bool
	}{
		{name: "missing binary", binaryOK: false, agentOK: true, wantStatus: http.StatusNotFound},
		{name: "missing agent", binaryOK: true, agentOK: false, wantStatus: http.StatusNotFound},
		{name: "valid scope", binaryOK: true, agentOK: true, wantStatus: http.StatusCreated, wantCreate: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeSecureCLIGrantStore{
				binaries: map[uuid.UUID]bool{binaryID: tt.binaryOK},
				agents:   map[uuid.UUID]bool{agentID: tt.agentOK},
				grants:   map[uuid.UUID]*store.SecureCLIAgentGrant{},
			}
			h := NewSecureCLIGrantHandler(fake, nil, nil)
			rr, req := requestWithBinaryPath(strings.NewReader(`{"agent_id":"`+agentID.String()+`","enabled":true}`), binaryID)

			h.handleCreate(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d body=%s", tt.wantStatus, rr.Code, rr.Body.String())
			}
			if fake.createCalled != tt.wantCreate {
				t.Fatalf("createCalled=%v, want %v", fake.createCalled, tt.wantCreate)
			}
		})
	}
}

func TestValidateAndSerializeEnvVarsRejectsGoClawGatewayToken(t *testing.T) {
	rr := httptest.NewRecorder()

	envJSON, ok := validateAndSerializeEnvVars(rr, "en", json.RawMessage(`{
		"GOCLAW_GATEWAY_TOKEN": {"kind":"sensitive","value":"test-secret-token"}
	}`))

	if ok || envJSON != nil {
		t.Fatalf("expected GOCLAW_GATEWAY_TOKEN to be rejected by public env validator")
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "GOCLAW_GATEWAY_TOKEN") {
		t.Fatalf("expected rejected key in response, got %s", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "test-secret-token") {
		t.Fatalf("secret value leaked in validation error: %s", rr.Body.String())
	}
}

func TestSecureCLIGrantUpdateRejectsInvalidEnvVarsBeforeScalarUpdate(t *testing.T) {
	binaryID := uuid.New()
	grantID := uuid.New()
	fake := &fakeSecureCLIGrantStore{
		grants: map[uuid.UUID]*store.SecureCLIAgentGrant{
			grantID: {
				BaseModel: store.BaseModel{ID: grantID},
				BinaryID:  binaryID,
				AgentID:   uuid.New(),
				Enabled:   true,
			},
		},
	}
	h := NewSecureCLIGrantHandler(fake, nil, nil)
	rr, req := requestWithGrantPath(http.MethodPut, strings.NewReader(`{"enabled":false,"env_vars":123}`), binaryID, grantID)

	h.handleUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	if fake.updateCalled {
		t.Fatal("invalid env_vars request must not persist scalar grant updates")
	}
}

func TestSecureCLIGrantGetSanitizesMixedEnv(t *testing.T) {
	binaryID := uuid.New()
	grantID := uuid.New()
	fake := &fakeSecureCLIGrantStore{
		grants: map[uuid.UUID]*store.SecureCLIAgentGrant{
			grantID: {
				BaseModel:    store.BaseModel{ID: grantID},
				BinaryID:     binaryID,
				AgentID:      uuid.New(),
				Enabled:      true,
				EncryptedEnv: []byte(`{"TOKEN":"secret-token","PUBLIC_BASE_URL":{"kind":"value","value":"https://goclaw.sh"}}`),
			},
		},
	}
	h := NewSecureCLIGrantHandler(fake, nil, nil)
	rr, req := requestWithGrantPath(http.MethodGet, nil, binaryID, grantID)

	h.handleGet(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "secret-token") {
		t.Fatalf("sensitive grant env leaked in response: %s", rr.Body.String())
	}
	var got struct {
		Env map[string]store.SecureCLIEnvResponseEntry `json:"env"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Env["TOKEN"].Masked || got.Env["TOKEN"].Value != nil {
		t.Fatalf("TOKEN not masked: %#v", got.Env["TOKEN"])
	}
	if got.Env["PUBLIC_BASE_URL"].Value == nil || *got.Env["PUBLIC_BASE_URL"].Value != "https://goclaw.sh" {
		t.Fatalf("PUBLIC_BASE_URL not returned: %#v", got.Env["PUBLIC_BASE_URL"])
	}
}
