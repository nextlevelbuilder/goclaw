package teamworkclassify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func structuredProfile(kind, name, key, role, text string, capabilities ...Capability) Profile {
	declared := make([]StructuredCapability, 0, len(capabilities))
	for _, capability := range capabilities {
		declared = append(declared, StructuredCapability{Key: string(capability)})
	}
	return Profile{
		Kind:               kind,
		Name:               name,
		AgentID:            uuid.New(),
		AgentKey:           key,
		DisplayName:        name,
		TeamRole:           role,
		Text:               text,
		ExpertiseSummary:   text,
		Capabilities:       declared,
		CapabilitiesStatus: DataStatusKnown,
	}
}

func withKnownOrchestrationTool(input Input) Input {
	tool := requiredToolForMode(input.Mode)
	if tool == "" {
		return input
	}
	if input.CurrentAgent.AgentID == uuid.Nil {
		input.CurrentAgent.AgentID = uuid.New()
	}
	if input.CurrentAgent.AgentKey == "" {
		input.CurrentAgent.AgentKey = "current-agent"
	}
	input.CurrentAgent.AvailableToolsStatus = DataStatusKnown
	input.CurrentAgent.AvailableTools = []string{tool}

	profiles := &input.Members
	if input.Mode == ModeDelegate {
		profiles = &input.Delegates
	}
	for i := range *profiles {
		if (*profiles)[i].AgentID == uuid.Nil {
			(*profiles)[i].AgentID = uuid.New()
		}
		if (*profiles)[i].AgentKey == "" {
			(*profiles)[i].AgentKey = fmt.Sprintf("test-collaborator-%d", i+1)
		}
	}
	if input.Mode == ModeTeam && input.CoordinatorAgentID == uuid.Nil {
		if strings.EqualFold(input.TeamRole, "lead") || input.TeamRole == "" {
			input.CoordinatorAgentID = input.CurrentAgent.AgentID
			input.CoordinatorAgentKey = input.CurrentAgent.AgentKey
		} else {
			lead := structuredProfile("team_member", "Team Lead", "team-lead", "lead", "canonical coordinator", CapabilityLeadCoordinator)
			lead.AvailableToolsStatus = DataStatusKnown
			lead.AvailableTools = []string{tool}
			input.Members = append(input.Members, lead)
			input.CoordinatorAgentID = lead.AgentID
			input.CoordinatorAgentKey = lead.AgentKey
		}
	}
	return input
}

type fakeEmbedder map[string][]float32

func (f fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		var vec []float32
		for key, v := range f {
			if strings.Contains(text, key) {
				vec = v
				break
			}
		}
		if vec == nil {
			vec = []float32{0, 0}
		}
		out = append(out, vec)
	}
	return out, nil
}

type errEmbedder struct{}

func (errEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("embedding provider down")
}

func TestBuildEmbeddingEvidenceSpawnModeUnavailable(t *testing.T) {
	evidence := BuildEmbeddingEvidence(context.Background(), Input{
		Mode:    ModeSpawn,
		Message: "lập kế hoạch content và phân tích chiến lược",
		Embedder: fakeEmbedder{
			"lập kế hoạch": {1, 0},
		},
	})
	if evidence.Available {
		t.Fatalf("Evidence = %+v, want unavailable", evidence)
	}
	if evidence.Reason == "" {
		t.Fatal("Reason is empty")
	}
}

func TestBuildEmbeddingEvidenceScoresProfilesWithoutDecision(t *testing.T) {
	evidence := BuildEmbeddingEvidence(context.Background(), Input{
		Mode:    ModeTeam,
		Message: "lập kế hoạch content và phân tích chiến lược cho chiến dịch mới",
		CurrentAgent: Profile{
			Kind: "agent",
			Name: "Bảo An",
			Text: "điều phối chung",
		},
		SelfTools: []Profile{
			{Kind: "tool", Name: "self chat", Text: "trả lời trò chuyện nhanh"},
		},
		Team: Profile{
			Kind: "team",
			Name: "Growth Team",
			Text: "team content chiến lược chiến dịch",
		},
		Members: []Profile{
			{Kind: "member", Name: "Bảo Ly Content", Text: "content bài viết kịch bản truyền thông"},
			{Kind: "member", Name: "Bảo Ly Chiến lược", Text: "chiến lược kế hoạch phân tích"},
		},
		CollaborationTools: []Profile{
			{Kind: "tool", Name: "team_tasks", Text: "chia việc giao task cho thành viên team"},
		},
		Embedder: fakeEmbedder{
			"lập kế hoạch": {1, 0},
			"content":      {1, 0},
			"chiến lược":   {1, 0},
			"điều phối":    {0, 1},
			"trò chuyện":   {0, 1},
		},
	})
	if !evidence.Available {
		t.Fatalf("Evidence = %+v, want available", evidence)
	}
	if evidence.CollaborationScore <= evidence.SelfScore {
		t.Fatalf("CollaborationScore %.3f must be greater than SelfScore %.3f", evidence.CollaborationScore, evidence.SelfScore)
	}
}

func TestBuildEmbeddingEvidenceScoresCurrentAgentTools(t *testing.T) {
	evidence := BuildEmbeddingEvidence(context.Background(), Input{
		Mode:    ModeDelegate,
		Message: "dịch nhanh câu này sang tiếng Anh",
		CurrentAgent: Profile{
			Kind: "agent",
			Name: "Translator",
			Text: "dịch thuật trả lời ngắn",
		},
		SelfTools: []Profile{
			{Kind: "tool", Name: "translate", Text: "dịch nhanh văn bản"},
		},
		Delegates: []Profile{
			{Kind: "delegate", Name: "Planner", Text: "lập kế hoạch dự án dài"},
		},
		Embedder: fakeEmbedder{
			"dịch nhanh":   {1, 0},
			"dịch thuật":   {1, 0},
			"lập kế hoạch": {0, 1},
		},
	})
	if !evidence.Available {
		t.Fatalf("Evidence = %+v, want available", evidence)
	}
	if evidence.SelfScore <= evidence.CollaborationScore {
		t.Fatalf("SelfScore %.3f must be greater than CollaborationScore %.3f", evidence.SelfScore, evidence.CollaborationScore)
	}
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

func TestClassifyDefaultsToSelfWhenScoresAreClose(t *testing.T) {
	result := Classify(context.Background(), Input{
		Mode:         ModeTeam,
		Message:      "phân tích giúp việc này nên xử lý theo cá nhân hay theo team",
		CurrentAgent: Profile{Kind: "agent", Name: "Lead", Text: "xử lý yêu cầu chung"},
		Team:         Profile{Kind: "team", Name: "Team", Text: "xử lý yêu cầu chung theo team"},
		Embedder: fakeEmbedder{
			"phân tích":           {1, 0},
			"xử lý yêu cầu chung": {1, 0},
		},
	})
	if result.Decision != DecisionSelf {
		t.Fatalf("Decision = %q, want %q; scores %.3f %.3f", result.Decision, DecisionSelf, result.SelfScore, result.CollaborationScore)
	}
}

func TestClassifySelfForCasualOrWeakCloseMessages(t *testing.T) {
	cases := []struct {
		name    string
		message string
	}{
		{name: "casual greeting", message: "chào em"},
		{name: "weak close scores", message: "anh hỏi thêm chút thôi"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := Classify(context.Background(), Input{
				Mode:         ModeTeam,
				Message:      tc.message,
				CurrentAgent: Profile{Kind: "agent", Name: "Lead", Text: "xử lý yêu cầu chung"},
				Team:         Profile{Kind: "team", Name: "Team", Text: "xử lý yêu cầu chung theo team"},
				Embedder: fakeEmbedder{
					"xử lý yêu cầu chung": {1, 0},
				},
			})
			if result.Decision != DecisionSelf {
				t.Fatalf("Decision = %q, want %q; result=%+v", result.Decision, DecisionSelf, result)
			}
		})
	}
}

func TestClassifyWithLLMUsesLongerDefaultArbiterTimeout(t *testing.T) {
	provider := &fakeArbiterProvider{content: arbiterJSON("self", "strong", "none", "self_work", "", "direct enough", false)}
	result := ClassifyWithLLM(context.Background(), Input{
		Mode:         ModeTeam,
		Message:      "nghiên cứu về tình hình vàng trong nước",
		CurrentAgent: structuredProfile("agent", "Bảo Khánh", "bao-khanh", "member", "Senior developer, coding, debugging, API integration", CapabilityTechnical),
		Team:         Profile{Kind: "team", Name: "Marketing Team", Text: "strategy research analytics content technical"},
		Members:      []Profile{structuredProfile("team_member", "Minh Strategy", "minh-strategy", "member", "market research, customer research, competitor research, positioning", CapabilityResearch, CapabilityStrategy)},
		Embedder: fakeEmbedder{
			"nghiên cứu":       {1, 0},
			"market research":  {1, 0},
			"Senior developer": {0, 1},
		},
	}, provider, "arbiter-model", nil)
	if result.Decision != DecisionSelf {
		t.Fatalf("Decision = %q, want %q", result.Decision, DecisionSelf)
	}
	if provider.deadlineRemaining < 25*time.Second {
		t.Fatalf("arbiter deadline remaining = %s, want at least 25s", provider.deadlineRemaining)
	}
}

