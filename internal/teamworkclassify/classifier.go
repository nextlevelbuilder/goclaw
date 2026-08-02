package teamworkclassify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
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
	defaultArbiterTimeout = 30 * time.Second
	defaultPlannerTimeout = 60 * time.Second
)

type Profile struct {
	Kind                 string
	Name                 string
	Text                 string
	AgentID              uuid.UUID
	AgentKey             string
	DisplayName          string
	TeamRole             string
	Capabilities         []StructuredCapability
	CapabilitiesStatus   DataStatus
	ExpertiseSummary     string
	AvailableTools       []string
	AvailableToolsStatus DataStatus
}

type Input struct {
	Mode                       Mode
	Message                    string
	RecentContext              string
	PinnedSkillsContext        string
	PinnedSkillNames           []string
	PinnedSkillsWarning        string
	CurrentAgent               Profile
	SelfTools                  []Profile
	Team                       Profile
	Members                    []Profile
	Delegates                  []Profile
	CollaborationTools         []Profile
	ToolAllow                  []string
	TeamRole                   string
	CanAssignTeamTasks         bool
	MemberRequestsEnabled      bool
	MemberRequestsAutoDispatch bool
	CoordinatorAgentID         uuid.UUID
	CoordinatorAgentKey        string
	Timeout                    time.Duration
}

type Result struct {
	Decision                 Decision
	Confidence               float64
	Reason                   string
	Mode                     Mode
	RequiredTool             string
	WorkflowHint             string
	CurrentAgentRole         string
	TaskType                 string
	CurrentAgentFit          string
	BestTeamOwner            string
	BestTeamOwnerID          uuid.UUID
	BestTeamOwnerRole        string
	BestTeamFit              string
	SpecialistMatchFound     bool
	LeadSelectedAsFallback   bool
	RoutingPriorityUsed      string
	OwnerSelectionReason     string
	FollowupContextReference bool
	BetterCollaboratorFit    string
	RequestKind              string
	WorkflowExecutable       bool
	DecisionBeforeValidation Decision
	ValidatorReason          string
	WorkflowMode             WorkflowMode
	Plan                     *WorkflowPlan
	PlannerRepaired          bool
	PlannerValidationReason  string
	RequestedWorkShape       WorkShape
	VerifiedWorkShape        WorkShape
	EffectiveWorkShape       WorkShape
	RequestedWorkflowMode    WorkflowMode
	EffectiveWorkflowMode    WorkflowMode
	ShapeTraits              []ShapeTrait
	RequestedReviewRequired  bool
	EffectiveReviewRequired  bool
	DegradedWorkflow         bool
	DegradedReasonCode       string
	NonExecutable            bool
	StandaloneRequest        string
	IntentRelation           IntentRelation
	IntentInheritedScope     []string
	IntentRequestedOutputs   []string
	StaffingGaps             []string
}

type IntentRelation string

const (
	IntentRelationNew          IntentRelation = "new"
	IntentRelationContinuation IntentRelation = "continuation"
	IntentRelationRefinement   IntentRelation = "refinement"
	IntentRelationCorrection   IntentRelation = "correction"
)

func callClassifierProvider(ctx context.Context, provider providers.Provider, req providers.ChatRequest, model string, caps *usagecaps.Service, purpose string) (*providers.ChatResponse, error) {
	if caps != nil {
		return caps.Chat(ctx, provider, req, usagecaps.ChatOptions{ModelID: model, Purpose: purpose})
	}
	return provider.Chat(ctx, req)
}

func callClassifierAttempt(parent context.Context, timeout time.Duration, provider providers.Provider, req providers.ChatRequest, model string, caps *usagecaps.Service, purpose string) (*providers.ChatResponse, error) {
	callCtx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return callClassifierProvider(callCtx, provider, req, model, caps, purpose)
}

// stageTimeouts returns the active route budget and a retained planner budget
// for persisted legacy workflow validation. Non-positive input keeps built-in
// defaults; a configured route budget preserves the historical ratio.
func stageTimeouts(configured time.Duration) (arbiter, planner time.Duration) {
	if configured <= 0 {
		return defaultArbiterTimeout, defaultPlannerTimeout
	}
	return configured, scalePlannerTimeout(configured)
}

