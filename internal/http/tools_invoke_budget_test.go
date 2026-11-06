package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// budgetProbeTool reports the request budget it finds in its context, the way
// read_image/read_audio/read_video read it before their budget guard.
type budgetProbeTool struct{ window, maxTokens int }

func (*budgetProbeTool) Name() string               { return "budget_probe" }
func (*budgetProbeTool) Description() string        { return "test" }
func (*budgetProbeTool) Parameters() map[string]any { return map[string]any{} }
func (p *budgetProbeTool) Execute(ctx context.Context, _ map[string]any) *tools.Result {
	p.window = store.AgentContextWindowFromContext(ctx)
	p.maxTokens = store.AgentMaxTokensFromContext(ctx)
	return tools.NewResult("ok")
}

type toolsInvokeAgentStore struct {
	store.AgentStore
	ag *store.AgentData // nil means the lookup fails
}

func (s *toolsInvokeAgentStore) GetByKey(context.Context, string) (*store.AgentData, error) {
	if s.ag == nil {
		return nil, sql.ErrNoRows
	}
	return s.ag, nil
}

// TestToolsInvoke_InjectsAgentBudget guards POST /v1/tools/invoke for media tools:
// without agent_context_window and agent_max_tokens in the context they fail with
// AgentBudgetWiringError. Unset columns and failed lookups use the loop defaults.
func TestToolsInvoke_InjectsAgentBudget(t *testing.T) {
	setupTestToken(t, "tok")
	tests := []struct {
		name       string
		ag         *store.AgentData
		wantWindow int
		wantMax    int
	}{
		{"explicit", &store.AgentData{AgentKey: "a", ContextWindow: 120000, MaxTokens: 4096}, 120000, 4096},
		{"unset falls back to defaults", &store.AgentData{AgentKey: "a"}, config.DefaultContextWindow, config.DefaultMaxTokens},
		{"lookup failure falls back to defaults", nil, config.DefaultContextWindow, config.DefaultMaxTokens},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := &budgetProbeTool{}
			reg := tools.NewRegistry()
			reg.Register(probe)
			h := NewToolsInvokeHandler(reg, &toolsInvokeAgentStore{ag: tt.ag})

			body, _ := json.Marshal(map[string]any{"tool": "budget_probe", "agentId": "a"})
			req := httptest.NewRequest(http.MethodPost, "/v1/tools/invoke", strings.NewReader(string(body)))
			req.Header.Set("Authorization", "Bearer tok")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if probe.window != tt.wantWindow {
				t.Errorf("context window = %d, want %d", probe.window, tt.wantWindow)
			}
			if probe.maxTokens != tt.wantMax {
				t.Errorf("max tokens = %d, want %d", probe.maxTokens, tt.wantMax)
			}
		})
	}
}