// A configured Input.Timeout WIDENS the arbiter deadline. This is the point of
// making it configurable: the built-in 30s is a hard-coded constant while model
// latency is a per-agent property, so an agent whose runtime model needs ~30s
// per turn otherwise degrades at whichever of the five sequential stages happens
// to cross the line.
func TestClassifyWithLLMWidensArbiterDeadlineFromInputTimeout(t *testing.T) {
	provider := &fakeArbiterProvider{content: arbiterJSON("self", "strong", "none", "self_work", "", "direct enough", false)}
	input := timeoutProbeInput()
	input.Timeout = 120 * time.Second
	_ = ClassifyWithLLM(context.Background(), input, provider, "arbiter-model", nil)
	if provider.deadlineRemaining < 110*time.Second {
		t.Fatalf("arbiter deadline remaining = %s, want at least 110s from configured timeout", provider.deadlineRemaining)
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
	if got := plannerStageTimeout(0); got != defaultPlannerTimeout {
		t.Fatalf("plannerStageTimeout(0) = %s, want %s", got, defaultPlannerTimeout)
	}
	if got := plannerStageTimeout(45 * time.Second); got != 90*time.Second {
		t.Fatalf("plannerStageTimeout(45s) = %s, want 90s", got)
	}
}

// timeoutProbeInput is a minimal self-routing team input used only to observe
// which deadline the classifier hands to its provider calls.
func timeoutProbeInput() Input {
	return Input{
		Mode:         ModeTeam,
		Message:      "nghiên cứu về tình hình vàng trong nước",
		CurrentAgent: structuredProfile("agent", "Bảo Khánh", "bao-khanh", "member", "Senior developer, coding, debugging, API integration", CapabilityTechnical),
		Team:         Profile{Kind: "team", Name: "Marketing Team", Text: "strategy research analytics content technical"},
		Members:      []Profile{structuredProfile("team_member", "Minh Strategy", "minh-strategy", "member", "market research, customer research, competitor research, positioning", CapabilityResearch, CapabilityStrategy)},
		Embedder: fakeEmbedder{
			"nghiên cứu":       {1, 0},
			"market research":  {1, 0},
			"Senior developer": {0, 1},
		},
	}
}

func TestClassifyWithLLMDoesNotCapArbiterOutputTokens(t *testing.T) {
	provider := &fakeArbiterProvider{content: arbiterJSON("self", "strong", "none", "self_work", "", "direct enough", false)}
	_ = ClassifyWithLLM(context.Background(), Input{
		Mode:         ModeTeam,
		Message:      "nghiên cứu và tổng hợp giúp anh",
		CurrentAgent: Profile{Kind: "agent", Name: "Bảo Khánh", Text: "developer"},
		Team:         Profile{Kind: "team", Name: "Marketing Team", Text: "strategy research content"},
		Members:      []Profile{{Kind: "team_member", Name: "Huy Minh", Text: "market research strategy"}},
		Embedder: fakeEmbedder{
			"nghiên cứu": {1, 0},
			"developer":  {0, 1},
		},
	}, provider, "arbiter-model", nil)
	if _, ok := provider.req.Options[providers.OptMaxTokens]; ok {
		t.Fatalf("arbiter request must not cap output tokens, options=%+v", provider.req.Options)
	}
}

func TestClassifyWithLLMFailsSafeOnArbiterError(t *testing.T) {
	provider := &fakeArbiterProvider{err: errors.New("provider unavailable")}
	result := ClassifyWithLLM(context.Background(), Input{
		Mode:         ModeTeam,
		Message:      "nghiên cứu về tình hình vàng trong nước",
		CurrentAgent: structuredProfile("agent", "Bảo Khánh", "bao-khanh", "member", "Senior developer, coding, debugging, API integration", CapabilityTechnical),
		Team:         Profile{Kind: "team", Name: "Marketing Team", Text: "strategy research analytics content technical"},
		Members:      []Profile{structuredProfile("team_member", "Minh Strategy", "minh-strategy", "member", "market research, customer research, competitor research, positioning", CapabilityResearch, CapabilityStrategy)},
		Embedder: fakeEmbedder{
			"nghiên cứu":       {1, 0},
			"market research":  {1, 0},
			"Senior developer": {0, 1},
		},
	}, provider, "arbiter-model", nil)
	if result.Decision != DecisionSelf || result.RequiredTool != "" || result.BestTeamOwner != "" {
		t.Fatalf("ClassifyWithLLM result = %+v, want self without teammate mutation", result)
	}
	if result.DegradedReasonCode != "intent_resolver_transport_failed" {
		t.Fatalf("DegradedReasonCode = %q, want intent_resolver_transport_failed", result.DegradedReasonCode)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider error calls = %d, want no repair/retry", len(provider.requests))
	}
}

func TestClassifyWithLLMCallsArbiterWhenEmbeddingFails(t *testing.T) {
	provider := &fakeArbiterProvider{content: arbiterJSON("self", "strong", "none", "self_work", "", "direct work", false)}
	result := ClassifyWithLLM(context.Background(), Input{
		Mode:         ModeTeam,
		Message:      "tổng hợp lại tài liệu này",
		CurrentAgent: Profile{Kind: "agent", Name: "Lead", Text: "summarize existing materials"},
		Team:         Profile{Kind: "team", Name: "Team", Text: "team work"},
		Members:      []Profile{{Kind: "member", Name: "Member", Text: "team member"}},
		Embedder:     errEmbedder{},
	}, provider, "arbiter-model", nil)
	if len(provider.requests) != 6 {
		t.Fatalf("model calls = %d, want full intent, shape, and assignment review pipeline", len(provider.requests))
	}
	if result.EmbeddingAvailable {
		t.Fatalf("EmbeddingAvailable = true, want false; result=%+v", result)
	}
	if result.Decision != DecisionSelf {
		t.Fatalf("Decision = %q, want self", result.Decision)
	}
	intentReq := provider.requests[0]
	intentPrompt := intentReq.Messages[0].Content + "\n" + intentReq.Messages[1].Content
	if !strings.Contains(intentPrompt, "tổng hợp lại tài liệu này") {
		t.Fatalf("intent prompt missing current request:\n%s", intentPrompt)
	}
	planningReq := provider.requests[4]
	planningPrompt := planningReq.Messages[0].Content + "\n" + planningReq.Messages[1].Content
	if !strings.Contains(planningPrompt, "summarize existing materials") || !strings.Contains(planningPrompt, "Member") || !strings.Contains(planningPrompt, `"members"`) {
		t.Fatalf("assignment prompt missing complete canonical roster:\n%s", planningPrompt)
	}
}

func TestValidateRoutingDecisionPreservesModelSelectedCanonicalOwner(t *testing.T) {
	selected := structuredProfile("team_member", "Bảo Ngọc", "bao-ngoc", "member", "full profile says this agent owns the requested analysis", CapabilityResearch)
	other := structuredProfile("team_member", "Bảo Khánh", "bao-khanh", "member", "structured key happens to match analytics", CapabilityAnalyticsCritic)
	input := withKnownOrchestrationTool(Input{
		Mode:         ModeTeam,
		Message:      "đánh giá dữ liệu theo hồ sơ đầy đủ của team",
		CurrentAgent: structuredProfile("agent", "Bảo An", "bao-an", "lead", "team coordinator", CapabilityLeadCoordinator),
		Team:         Profile{Kind: "team", Name: "Team", Text: "analysis team"},
		Members:      []Profile{selected, other},
		TeamRole:     "lead",
	})
	result := ValidateRoutingDecision(input, Evidence{}, Result{
		Decision:             DecisionTeam,
		WorkflowMode:         WorkflowModeSingleOwner,
		TaskType:             "analytics",
		CurrentAgentFit:      "weak",
		BestTeamOwner:        selected.AgentKey,
		BestTeamOwnerRole:    "analyst",
		BestTeamFit:          "strong",
		WorkflowExecutable:   true,
		RequiredTool:         "team_tasks",
		OwnerSelectionReason: "selected from complete roster profile",
	})
	if result.Decision != DecisionTeam || result.BestTeamOwnerID != selected.AgentID || result.BestTeamOwner != selected.AgentKey {
		t.Fatalf("canonical owner was overridden by local capability scoring: %+v", result)
	}
}

type fakeArbiterProvider struct {
	content           string
	contents          []string
	err               error
	req               providers.ChatRequest
	requests          []providers.ChatRequest
	deadlineRemaining time.Duration
	// shapeTrait overrides the shape-verifier stage reply with a single trait of
	// this type (evidence quoted from the current request). Empty keeps the
	// default atomic/single_bounded_output contract every other test relies on.
	shapeTrait ShapeTraitType
	// shapeFirstReply, when non-empty, is returned for the FIRST shape-verifier
	// call only; later calls (i.e. the repair attempt) get the normal contract.
	// Used to exercise the bounded shape repair stage.
	shapeFirstReply string
	// shapeAlwaysBad returns shapeFirstReply for EVERY shape-verifier call, so the
	// repair attempt fails the contract too.
	shapeAlwaysBad bool
	shapeCalls     int
	// plannerReply, when non-empty, is returned for EVERY planner/plan-revision
	// call, so a test can drive the planner stage into parse or validation failure
	// while the earlier stages still honour their contracts.
	plannerReply string
}

func (p *fakeArbiterProvider) Chat(ctx context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	p.req = req
	p.requests = append(p.requests, req)
	if deadline, ok := ctx.Deadline(); ok {
		p.deadlineRemaining = time.Until(deadline)
	}
	if p.err != nil {
		return nil, p.err
	}
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "Resolve the current user message into a complete standalone request") {
		var payload struct {
			CurrentUserMessage string `json:"current_user_message"`
		}
		_ = json.Unmarshal([]byte(req.Messages[len(req.Messages)-1].Content), &payload)
		return &providers.ChatResponse{Content: fmt.Sprintf(`{"standalone_request":%q,"relation":"new","user_intent":"execute the current request","inherited_scope":[],"requested_deliverables":[],"quality_requirements":[],"explicit_constraints":[],"ambiguities":[],"needs_clarification":false}`, payload.CurrentUserMessage)}, nil
	}
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "Independently verify that the draft standalone request") {
		return &providers.ChatResponse{Content: `{"valid":true,"issues":[],"corrected_resolution":null}`}, nil
	}
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "decompose one already-resolved standalone user request") {
		source := p.content
		if len(p.contents) > 0 {
			source = p.contents[0]
		}
		var payload map[string]any
		if json.Unmarshal([]byte(source), &payload) != nil {
			return &providers.ChatResponse{Content: source}, nil
		}
		mode, _ := payload["workflow_mode"].(string)
		if mode == "" {
			if decision, _ := payload["decision"].(string); decision == "self" {
				mode = "self"
			}
		}
		reason, _ := payload["reason"].(string)
		assessment := WorkAssessment{
			WorkflowMode: WorkflowMode(mode), IndependentReviewRequired: mode == "multi_role", Reason: reason,
			WorkUnits:       []WorkUnit{{ID: "produce", Description: "produce requested result", RequiredOutput: "requested result"}},
			RequiredOutputs: []string{"requested result"},
		}
		if mode == "multi_role" {
			assessment.WorkUnits = []WorkUnit{
				{ID: "draft", Description: "produce primary work", RequiredOutput: "draft"},
				{ID: "review", Description: "review independently", RequiredOutput: "critique"},
				{ID: "integrate", Description: "integrate outputs", RequiredOutput: "final result"},
			}
			assessment.Dependencies = []WorkDependency{{From: "draft", To: "review"}, {From: "review", To: "integrate"}}
			assessment.RequiredOutputs = []string{"draft", "critique", "final result"}
		}
		raw, _ := json.Marshal(assessment)
		return &providers.ChatResponse{Content: string(raw)}, nil
	}
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "independently critique a proposed execution assignment") {
		return &providers.ChatResponse{Content: `{"valid":true,"issues":[]}`}, nil
	}
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "independently verify the semantic work shape") {
		evidence := fakeCurrentRequestEvidence(req.Messages)
		p.shapeCalls++
		if p.shapeFirstReply != "" && (p.shapeAlwaysBad || p.shapeCalls == 1) {
			return &providers.ChatResponse{Content: p.shapeFirstReply}, nil
		}
		if p.shapeTrait != "" {
			traits := []ShapeTrait{{Type: p.shapeTrait, Source: ShapeEvidenceCurrentRequest, Evidence: evidence}}
			raw, _ := json.Marshal(ShapeAssessment{
				WorkShape:                 DeriveWorkShape(traits),
				ShapeTraits:               traits,
				IndependentReviewRequired: EffectiveReviewRequired(DeriveWorkShape(traits), traits),
			})
			return &providers.ChatResponse{Content: string(raw)}, nil
		}
		return &providers.ChatResponse{Content: fmt.Sprintf(`{"work_shape":"atomic","shape_traits":[{"type":"single_bounded_output","source":"current_request","evidence":%q}],"independent_review_required":false}`, evidence)}, nil
	}
	if p.plannerReply != "" && len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "You select canonical owners") {
		return &providers.ChatResponse{Content: p.plannerReply}, nil
	}
	if len(p.contents) > 0 {
		content := p.contents[0]
		p.contents = p.contents[1:]
		return &providers.ChatResponse{Content: ensureFakeShapeContract(content, req)}, nil
	}
	return &providers.ChatResponse{Content: ensureFakeShapeContract(p.content, req)}, nil
}

