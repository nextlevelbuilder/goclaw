package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

type stubHTTPAgent struct {
	id      string
	model   string
	lastReq agent.RunRequest
}

func (s *stubHTTPAgent) ID() string                   { return s.id }
func (s *stubHTTPAgent) IsRunning() bool              { return false }
func (s *stubHTTPAgent) Model() string                { return s.model }
func (s *stubHTTPAgent) ProviderName() string         { return "stub" }
func (s *stubHTTPAgent) Provider() providers.Provider { return nil }
func (s *stubHTTPAgent) Run(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
	s.lastReq = req
	return &agent.RunResult{Content: "ok"}, nil
}

func TestChatCompletions_UserFieldFallbacksWithoutHeader(t *testing.T) {
	setupTestToken(t, "gateway-token")

	router := agent.NewRouter()
	stub := &stubHTTPAgent{id: "prod-operator", model: "stub-model"}
	router.SetResolver(func(context.Context, string) (agent.Agent, error) {
		return stub, nil
	})

	h := NewChatCompletionsHandler(router, nil, true)
	body := `{"model":"goclaw:prod-operator","messages":[{"role":"user","content":"ping"}],"user":"api-user-1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer gateway-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if stub.lastReq.UserID != "api-user-1" {
		t.Fatalf("RunRequest.UserID = %q, want api-user-1", stub.lastReq.UserID)
	}

	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Choices) != 1 || payload.Choices[0].Message.Content != "ok" {
		t.Fatalf("unexpected response payload: %s", rec.Body.String())
	}
}