// scalePlannerTimeout applies the built-in planner:arbiter ratio to a configured
// budget. The ratio is computed as a plain integer FIRST: multiplying two
// time.Duration values multiplies their nanosecond counts, which yields a
// meaningless (and overflow-prone) product rather than a scaled duration.
func scalePlannerTimeout(configured time.Duration) time.Duration {
	ratio := int64(defaultPlannerTimeout / defaultArbiterTimeout)
	if ratio < 1 {
		ratio = 1
	}
	return configured * time.Duration(ratio)
}

func callFailureReason(parent context.Context, err error, prefix string) string {
	if parent.Err() != nil {
		return prefix + "_transport_failed"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return prefix + "_timeout"
	}
	return prefix + "_transport_failed"
}

func safeSelfResult(input Input, reason string) Result {
	return Result{
		Decision: DecisionSelf, DecisionBeforeValidation: DecisionSelf, Mode: input.Mode,
		Reason: reason, ValidatorReason: reason, DegradedWorkflow: true, DegradedReasonCode: reason,
		WorkflowMode: WorkflowModeSelf, RequestedWorkflowMode: WorkflowModeSelf, EffectiveWorkflowMode: WorkflowModeSelf,
		CurrentAgentFit: "partial", BestTeamFit: "none",
		TaskType: classifyTaskType(input.Message, input.RecentContext), RequestKind: requestKindFromTaskType(classifyTaskType(input.Message, input.RecentContext), DecisionSelf),
	}
}

type Capability string

const (
	CapabilityLeadCoordinator Capability = "lead_coordinator"
	CapabilityResearch        Capability = "research"
	CapabilityStrategy        Capability = "strategy"
	CapabilityAnalyticsCritic Capability = "analytics_critic"
	CapabilityContentLead     Capability = "content_lead"
	CapabilityVisualPrompt    Capability = "visual_prompt_artist"
	CapabilityTechnical       Capability = "technical"
	CapabilityQA              Capability = "qa"
)

func classifyTaskType(message, recent string) string {
	text := strings.ToLower(message + "\n" + recent)
	switch {
	case containsAny(text, "kpi", "performance", "data", "evidence", "risk", "quota", "analytics", "analyst", "số liệu", "du lieu", "bằng chứng", "bang chung", "rủi ro", "rui ro"):
		return "analytics"
	case containsAny(text, "research", "nghiên cứu", "nghien cuu", "market", "thị trường", "thi truong", "customer", "competitor", "đối thủ", "doi thu", "category", "vendor", "pricing", "nhà cung cấp", "nha cung cap", "giá", "gia", "vàng", "vang"):
		return "research"
	case containsAny(text, "strategy", "campaign", "funnel", "positioning", "messaging", "chiến lược", "chien luoc"):
		return "strategy"
	case containsAny(text, "content", "copy", "bài viết", "bai viet", "kịch bản", "kich ban"):
		return "content"
	case containsAny(text, "visual", "image", "video", "prompt", "ảnh", "anh"):
		return "visual"
	case containsAny(text, "dev", "code", "debug", "api", "automation", "tracking", "landing", "form", "crm", "sửa lỗi", "sua loi"):
		return "dev"
	case containsAny(text, "qa", "final", "coordination", "điều phối", "dieu phoi", "intake", "review"):
		return "coordination"
	default:
		return "other"
	}
}

func agentKeyFromProfile(profile Profile) string {
	if strings.TrimSpace(profile.AgentKey) != "" {
		return strings.TrimSpace(profile.AgentKey)
	}
	for _, line := range strings.Split(profile.Text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "agent_key:") {
			return strings.TrimSpace(line[len("agent_key:"):])
		}
	}
	return ""
}

func profileAgentKey(profile Profile) string {
	return firstNonEmpty(profile.AgentKey, agentKeyFromProfile(profile), profile.Name)
}

