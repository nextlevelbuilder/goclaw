package teamworkclassify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/teamconfig"
)

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

func TestClassifySpawnModeSkipsToSelf(t *testing.T) {
	result := Classify(context.Background(), Input{
		Mode:    ModeSpawn,
		Message: "lập kế hoạch content và phân tích chiến lược",
		Embedder: fakeEmbedder{
			"lập kế hoạch": {1, 0},
		},
	})
	if result.Decision != DecisionSelf {
		t.Fatalf("Decision = %q, want %q", result.Decision, DecisionSelf)
	}
	if result.Reason == "" {
		t.Fatal("Reason is empty")
	}
}

func TestClassifyTeamWhenRequestMatchesTeamMemberOrToolProfile(t *testing.T) {
	result := Classify(context.Background(), Input{
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
	if result.Decision != DecisionTeam {
		t.Fatalf("Decision = %q, want %q; result=%+v", result.Decision, DecisionTeam, result)
	}
	if result.CollaborationScore <= result.SelfScore {
		t.Fatalf("CollaborationScore %.3f must be greater than SelfScore %.3f", result.CollaborationScore, result.SelfScore)
	}
}

func TestClassifySelfWhenCurrentAgentToolsAreBestMatch(t *testing.T) {
	result := Classify(context.Background(), Input{
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
	if result.Decision != DecisionSelf {
		t.Fatalf("Decision = %q, want %q; result=%+v", result.Decision, DecisionSelf, result)
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
	provider := &fakeArbiterProvider{content: `{"decision":"self","confidence":0.5,"mode":"team","reason":"direct enough"}`}
	result := ClassifyWithLLM(context.Background(), ambiguousInput(), provider, "arbiter-model", nil)
	if result.Decision != DecisionSelf {
		t.Fatalf("Decision = %q, want %q", result.Decision, DecisionSelf)
	}
	if provider.calls != 1 {
		t.Fatalf("arbiter calls = %d, want 1 for ambiguous evidence", provider.calls)
	}
	if provider.deadlineRemaining < 25*time.Second {
		t.Fatalf("arbiter deadline remaining = %s, want at least 25s", provider.deadlineRemaining)
	}
}

func TestClassifyWithLLMFallsBackToSelfOnArbiterError(t *testing.T) {
	provider := &fakeArbiterProvider{err: errors.New("provider unavailable")}
	result := ClassifyWithLLM(context.Background(), ambiguousInput(), provider, "arbiter-model", nil)
	if result.Decision != DecisionSelf {
		t.Fatalf("Decision = %q, want %q; result=%+v", result.Decision, DecisionSelf, result)
	}
	if !strings.Contains(result.Reason, "arbiter_failed") {
		t.Fatalf("Reason = %q, want arbiter_failed marker", result.Reason)
	}
}

type fakeArbiterProvider struct {
	content           string
	err               error
	req               providers.ChatRequest
	calls             int
	deadlineRemaining time.Duration
}

func (p *fakeArbiterProvider) Chat(ctx context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	p.calls++
	p.req = req
	if deadline, ok := ctx.Deadline(); ok {
		p.deadlineRemaining = time.Until(deadline)
	}
	if p.err != nil {
		return nil, p.err
	}
	return &providers.ChatResponse{Content: p.content}, nil
}

// ambiguousInput produces |collab-self| <= margin (both scores are 1.0), so the
// embedding evidence is undecided and the arbiter must be consulted.
func ambiguousInput() Input {
	return Input{
		Mode:         ModeTeam,
		Message:      "lập kế hoạch chiến lược dài cho chiến dịch mới",
		CurrentAgent: Profile{Kind: "agent", Name: "Bảo An", Text: "điều phối"},
		Team:         Profile{Kind: "team", Name: "Growth Team", Text: "chiến lược"},
		Members:      []Profile{{Kind: "team_member", Name: "Bảo Ly", Text: "chiến lược"}},
		CollaborationTools: []Profile{
			{Kind: "tool", Name: "team_tasks", Text: "chiến lược"},
		},
		Embedder: fakeEmbedder{
			"lập kế hoạch": {1, 0}, // message
			"điều phối":    {1, 0}, // self doc — identical vector → selfScore 1.0
			"chiến lược":   {1, 0}, // collab docs — identical vector → collabScore 1.0
		},
	}
}

// decisiveTeamInput produces diff = 1.0 > margin and collab = 1.0 >= threshold,
// so the embedding evidence decides "team" without the arbiter.
func decisiveTeamInput() Input {
	return Input{
		Mode:         ModeTeam,
		Message:      "giao việc lập kế hoạch chiến lược dài cho team",
		CurrentAgent: Profile{Kind: "agent", Name: "Lead", Text: "điều phối chung"},
		Team:         Profile{Kind: "team", Name: "Team", Text: "chiến lược"},
		Members:      []Profile{{Kind: "team_member", Name: "Bảo Ly", Text: "chiến lược"}},
		CollaborationTools: []Profile{
			{Kind: "tool", Name: "team_tasks", Text: "assign"},
		},
		Embedder: fakeEmbedder{
			"lập kế hoạch": {1, 0}, // message → collab 1.0
			"chiến lược":   {1, 0}, // collab docs
			"điều phối":    {0, 1}, // self doc → self 0.0
			"assign":       {0, 1},
		},
	}
}

// decisiveSelfInput is the mirror of decisiveTeamInput: the message matches the
// self doc and the collab docs are orthogonal, so diff = -1.0 (decisively self).
func decisiveSelfInput() Input {
	in := decisiveTeamInput()
	in.Message = "tóm tắt giúp phần này"
	in.Embedder = fakeEmbedder{
		"chiến lược": {0, 1},
		"tóm tắt":    {1, 0}, // message → self 1.0
		"điều phối":  {1, 0}, // self doc
		"assign":     {0, 1},
	}
	return in
}

func (p *fakeArbiterProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return nil, nil
}

func (p *fakeArbiterProvider) DefaultModel() string { return "fake-model" }
func (p *fakeArbiterProvider) Name() string         { return "fake-provider" }

func TestParseArbiterResultAcceptsSelfOrTeamOnly(t *testing.T) {
	team, err := ParseArbiterResult(`{"decision":"team","confidence":0.91,"mode":"team","required_tool":"team_tasks","reason":"needs members"}`, ModeTeam)
	if err != nil {
		t.Fatalf("ParseArbiterResult(team) error = %v", err)
	}
	if team.Decision != DecisionTeam || team.RequiredTool != "team_tasks" || team.Mode != ModeTeam {
		t.Fatalf("team result = %+v", team)
	}

	self, err := ParseArbiterResult(`{"decision":"ask","confidence":0.55,"mode":"team","reason":"unclear"}`, ModeTeam)
	if err != nil {
		t.Fatalf("ParseArbiterResult(unsupported decision fallback) error = %v", err)
	}
	if self.Decision != DecisionSelf {
		t.Fatalf("unsupported decision must fall back to self, got %+v", self)
	}
}

func TestClassifyWithLLMUsesArbiterDecisionAndEvidence(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team","confidence":0.86,"mode":"team","required_tool":"team_tasks","reason":"requires content and strategy members"}`}
	in := ambiguousInput()
	in.TeamRole = "lead"
	in.CanAssignTeamTasks = true
	result := ClassifyWithLLM(context.Background(), in, provider, "arbiter-model", nil)
	if result.Decision != DecisionTeam || result.RequiredTool != "team_tasks" {
		t.Fatalf("ClassifyWithLLM result = %+v", result)
	}
	if provider.calls != 1 {
		t.Fatalf("arbiter calls = %d, want 1", provider.calls)
	}
	if len(provider.req.Messages) < 2 {
		t.Fatalf("arbiter request messages missing: %+v", provider.req)
	}
	joined := provider.req.Messages[0].Content + "\n" + provider.req.Messages[1].Content
	for _, want := range []string{"Return ONLY JSON", "Bảo An", "Growth Team", "team_tasks", "self_score", "collaboration_score"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("arbiter prompt missing %q:\n%s", want, joined)
		}
	}
}

func TestClassifyWithLLMForcesSelfWhenMemberCannotAssignOrRequest(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team","confidence":0.91,"mode":"team","required_tool":"team_tasks","reason":"route to teammate"}`}
	result := ClassifyWithLLM(context.Background(), Input{
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
	}, provider, "arbiter-model", nil)
	if provider.calls != 0 {
		t.Fatalf("arbiter calls = %d, want 0 — gate must hold on the no-arbiter path", provider.calls)
	}
	if result.Decision != DecisionSelf {
		t.Fatalf("Decision = %q, want self for member without request permission; result=%+v", result.Decision, result)
	}
	if result.RequiredTool != "" {
		t.Fatalf("RequiredTool = %q, want empty", result.RequiredTool)
	}
	if !strings.Contains(result.Reason, "member lacks team request permission") {
		t.Fatalf("Reason = %q, want member permission marker", result.Reason)
	}
}

func TestClassifyWithLLMForcesSelfWhenMemberRequestNeedsLeaderReview(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team","confidence":0.88,"mode":"team","required_tool":"team_tasks","reason":"member should request help"}`}
	result := ClassifyWithLLM(context.Background(), Input{
		Mode:                       ModeTeam,
		Message:                    "em tạo request nhờ bạn content hỗ trợ phần này",
		CurrentAgent:               Profile{Kind: "agent", Name: "Member", Text: "team member"},
		Team:                       Profile{Kind: "team", Name: "Team", Text: "content team"},
		TeamRole:                   "member",
		CanAssignTeamTasks:         false,
		MemberRequestsEnabled:      true,
		MemberRequestsAutoDispatch: false,
		Embedder: fakeEmbedder{
			"request":      {1, 0},
			"content team": {1, 0},
			"team member":  {0, 1},
		},
	}, provider, "arbiter-model", nil)
	if provider.calls != 0 {
		t.Fatalf("arbiter calls = %d, want 0 — gate must hold on the no-arbiter path", provider.calls)
	}
	if result.Decision != DecisionSelf {
		t.Fatalf("Decision = %q, want self when member request does not auto-dispatch; result=%+v", result.Decision, result)
	}
	if !strings.Contains(result.Reason, "member lacks auto-dispatch team request permission") {
		t.Fatalf("Reason = %q, want auto-dispatch permission marker", result.Reason)
	}
}

func TestClassifyWithLLMAllowsAutoDispatchMemberRequest(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team","confidence":0.88,"mode":"team","required_tool":"team_tasks","reason":"member should request help"}`}
	result := ClassifyWithLLM(context.Background(), Input{
		Mode:                       ModeTeam,
		Message:                    "em tạo request nhờ bạn content hỗ trợ phần này",
		CurrentAgent:               Profile{Kind: "agent", Name: "Member", Text: "team member"},
		Team:                       Profile{Kind: "team", Name: "Team", Text: "content team"},
		TeamRole:                   "member",
		CanAssignTeamTasks:         false,
		MemberRequestsEnabled:      true,
		MemberRequestsAutoDispatch: true,
		Embedder: fakeEmbedder{
			"request":      {1, 0},
			"content team": {1, 0},
			"team member":  {0, 1},
		},
	}, provider, "arbiter-model", nil)
	if result.Decision != DecisionTeam || result.RequiredTool != "team_tasks" {
		t.Fatalf("ClassifyWithLLM result = %+v, want team/team_tasks", result)
	}
	if !strings.Contains(result.WorkflowHint, `task_type="request"`) {
		t.Fatalf("WorkflowHint = %q, want task_type=request guidance", result.WorkflowHint)
	}
	if !strings.Contains(result.WorkflowHint, "auto-dispatch") {
		t.Fatalf("WorkflowHint = %q, want auto-dispatch guidance", result.WorkflowHint)
	}
}

func TestBuildArbiterMessagesIncludesStrictTeamRoutingPolicy(t *testing.T) {
	messages := BuildArbiterMessages(Input{
		Mode:         ModeTeam,
		Message:      "em đọc cả 2 file rồi diễn giải lại cho anh về phương án em chọn",
		CurrentAgent: Profile{Kind: "agent", Name: "Bảo An", Text: "lead and synthesize existing team outputs"},
		Team:         Profile{Kind: "team", Name: "Strategy Team", Text: "team research and execution"},
		CollaborationTools: []Profile{
			{Kind: "tool", Name: "team_tasks", Text: "search existing tasks, create tasks, assign work"},
			{Kind: "capability", Name: "shared team workspace", Text: "files from previous team work"},
		},
	}, Result{
		Decision:           DecisionSelf,
		SelfScore:          0.6341,
		CollaborationScore: 0.6362,
	})
	if len(messages) < 2 {
		t.Fatalf("messages = %+v, want system and user messages", messages)
	}
	prompt := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{
		"Choose self when the user directly asks the current agent to read, summarize, explain, compare, or interpret existing files",
		"Do not choose team only because files are located in the team workspace",
		"Do not choose team only because the topic mentions team, workflow, strategy, content, or prior team output",
		"Choose team only when the user explicitly asks to assign, delegate, split work, ask other members, create tasks, gather opinions, or perform new multi-role work",
		"When self_score and collaboration_score are close, choose self unless there is a clear team signal",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("arbiter prompt missing policy %q:\n%s", want, prompt)
		}
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
	}, Result{Decision: DecisionSelf})
	prompt := messages[0].Content + "\n" + messages[1].Content
	for _, want := range []string{
		"Team permission context",
		"current_agent_team_role: member",
		"can_assign_team_tasks: false",
		"member_requests_enabled: true",
		"member_requests_auto_dispatch: false",
		"choose self",
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

// Zero arbiter calls when embedding evidence decides the outcome.
func TestClassifyWithLLMSkipsArbiterOnDecisiveTeam(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"self"}`}
	in := decisiveTeamInput()
	in.TeamRole = "lead"
	in.CanAssignTeamTasks = true
	result := ClassifyWithLLM(context.Background(), in, provider, "arbiter-model", nil)
	if provider.calls != 0 {
		t.Fatalf("arbiter calls = %d, want 0 for decisive evidence", provider.calls)
	}
	if result.Decision != DecisionTeam || result.RequiredTool != "team_tasks" {
		t.Fatalf("result = %+v, want team/team_tasks", result)
	}
	if result.WorkflowHint == "" {
		t.Fatal("WorkflowHint empty on decisive-team path")
	}
}

func TestClassifyWithLLMSkipsArbiterOnDecisiveSelf(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team"}`}
	result := ClassifyWithLLM(context.Background(), decisiveSelfInput(), provider, "arbiter-model", nil)
	if provider.calls != 0 {
		t.Fatalf("arbiter calls = %d, want 0", provider.calls)
	}
	if result.Decision != DecisionSelf {
		t.Fatalf("Decision = %q, want self", result.Decision)
	}
}

func TestClassifyWithLLMSkipsArbiterOnCasualMessage(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team"}`}
	in := decisiveTeamInput()
	in.Message = "chào em"
	result := ClassifyWithLLM(context.Background(), in, provider, "arbiter-model", nil)
	if provider.calls != 0 {
		t.Fatalf("arbiter calls = %d, want 0 for casual guard", provider.calls)
	}
	if result.Decision != DecisionSelf {
		t.Fatalf("Decision = %q, want self", result.Decision)
	}
}

// C1 regression: permission gate applies even when the arbiter is skipped.
func TestClassifyWithLLMAppliesPermissionGateWithoutArbiter(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"team"}`}
	in := decisiveTeamInput()
	in.TeamRole = "member"
	in.CanAssignTeamTasks = false
	in.MemberRequestsEnabled = false
	in.MemberRequestsAutoDispatch = false
	result := ClassifyWithLLM(context.Background(), in, provider, "arbiter-model", nil)
	if provider.calls != 0 {
		t.Fatalf("arbiter calls = %d, want 0", provider.calls)
	}
	if result.Decision != DecisionSelf {
		t.Fatalf("Decision = %q, want self for member without permission", result.Decision)
	}
	if result.RequiredTool != "" || result.WorkflowHint != "" {
		t.Fatalf("gated result leaked tool/hint: %+v", result)
	}
	if !strings.Contains(result.Reason, "member lacks team request permission") {
		t.Fatalf("Reason = %q, want member permission marker", result.Reason)
	}
}

// Ambiguous flag is set only on the two undecided branches.
func TestClassifyMarksAmbiguousOnlyForCloseOrWeakScores(t *testing.T) {
	cases := []struct {
		name  string
		input Input
		want  bool
	}{
		{"spawn mode", Input{Mode: ModeSpawn, Message: "x"}, false},
		{"empty message", Input{Mode: ModeTeam, Message: "   ", Embedder: fakeEmbedder{}}, false},
		{"casual guard", func() Input { in := decisiveTeamInput(); in.Message = "ok"; return in }(), false},
		{"no embedder", Input{Mode: ModeTeam, Message: "phân tích chiến lược"}, false},
		{"close margin", ambiguousInput(), true},
		{"decisive team", decisiveTeamInput(), false},
		{"decisive self", decisiveSelfInput(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(context.Background(), tc.input).Ambiguous; got != tc.want {
				t.Fatalf("Ambiguous = %v, want %v (result=%+v)", got, tc.want, Classify(context.Background(), tc.input))
			}
		})
	}
}

func TestClassifyRuleMatchWinsOverCasualGuard(t *testing.T) {
	// Message is short ("ok go") — casual guard would normally claim it, but
	// the operator rule must win (explicit config beats heuristic).
	rule := teamconfig.RoutingRule{AgentKey: "coder-agent", Keywords: []string{"go"}}
	in := decisiveTeamInput()
	in.Message = "ok go"
	in.RoutingRules = []teamconfig.RoutingRule{rule}
	in.TeamRole = "lead"
	in.CanAssignTeamTasks = true
	result := Classify(context.Background(), in)
	if result.Decision != DecisionTeam || result.TargetAgentKey != "coder-agent" {
		t.Fatalf("rule must win over casual guard, got %+v", result)
	}
	if result.RequiredTool != "team_tasks" || !strings.Contains(result.WorkflowHint, "search") || !strings.Contains(result.WorkflowHint, "coder-agent") {
		t.Fatalf("rule hint wrong: %+v", result)
	}
}

func TestClassifyRuleMatchNoEmbedder(t *testing.T) {
	// Rule-matched messages route even with no embedder (spec success #2).
	rule := teamconfig.RoutingRule{AgentKey: "coder-agent", FilePatterns: []string{".go"}}
	in := Input{Mode: ModeTeam, Message: "fix internal/foo.go", RoutingRules: []teamconfig.RoutingRule{rule}}
	result := Classify(context.Background(), in)
	if result.Decision != DecisionTeam {
		t.Fatalf("rule must route without embedder, got %+v", result)
	}
}

func TestClassifyRuleStaleMemberIgnored(t *testing.T) {
	// EffectiveRules already dropped this — but test the classifier contract:
	// a rule naming a member absent from RoutingRules must not fire.
	in := decisiveTeamInput()
	in.Message = "fix internal/foo.go"
	in.RoutingRules = nil // stale rule removed by EffectiveRules
	result := Classify(context.Background(), in)
	if result.TargetAgentKey != "" {
		t.Fatalf("no routing target expected, got %q", result.TargetAgentKey)
	}
}

func TestClassifyWithLLMRuleMatchPermissionGateMember(t *testing.T) {
	// Rule-driven team decision + member without member_requests → DecisionSelf.
	provider := &fakeArbiterProvider{content: `{"decision":"team"}`}
	rule := teamconfig.RoutingRule{AgentKey: "peer-agent", Keywords: []string{"go"}}
	in := decisiveTeamInput()
	in.Message = "go fix it"
	in.RoutingRules = []teamconfig.RoutingRule{rule}
	in.TeamRole = "member"
	in.CanAssignTeamTasks = false
	in.MemberRequestsEnabled = false
	result := ClassifyWithLLM(context.Background(), in, provider, "m", nil)
	if provider.calls != 0 {
		t.Fatalf("no arbiter for rule match, calls=%d", provider.calls)
	}
	if result.Decision != DecisionSelf {
		t.Fatalf("member without permission must be gated to self, got %+v", result)
	}
	if result.TargetAgentKey != "" {
		t.Fatalf("member without permission must have TargetAgentKey cleared, got %q", result.TargetAgentKey)
	}
}

func TestClassifyWithLLMRuleMatchLeadZeroArbiter(t *testing.T) {
	provider := &fakeArbiterProvider{content: `{"decision":"self"}`}
	rule := teamconfig.RoutingRule{AgentKey: "coder-agent", FilePatterns: []string{".go"}}
	in := decisiveTeamInput()
	in.Message = "fix internal/foo.go"
	in.RoutingRules = []teamconfig.RoutingRule{rule}
	in.TeamRole = "lead"
	in.CanAssignTeamTasks = true
	result := ClassifyWithLLM(context.Background(), in, provider, "m", nil)
	if provider.calls != 0 {
		t.Fatalf("rule match must skip arbiter, calls=%d", provider.calls)
	}
	if result.Decision != DecisionTeam || result.TargetAgentKey != "coder-agent" {
		t.Fatalf("rule result wrong: %+v", result)
	}
	if result.Source != "rule" {
		t.Fatalf("expected source=rule, got %q", result.Source)
	}
}

// Step 3 fields are inert on the deterministic rule path: a threshold or
// fallback change must not alter a rule-driven team decision.
func TestClassifyRuleDecisionIgnoresFallbackAndThreshold(t *testing.T) {
	settingsFor := func(fallback bool, threshold float64) []teamconfig.RoutingRule {
		raw := fmt.Sprintf(`{"routing":{"enabled":true,"fallback_to_self":%t,"confidence_threshold":%g,
			"rules":[{"agent_key":"coder-agent","file_patterns":[".go"]}]}}`, fallback, threshold)
		r := teamconfig.ParseRouting(json.RawMessage(raw))
		return teamconfig.EffectiveRules(r, []string{"coder-agent"})
	}
	var baseline Result
	for i, tc := range []struct {
		fallback  bool
		threshold float64
	}{{true, 0.65}, {false, 0.99}, {false, 0.01}} {
		in := Input{Mode: ModeTeam, Message: "fix internal/foo.go", RoutingRules: settingsFor(tc.fallback, tc.threshold)}
		got := Classify(context.Background(), in)
		if got.Decision != DecisionTeam || got.TargetAgentKey != "coder-agent" {
			t.Fatalf("case %d (fallback=%v threshold=%v): got %+v, want team/coder-agent", i, tc.fallback, tc.threshold, got)
		}
		if i == 0 {
			baseline = got
			continue
		}
		if got.Decision != baseline.Decision || got.TargetAgentKey != baseline.TargetAgentKey ||
			got.RequiredTool != baseline.RequiredTool || got.WorkflowHint != baseline.WorkflowHint {
			t.Fatalf("case %d result %+v differs from baseline %+v", i, got, baseline)
		}
	}
}

func TestClassifySourceProvenance(t *testing.T) {
	// 1. Decisive evidence
	inEv := decisiveTeamInput()
	inEv.TeamRole = "lead"
	inEv.CanAssignTeamTasks = true
	resEv := ClassifyWithLLM(context.Background(), inEv, nil, "", nil)
	if resEv.Source != "evidence" {
		t.Fatalf("decisive evidence source = %q, want evidence", resEv.Source)
	}

	// 2. Rule match
	inRule := decisiveTeamInput()
	inRule.Message = "fix the bug in internal/foo.go"
	inRule.TeamRole = "lead"
	inRule.CanAssignTeamTasks = true
	inRule.RoutingRules = []teamconfig.RoutingRule{{AgentKey: "coder-agent", Keywords: []string{"fix"}}}
	resRule := ClassifyWithLLM(context.Background(), inRule, nil, "", nil)
	if resRule.Source != "rule" {
		t.Fatalf("rule match source = %q, want rule", resRule.Source)
	}

	// 3. Ambiguous evidence resolved by arbiter
	inAmb := ambiguousInput()
	inAmb.TeamRole = "lead"
	inAmb.CanAssignTeamTasks = true
	provider := &fakeArbiterProvider{content: `{"decision":"team","reason":"lead arbiter decided team"}`}
	resArb := ClassifyWithLLM(context.Background(), inAmb, provider, "m", nil)
	if resArb.Source != "arbiter" {
		t.Fatalf("arbiter source = %q, want arbiter", resArb.Source)
	}

	// 4. Ambiguous evidence with arbiter unavailable
	resNoArb := ClassifyWithLLM(context.Background(), inAmb, nil, "", nil)
	if resNoArb.Source != "evidence" {
		t.Fatalf("unavailable arbiter source = %q, want evidence", resNoArb.Source)
	}
}