func (p *fakeArbiterProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return nil, nil
}

func (p *fakeArbiterProvider) DefaultModel() string { return "fake-model" }
func (p *fakeArbiterProvider) Name() string         { return "fake-provider" }

func arbiterJSON(decision, currentFit, collaboratorFit, requestKind, requiredTool, reason string, executable bool) string {
	taskType := "other"
	if requestKind == "team_work" {
		taskType = "research"
	} else if requestKind == "self_work" {
		taskType = "dev"
	}
	bestOwner := ""
	if collaboratorFit != "none" {
		bestOwner = "best teammate"
	}
	bestOwnerRole := ""
	if bestOwner != "" {
		bestOwnerRole = bestOwner
	}
	workflowMode := "single_owner"
	if decision == "self" {
		workflowMode = "self"
	}
	return fmt.Sprintf(`{"workflow_mode":%q,"work_shape":"atomic","shape_traits":[{"type":"single_bounded_output","source":"current_request","evidence":"test request"}],"independent_review_required":false,"current_agent_role":%q,"task_type":%q,"current_agent_fit":%q,"best_team_owner":%q,"best_team_owner_role":%q,"best_team_fit":%q,"specialist_match_found":%t,"lead_selected_as_fallback":%t,"routing_priority_used":%q,"owner_selection_reason":%q,"followup_context_used_for_reference_only":%t,"workflow_executable":%t,"decision":%q,"required_tool":%q,"reason":%q}`,
		workflowMode, "current agent", taskType, currentFit, bestOwner, bestOwnerRole, collaboratorFit, collaboratorFit != "none", false, "role_task_match", "owner selected by role fit", true, executable, decision, requiredTool, reason)
}

func fakeCurrentRequestEvidence(messages []providers.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		content := messages[i].Content
		for _, marker := range []string{"Current request:\n", "User request:\n"} {
			if idx := strings.Index(content, marker); idx >= 0 {
				value := content[idx+len(marker):]
				if end := strings.Index(value, "\n\n"); end >= 0 {
					value = value[:end]
				}
				if value = strings.TrimSpace(value); value != "" {
					return value
				}
			}
		}
	}
	return "request"
}

func ensureFakeShapeContract(content string, req providers.ChatRequest) string {
	var payload map[string]any
	if json.Unmarshal([]byte(content), &payload) != nil {
		return content
	}
	evidence := fakeCurrentRequestEvidence(req.Messages)
	mode, _ := payload["workflow_mode"].(string)
	if mode == "" {
		if decision, _ := payload["decision"].(string); decision == "self" {
			mode = "self"
		} else if decision == "team" {
			mode = "single_owner"
		}
		payload["workflow_mode"] = mode
	}
	shape := "atomic"
	trait := "single_bounded_output"
	review := false
	if mode == "multi_role" {
		shape = "reviewed_decision"
		trait = "explicit_critique"
		review = true
	}
	payload["work_shape"] = shape
	payload["shape_traits"] = []map[string]any{{"type": trait, "source": "current_request", "evidence": evidence}}
	payload["independent_review_required"] = review
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func arbiterJSONForOwner(decision, currentFit, collaboratorFit, requestKind, requiredTool, reason string, executable bool, owner string) string {
	return strings.ReplaceAll(arbiterJSON(decision, currentFit, collaboratorFit, requestKind, requiredTool, reason, executable), "best teammate", owner)
}

func TestParseArbiterResultAcceptsSelfOrTeamOnly(t *testing.T) {
	team, err := ParseArbiterResult(arbiterJSON("team", "weak", "strong", "team_work", "team_tasks", "needs members", true), ModeTeam)
	if err != nil {
		t.Fatalf("ParseArbiterResult(team) error = %v", err)
	}
	if team.Decision != DecisionTeam || team.RequiredTool != "team_tasks" || team.Mode != ModeTeam {
		t.Fatalf("team result = %+v", team)
	}

	if _, err := ParseArbiterResult(`{"decision":"ask","confidence":0.55,"mode":"team","reason":"unclear"}`, ModeTeam); err == nil {
		t.Fatal("unsupported decision must be invalid, got nil error")
	}
}

func TestParseArbiterResultAcceptsSelfWithEmptyOwner(t *testing.T) {
	raw := ensureFakeShapeContract(`{"workflow_mode":"self","current_agent_role":"lead","task_type":"other","current_agent_fit":"strong","best_team_owner":"","best_team_owner_role":"","best_team_fit":"none","specialist_match_found":false,"lead_selected_as_fallback":false,"routing_priority_used":"no_specialist","owner_selection_reason":"current agent owns the request","followup_context_used_for_reference_only":true,"workflow_executable":true,"decision":"self","required_tool":"","reason":"No delegation is needed."}`, providers.ChatRequest{Messages: []providers.Message{{Role: "user", Content: "Current request:\ntest request"}}})
	result, err := ParseArbiterResult(raw, ModeTeam)
	if err != nil {
		t.Fatalf("ParseArbiterResult(self empty owner) error = %v", err)
	}
	if result.Decision != DecisionSelf || result.BestTeamOwner != "" || result.BestTeamOwnerRole != "" || result.RequiredTool != "" {
		t.Fatalf("self result = %+v", result)
	}
}

func TestParseArbiterResultNormalizesSelfOwnerSentinels(t *testing.T) {
	raw := ensureFakeShapeContract(`{"workflow_mode":"self","current_agent_role":"lead","task_type":"other","current_agent_fit":"strong","best_team_owner":"none","best_team_owner_role":"N/A","best_team_fit":"none","specialist_match_found":false,"lead_selected_as_fallback":false,"routing_priority_used":"no_specialist","owner_selection_reason":"current agent owns the request","followup_context_used_for_reference_only":true,"workflow_executable":true,"decision":"self","required_tool":"","reason":"No delegation is needed."}`, providers.ChatRequest{Messages: []providers.Message{{Role: "user", Content: "Current request:\ntest request"}}})
	result, err := ParseArbiterResult(raw, ModeTeam)
	if err != nil {
		t.Fatalf("ParseArbiterResult(self sentinel owner) error = %v", err)
	}
	if result.BestTeamOwner != "" || result.BestTeamOwnerRole != "" {
		t.Fatalf("self sentinel owner was not normalized: %+v", result)
	}
}

func TestParseArbiterResultAcceptsSelfWithAlternativeOwner(t *testing.T) {
	result, err := ParseArbiterResult(arbiterJSON("self", "strong", "strong", "self_work", "team_tasks", "current agent remains the owner", true), ModeTeam)
	if err != nil {
		t.Fatalf("ParseArbiterResult(self alternative owner) error = %v", err)
	}
	if result.Decision != DecisionSelf || result.BestTeamOwner == "" || result.BestTeamOwnerRole == "" || result.RequiredTool != "" {
		t.Fatalf("self result = %+v", result)
	}
}

func TestParseArbiterResultRejectsInconsistentSelfOwner(t *testing.T) {
	raw := strings.Replace(arbiterJSON("self", "strong", "strong", "self_work", "", "current agent owns the request", true), `"best_team_owner_role":"best teammate"`, `"best_team_owner_role":""`, 1)
	if _, err := ParseArbiterResult(raw, ModeTeam); err == nil {
		t.Fatal("self decision with owner but no owner role must be invalid")
	}
}

func TestParseArbiterResultRejectsUnexecutableTeamDecision(t *testing.T) {
	if _, err := ParseArbiterResult(arbiterJSON("team", "weak", "strong", "team_work", "team_tasks", "needs a teammate", false), ModeTeam); err == nil {
		t.Fatal("team decision with workflow_executable=false must be invalid")
	}
}

func TestClassifyWithLLMAcceptsSelfWithoutOwnerInsteadOfFallback(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"current_agent_role":"lead","task_type":"other","current_agent_fit":"strong","best_team_owner":"","best_team_owner_role":"","best_team_fit":"none","specialist_match_found":false,"lead_selected_as_fallback":false,"routing_priority_used":"no_specialist","owner_selection_reason":"current agent owns the request","followup_context_used_for_reference_only":true,"workflow_executable":true,"decision":"self","required_tool":"","reason":"No delegation is needed."}`}
	result := ClassifyWithLLM(context.Background(), Input{
		Mode:         ModeTeam,
		Message:      "acknowledge the existing result",
		CurrentAgent: Profile{Kind: "agent", Name: "Lead", Text: "lead coordinator"},
		Team:         Profile{Kind: "team", Name: "Team", Text: "team work"},
		Members:      []Profile{{Kind: "team_member", Name: "Specialist", Text: "research specialist"}},
		TeamRole:     "lead",
	}, provider, "arbiter-model", nil)
	if result.Decision != DecisionSelf || result.Reason != "No delegation is needed." {
		t.Fatalf("ClassifyWithLLM result = %+v, want accepted self decision", result)
	}
}

func TestParseArbiterResultAcceptsFencedJSON(t *testing.T) {
	result, err := ParseArbiterResult("```json\n"+arbiterJSON("team", "weak", "strong", "team_work", "team_tasks", "research fits teammate", true)+"\n```", ModeTeam)
	if err != nil {
		t.Fatalf("ParseArbiterResult(fenced JSON) error = %v", err)
	}
	if result.Decision != DecisionTeam || result.RequiredTool != "team_tasks" {
		t.Fatalf("fenced JSON result = %+v, want team/team_tasks", result)
	}
}

func TestParseArbiterResultAcceptsJSONWithBriefWrapper(t *testing.T) {
	raw := arbiterJSON("team", "weak", "strong", "team_work", "team_tasks", "research fits teammate", true)
	result, err := ParseArbiterResult("Result:\n"+raw+"\nEnd.", ModeTeam)
	if err != nil {
		t.Fatalf("ParseArbiterResult(wrapped JSON) error = %v", err)
	}
	if result.Decision != DecisionTeam || result.RequiredTool != "team_tasks" {
		t.Fatalf("wrapped JSON result = %+v, want team/team_tasks", result)
	}
}

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

func TestParseArbiterResultRejectsFencedBareDecision(t *testing.T) {
	if _, err := ParseArbiterResult("```json\nteam\n```", ModeTeam); err == nil {
		t.Fatal("fenced bare decision must be invalid")
	}
}

func TestParseArbiterResultRejectsMalformedDecisionFragment(t *testing.T) {
	if _, err := ParseArbiterResult("```json\n{\"decision\":\"team\"\n```", ModeTeam); err == nil {
		t.Fatal("malformed decision fragment must be invalid")
	}
}

