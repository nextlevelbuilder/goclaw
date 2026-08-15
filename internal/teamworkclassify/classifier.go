package teamworkclassify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

type Decision string

const (
	DecisionSelf Decision = "self"
	DecisionTeam Decision = "team"
)

type Mode string

const (
	ModeSpawn    Mode = "spawn"
	ModeDelegate Mode = "delegate"
	ModeTeam     Mode = "team"
)

const (
	DefaultCloseMargin     = 0.08
	defaultTeamThreshold   = 0.35
	defaultEvidenceTimeout = 8 * time.Second
	defaultArbiterTimeout  = 30 * time.Second
)

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type Profile struct {
	Kind string
	Name string
	Text string
}

type Input struct {
	Mode                       Mode
	Message                    string
	CurrentAgent               Profile
	SelfTools                  []Profile
	Team                       Profile
	Members                    []Profile
	Delegates                  []Profile
	CollaborationTools         []Profile
	TeamRole                   string
	CanAssignTeamTasks         bool
	MemberRequestsEnabled      bool
	MemberRequestsAutoDispatch bool
	Embedder                   Embedder
	CloseMargin                float64
	TeamThreshold              float64
	Timeout                    time.Duration
}

type Result struct {
	Decision           Decision
	Confidence         float64
	Reason             string
	SelfScore          float64
	CollaborationScore float64
	Mode               Mode
	RequiredTool       string
	WorkflowHint       string
}