func containsAny(text string, markers ...string) bool {
	for _, marker := range markers {
		if strings.Contains(text, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func requestKindFromTaskType(taskType string, decision Decision) string {
	switch taskType {
	case "other":
		return "unclear"
	}
	if decision == DecisionTeam {
		return "team_work"
	}
	return "self_work"
}

func workflowExecutability(input Input) (bool, string) {
	requiredTool := requiredToolForMode(input.Mode)
	if requiredTool == "" {
		return false, "required_tool_unavailable"
	}
	// Positive-presence policy: the orchestration required tool must actually
	// appear in the current agent's available-tool snapshot. Unknown ABSENCE is
	// not availability — a snapshot that could not confirm the tool (nil list,
	// failed roster load, or a tool from a source that could not be enumerated)
	// fails safe to self. Positive evidence still passes even when an UNRELATED
	// tool source is unknown, because builtins/static-server tools remain listed
	// even when a dynamic MCP server flips the status to unknown.
	if !profileHasAvailableTool(input.CurrentAgent, requiredTool) {
		return false, "required_tool_unavailable"
	}
	switch input.Mode {
	case ModeTeam:
		if input.CoordinatorAgentID == uuid.Nil || strings.TrimSpace(input.CoordinatorAgentKey) == "" {
			return false, "canonical_coordinator_unavailable"
		}
		canonicalMembers := 0
		for _, member := range input.Members {
			if member.AgentID != uuid.Nil && strings.TrimSpace(profileAgentKey(member)) != "" {
				canonicalMembers++
			}
		}
		if canonicalMembers == 0 {
			return false, "insufficient_canonical_members"
		}
		role := strings.ToLower(strings.TrimSpace(input.TeamRole))
		if role == "" || role == "lead" || input.CanAssignTeamTasks {
			return true, ""
		}
		if !input.MemberRequestsEnabled {
			return false, "member_request_path_unavailable"
		}
		return true, ""
	case ModeDelegate:
		for _, delegate := range input.Delegates {
			if delegate.AgentID != uuid.Nil && strings.TrimSpace(profileAgentKey(delegate)) != "" {
				return true, ""
			}
		}
		return false, "insufficient_canonical_members"
	default:
		return false, "workflow_permission_unavailable"
	}
}

func profileHasAvailableTool(profile Profile, required string) bool {
	for _, name := range profile.AvailableTools {
		if strings.EqualFold(strings.TrimSpace(name), required) {
			return true
		}
	}
	return false
}

func workflowHintForInput(input Input) string {
	switch input.Mode {
	case ModeDelegate:
		return "Use the `delegate` tool with an available linked agent. Do not invent agent keys."
	case ModeTeam:
		role := strings.ToLower(strings.TrimSpace(input.TeamRole))
		if role != "" && role != "lead" && !input.CanAssignTeamTasks {
			if input.MemberRequestsEnabled {
				if input.MemberRequestsAutoDispatch {
					return `As a team member, do not create or assign general team tasks. Use team_tasks(action="create", task_type="request", ...) for the canonical coordinator; the backend expands validated workflows without a coordinator LLM turn.`
				}
				return `As a team member, use team_tasks(action="create", task_type="request", ...) for the canonical coordinator. The durable workflow waits for explicit lead approval before expansion.`
			}
			return "As a team member, you cannot create or assign general team tasks, and member request tasks are disabled. Handle the request directly or explain that a lead must coordinate the team work."
		}
		return `Use team_tasks(action="search" or "list") first, then team_tasks(action="create", ...) to assign work to an appropriate team member when new team work is required.`
	default:
		return ""
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

func normalizeArbiterContent(content string) (string, error) {
	raw := strings.TrimSpace(content)
	if strings.HasPrefix(raw, "{") && strings.HasSuffix(raw, "}") {
		if json.Valid([]byte(raw)) {
			return raw, nil
		}
	}
	if strings.HasPrefix(raw, "```") && strings.HasSuffix(raw, "```") {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "```"), "```"))
		lines := strings.Split(inner, "\n")
		if len(lines) > 0 {
			first := strings.ToLower(strings.TrimSpace(lines[0]))
			if first == "json" || first == "javascript" || first == "js" {
				inner = strings.TrimSpace(strings.Join(lines[1:], "\n"))
			}
		}
		if strings.HasPrefix(inner, "{") && strings.HasSuffix(inner, "}") {
			return inner, nil
		}
		return "", fmt.Errorf("fenced arbiter response is not a JSON object")
	}
	if object, ok := firstJSONObject(raw); ok {
		return object, nil
	}
	return "", fmt.Errorf("arbiter response is not a JSON object")
}

func firstJSONObject(raw string) (string, bool) {
	start := strings.IndexByte(raw, '{')
	if start < 0 {
		return "", false
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(raw); i++ {
		switch raw[i] {
		case '\\':
			if inString {
				escaped = !escaped
			}
			continue
		case '"':
			if !escaped {
				inString = !inString
			}
		case '{':
			if !inString {
				depth++
			}
		case '}':
			if !inString {
				depth--
				if depth == 0 {
					object := raw[start : i+1]
					return object, json.Valid([]byte(object))
				}
			}
		}
		escaped = false
	}
	return "", false
}

func validEnum(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
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