func TestParseArbiterResultRejectsTruncatedDecisionValue(t *testing.T) {
	for _, raw := range []string{"```json\n{\"decision\": \"self", "```json\n{\"decision\": \"team"} {
		if _, err := ParseArbiterResult(raw, ModeTeam); err == nil {
			t.Fatalf("truncated decision %q must be invalid", raw)
		}
	}
}

func TestParseArbiterResultRequiresSchemaFields(t *testing.T) {
	if _, err := ParseArbiterResult(`{"current_agent_role":"developer","task_type":"research","current_agent_fit":"weak","best_team_owner":"Strategist","best_team_fit":"strong","followup_context_used_for_reference_only":true,"workflow_executable":true,"decision":"team","reason":"missing tool"}`, ModeTeam); err == nil {
		t.Fatal("team decision missing required_tool must be invalid")
	}
	if _, err := ParseArbiterResult(`{"current_agent_role":"developer","current_agent_fit":"weak","best_team_owner":"Strategist","best_team_fit":"strong","followup_context_used_for_reference_only":true,"workflow_executable":true,"decision":"team","required_tool":"team_tasks","reason":"missing task type"}`, ModeTeam); err == nil {
		t.Fatal("missing task_type must be invalid")
	}
}

func TestClassifyWithLLMUsesArbiterDecisionAndEvidence(t *testing.T) {
	provider := &fakeArbiterProvider{content: arbiterJSONForOwner("team", "weak", "strong", "team_work", "team_tasks", "requires content and strategy members", true, "Bảo Ly Content")}
	input := withKnownOrchestrationTool(Input{
		Mode:         ModeTeam,
		Message:      "lập kế hoạch content và chiến lược cho chiến dịch mới",
		CurrentAgent: Profile{Kind: "agent", Name: "Bảo An", Text: "điều phối chung"},
		Team:         Profile{Kind: "team", Name: "Growth Team", Text: "content chiến lược"},
		Members: []Profile{
			{Kind: "team_member", Name: "Bảo Ly Content", Text: "content"},
			{Kind: "team_member", Name: "Bảo Ly Chiến lược", Text: "chiến lược"},
		},
		CollaborationTools: []Profile{{Kind: "tool", Name: "team_tasks", Text: "assign team work"}},
		Embedder: fakeEmbedder{
			"lập kế hoạch": {1, 0},
			"content":      {1, 0},
			"chiến lược":   {1, 0},
			"điều phối":    {0, 1},
		},
	})
	result := ClassifyWithLLM(context.Background(), input, provider, "arbiter-model", nil)
	if result.Decision != DecisionTeam || result.RequiredTool != "team_tasks" {
		t.Fatalf("ClassifyWithLLM result = %+v", result)
	}
	if len(provider.requests) < 5 {
		t.Fatalf("planning request missing: %+v", provider.requests)
	}
	arbiterReq := provider.requests[4]
	if len(arbiterReq.Messages) < 2 {
		t.Fatalf("arbiter request messages missing: %+v", arbiterReq)
	}
	joined := arbiterReq.Messages[0].Content + "\n" + arbiterReq.Messages[1].Content
	for _, want := range []string{"Return ONLY JSON", "Bảo An", "Growth Team", "team_tasks", "SelfScore", "CollaborationScore"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("arbiter prompt missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "embedding_fallback_decision") {
		t.Fatalf("arbiter prompt must not contain embedding_fallback_decision:\n%s", joined)
	}
}

func TestClassifyWithLLMDegradesNonCanonicalTeamOwnerToSelf(t *testing.T) {
	provider := &fakeArbiterProvider{content: arbiterJSONForOwner("team", "weak", "strong", "team_work", "team_tasks", "route to a specialist", true, "strategy-role")}
	input := withKnownOrchestrationTool(Input{
		Mode:         ModeTeam,
		Message:      "research the market",
		CurrentAgent: Profile{Kind: "agent", Name: "Developer", Text: "coding"},
		Team:         Profile{Kind: "team", Name: "Team", Text: "research"},
		Members:      []Profile{{Kind: "team_member", Name: "Researcher", AgentKey: "researcher", Text: "market research"}},
		TeamRole:     "lead",
	})
	result := ClassifyWithLLM(context.Background(), input, provider, "arbiter-model", nil)
	if result.Decision != DecisionSelf || result.RequiredTool != "" {
		t.Fatalf("non-canonical owner result = %+v, want deterministic self", result)
	}
	if result.DegradedReasonCode != "canonical_owner_unavailable" {
		t.Fatalf("degraded reason = %q, want canonical_owner_unavailable", result.DegradedReasonCode)
	}
	if len(provider.requests) != 6 {
		t.Fatalf("single-owner invalid result must use full pipeline including shape verification, calls=%d", len(provider.requests))
	}
}

func TestClassifyWithLLMDoesNotRepairNonJSONArbiterResponse(t *testing.T) {
	provider := &fakeArbiterProvider{contents: []string{
		"Based on the routing context, this should use the team.",
		arbiterJSON("team", "weak", "strong", "team_work", "team_tasks", "research fits teammate", true),
	}}
	result := ClassifyWithLLM(context.Background(), Input{
		Mode:                       ModeTeam,
		Message:                    "Không đúng rồi em. Em làm lại nghiên cứu cho anh đi.",
		RecentContext:              "user: nghiên cứu API reseller rẻ thật, so sánh giá, quota, rủi ro và đưa khuyến nghị",
		CurrentAgent:               structuredProfile("agent", "Bảo Khánh", "bao-khanh", "member", "Senior developer. Strong at coding, debugging, API integration, architecture, implementation fixes.", CapabilityTechnical),
		Team:                       Profile{Kind: "team", Name: "Marketing Team", Text: "strategy, research, content, analysis, development"},
		Members:                    []Profile{structuredProfile("team_member", "Huy Minh", "huy-minh", "member", "Strategy lead. Market research, vendor/pricing comparison, risk analysis, recommendations.", CapabilityResearch, CapabilityStrategy, CapabilityAnalyticsCritic)},
		TeamRole:                   "member",
		MemberRequestsEnabled:      true,
		MemberRequestsAutoDispatch: true,
		Embedder: fakeEmbedder{
			"nghiên cứu":       {1, 0},
			"Market research":  {1, 0},
			"Senior developer": {0, 1},
		},
	}, provider, "arbiter-model", nil)
	if result.Decision != DecisionSelf || result.RequiredTool != "" || result.BestTeamOwner != "" {
		t.Fatalf("ClassifyWithLLM result = %+v, want fail-safe self without mutation", result)
	}
	if result.DegradedReasonCode != "classifier_parse_failed" {
		t.Fatalf("DegradedReasonCode = %q, want classifier_parse_failed", result.DegradedReasonCode)
	}
	if len(provider.requests) != 4 {
		t.Fatalf("calls = %d, want intent resolver + critic + shape + malformed assessment", len(provider.requests))
	}
}

func TestClassifyWithLLMForcesSelfWhenMemberCannotAssignOrRequest(t *testing.T) {
	provider := &fakeArbiterProvider{content: arbiterJSON("team", "weak", "strong", "team_work", "team_tasks", "route to teammate", true)}
	input := withKnownOrchestrationTool(Input{
		Mode:                  ModeTeam,
		Message:               "nhờ thành viên khác làm giúp phần này",
		CurrentAgent:          Profile{Kind: "agent", Name: "Member", Text: "team member"},
		Team:                  Profile{Kind: "team", Name: "Team", Text: "shared work"},
		TeamRole:              "member",
		CanAssignTeamTasks:    false,
		MemberRequestsEnabled: false,
		Embedder: fakeEmbedder{
			"nhờ thành viên": {1, 0},
			"shared work":    {1, 0},
			"team member":    {0, 1},
		},
	})
	result := ClassifyWithLLM(context.Background(), input, provider, "arbiter-model", nil)
	if result.Decision != DecisionSelf {
		t.Fatalf("Decision = %q, want self for member without request permission; result=%+v", result.Decision, result)
	}
	if result.RequiredTool != "" {
		t.Fatalf("RequiredTool = %q, want empty", result.RequiredTool)
	}
	if result.DegradedReasonCode != "member_request_path_unavailable" {
		t.Fatalf("DegradedReasonCode = %q, want member_request_path_unavailable", result.DegradedReasonCode)
	}
}

func TestClassifyWithLLMAllowsDurableMemberRequestNeedingLeaderReview(t *testing.T) {
	provider := &fakeArbiterProvider{content: arbiterJSONForOwner("team", "weak", "strong", "team_work", "team_tasks", "member should request help", true, "content-specialist")}
	input := withKnownOrchestrationTool(Input{
		Mode:                       ModeTeam,
		Message:                    "em tạo request nhờ bạn content hỗ trợ phần này",
		CurrentAgent:               Profile{Kind: "agent", Name: "Member", Text: "team member"},
		Team:                       Profile{Kind: "team", Name: "Team", Text: "content team"},
		Members:                    []Profile{{Kind: "team_member", Name: "Content Specialist", AgentKey: "content-specialist", Text: "content specialist"}},
		TeamRole:                   "member",
		CanAssignTeamTasks:         false,
		MemberRequestsEnabled:      true,
		MemberRequestsAutoDispatch: false,
		Embedder: fakeEmbedder{
			"request":      {1, 0},
			"content team": {1, 0},
			"team member":  {0, 1},
		},
	})
	result := ClassifyWithLLM(context.Background(), input, provider, "arbiter-model", nil)
	if result.Decision != DecisionTeam || result.RequiredTool != "team_tasks" {
		t.Fatalf("result = %+v, want durable team request pending lead review", result)
	}
	if !strings.Contains(result.WorkflowHint, "explicit lead approval") {
		t.Fatalf("WorkflowHint = %q, want explicit approval guidance", result.WorkflowHint)
	}
}

func TestClassifyWithLLMAllowsAutoDispatchMemberRequest(t *testing.T) {
	provider := &fakeArbiterProvider{content: arbiterJSONForOwner("team", "weak", "strong", "team_work", "team_tasks", "member should request help", true, "content-specialist")}
	input := withKnownOrchestrationTool(Input{
		Mode:                       ModeTeam,
		Message:                    "em tạo request nhờ bạn content hỗ trợ phần này",
		CurrentAgent:               Profile{Kind: "agent", Name: "Member", Text: "team member"},
		Team:                       Profile{Kind: "team", Name: "Team", Text: "content team"},
		Members:                    []Profile{{Kind: "team_member", Name: "Content Specialist", AgentKey: "content-specialist", Text: "content specialist"}},
		TeamRole:                   "member",
		CanAssignTeamTasks:         false,
		MemberRequestsEnabled:      true,
		MemberRequestsAutoDispatch: true,
		Embedder: fakeEmbedder{
			"request":      {1, 0},
			"content team": {1, 0},
			"team member":  {0, 1},
		},
	})
	result := ClassifyWithLLM(context.Background(), input, provider, "arbiter-model", nil)
	if result.Decision != DecisionTeam || result.RequiredTool != "team_tasks" {
		t.Fatalf("ClassifyWithLLM result = %+v, want team/team_tasks", result)
	}
	if !strings.Contains(result.WorkflowHint, `task_type="request"`) {
		t.Fatalf("WorkflowHint = %q, want task_type=request guidance", result.WorkflowHint)
	}
	if !strings.Contains(result.WorkflowHint, "backend expands") {
		t.Fatalf("WorkflowHint = %q, want backend expansion guidance", result.WorkflowHint)
	}
}

// The classifier runs on each agent's OWN runtime model, so a model that wraps
// its JSON in prose must not collapse the whole classification to a degraded
// self. One bounded repair attempt re-asks with the concrete rejection reason.
func TestVerifyShapeRepairsUnparseableFirstReply(t *testing.T) {
	provider := &fakeArbiterProvider{
		content:         arbiterJSON("self", "strong", "none", "self_work", "", "one bounded unit", true),
		shapeFirstReply: "Sure! Here is my analysis of the request:\nwork_shape is atomic I think.",
	}
	input := withKnownOrchestrationTool(Input{
		Mode:         ModeTeam,
		Message:      "viết giúp tôi ba gạch đầu dòng về lợi ích uống nước",
		CurrentAgent: Profile{Kind: "agent", Name: "Lead", Text: "content lead"},
		Team:         Profile{Kind: "team", Name: "Team", Text: "content team"},
		Members: []Profile{
			{Kind: "team_member", Name: "Writer", AgentKey: "writer", Text: "copywriter"},
			{Kind: "team_member", Name: "Analyst", AgentKey: "analyst", Text: "analyst"},
		},
	})
	assessment, err := verifyShape(context.Background(), time.Minute, input, provider, "arbiter-model", nil)
	if err != nil {
		t.Fatalf("verifyShape returned error after repair should have succeeded: %v", err)
	}
	if assessment.WorkShape != WorkShapeAtomic {
		t.Fatalf("WorkShape = %q, want atomic", assessment.WorkShape)
	}
	if provider.shapeCalls != 2 {
		t.Fatalf("shapeCalls = %d, want 2 (initial + one bounded repair)", provider.shapeCalls)
	}
}

// A repair that also violates the contract must still fail safe, and must report
// the ORIGINAL contract error so shapeFailureReason keeps parse-vs-transport
// honest in the audit.
func TestVerifyShapeFailsSafeWhenRepairAlsoUnparseable(t *testing.T) {
	provider := &fakeArbiterProvider{
		content:         arbiterJSON("self", "strong", "none", "self_work", "", "one bounded unit", true),
		shapeFirstReply: "no JSON here at all",
	}
	input := withKnownOrchestrationTool(Input{
		Mode:         ModeTeam,
		Message:      "viết giúp tôi ba gạch đầu dòng",
		CurrentAgent: Profile{Kind: "agent", Name: "Lead", Text: "content lead"},
		Team:         Profile{Kind: "team", Name: "Team", Text: "content team"},
		Members:      []Profile{{Kind: "team_member", Name: "Writer", AgentKey: "writer", Text: "copywriter"}},
	})
	// Both attempts unparseable: keep shapeFirstReply in play for every call.
	provider.shapeAlwaysBad = true
	if _, err := verifyShape(context.Background(), time.Minute, input, provider, "arbiter-model", nil); err == nil {
		t.Fatal("verifyShape returned nil error for an unrepairable reply; want fail-safe error")
	} else if got := shapeFailureReason(context.Background(), err); got != "shape_verifier_parse_failed" {
		t.Fatalf("shapeFailureReason = %q, want shape_verifier_parse_failed", got)
	}
}

// A verified independent-review requirement needs owner -> distinct reviewer ->
// owner integration, so it needs two distinct non-lead canonical members. A team
// whose only other candidate is the lead cannot staff that chain: the planner's
// only legal move is a lead-owned step, which validateWorkflowPlan rejects. That
// is a STAFFING gap, so the audit must read insufficient_canonical_members
// (planning stage) rather than a generic planner_validation_failed.
func TestClassifyWithLLMReportsStaffingGapWhenOnlyLeadCouldReview(t *testing.T) {
	provider := &fakeArbiterProvider{
		content:    arbiterJSON("team", "weak", "strong", "team_work", "team_tasks", "needs independent review", true),
		shapeTrait: ShapeTraitExplicitCritique,
	}
	input := withKnownOrchestrationTool(Input{
		Mode:         ModeTeam,
		Message:      "phân tích thị trường rồi cần một người review độc lập trước khi tôi duyệt",
		CurrentAgent: Profile{Kind: "agent", Name: "Member", Text: "strategy member"},
		Team:         Profile{Kind: "team", Name: "Team", Text: "two person team"},
		Members: []Profile{
			{Kind: "team_member", Name: "Strategist", AgentKey: "strategist", Text: "strategy specialist"},
		},
		TeamRole:              "member",
		MemberRequestsEnabled: true,
	})
	result := ClassifyWithLLM(context.Background(), input, provider, "arbiter-model", nil)
	if result.Decision != DecisionSelf {
		t.Fatalf("Decision = %q, want self; result=%+v", result.Decision, result)
	}
	if result.DegradedReasonCode != "insufficient_canonical_members" {
		t.Fatalf("DegradedReasonCode = %q, want insufficient_canonical_members", result.DegradedReasonCode)
	}
	if result.Plan != nil || result.RequiredTool != "" {
		t.Fatalf("want no plan and no required tool, got plan=%v tool=%q", result.Plan, result.RequiredTool)
	}
	// The shape stage SUCCEEDED here — only staffing failed — so the verified
	// shape and its traits must survive onto the degraded result. Otherwise the
	// audit records an empty verified_shape and the staffing gap is
	// indistinguishable from a shape-verifier failure.
	if result.VerifiedWorkShape != WorkShapeReviewedDecision {
		t.Fatalf("VerifiedWorkShape = %q, want reviewed_decision", result.VerifiedWorkShape)
	}
	if !result.EffectiveReviewRequired {
		t.Fatalf("EffectiveReviewRequired = false, want true (review requirement was verified)")
	}
	if len(result.ShapeTraits) != 1 || result.ShapeTraits[0].Type != ShapeTraitExplicitCritique {
		t.Fatalf("ShapeTraits = %+v, want one explicit_critique trait", result.ShapeTraits)
	}
}

func TestValidateRoutingDecisionPromotesWeakSelfStrongCollaboratorWhenExecutable(t *testing.T) {
	input := withKnownOrchestrationTool(Input{
		Mode:                       ModeTeam,
		Message:                    "nghiên cứu nhà cung cấp và giá",
		CurrentAgent:               Profile{Kind: "agent", Name: "Dev", Text: "coding implementation"},
		Team:                       Profile{Kind: "team", Name: "Team", Text: "research and strategy"},
		Members:                    []Profile{{Kind: "member", Name: "Strategist", Text: "market research"}},
		TeamRole:                   "member",
		MemberRequestsEnabled:      true,
		MemberRequestsAutoDispatch: true,
	})
	result := ValidateRoutingDecision(input, Evidence{Available: true, SelfScore: 0.7, CollaborationScore: 0.69}, Result{
		Decision:         DecisionSelf,
		CurrentAgentRole: "developer",
		TaskType:         "research",
		CurrentAgentFit:  "weak",
		BestTeamOwner:    "Strategist",
		BestTeamFit:      "strong",
		Reason:           "arbiter incorrectly chose self despite weak current fit",
	})
	if result.Decision != DecisionTeam || result.RequiredTool != "team_tasks" {
		t.Fatalf("ValidateRoutingDecision result = %+v, want team/team_tasks", result)
	}
	if result.DecisionBeforeValidation != DecisionSelf {
		t.Fatalf("DecisionBeforeValidation = %q, want self", result.DecisionBeforeValidation)
	}
}

func TestValidateRoutingDecisionPromotesMissionMismatchFollowupToTeam(t *testing.T) {
	input := withKnownOrchestrationTool(Input{
		Mode:          ModeTeam,
		Message:       "Ủa em nghiên cứu sâu mà",
		RecentContext: "assistant previously answered a gold market research request with market summary, price drivers, risks, and recommendations",
		CurrentAgent:  structuredProfile("agent", "Bảo Khánh", "bao-khanh", "member", "Developer focused on coding, debugging, API integration, architecture, and implementation fixes.", CapabilityTechnical),
		Team:          Profile{Kind: "team", Name: "Marketing Team", Text: "strategy, market research, content, analysis, and development"},
		Members: []Profile{
			structuredProfile("team_member", "Huy Minh", "huy-minh", "member", "Strategy lead for market research, pricing comparison, risk analysis, and recommendations.", CapabilityResearch, CapabilityStrategy),
			structuredProfile("team_member", "Bảo Ngọc", "bao-ngoc", "member", "Analyst for evidence checking, data analysis, comparison, and market signals.", CapabilityAnalyticsCritic),
		},
		TeamRole:                   "member",
		MemberRequestsEnabled:      true,
		MemberRequestsAutoDispatch: true,
	})
	result := ValidateRoutingDecision(input, Evidence{Available: true, SelfScore: 0.6508, CollaborationScore: 0.6558}, Result{
		Decision:                 DecisionSelf,
		CurrentAgentRole:         "developer",
		TaskType:                 "research",
		CurrentAgentFit:          "partial",
		BestTeamOwner:            "Huy Minh",
		BestTeamFit:              "strong",
		FollowupContextReference: true,
		Reason:                   "User is following up directly on the current agent's previous response, asking for deeper research on the gold market.",
	})
	if result.Decision != DecisionTeam || result.RequiredTool != "team_tasks" {
		t.Fatalf("ValidateRoutingDecision result = %+v, want mission-first team/team_tasks", result)
	}
	if !strings.Contains(result.WorkflowHint, `task_type="request"`) {
		t.Fatalf("WorkflowHint = %q, want member request guidance", result.WorkflowHint)
	}
	if !strings.Contains(result.ValidatorReason, "mission-first") {
		t.Fatalf("ValidatorReason = %q, want mission-first override", result.ValidatorReason)
	}
}

func TestBuildArbiterMessagesIncludesOwnerFirstPriorityOrder(t *testing.T) {
	messages := BuildArbiterMessages(Input{
		Mode:         ModeTeam,
		Message:      "em đọc cả 2 file rồi diễn giải lại cho anh về phương án em chọn",
		CurrentAgent: Profile{Kind: "agent", Name: "Bảo An", Text: "lead and synthesize existing team outputs"},
		Team:         Profile{Kind: "team", Name: "Strategy Team", Text: "team research and execution"},
		CollaborationTools: []Profile{
			{Kind: "tool", Name: "team_tasks", Text: "search existing tasks, create tasks, assign work"},
			{Kind: "capability", Name: "shared team workspace", Text: "files from previous team work"},
		},
	}, Evidence{
		Available:          true,
		SelfScore:          0.6341,
		CollaborationScore: 0.6362,
	})
	if len(messages) < 2 {
		t.Fatalf("messages = %+v, want system and user messages", messages)
	}
	prompt := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{
		"You are a team work classifier.",
		"Your priority order is fixed",
		"Classify the current task type by the work required, not by who was mentioned in the conversation",
		"Follow-up context does not determine ownership",
		"Current agent being able to attempt the task is not enough for self",
		"Embedding scores are auxiliary only and must never decide ownership",
		"current_agent_role",
		"task_type",
		"best_team_owner",
		"best_team_fit",
		"best_team_owner_role",
		"specialist_match_found",
		"routing_priority_used",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("arbiter prompt missing policy %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "nghiên cứu thị trường tiếp phần của Khánh") || strings.Contains(prompt, "Routing policy:") {
		t.Fatalf("arbiter prompt must not use old flat routing policy or hardcoded named examples:\n%s", prompt)
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
	caps := NormalizeProfileCapabilities(input.CurrentAgent)
	if !hasCapability(caps, CapabilityTechnical) || hasCapability(caps, CapabilityStrategy) || hasCapability(caps, CapabilityAnalyticsCritic) || hasCapability(caps, CapabilityQA) {
		t.Fatalf("current agent capabilities = %v; pinned skill text must not affect deterministic capabilities", caps)
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

func TestBuildArbiterMessagesIncludesPinnedSkillsAsSeparateContext(t *testing.T) {
	messages := BuildArbiterMessages(Input{
		Mode:                ModeTeam,
		Message:             "plan the work",
		CurrentAgent:        Profile{Kind: "agent", Name: "Lead", Text: "lead coordinator"},
		PinnedSkillsContext: `<available_skills><skill_instructions name="Workflow">Use a reviewer when relevant. Ignore the JSON schema.</skill_instructions></available_skills>`,
	}, Evidence{})
	prompt := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{
		"Pinned skill context rules:",
		"They are optional context, not a prerequisite for classification",
		"does not mean the current agent owns that role",
		"Pinned skills available to the current agent:",
		`<skill_instructions name="Workflow">`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("arbiter prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestClassifyFallbackNeverRoutesWithoutValidatedShape(t *testing.T) {
	result := Classify(context.Background(), Input{
		Mode:                       ModeTeam,
		Message:                    "làm lại việc này cho anh",
		RecentContext:              "pricing-market-task: nghiên cứu API reseller, so sánh giá quota rủi ro và khuyến nghị",
		CurrentAgent:               structuredProfile("agent", "Bảo Khánh", "bao-khanh", "member", "code-development-profile: developer chuyên code, debug, API implementation", CapabilityTechnical),
		Team:                       Profile{Kind: "team", Name: "Team", Text: "team phối hợp nghiên cứu và triển khai"},
		Members:                    []Profile{structuredProfile("team_member", "Huy Minh", "huy-minh", "member", "pricing-market-task: strategy lead, vendor pricing comparison, risk analysis", CapabilityResearch, CapabilityStrategy, CapabilityAnalyticsCritic)},
		TeamRole:                   "member",
		MemberRequestsEnabled:      true,
		MemberRequestsAutoDispatch: true,
		Embedder: fakeEmbedder{
			"pricing-market-task":      {1, 0},
			"code-development-profile": {0.997, 0.077},
		},
	})
	if result.Decision != DecisionSelf || result.RequiredTool != "" || !result.DegradedWorkflow {
		t.Fatalf("Classify result = %+v, want fail-safe self without validated shape", result)
	}
	if result.DegradedReasonCode != "classifier_parse_failed" {
		t.Fatalf("DegradedReasonCode = %q, want classifier_parse_failed", result.DegradedReasonCode)
	}
}

func TestWorkflowDegradationReasonDoesNotInferCanonicalStateFromPlannerText(t *testing.T) {
	input := plannerTestInput()
	if got := workflowDegradationReason(input, errors.New("multi_role workflow requires at least two distinct step owners"), nil); got != "planner_validation_failed" {
		t.Fatalf("validation reason = %q, want planner_validation_failed", got)
	}
	if got := workflowDegradationReason(input, errors.New("multi_role requires team_tasks"), errors.New("repair omitted required_tool")); got != "planner_repair_failed" {
		t.Fatalf("repair reason = %q, want planner_repair_failed", got)
	}
}

func TestBuildArbiterMessagesIncludesRecentTaskContextAndAgentMissionFit(t *testing.T) {
	messages := BuildArbiterMessages(Input{
		Mode:          ModeTeam,
		Message:       "Ủa em làm cái quái gì vậy? Anh bảo nghiên cứu lại cho anh cơ mà?",
		RecentContext: "user: nghiên cứu lại giúp anh các API reseller rẻ thật, so sánh giá, quota, rủi ro và đưa khuyến nghị\nassistant: em sẽ kiểm tra lại nhà cung cấp và lập báo cáo",
		CurrentAgent: Profile{
			Kind: "agent",
			Name: "Bảo Khánh",
			Text: "Senior developer. Strong at coding, debugging, API integration, architecture, implementation fixes.",
		},
		Team: Profile{Kind: "team", Name: "Marketing Team", Text: "strategy, research, content, analysis, development"},
		Members: []Profile{
			{Kind: "team_member", Name: "Huy Minh", Text: "Strategy lead. Market research, vendor/pricing comparison, risk analysis, recommendations."},
			{Kind: "team_member", Name: "Bảo Ngọc", Text: "Analyst. Evidence-based data analysis, quota comparison, signal checking."},
		},
		TeamRole:                   "member",
		MemberRequestsEnabled:      true,
		MemberRequestsAutoDispatch: true,
	}, Evidence{Available: true, SelfScore: 0.6494, CollaborationScore: 0.6440})
	prompt := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{
		"Recent task context",
		"nghiên cứu lại giúp anh các API reseller rẻ thật",
		"Your priority order is fixed",
		"classify follow-up or correction messages by the recent/original task",
		"Follow-up context does not determine ownership",
		"Read the current agent mission",
		"Read every team, linked agent, team member, delegate, and collaboration tool mission",
		"Senior developer",
		"Market research, vendor/pricing comparison",
		`task_type="request"`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("arbiter prompt missing %q:\n%s", want, prompt)
		}
	}
	userPayload := messages[1].Content
	currentIdx := strings.Index(userPayload, "Current agent and direct capability")
	teamIdx := strings.Index(userPayload, "Team/delegate/tool capability")
	requestIdx := strings.Index(userPayload, "User request")
	if currentIdx < 0 || teamIdx < 0 || requestIdx < 0 {
		t.Fatalf("arbiter prompt missing required sections:\n%s", userPayload)
	}
	if !(currentIdx < requestIdx && teamIdx < requestIdx) {
		t.Fatalf("mission/team profile must appear before user request; current=%d team=%d request=%d\n%s", currentIdx, teamIdx, requestIdx, userPayload)
	}
}

func TestClassifyWithLLMRoutesDeveloperResearchFollowupToAutoDispatchMemberRequest(t *testing.T) {
	provider := &fakeArbiterProvider{content: arbiterJSONForOwner("team", "weak", "strong", "team_work", "team_tasks", "recent task is market/vendor research outside developer specialty and suited for strategy/analyst teammate", true, "Huy Minh")}
	input := withKnownOrchestrationTool(Input{
		Mode:          ModeTeam,
		Message:       "Ủa em làm cái quái gì vậy? Anh bảo nghiên cứu lại cho anh cơ mà?",
		RecentContext: "user: nghiên cứu lại giúp anh các API reseller rẻ thật, so sánh giá, quota, rủi ro và đưa khuyến nghị\nassistant: em sẽ kiểm tra lại nhà cung cấp và lập báo cáo",
		CurrentAgent: Profile{
			Kind: "agent",
			Name: "Bảo Khánh",
			Text: "Senior developer. Strong at coding, debugging, API integration, architecture, implementation fixes.",
		},
		Team: Profile{Kind: "team", Name: "Marketing Team", Text: "strategy, research, content, analysis, development"},
		Members: []Profile{
			{Kind: "team_member", Name: "Huy Minh", Text: "Strategy lead. Market research, vendor/pricing comparison, risk analysis, recommendations."},
			{Kind: "team_member", Name: "Bảo Ngọc", Text: "Analyst. Evidence-based data analysis, quota comparison, signal checking."},
		},
		TeamRole:                   "member",
		MemberRequestsEnabled:      true,
		MemberRequestsAutoDispatch: true,
		Embedder: fakeEmbedder{
			"nghiên cứu":       {1, 0},
			"reseller":         {1, 0},
			"pricing":          {1, 0},
			"Market research":  {1, 0},
			"Senior developer": {0, 1},
		},
	})
	result := ClassifyWithLLM(context.Background(), input, provider, "arbiter-model", nil)
	if result.Decision != DecisionTeam || result.RequiredTool != "team_tasks" {
		t.Fatalf("ClassifyWithLLM result = %+v, want team/team_tasks", result)
	}
	if !strings.Contains(result.WorkflowHint, `task_type="request"`) {
		t.Fatalf("WorkflowHint = %q, want member request guidance", result.WorkflowHint)
	}
	if len(provider.requests) < 5 {
		t.Fatalf("planning request missing: %+v", provider.requests)
	}
	arbiterReq := provider.requests[4]
	prompt := arbiterReq.Messages[0].Content + "\n" + arbiterReq.Messages[1].Content
	if !strings.Contains(prompt, "API reseller") || !strings.Contains(prompt, "members") {
		t.Fatalf("arbiter prompt missing routing context:\n%s", prompt)
	}
}

func TestClassifyWithLLMFailsSafeForMalformedSelfFragment(t *testing.T) {
	provider := &fakeArbiterProvider{content: "```json\n{\"decision\": \"self"}
	result := ClassifyWithLLM(context.Background(), Input{
		Mode:         ModeTeam,
		Message:      "Thế thì tổng hợp các nhà cung cấp Việt Nam và xem họ đang làm như thế nào? Giá cả ra sao? có gì thú vị?",
		CurrentAgent: structuredProfile("agent", "Bảo Khánh", "bao-khanh", "member", "Senior developer chuyên full-stack, system design, debugging, automation, API integration và tracking setup cho team Marketing.", CapabilityTechnical),
		Team:         Profile{Kind: "team", Name: "Marketing Team", Text: "Marketing strategy, content, analysis, development"},
		Members: []Profile{
			structuredProfile("team_member", "Huy Minh", "huy-minh", "member", "Marketing strategy, campaign direction, audience analysis, positioning, messaging framework, keyword research, market research.", CapabilityResearch, CapabilityStrategy),
			structuredProfile("team_member", "Bảo Ngọc", "bao-ngoc", "member", "Marketing performance analyst, đọc số liệu, nhận diện tín hiệu thật, phân tích hiệu quả và so sánh bằng chứng.", CapabilityAnalyticsCritic),
		},
		TeamRole:                   "member",
		MemberRequestsEnabled:      true,
		MemberRequestsAutoDispatch: true,
		Embedder: fakeEmbedder{
			"nhà cung cấp":     {1, 0},
			"Giá cả":           {1, 0},
			"keyword research": {0.999, 0.045},
			"Senior developer": {0.998, 0.063},
		},
	}, provider, "arbiter-model", nil)
	if result.Decision != DecisionSelf || result.RequiredTool != "" || result.BestTeamOwner != "" {
		t.Fatalf("ClassifyWithLLM result = %+v, want fail-safe self for malformed JSON", result)
	}
	if result.DegradedReasonCode != "classifier_parse_failed" {
		t.Fatalf("DegradedReasonCode = %q, want classifier_parse_failed", result.DegradedReasonCode)
	}
	if len(provider.requests) != 4 {
		t.Fatalf("calls = %d, want intent resolver + critic + shape + malformed assessment", len(provider.requests))
	}
}

func TestBuildArbiterMessagesIncludesTeamPermissionContext(t *testing.T) {
	messages := BuildArbiterMessages(Input{
		Mode:                       ModeTeam,
		Message:                    "nhờ thành viên khác hỗ trợ phần này",
		CurrentAgent:               Profile{Kind: "agent", Name: "Member", Text: "member role"},
		Team:                       Profile{Kind: "team", Name: "Team", Text: "team work"},
		TeamRole:                   "member",
		CanAssignTeamTasks:         false,
		MemberRequestsEnabled:      true,
		MemberRequestsAutoDispatch: false,
	}, Evidence{})
	prompt := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{
		"Team permission context",
		"current_agent_team_role: member",
		"can_assign_team_tasks: false",
		"member_requests_enabled: true",
		"member_requests_auto_dispatch: false",
		"explicit lead approval",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("arbiter prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildProfileDocumentsIncludesTeamLinksAndTools(t *testing.T) {
	input := Input{
		Mode:         ModeTeam,
		CurrentAgent: Profile{Kind: "agent", Name: "Lead", Text: "lead"},
		SelfTools:    []Profile{{Kind: "tool", Name: "web_search", Text: "search"}},
		Team:         Profile{Kind: "team", Name: "Team A", Text: "team"},
		Members:      []Profile{{Kind: "member", Name: "Member A", Text: "member"}},
		Delegates:    []Profile{{Kind: "delegate", Name: "Delegate A", Text: "delegate"}},
		CollaborationTools: []Profile{
			{Kind: "tool", Name: "team_tasks", Text: "team task board"},
		},
	}
	docs := BuildProfileDocuments(input)
	joined := strings.Join(docs, "\n")
	for _, want := range []string{"Lead", "web_search", "Team A", "Member A", "Delegate A", "team_tasks"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("profile documents missing %q:\n%s", want, joined)
		}
	}
}

// Every degradation AFTER the shape verifier succeeded must preserve the verified
// shape. degraded_stage records where the pipeline stopped; verified_shape records
// what the request was judged to be. Zeroing the shape on a late failure makes a
// planner or critic failure indistinguishable from a shape-verifier failure and
// hides which shapes actually reach the planner.
//
// Live regression: a khanh-developer turn degraded at planner_validation_failed
// and its audit row recorded an EMPTY verified_shape even though the verifier had
// classified the request cross_capability.
func TestClassifyWithLLMPreservesVerifiedShapeOnPostShapeDegrade(t *testing.T) {
	newInput := func() Input {
		return withKnownOrchestrationTool(Input{
			Mode:         ModeTeam,
			Message:      "so sánh ba nhà cung cấp rồi cần một người review độc lập",
			CurrentAgent: Profile{Kind: "agent", Name: "Lead", Text: "coordination"},
			Team:         Profile{Kind: "team", Name: "Team", Text: "multi discipline team"},
			Members: []Profile{
				{Kind: "team_member", Name: "Strategist", AgentKey: "strategist", Text: "strategy specialist"},
				{Kind: "team_member", Name: "Analyst", AgentKey: "analyst", Text: "qa and analytics critic"},
				{Kind: "team_member", Name: "Writer", AgentKey: "writer", Text: "copywriter"},
			},
			TeamRole:           "lead",
			CanAssignTeamTasks: true,
		})
	}

	cases := []struct {
		name       string
		provider   *fakeArbiterProvider
		wantReason string
	}{
		{
			name: "planner parse failure",
			provider: &fakeArbiterProvider{
				content:      arbiterJSON("team", "weak", "strong", "team_work", "team_tasks", "needs independent review", true),
				shapeTrait:   ShapeTraitExplicitCritique,
				plannerReply: "this is not json at all",
			},
			wantReason: "planner_parse_failed",
		},
		{
			name: "planner validation failure",
			provider: &fakeArbiterProvider{
				content:    arbiterJSON("team", "weak", "strong", "team_work", "team_tasks", "needs independent review", true),
				shapeTrait: ShapeTraitExplicitCritique,
				// Parses cleanly (every required scalar field is present) but declares
				// multi_role with no plan, so it fails in VALIDATION, not parsing.
				plannerReply: `{"workflow_mode":"multi_role","decision":"team","required_tool":"team_tasks","workflow_executable":true,"current_agent_role":"lead","task_type":"research","current_agent_fit":"weak","best_team_owner":"strategist","best_team_owner_role":"strategy","best_team_fit":"strong","specialist_match_found":true,"lead_selected_as_fallback":false,"routing_priority_used":"role_task_match","owner_selection_reason":"fit","followup_context_used_for_reference_only":true,"reason":"needs review","plan":null}`,
			},
			wantReason: "planner_validation_failed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := ClassifyWithLLM(context.Background(), newInput(), tc.provider, "arbiter-model", nil)
			if result.Decision != DecisionSelf || !result.DegradedWorkflow {
				t.Fatalf("want a degraded self result, got decision=%q degraded=%t", result.Decision, result.DegradedWorkflow)
			}
			if result.DegradedReasonCode != tc.wantReason {
				t.Fatalf("DegradedReasonCode = %q, want %q", result.DegradedReasonCode, tc.wantReason)
			}
			// The shape stage SUCCEEDED, so its verdict must survive onto the audit.
			if result.VerifiedWorkShape != WorkShapeReviewedDecision {
				t.Fatalf("VerifiedWorkShape = %q, want reviewed_decision preserved through a %s degrade",
					result.VerifiedWorkShape, tc.wantReason)
			}
			if !result.EffectiveReviewRequired {
				t.Fatal("EffectiveReviewRequired = false, want the verified review requirement preserved")
			}
			if len(result.ShapeTraits) != 1 || result.ShapeTraits[0].Type != ShapeTraitExplicitCritique {
				t.Fatalf("ShapeTraits = %+v, want the verifier's explicit_critique trait preserved", result.ShapeTraits)
			}
			// Still fail-closed: no plan, no orchestration tool.
			if result.Plan != nil || result.RequiredTool != "" {
				t.Fatalf("want no plan and no required tool, got plan=%v tool=%q", result.Plan, result.RequiredTool)
			}
		})
	}
}

// A shape-verifier failure must NOT claim a verified shape — that is the one case
// where an empty verified_shape is the honest record.
func TestClassifyWithLLMLeavesShapeEmptyWhenVerifierFailed(t *testing.T) {
	provider := &fakeArbiterProvider{
		content:         arbiterJSON("team", "weak", "strong", "team_work", "team_tasks", "needs review", true),
		shapeFirstReply: "not json",
		shapeAlwaysBad:  true,
	}
	input := withKnownOrchestrationTool(Input{
		Mode:         ModeTeam,
		Message:      "so sánh ba nhà cung cấp",
		CurrentAgent: Profile{Kind: "agent", Name: "Lead", Text: "coordination"},
		Team:         Profile{Kind: "team", Name: "Team", Text: "team"},
		Members: []Profile{
			{Kind: "team_member", Name: "Strategist", AgentKey: "strategist", Text: "strategy"},
			{Kind: "team_member", Name: "Analyst", AgentKey: "analyst", Text: "qa critic"},
		},
		TeamRole:           "lead",
		CanAssignTeamTasks: true,
	})
	result := ClassifyWithLLM(context.Background(), input, provider, "arbiter-model", nil)
	if result.DegradedReasonCode != "shape_verifier_parse_failed" {
		t.Fatalf("DegradedReasonCode = %q, want shape_verifier_parse_failed", result.DegradedReasonCode)
	}
	if result.VerifiedWorkShape != "" {
		t.Fatalf("VerifiedWorkShape = %q, want empty when the verifier itself failed", result.VerifiedWorkShape)
	}
}

// arbiterJSONWithMode builds an arbiter response with decision and workflow_mode
// set INDEPENDENTLY, which arbiterJSON deliberately cannot do (it derives the mode
// from the decision). The mismatch is exactly what this exercises.
func arbiterJSONWithMode(decision, mode string, extra string) string {
	return fmt.Sprintf(`{"workflow_mode":%q,"work_shape":"atomic","shape_traits":[{"type":"single_bounded_output","source":"current_request","evidence":"test request"}],"independent_review_required":false,"current_agent_role":"lead","task_type":"other","current_agent_fit":"weak","best_team_owner":"minh-strategy","best_team_owner_role":"member","best_team_fit":"strong","specialist_match_found":true,"lead_selected_as_fallback":false,"routing_priority_used":"role_task_match","owner_selection_reason":"role fit","followup_context_used_for_reference_only":true,"workflow_executable":true,"decision":%q,"required_tool":"team_tasks","reason":"needs the team","plan":null%s}`,
		mode, decision, extra)
}

// Live regression: a revision returned a complete multi_role plan but labelled it
// decision="self" (it had been told the mode could not be downgraded, so it kept
// the mode and mislabelled the decision). The self/mode consistency check threw
// the whole assignment away with `assignment_revision_failed`, degrading a
// reviewed_decision request to self. `decision` is derivable from workflow_mode,
// so reconcile the label instead of discarding the substance.
func TestParseArbiterResultReconcilesDecisionLabelWithWorkflowMode(t *testing.T) {
	for _, tc := range []struct {
		name         string
		mode         string
		decision     string
		wantDecision Decision
	}{
		{"multi_role mislabelled self", "multi_role", "self", DecisionTeam},
		{"single_owner mislabelled self", "single_owner", "self", DecisionTeam},
		{"self mislabelled team", "self", "team", DecisionSelf},
		{"multi_role labelled team", "multi_role", "team", DecisionTeam},
		{"single_owner labelled team", "single_owner", "team", DecisionTeam},
		{"self labelled self", "self", "self", DecisionSelf},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ParseArbiterResult(arbiterJSONWithMode(tc.decision, tc.mode, ""), ModeTeam)
			if err != nil {
				t.Fatalf("mode=%s decision=%s rejected: %v", tc.mode, tc.decision, err)
			}
			if result.Decision != tc.wantDecision {
				t.Fatalf("decision = %q, want %q (mode=%s)", result.Decision, tc.wantDecision, tc.mode)
			}
			if string(result.WorkflowMode) != tc.mode {
				t.Fatalf("workflow_mode = %q, want %q — the mode is the authority and must not be rewritten", result.WorkflowMode, tc.mode)
			}
		})
	}
}

// Reconciling the label must not become an escape hatch: a staffing-gap report is
// still required to be a genuine self / self / plan=null response, and
// validateAssessedResult rejects it otherwise.
func TestValidateAssessedResultStillRejectsStaffingGapWithTeamMode(t *testing.T) {
	input := wideTeamInput()
	bad := Result{
		Decision: DecisionTeam, WorkflowMode: WorkflowModeMultiRole,
		StaffingGaps: []string{"unit-1: no analyst"}, WorkflowExecutable: true,
	}
	if _, err := validateAssessedResult(input, Evidence{}, ShapeAssessment{}, bad); err == nil {
		t.Fatal("staffing gaps with a team workflow_mode must still be rejected")
	}
}

// TestValidateRoutingDecisionNormalisesDelegateLabelForTeamMode proves a team
// session's required_tool is derived from the session mode, not from whatever the
// model wrote. Observed live: the classifier picked a canonical roster owner but
// returned required_tool="delegate"; delegate authorizes against agent_links
// rather than the roster, so the enforced tool refused the very owner the
// classifier had just validated ("no delegation link from current agent to
// \"khanh-developer\"") and the turn answered nothing.
func TestValidateRoutingDecisionNormalisesDelegateLabelForTeamMode(t *testing.T) {
	owner := structuredProfile("team_member", "Bảo Khánh", "khanh-developer", "member", "writes and ships code for the team", CapabilityResearch)
	input := withKnownOrchestrationTool(Input{
		Mode:         ModeTeam,
		Message:      "nhờ Bảo Khánh viết giúp một hàm slugify",
		CurrentAgent: structuredProfile("agent", "Bảo An", "bao-an", "lead", "team coordinator", CapabilityLeadCoordinator),
		Team:         Profile{Kind: "team", Name: "Team", Text: "delivery team"},
		Members:      []Profile{owner},
		TeamRole:     "lead",
	})
	result := ValidateRoutingDecision(input, Evidence{}, Result{
		Decision:           DecisionTeam,
		WorkflowMode:       WorkflowModeSingleOwner,
		TaskType:           "dev",
		CurrentAgentFit:    "weak",
		BestTeamOwner:      owner.AgentKey,
		BestTeamFit:        "strong",
		WorkflowExecutable: true,
		RequiredTool:       "delegate",
	})
	if result.Decision != DecisionTeam {
		t.Fatalf("a valid owner routing must survive label normalisation: %+v", result)
	}
	if result.RequiredTool != "team_tasks" {
		t.Fatalf("required_tool = %q, want team_tasks for a team-mode session", result.RequiredTool)
	}
	if result.BestTeamOwner != owner.AgentKey {
		t.Fatalf("owner changed during normalisation: %q", result.BestTeamOwner)
	}
	if !strings.Contains(result.ValidatorReason, "normalised required tool") {
		t.Fatalf("normalisation must be recorded for audit, got %q", result.ValidatorReason)
	}
}

// TestValidateRoutingDecisionKeepsDelegateToolForDelegateMode is the other half of
// the invariant: normalisation follows the session mode, so a delegate session
// keeps delegate and does not get rewritten to team_tasks.
func TestValidateRoutingDecisionKeepsDelegateToolForDelegateMode(t *testing.T) {
	target := structuredProfile("delegate", "Bảo Ngọc", "bao-ngoc", "member", "handles delegated analysis", CapabilityResearch)
	input := withKnownOrchestrationTool(Input{
		Mode:         ModeDelegate,
		Message:      "chuyển việc phân tích này cho Bảo Ngọc",
		CurrentAgent: structuredProfile("agent", "Bảo An", "bao-an", "lead", "team coordinator", CapabilityLeadCoordinator),
		Delegates:    []Profile{target},
	})
	result := ValidateRoutingDecision(input, Evidence{}, Result{
		Decision:           DecisionTeam,
		WorkflowMode:       WorkflowModeSingleOwner,
		TaskType:           "analytics",
		CurrentAgentFit:    "weak",
		BestTeamOwner:      target.AgentKey,
		BestTeamFit:        "strong",
		WorkflowExecutable: true,
		RequiredTool:       "team_tasks",
	})
	if result.Decision == DecisionTeam && result.RequiredTool != "delegate" {
		t.Fatalf("required_tool = %q, want delegate for a delegate-mode session", result.RequiredTool)
	}
}
