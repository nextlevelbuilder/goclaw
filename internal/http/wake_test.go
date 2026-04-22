package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// stubAgent implements agent.Agent by capturing the ctx passed to Run.
type stubAgent struct {
	capturedCtx context.Context
	capturedReq agent.RunRequest
}

func (s *stubAgent) ID() string                   { return "eng" }
func (s *stubAgent) UUID() uuid.UUID              { return uuid.Nil }
func (s *stubAgent) OtherConfig() json.RawMessage { return nil }
func (s *stubAgent) IsRunning() bool              { return false }
func (s *stubAgent) Model() string                { return "stub-model" }
func (s *stubAgent) ProviderName() string         { return "stub" }
func (s *stubAgent) Provider() providers.Provider { return nil }

func (s *stubAgent) Run(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
	s.capturedCtx = ctx
	s.capturedReq = req
	return &agent.RunResult{Content: "ok"}, nil
}

// stubGetter implements agentGetter by returning the same stubAgent on every lookup.
type stubGetter struct{ a *stubAgent }

func (g stubGetter) Get(_ context.Context, _ string) (agent.Agent, error) {
	return g.a, nil
}

// TestHandleWake_PropagatesMetadataIntoCtx is the central contract: when a
// caller POSTs /v1/agents/{id}/wake with a `metadata` JSON object, the
// downstream agent run receives a ctx where tools.WakeMetadataFromCtx returns
// that object. Without this, the github_complete_check tool can't read
// check_run_id and every PR-review check run sticks `in_progress` forever.
func TestHandleWake_PropagatesMetadataIntoCtx(t *testing.T) {
	// Gateway token auth: simplest path past resolveAuth's admin branch.
	prevTok := pkgGatewayToken
	pkgGatewayToken = "test-gw-token"
	t.Cleanup(func() { pkgGatewayToken = prevTok })

	stub := &stubAgent{}
	h := &WakeHandler{agents: stubGetter{stub}}

	body := map[string]any{
		"message":     "Run /review on PR",
		"session_key": "eng:test",
		"metadata": map[string]any{
			"check_run_id": 72551876077,
			"pr_number":    4381,
			"repo_slug":    "cartridge-gg/internal",
			"event_type":   "agent-trigger-pr-review",
		},
	}
	raw, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/v1/agents/eng/wake", bytes.NewReader(raw))
	req.SetPathValue("id", "eng")
	req.Header.Set("Authorization", "Bearer test-gw-token")
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	h.handleWake(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", rw.Code, rw.Body.String())
	}
	if stub.capturedCtx == nil {
		t.Fatal("stub.Run not called")
	}

	md := tools.WakeMetadataFromCtx(stub.capturedCtx)
	if md == nil {
		t.Fatal("WakeMetadataFromCtx returned nil — metadata not propagated")
	}
	// json.Decode delivers JSON numbers as float64.
	if got, _ := md["check_run_id"].(float64); int64(got) != 72551876077 {
		t.Errorf("check_run_id = %v, want 72551876077", md["check_run_id"])
	}
	if got, _ := md["repo_slug"].(string); got != "cartridge-gg/internal" {
		t.Errorf("repo_slug = %v, want cartridge-gg/internal", md["repo_slug"])
	}
	if got, _ := md["event_type"].(string); got != "agent-trigger-pr-review" {
		t.Errorf("event_type not propagated")
	}
}

// TestHandleWake_NoMetadataLeavesCtxUnchanged confirms the ctx-key is
// absent (not just empty) when the wake had no metadata — belt-and-braces
// for tools that distinguish "no metadata at all" from "empty metadata".
func TestHandleWake_NoMetadataLeavesCtxUnchanged(t *testing.T) {
	prevTok := pkgGatewayToken
	pkgGatewayToken = "test-gw-token"
	t.Cleanup(func() { pkgGatewayToken = prevTok })

	stub := &stubAgent{}
	h := &WakeHandler{agents: stubGetter{stub}}

	body := map[string]any{"message": "hello"}
	raw, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/v1/agents/eng/wake", bytes.NewReader(raw))
	req.SetPathValue("id", "eng")
	req.Header.Set("Authorization", "Bearer test-gw-token")
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	h.handleWake(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", rw.Code, rw.Body.String())
	}
	if md := tools.WakeMetadataFromCtx(stub.capturedCtx); md != nil {
		t.Errorf("expected nil metadata, got %+v", md)
	}
}

func TestHandleWake_MissingMessageRejected(t *testing.T) {
	prevTok := pkgGatewayToken
	pkgGatewayToken = "test-gw-token"
	t.Cleanup(func() { pkgGatewayToken = prevTok })

	h := &WakeHandler{agents: stubGetter{&stubAgent{}}}

	body := map[string]any{"metadata": map[string]any{"check_run_id": 1}}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/agents/eng/wake", bytes.NewReader(raw))
	req.SetPathValue("id", "eng")
	req.Header.Set("Authorization", "Bearer test-gw-token")
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	h.handleWake(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rw.Code)
	}
	if !strings.Contains(rw.Body.String(), "message is required") {
		t.Errorf("error message should say 'message is required', got: %s", rw.Body.String())
	}
}
