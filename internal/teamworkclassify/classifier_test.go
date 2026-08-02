package teamworkclassify

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakePinnedSkillsBuilder struct {
	summary string
	got     []string
}

func (b *fakePinnedSkillsBuilder) BuildPinnedSummary(_ context.Context, names []string) string {
	b.got = append([]string(nil), names...)
	return b.summary
}

type pinnedSkillsAgentStore struct {
	store.AgentStore
	agent *store.AgentData
}

func (s *pinnedSkillsAgentStore) GetByID(context.Context, uuid.UUID) (*store.AgentData, error) {
	return s.agent, nil
}

func (s *pinnedSkillsAgentStore) GetByIDs(_ context.Context, ids []uuid.UUID) ([]store.AgentData, error) {
	if s.agent == nil {
		return nil, nil
	}
	for _, id := range ids {
		if id == s.agent.ID {
			return []store.AgentData{*s.agent}, nil
		}
	}
	return nil, nil
}

func TestBuildRecentContextKeepsBothEndsOfLongRelevantResponse(t *testing.T) {
	long := "SCOPE-BEGIN: vĩ mô, lãi suất, tỷ giá, chứng khoán." + strings.Repeat(" dữ liệu", 900) + " SCOPE-END: trái phiếu, bất động sản, vàng và ba kịch bản."
	context := BuildRecentContext([]providers.Message{
		{Role: "user", Content: "Nghiên cứu toàn cảnh tài chính Việt Nam cuối năm 2026"},
		{Role: "assistant", Content: long},
	}, "Thế còn nửa đầu năm 2027 thì sao?")
	for _, want := range []string{"SCOPE-BEGIN", "SCOPE-END", "[middle omitted]", "Nghiên cứu toàn cảnh tài chính Việt Nam"} {
		if !strings.Contains(context, want) {
			t.Fatalf("recent context dropped %q: %s", want, context)
		}
	}
}

// The planner keeps its larger share of a CONFIGURED budget. Taking
// Input.Timeout literally for both stages meant that configuring 45s to rescue a
// slow arbiter silently CUT the planner from its 60s default to 45s — a setting
// meant to stop timeouts would have caused one. The ratio must be preserved
// instead, at any configured value.
func TestStageTimeoutsPreservePlannerRatio(t *testing.T) {
	arbiter, planner := stageTimeouts(0)
	if arbiter != defaultArbiterTimeout || planner != defaultPlannerTimeout {
		t.Fatalf("unset timeout = (%s, %s), want built-in defaults (%s, %s)", arbiter, planner, defaultArbiterTimeout, defaultPlannerTimeout)
	}
	arbiter, planner = stageTimeouts(45 * time.Second)
	if arbiter != 45*time.Second {
		t.Fatalf("configured arbiter = %s, want 45s", arbiter)
	}
	if planner <= arbiter {
		t.Fatalf("configured planner = %s, must stay larger than arbiter %s", planner, arbiter)
	}
	if planner != 90*time.Second {
		t.Fatalf("configured planner = %s, want 90s (45s scaled by the 2x built-in ratio)", planner)
	}
}

type fakeArbiterProvider struct {
	content  string
	err      error
	requests []providers.ChatRequest
}

func (p *fakeArbiterProvider) Chat(_ context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	p.requests = append(p.requests, req)
	if p.err != nil {
		return nil, p.err
	}
	return &providers.ChatResponse{Content: p.content}, nil
}

func (p *fakeArbiterProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return nil, nil
}

func (p *fakeArbiterProvider) DefaultModel() string { return "fake-model" }
func (p *fakeArbiterProvider) Name() string         { return "fake-provider" }

func TestNormalizeArbiterContentHandlesEscapedBraces(t *testing.T) {
	raw := `prefix {"reason":"quoted \"{value}\" and slash \\ ","decision":"self"} suffix`
	got, err := normalizeArbiterContent(raw)
	if err != nil {
		t.Fatalf("normalizeArbiterContent() error = %v", err)
	}
	if !json.Valid([]byte(got)) {
		t.Fatalf("normalized object is invalid JSON: %s", got)
	}
}

func TestBuildInputFromStoresIncludesOnlyPinnedSkillContext(t *testing.T) {
	agentID := uuid.New()
	builder := &fakePinnedSkillsBuilder{summary: `<available_skills><skill_instructions name="Workflow">workflow body mentioning Strategy, Analytics, and QA</skill_instructions></available_skills>`}
	input := BuildInputFromStores(context.Background(), ProfileStores{
		Agents: &pinnedSkillsAgentStore{agent: &store.AgentData{
			BaseModel:        store.BaseModel{ID: agentID},
			AgentKey:         "developer",
			AgentDescription: "technical developer",
			OtherConfig:      []byte(`{"pinned_skills":["one","two","three","four","five","six","seven","eight","nine","ten","eleven"],"capabilities":["technical"]}`),
		}},
		PinnedSkills: builder,
	}, BuildInputOptions{Mode: ModeDelegate, AgentID: agentID})

	if len(input.PinnedSkillNames) != 10 || len(builder.got) != 10 {
		t.Fatalf("pinned names = %v, builder got = %v; want max 10", input.PinnedSkillNames, builder.got)
	}
	if input.PinnedSkillsContext != builder.summary || input.PinnedSkillsWarning != "" {
		t.Fatalf("pinned context = %q warning = %q", input.PinnedSkillsContext, input.PinnedSkillsWarning)
	}
	if len(input.CurrentAgent.Capabilities) != 1 || input.CurrentAgent.Capabilities[0].Key != string(CapabilityTechnical) {
		t.Fatalf("current agent capabilities = %v; pinned skill text must not affect deterministic capabilities", input.CurrentAgent.Capabilities)
	}
}

func TestBuildInputFromStoresPinnedSkillLoaderIsOptional(t *testing.T) {
	agentID := uuid.New()
	input := BuildInputFromStores(context.Background(), ProfileStores{
		Agents: &pinnedSkillsAgentStore{agent: &store.AgentData{
			BaseModel:   store.BaseModel{ID: agentID},
			AgentKey:    "lead",
			OtherConfig: []byte(`{"pinned_skills":["workflow"]}`),
		}},
	}, BuildInputOptions{Mode: ModeDelegate, AgentID: agentID})
	if input.PinnedSkillsContext != "" || input.PinnedSkillsWarning == "" {
		t.Fatalf("context = %q warning = %q, want optional loader warning", input.PinnedSkillsContext, input.PinnedSkillsWarning)
	}
}