func Classify(ctx context.Context, input Input) Result {
	if input.Mode == "" || input.Mode == ModeSpawn {
		return Result{Decision: DecisionSelf, Reason: "no team or delegate capability"}
	}
	if input.Embedder == nil {
		return Result{Decision: DecisionSelf, Reason: "embedding unavailable"}
	}
	if strings.TrimSpace(input.Message) == "" {
		return Result{Decision: DecisionSelf, Reason: "empty message"}
	}
	if looksCasualOrSmallDirect(input.Message) {
		return Result{Decision: DecisionSelf, Reason: "message looks casual or direct"}
	}

	timeout := input.Timeout
	if timeout <= 0 {
		timeout = defaultEvidenceTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	selfDocs, collaborationDocs := splitProfileDocuments(input)
	if len(selfDocs) == 0 || len(collaborationDocs) == 0 {
		return Result{Decision: DecisionSelf, Reason: "insufficient collaboration profile"}
	}

	texts := append([]string{input.Message}, append(selfDocs, collaborationDocs...)...)
	vectors, err := input.Embedder.Embed(ctx, texts)
	if err != nil || len(vectors) != len(texts) {
		return Result{Decision: DecisionSelf, Reason: "embedding failed"}
	}
	query := vectors[0]
	selfScore := bestCosine(query, vectors[1:1+len(selfDocs)])
	collabScore := bestCosine(query, vectors[1+len(selfDocs):])

	margin := input.CloseMargin
	if margin <= 0 {
		margin = DefaultCloseMargin
	}
	threshold := input.TeamThreshold
	if threshold <= 0 {
		threshold = defaultTeamThreshold
	}

	diff := collabScore - selfScore
	bestScore := math.Max(selfScore, collabScore)
	switch {
	case math.Abs(diff) <= margin:
		return Result{
			Decision:           DecisionSelf,
			Confidence:         bestScore,
			Reason:             "profiles are close; defaulting to self",
			SelfScore:          selfScore,
			CollaborationScore: collabScore,
			Mode:               input.Mode,
		}
	case diff > margin && collabScore >= threshold:
		return Result{
			Decision:           DecisionTeam,
			Confidence:         diff,
			Reason:             "request is closer to team/delegate capability",
			SelfScore:          selfScore,
			CollaborationScore: collabScore,
			Mode:               input.Mode,
			RequiredTool:       requiredToolForMode(input.Mode),
		}
	default:
		return Result{
			Decision:           DecisionSelf,
			Confidence:         -diff,
			Reason:             "request is closer to current agent capability",
			SelfScore:          selfScore,
			CollaborationScore: collabScore,
			Mode:               input.Mode,
		}
	}
}

func ClassifyWithLLM(ctx context.Context, input Input, provider providers.Provider, model string, caps *usagecaps.Service) Result {
	fallback := Classify(ctx, input)
	if input.Mode == "" || input.Mode == ModeSpawn || provider == nil || strings.TrimSpace(model) == "" {
		return forceSelfDecision(fallback, "arbiter unavailable: ")
	}

	timeout := input.Timeout
	if timeout <= 0 {
		timeout = defaultArbiterTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req := providers.ChatRequest{
		Messages: BuildArbiterMessages(input, fallback),
		Model:    model,
		Options: map[string]any{
			providers.OptMaxTokens:   300,
			providers.OptTemperature: 0.0,
		},
	}
	var (
		resp *providers.ChatResponse
		err  error
	)
	if caps != nil {
		resp, err = caps.Chat(ctx, provider, req, usagecaps.ChatOptions{
			ModelID:         model,
			Purpose:         "team-work-classify",
			MaxOutputTokens: 300,
		})
	} else {
		resp, err = provider.Chat(ctx, req)
	}
	if err != nil || resp == nil {
		slog.Warn("team_work_classify: arbiter call failed",
			"model", model,
			"provider_type", fmt.Sprintf("%T", provider),
			"has_usage_caps", caps != nil,
			"mode", input.Mode,
			"timeout", timeout.String(),
			"response_nil", resp == nil,
			"error", err,
		)
		return forceSelfDecision(fallback, "arbiter_failed: ")
	}
	result, err := ParseArbiterResult(resp.Content, input.Mode)
	if err != nil {
		slog.Warn("team_work_classify: arbiter parse failed",
			"model", model,
			"provider_type", fmt.Sprintf("%T", provider),
			"mode", input.Mode,
			"content_len", len(resp.Content),
			"error", err,
		)
		return forceSelfDecision(fallback, "arbiter_parse_failed: ")
	}
	result.SelfScore = fallback.SelfScore
	result.CollaborationScore = fallback.CollaborationScore
	if result.Reason == "" {
		result.Reason = fallback.Reason
	}
	return applyTeamPermissionGate(input, result)
}

func forceSelfDecision(evidence Result, reasonPrefix string) Result {
	evidence.Decision = DecisionSelf
	evidence.RequiredTool = ""
	evidence.WorkflowHint = ""
	evidence.Reason = reasonPrefix + evidence.Reason
	return evidence
}

func applyTeamPermissionGate(input Input, result Result) Result {
	if result.Decision != DecisionTeam {
		return result
	}
	if result.WorkflowHint == "" {
		result.WorkflowHint = workflowHintForInput(input)
	}
	if input.Mode != ModeTeam {
		return result
	}
	role := strings.ToLower(strings.TrimSpace(input.TeamRole))
	if role == "" || role == "lead" || input.CanAssignTeamTasks {
		return result
	}
	if input.MemberRequestsEnabled && input.MemberRequestsAutoDispatch {
		return result
	}
	result.Decision = DecisionSelf
	result.RequiredTool = ""
	result.WorkflowHint = ""
	reason := "member lacks auto-dispatch team request permission"
	if !input.MemberRequestsEnabled {
		reason = "member lacks team request permission"
	}
	if strings.TrimSpace(result.Reason) == "" {
		result.Reason = reason
	} else {
		result.Reason = reason + ": " + result.Reason
	}
	return result
}

func workflowHintForInput(input Input) string {
	switch input.Mode {
	case ModeDelegate:
		return "Use the `delegate` tool with an available linked agent. Do not invent agent keys."
	case ModeTeam:
		role := strings.ToLower(strings.TrimSpace(input.TeamRole))
		if role != "" && role != "lead" && !input.CanAssignTeamTasks {
			if input.MemberRequestsEnabled && input.MemberRequestsAutoDispatch {
				return `As a team member, do not create or assign general team tasks. Use team_tasks(action="create", task_type="request", ...) to ask a teammate for help. This team's member requests auto-dispatch to the assignee after creation.`
			}
			if input.MemberRequestsEnabled {
				return "As a team member, member requests are enabled but auto-dispatch is disabled; choose self because request tasks would stay pending for leader review and may not run in this turn."
			}
			return "As a team member, you cannot create or assign general team tasks, and member request tasks are disabled. Handle the request directly or explain that a lead must coordinate the team work."
		}
		return `Use team_tasks(action="search" or "list") first, then team_tasks(action="create", ...) to assign work to an appropriate team member when new team work is required.`
	default:
		return ""
	}
}

func BuildArbiterMessages(input Input, evidence Result) []providers.Message {
	system := `You are a Team Work routing arbiter.
Return ONLY JSON. Do not answer the user.
Choose exactly one decision:
- self: the current agent should handle the request directly.
- team: the current agent must use the available team/delegate workflow.
Never return ask. If uncertain, choose self so normal chat is not disrupted.

Strict routing policy:
- Choose self when the user directly asks the current agent to read, summarize, explain, compare, or interpret existing files, documents, results, or prior team outputs.
- Choose self when the current agent can answer by using existing files, existing task results, or already completed team work without assigning new work.
- Do not choose team only because files are located in the team workspace.
- Do not choose team only because the topic mentions team, workflow, strategy, content, or prior team output.
- Choose team only when the user explicitly asks to assign, delegate, split work, ask other members, create tasks, gather opinions, or perform new multi-role work.
- Choose team when the request clearly needs new work from multiple roles, not merely synthesis of existing material.
- Permission matters: a team member who is not lead cannot assign or create general team tasks.
- If the current agent is a member and member requests are disabled, choose self for requests to assign, split, coordinate, or ask teammates.
- If the current agent is a member and member requests are enabled but auto-dispatch is disabled, choose self because pending leader review is not an immediate executable workflow.
- If the current agent is a member and member requests plus auto-dispatch are enabled, choose team only for a request-help workflow using task_type="request"; do not treat it as lead-style assignment.
- When self_score and collaboration_score are close, choose self unless there is a clear team signal.`

	var b strings.Builder
	b.WriteString("User request:\n")
	b.WriteString(input.Message)
	b.WriteString("\n\nRouting mode: ")
	b.WriteString(string(input.Mode))
	b.WriteString("\nRequired workflow tool when team is chosen: ")
	b.WriteString(requiredToolForMode(input.Mode))
	if input.Mode == ModeTeam {
		b.WriteString("\n\nTeam permission context:\n")
		b.WriteString("current_agent_team_role: ")
		b.WriteString(firstNonEmpty(input.TeamRole, "unknown"))
		b.WriteString("\ncan_assign_team_tasks: ")
		b.WriteString(fmt.Sprintf("%t", input.CanAssignTeamTasks))
		b.WriteString("\nmember_requests_enabled: ")
		b.WriteString(fmt.Sprintf("%t", input.MemberRequestsEnabled))
		b.WriteString("\nmember_requests_auto_dispatch: ")
		b.WriteString(fmt.Sprintf("%t", input.MemberRequestsAutoDispatch))
		if hint := workflowHintForInput(input); hint != "" {
			b.WriteString("\nworkflow_hint: ")
			b.WriteString(hint)
		}
	}
	b.WriteString("\n\nEmbedding evidence:\n")
	b.WriteString(fmt.Sprintf("self_score: %.4f\n", evidence.SelfScore))
	b.WriteString(fmt.Sprintf("collaboration_score: %.4f\n", evidence.CollaborationScore))
	b.WriteString("embedding_fallback_decision: ")
	b.WriteString(string(evidence.Decision))
	b.WriteString("\n\nCurrent agent and direct capability:\n")
	for _, doc := range appendProfileDocs(input.CurrentAgent, input.SelfTools) {
		b.WriteString("---\n")
		b.WriteString(doc)
		b.WriteString("\n")
	}
	b.WriteString("\nTeam/delegate/tool capability:\n")
	collaborationProfiles := append([]Profile{}, input.Members...)
	collaborationProfiles = append(collaborationProfiles, input.Delegates...)
	collaborationProfiles = append(collaborationProfiles, input.CollaborationTools...)
	for _, doc := range appendProfileDocs(input.Team, collaborationProfiles) {
		b.WriteString("---\n")
		b.WriteString(doc)
		b.WriteString("\n")
	}
	b.WriteString("\nReturn JSON shape:\n")
	b.WriteString(`{"decision":"self|team","confidence":0.0,"mode":"team|delegate","required_tool":"team_tasks|delegate","workflow_hint":"short permission-aware tool guidance","reason":"short internal reason"}`)

	return []providers.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: b.String()},
	}
}

func appendProfileDocs(first Profile, rest []Profile) []string {
	var docs []string
	if doc := renderProfile(first); doc != "" {
		docs = append(docs, doc)
	}
	for _, p := range rest {
		if doc := renderProfile(p); doc != "" {
			docs = append(docs, doc)
		}
	}
	return docs
}

func ParseArbiterResult(content string, mode Mode) (Result, error) {
	raw := strings.TrimSpace(content)
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end >= start {
			raw = raw[start : end+1]
		}
	}
	var parsed struct {
		Decision     string  `json:"decision"`
		Confidence   float64 `json:"confidence"`
		Mode         string  `json:"mode"`
		RequiredTool string  `json:"required_tool"`
		WorkflowHint string  `json:"workflow_hint"`
		Reason       string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return Result{}, err
	}
	resultMode := Mode(strings.TrimSpace(parsed.Mode))
	if resultMode == "" || resultMode == ModeSpawn {
		resultMode = mode
	}
	requiredTool := strings.TrimSpace(parsed.RequiredTool)
	if requiredTool == "" {
		requiredTool = requiredToolForMode(resultMode)
	}
	switch Decision(strings.ToLower(strings.TrimSpace(parsed.Decision))) {
	case DecisionTeam:
		if resultMode != ModeTeam && resultMode != ModeDelegate {
			resultMode = mode
		}
		if resultMode != ModeTeam && resultMode != ModeDelegate {
			return Result{Decision: DecisionSelf, Mode: mode, Reason: "arbiter requested team without workflow mode"}, nil
		}
		return Result{
			Decision:     DecisionTeam,
			Confidence:   parsed.Confidence,
			Reason:       strings.TrimSpace(parsed.Reason),
			Mode:         resultMode,
			RequiredTool: requiredTool,
			WorkflowHint: strings.TrimSpace(parsed.WorkflowHint),
		}, nil
	case DecisionSelf:
		return Result{Decision: DecisionSelf, Confidence: parsed.Confidence, Reason: strings.TrimSpace(parsed.Reason), Mode: mode}, nil
	default:
		return Result{Decision: DecisionSelf, Confidence: parsed.Confidence, Reason: "arbiter returned unsupported decision; defaulting to self", Mode: mode}, nil
	}
}

func requiredToolForMode(mode Mode) string {
	switch mode {
	case ModeTeam:
		return "team_tasks"
	case ModeDelegate:
		return "delegate"
	default:
		return ""
	}
}

func looksCasualOrSmallDirect(message string) bool {
	s := strings.ToLower(strings.TrimSpace(message))
	if s == "" {
		return true
	}
	actionMarkers := []string{
		"hãy ", "hay ", "viết", "viet", "tạo", "tao", "làm", "lam",
		"kiểm tra", "kiem tra", "phân tích", "phan tich", "soạn", "soan",
		"dịch", "dich", "tìm", "tim", "lập kế hoạch", "lap ke hoach",
		"triển khai", "trien khai", "thiết kế", "thiet ke", "sửa", "sua",
		"đánh giá", "danh gia", "tóm tắt", "tom tat", "nghiên cứu", "nghien cuu",
		"check", "create", "write", "analyze", "analyse", "fix", "build",
		"plan", "research", "design", "review", "summarize", "summarise",
		"查", "写", "创建", "分析", "修复", "设计", "总结",
		"작성", "생성", "분석", "수정", "설계", "요약",
	}
	for _, marker := range actionMarkers {
		if strings.Contains(s, marker) {
			return false
		}
	}
	casualMarkers := []string{
		"chào", "chao", "hello", "hi", "ok", "ừ", "uh", "cảm ơn", "cam on",
		"thanks", "thank you", "xin lỗi", "sorry", "được rồi", "duoc roi",
	}
	for _, marker := range casualMarkers {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return len([]rune(s)) <= 80
}

func BuildProfileDocuments(input Input) []string {
	selfDocs, collaborationDocs := splitProfileDocuments(input)
	return append(selfDocs, collaborationDocs...)
}

func splitProfileDocuments(input Input) ([]string, []string) {
	var selfDocs []string
	if doc := renderProfile(input.CurrentAgent); doc != "" {
		selfDocs = append(selfDocs, doc)
	}
	for _, p := range input.SelfTools {
		if doc := renderProfile(p); doc != "" {
			selfDocs = append(selfDocs, doc)
		}
	}

	var collaborationDocs []string
	if doc := renderProfile(input.Team); doc != "" {
		collaborationDocs = append(collaborationDocs, doc)
	}
	for _, group := range [][]Profile{input.Members, input.Delegates, input.CollaborationTools} {
		for _, p := range group {
			if doc := renderProfile(p); doc != "" {
				collaborationDocs = append(collaborationDocs, doc)
			}
		}
	}
	return selfDocs, collaborationDocs
}

func renderProfile(p Profile) string {
	name := strings.TrimSpace(p.Name)
	text := strings.TrimSpace(p.Text)
	kind := strings.TrimSpace(p.Kind)
	if name == "" && text == "" {
		return ""
	}
	var b strings.Builder
	if kind != "" {
		b.WriteString("kind: ")
		b.WriteString(kind)
		b.WriteString("\n")
	}
	if name != "" {
		b.WriteString("name: ")
		b.WriteString(name)
		b.WriteString("\n")
	}
	if text != "" {
		b.WriteString("description: ")
		b.WriteString(text)
	}
	return b.String()
}

func bestCosine(query []float32, docs [][]float32) float64 {
	best := -1.0
	for _, doc := range docs {
		score, err := cosine(query, doc)
		if err == nil && score > best {
			best = score
		}
	}
	if best < 0 {
		return 0
	}
	return best
}

func cosine(a, b []float32) (float64, error) {
	if len(a) == 0 || len(a) != len(b) {
		return 0, errors.New("dimension mismatch")
	}
	var dot, na, nb float64
	for i := range a {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 {
		return 0, nil
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb)), nil
}
