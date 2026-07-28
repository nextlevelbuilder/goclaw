package teamworkclassify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
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
	DefaultCloseMargin     = 0.08
	defaultTeamThreshold   = 0.35
	defaultEvidenceTimeout = 8 * time.Second
	defaultArbiterTimeout  = 30 * time.Second
	defaultPlannerTimeout  = 60 * time.Second
)

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

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
	Embedder                   Embedder
	CloseMargin                float64
	TeamThreshold              float64
	Timeout                    time.Duration
}

type Result struct {
	Decision                 Decision
	Confidence               float64
	Reason                   string
	SelfScore                float64
	CollaborationScore       float64
	Mode                     Mode
	RequiredTool             string
	WorkflowHint             string
	EmbeddingAvailable       bool
	EmbeddingReason          string
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
	StandaloneRequest        string
	IntentRelation           IntentRelation
	IntentInheritedScope     []string
	IntentRequestedOutputs   []string
	StaffingGaps             []string
}

type Evidence struct {
	Available          bool
	Reason             string
	SelfScore          float64
	CollaborationScore float64
}

type workflowExecutabilityError struct {
	reason string
}

func (e *workflowExecutabilityError) Error() string {
	return e.reason
}

func Classify(ctx context.Context, input Input) Result {
	evidence := BuildEmbeddingEvidence(ctx, input)
	return BuildRoleCapabilityFallback(input, evidence, "classifier_parse_failed")
}

func BuildEmbeddingEvidence(ctx context.Context, input Input) Evidence {
	if input.Mode == "" || input.Mode == ModeSpawn {
		return Evidence{Available: false, Reason: "no team or delegate capability"}
	}
	if input.Embedder == nil {
		return Evidence{Available: false, Reason: "embedding unavailable"}
	}
	if strings.TrimSpace(input.Message) == "" {
		return Evidence{Available: false, Reason: "empty message"}
	}

	timeout := input.Timeout
	if timeout <= 0 {
		timeout = defaultEvidenceTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	selfDocs, collaborationDocs := splitProfileDocuments(input)
	if len(selfDocs) == 0 || len(collaborationDocs) == 0 {
		return Evidence{Available: false, Reason: "insufficient collaboration profile"}
	}

	texts := append([]string{routingQueryText(input)}, append(selfDocs, collaborationDocs...)...)
	vectors, err := input.Embedder.Embed(ctx, texts)
	if err != nil || len(vectors) != len(texts) {
		reason := "embedding failed"
		if err != nil {
			reason = "embedding failed: " + err.Error()
		}
		return Evidence{Available: false, Reason: reason}
	}
	query := vectors[0]
	selfScore := bestCosine(query, vectors[1:1+len(selfDocs)])
	collabScore := bestCosine(query, vectors[1+len(selfDocs):])
	return Evidence{
		Available:          true,
		Reason:             "embedding evidence available",
		SelfScore:          selfScore,
		CollaborationScore: collabScore,
	}
}

func ClassifyWithLLM(ctx context.Context, input Input, provider providers.Provider, model string, caps *usagecaps.Service) Result {
	evidence := BuildEmbeddingEvidence(ctx, input)
	if input.Mode == "" || input.Mode == ModeSpawn || provider == nil || strings.TrimSpace(model) == "" {
		return safeSelfResult(input, evidence, "classifier_transport_failed")
	}

	// A configured Timeout sets the arbiter-class budget and SCALES the planner
	// budget by the same built-in ratio (planner gets 2x because it emits a whole
	// plan). Taking input.Timeout literally for BOTH stages would mean that
	// configuring 45s to rescue a slow arbiter silently CUT the planner from 60s
	// to 45s — a setting meant to stop timeouts would cause one.
	timeout, plannerTimeout := stageTimeouts(input.Timeout)

	intent, err := resolveIntent(ctx, timeout, input, provider, model, caps)
	if err != nil {
		return safeSelfResult(input, evidence, intentFailureReason(ctx, err, "intent_resolver"))
	}
	if critiqued, critiqueErr := critiqueIntent(ctx, timeout, input, intent, provider, model, caps); critiqueErr == nil {
		intent = critiqued
	} else {
		var rejected *intentCriticRejectedError
		if errors.As(critiqueErr, &rejected) {
			return attachIntent(safeSelfResult(input, evidence, "intent_critic_rejected"), intent)
		}
		// A transport/timeout/parse/nil failure means the resolved intent could not
		// be independently verified. A required-stage error fails safe to self
		// rather than proceeding on an unverified intent that could spuriously
		// escalate to Team Work; the gate then blocks the orchestration tools.
		return attachIntent(safeSelfResult(input, evidence, intentFailureReason(ctx, critiqueErr, "intent_critic")), intent)
	}
	if intent.NeedsClarification {
		return attachIntent(safeSelfResult(input, evidence, "intent_clarification_required"), intent)
	}
	resolvedInput := input
	resolvedInput.Message = intent.StandaloneRequest
	evidence = BuildEmbeddingEvidence(ctx, resolvedInput)

	// Stage 3: independent, evidence-backed shape verification (runs before
	// decomposition and planning). The verified shape is the SOLE authority for
	// whether the request requires independent review — the decomposition and
	// planner may not manufacture one. A shape-stage failure fails safe to self.
	shape, err := verifyShape(ctx, timeout, resolvedInput, provider, model, caps)
	if err != nil {
		return attachIntent(safeSelfResult(input, evidence, shapeFailureReason(ctx, err)), intent)
	}

	// A verified independent-review requirement forces multi_role with a critic
	// who did not produce the work being reviewed (see validateIndependentReview),
	// which needs at least two distinct non-lead canonical members. When the roster
	// cannot staff that the outcome is a STAFFING gap, not a planner defect: the
	// planner's only legal move is to own a step with the lead, which
	// validateWorkflowPlan then rejects. Reporting it here keeps the audit's
	// degraded reason honest (insufficient_canonical_members -> planning stage)
	// instead of surfacing a generic planner_validation_failed, and skips two
	// LLM stages that cannot succeed. Still fails safe to self.
	if shape.IndependentReviewRequired {
		if executable, reason := workflowRosterExecutability(resolvedInput, shape.WorkShape, shape.IndependentReviewRequired); !executable {
			return attachIntent(degradedVerifiedShapeSelfResult(input, evidence, shape, reason), intent)
		}
	}

	assessmentReq := providers.ChatRequest{
		Messages: BuildWorkAssessmentMessages(resolvedInput),
		Model:    model,
		Options:  map[string]any{providers.OptTemperature: 0.0},
	}
	assessmentResp, err := callClassifierAttempt(ctx, timeout, provider, assessmentReq, model, caps, "team-work-assess")
	if err != nil || assessmentResp == nil {
		return attachIntent(degradedVerifiedShapeSelfResult(input, evidence, shape, callFailureReason(ctx, err, "classifier")), intent)
	}
	assessment, err := ParseWorkAssessment(assessmentResp.Content)
	if err != nil {
		return attachIntent(degradedVerifiedShapeSelfResult(input, evidence, shape, "classifier_parse_failed"), intent)
	}

	planningReq := providers.ChatRequest{
		Messages: BuildPlanningMessages(resolvedInput, evidence, assessment, shape.IndependentReviewRequired),
		Model:    model,
		Options:  map[string]any{providers.OptTemperature: 0.0},
	}
	resp, err := callClassifierAttempt(ctx, plannerTimeout, provider, planningReq, model, caps, "team-work-plan")
	if err != nil || resp == nil {
		return attachIntent(degradedVerifiedShapeSelfResult(input, evidence, shape, callFailureReason(ctx, err, "planner")), intent)
	}
	draft, parseErr := ParseArbiterResult(resp.Content, resolvedInput.Mode)
	var validated Result
	if parseErr != nil {
		revised, revisionErr := revisePlan(ctx, plannerTimeout, resolvedInput, evidence, assessment, shape, provider, model, caps, planningReq, resp.Content, []string{"planner response could not be parsed: " + parseErr.Error()})
		if revisionErr != nil {
			fallback := degradedVerifiedShapeSelfResult(input, evidence, shape, "planner_parse_failed")
			fallback.PlannerValidationReason = parseErr.Error()
			return attachIntent(fallback, intent)
		}
		revised.PlannerRepaired = true
		validated = revised
	} else {
		draft = applyAssessmentToPlan(draft, assessment, shape)
		var validationErr error
		validated, validationErr = validateAssessedResult(resolvedInput, evidence, shape, draft)
		if validationErr != nil {
			revised, revisionErr := revisePlan(ctx, plannerTimeout, resolvedInput, evidence, assessment, shape, provider, model, caps, planningReq, resp.Content, []string{validationErr.Error()})
			if revisionErr != nil {
				fallback := degradedVerifiedShapeSelfResult(input, evidence, shape, "planner_validation_failed")
				fallback.PlannerValidationReason = validationErr.Error()
				return attachIntent(fallback, intent)
			}
			revised.PlannerRepaired = true
			validated = revised
		}
	}

	critique, critiqueErr := critiqueAssignment(ctx, timeout, resolvedInput, assessment, validated, provider, model, caps)
	if critiqueErr != nil {
		// The independent assignment critic could not run (transport/timeout/parse/
		// nil). A required verification stage that could not run IS a degradation
		// event regardless of the decision it was verifying, so fail safe to a
		// degraded self and let the gate block the orchestration tools. This holds
		// even when the decision already resolved to self: recording an un-run critic
		// as "accepted" would understate the degradation rate the audit exists to
		// measure (§1.3.7). forceDegradedSelf keeps DecisionSelf /
		// EffectiveWorkflowMode=self / no plan / no required tool and stamps
		// DegradedWorkflow + the transport-vs-parse reason code, which the audit
		// projection (degradedStageFromReason) maps to the assignment_critic stage.
		return attachIntent(forceDegradedSelf(validated, assignmentCriticFailureReason(ctx, critiqueErr)), intent)
	}
	if critique.Valid {
		validated.PlannerValidationReason = "accepted backend-valid canonical assignment"
		return attachIntent(validated, intent)
	}
	if len(critique.Issues) == 0 {
		fallback := degradedVerifiedShapeSelfResult(input, evidence, shape, "assignment_critic_rejected")
		fallback.PlannerValidationReason = "assignment critic rejected without actionable issues"
		return attachIntent(fallback, intent)
	}
	revised, revisionErr := revisePlan(ctx, plannerTimeout, resolvedInput, evidence, assessment, shape, provider, model, caps, planningReq, resp.Content, critique.Issues)
	if revisionErr == nil {
		revised.PlannerRepaired = true
		revised.PlannerValidationReason = "accepted assignment revised from independent critique"
		return attachIntent(revised, intent)
	}
	fallback := degradedVerifiedShapeSelfResult(input, evidence, shape, "assignment_revision_failed")
	fallback.PlannerValidationReason = revisionErr.Error()
	return attachIntent(fallback, intent)
}

type IntentRelation string

const (
	IntentRelationNew          IntentRelation = "new"
	IntentRelationContinuation IntentRelation = "continuation"
	IntentRelationRefinement   IntentRelation = "refinement"
	IntentRelationCorrection   IntentRelation = "correction"
)

type IntentResolution struct {
	StandaloneRequest     string         `json:"standalone_request"`
	Relation              IntentRelation `json:"relation"`
	UserIntent            string         `json:"user_intent"`
	InheritedScope        []string       `json:"inherited_scope"`
	RequestedDeliverables []string       `json:"requested_deliverables"`
	QualityRequirements   []string       `json:"quality_requirements"`
	ExplicitConstraints   []string       `json:"explicit_constraints"`
	Ambiguities           []string       `json:"ambiguities"`
	NeedsClarification    bool           `json:"needs_clarification"`
}

type IntentCritique struct {
	Valid      bool              `json:"valid"`
	Issues     []string          `json:"issues"`
	Correction *IntentResolution `json:"corrected_resolution"`
}

type intentCriticRejectedError struct {
	reason string
}

func (e *intentCriticRejectedError) Error() string { return e.reason }

func resolveIntent(ctx context.Context, timeout time.Duration, input Input, provider providers.Provider, model string, caps *usagecaps.Service) (IntentResolution, error) {
	req := providers.ChatRequest{
		Model:    model,
		Options:  map[string]any{providers.OptTemperature: 0.0},
		Messages: BuildIntentResolutionMessages(input),
	}
	resp, err := callClassifierAttempt(ctx, timeout, provider, req, model, caps, "team-work-intent-resolve")
	if err != nil || resp == nil {
		return IntentResolution{}, err
	}
	return ParseIntentResolution(resp.Content)
}

func BuildIntentResolutionMessages(input Input) []providers.Message {
	system := `Resolve the current user message into a complete standalone request before any routing decision.
Use conversation context only to resolve references, omitted scope, corrections, refinements, and continuation requirements. Preserve the user's actual intent; do not add work they did not request. Do not select an agent, workflow mode, tools, or plan.
Set needs_clarification=true only when a missing fact makes useful execution impossible or materially unsafe. Broad research scope, relative wording, unspecified presentation details, or choices that can be handled with explicit reasonable assumptions are not blockers: record them in ambiguities and continue with needs_clarification=false. A request to research, compare, evaluate, recommend, or independently review is executable unless the subject itself cannot be identified.
Return ONLY JSON: {"standalone_request":"complete request","relation":"new|continuation|refinement|correction","user_intent":"short intent","inherited_scope":["item"],"requested_deliverables":["item"],"quality_requirements":["item"],"explicit_constraints":["item"],"ambiguities":["item"],"needs_clarification":false}.`
	payload := map[string]any{
		"current_user_message": input.Message,
		"conversation_context": input.RecentContext,
	}
	raw, _ := json.Marshal(payload)
	return []providers.Message{{Role: "system", Content: system}, {Role: "user", Content: string(raw)}}
}

func ParseIntentResolution(content string) (IntentResolution, error) {
	raw, err := normalizeArbiterContent(content)
	if err != nil {
		return IntentResolution{}, err
	}
	var resolution IntentResolution
	if err := json.Unmarshal([]byte(raw), &resolution); err != nil {
		return IntentResolution{}, err
	}
	resolution.StandaloneRequest = strings.TrimSpace(resolution.StandaloneRequest)
	resolution.UserIntent = strings.TrimSpace(resolution.UserIntent)
	resolution.Relation = IntentRelation(strings.ToLower(strings.TrimSpace(string(resolution.Relation))))
	resolution.InheritedScope = compactSortedStrings(resolution.InheritedScope)
	resolution.RequestedDeliverables = compactSortedStrings(resolution.RequestedDeliverables)
	resolution.QualityRequirements = compactSortedStrings(resolution.QualityRequirements)
	resolution.ExplicitConstraints = compactSortedStrings(resolution.ExplicitConstraints)
	resolution.Ambiguities = compactSortedStrings(resolution.Ambiguities)
	if resolution.StandaloneRequest == "" || len([]rune(resolution.StandaloneRequest)) > 12000 {
		return IntentResolution{}, fmt.Errorf("standalone_request is missing or too long")
	}
	if resolution.UserIntent == "" {
		return IntentResolution{}, fmt.Errorf("user_intent is required")
	}
	if !validEnum(string(resolution.Relation), string(IntentRelationNew), string(IntentRelationContinuation), string(IntentRelationRefinement), string(IntentRelationCorrection)) {
		return IntentResolution{}, fmt.Errorf("invalid intent relation %q", resolution.Relation)
	}
	if len(resolution.InheritedScope) > 32 || len(resolution.RequestedDeliverables) > 32 || len(resolution.QualityRequirements) > 32 || len(resolution.ExplicitConstraints) > 32 || len(resolution.Ambiguities) > 32 {
		return IntentResolution{}, fmt.Errorf("intent resolution exceeds list limits")
	}
	return resolution, nil
}

func critiqueIntent(ctx context.Context, timeout time.Duration, input Input, draft IntentResolution, provider providers.Provider, model string, caps *usagecaps.Service) (IntentResolution, error) {
	payload := map[string]any{
		"current_user_message": input.Message,
		"conversation_context": input.RecentContext,
		"draft_resolution":     draft,
	}
	raw, _ := json.Marshal(payload)
	req := providers.ChatRequest{
		Model:   model,
		Options: map[string]any{providers.OptTemperature: 0.0},
		Messages: []providers.Message{
			{Role: "system", Content: `Independently verify that the draft standalone request preserves the user's real meaning. Check references, omitted scope, time periods, correction versus continuation, requested deliverables, and accidental invention. Do not route or select agents. needs_clarification is true only when a missing fact makes useful execution impossible or materially unsafe; broad research scope and details that can be handled with explicit reasonable assumptions are not blockers. Return ONLY JSON: {"valid":true,"issues":[],"corrected_resolution":null}. When invalid, corrected_resolution must contain the complete corrected intent-resolution object.`},
			{Role: "user", Content: string(raw)},
		},
	}
	resp, err := callClassifierAttempt(ctx, timeout, provider, req, model, caps, "team-work-intent-critic")
	if err != nil || resp == nil {
		return draft, err
	}
	content, err := normalizeArbiterContent(resp.Content)
	if err != nil {
		return draft, err
	}
	var critique IntentCritique
	if err := json.Unmarshal([]byte(content), &critique); err != nil {
		return draft, err
	}
	if critique.Valid {
		return draft, nil
	}
	if critique.Correction == nil {
		return draft, &intentCriticRejectedError{reason: "intent critic rejected draft without correction"}
	}
	correctedRaw, _ := json.Marshal(critique.Correction)
	return ParseIntentResolution(string(correctedRaw))
}

func intentFailureReason(parent context.Context, err error, prefix string) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return callFailureReason(parent, err, prefix)
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) || strings.Contains(strings.ToLower(err.Error()), "json") || strings.Contains(strings.ToLower(err.Error()), "standalone_request") || strings.Contains(strings.ToLower(err.Error()), "intent relation") {
		return prefix + "_parse_failed"
	}
	return callFailureReason(parent, err, prefix)
}

// verifyShape runs the independent shape verifier as a live classification stage.
// It is the evidence-backed authority for whether the request requires independent
// review: ValidateShapeAssessment rejects any trait whose quoted evidence is not
// literally present in the resolved request or a pinned skill, so the model cannot
// invent a review requirement. A verified shape with independent_verification or
// explicit_critique is the only hard signal that forces a multi_role reviewer
// chain; every other trait is descriptive and leaves staffing to the planner.
func verifyShape(ctx context.Context, timeout time.Duration, input Input, provider providers.Provider, model string, caps *usagecaps.Service) (ShapeAssessment, error) {
	req := providers.ChatRequest{
		Model:    model,
		Options:  map[string]any{providers.OptTemperature: 0.0},
		Messages: BuildShapeVerifierMessages(input),
	}
	resp, err := callClassifierAttempt(ctx, timeout, provider, req, model, caps, "team-work-shape-verify")
	if err != nil {
		return ShapeAssessment{}, err
	}
	if resp == nil {
		return ShapeAssessment{}, fmt.Errorf("shape verifier returned no response")
	}
	assessment, contractErr := parseAndValidateShape(input, resp.Content)
	if contractErr == nil {
		return assessment, nil
	}
	// One bounded repair attempt, mirroring the planner's repair stage. The
	// classifier runs on each agent's OWN runtime model, so a model that wraps
	// its JSON in prose or paraphrases the quoted evidence would otherwise
	// collapse the entire classification to a degraded self even when the
	// request is plainly team-shaped (observed live on two agent models while a
	// third parsed cleanly). A repair failure returns the ORIGINAL contract
	// error so shapeFailureReason still reports parse-vs-transport truthfully
	// and the caller still fails safe to self.
	repairReq := req
	repairReq.Messages = BuildShapeRepairMessages(input, resp.Content, contractErr)
	repairResp, repairErr := callClassifierAttempt(ctx, timeout, provider, repairReq, model, caps, "team-work-shape-verify-repair")
	if repairErr != nil || repairResp == nil {
		return ShapeAssessment{}, contractErr
	}
	repaired, repairContractErr := parseAndValidateShape(input, repairResp.Content)
	if repairContractErr != nil {
		return ShapeAssessment{}, contractErr
	}
	return repaired, nil
}

// parseAndValidateShape applies the full shape contract (JSON envelope, trait
// vocabulary, quoted-evidence presence, derived shape/review agreement) to one
// verifier reply.
func parseAndValidateShape(input Input, content string) (ShapeAssessment, error) {
	parsed, err := ParseShapeAssessment(content)
	if err != nil {
		return ShapeAssessment{}, err
	}
	return ValidateShapeAssessment(input, parsed)
}

// shapeFailureReason classifies a shape-stage failure into a degraded reason
// code, consistent with intentFailureReason/assignmentCriticFailureReason so the
// audit's degradation-rate metric distinguishes transport from parse. A cancelled
// parent or deadline is transport/timeout; a JSON/trait/evidence/shape-validation
// error is a parse failure; a nil response or plain provider error is transport.
// Any of these fails safe to self.
func shapeFailureReason(parent context.Context, err error) string {
	if errors.Is(err, context.DeadlineExceeded) || parent.Err() != nil {
		return callFailureReason(parent, err, "shape_verifier")
	}
	if err != nil {
		msg := strings.ToLower(err.Error())
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) || strings.Contains(msg, "json") || strings.Contains(msg, "trait") || strings.Contains(msg, "evidence") || strings.Contains(msg, "work_shape") || strings.Contains(msg, "independent_review") {
			return "shape_verifier_parse_failed"
		}
	}
	return "shape_verifier_transport_failed"
}

// assignmentCriticFailureReason classifies an assignment-critic failure into a
// degraded reason code, mirroring intentFailureReason so a genuine provider/
// transport error reads as transport while an unparseable critique reads as
// parse. A cancelled parent or deadline is transport/timeout. All map to the
// assignment_critic audit stage and fail safe to self for a team decision that
// could not be independently critiqued.
func assignmentCriticFailureReason(parent context.Context, err error) string {
	if errors.Is(err, context.DeadlineExceeded) || parent.Err() != nil {
		return callFailureReason(parent, err, "assignment_critic")
	}
	if err != nil {
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) || strings.Contains(strings.ToLower(err.Error()), "json") || strings.Contains(strings.ToLower(err.Error()), "invalid") {
			return "assignment_critic_parse_failed"
		}
	}
	return "assignment_critic_transport_failed"
}

func attachIntent(result Result, intent IntentResolution) Result {
	result.StandaloneRequest = intent.StandaloneRequest
	result.IntentRelation = intent.Relation
	result.IntentInheritedScope = append([]string(nil), intent.InheritedScope...)
	result.IntentRequestedOutputs = append([]string(nil), intent.RequestedDeliverables...)
	return result
}

type WorkAssessment struct {
	WorkflowMode              WorkflowMode     `json:"workflow_mode"`
	IndependentReviewRequired bool             `json:"independent_review_required"`
	Reason                    string           `json:"reason"`
	WorkUnits                 []WorkUnit       `json:"work_units"`
	Dependencies              []WorkDependency `json:"dependencies"`
	RequiredOutputs           []string         `json:"required_outputs"`
}

type WorkUnit struct {
	ID             string `json:"id"`
	Description    string `json:"description"`
	RequiredOutput string `json:"required_output"`
}

type WorkDependency struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type PlanCritique struct {
	Valid  bool     `json:"valid"`
	Issues []string `json:"issues"`
}

func acceptedSelfResult(input Input, evidence Evidence, reason string) Result {
	result := safeSelfResult(input, evidence, reason)
	result.DegradedWorkflow = false
	result.DegradedReasonCode = ""
	result.ValidatorReason = "accepted self assessment"
	result.CurrentAgentRole = input.TeamRole
	return result
}

// applyAssessmentToPlan records the decomposition's advisory mode and stamps the
// independently VERIFIED shape onto the result. Review requirement comes from the
// verified shape (shape.IndependentReviewRequired), never from the decomposition's
// self-declared mode — the decomposition may suggest work units but must not lock
// staffing or manufacture a reviewer requirement.
func applyAssessmentToPlan(result Result, assessment WorkAssessment, shape ShapeAssessment) Result {
	result.RequestedWorkflowMode = assessment.WorkflowMode
	result.VerifiedWorkShape = shape.WorkShape
	result.EffectiveWorkShape = shape.WorkShape
	result.ShapeTraits = append([]ShapeTrait(nil), shape.ShapeTraits...)
	result.RequestedReviewRequired = assessment.IndependentReviewRequired || result.RequestedReviewRequired
	result.EffectiveReviewRequired = shape.IndependentReviewRequired
	return result
}

func BuildWorkAssessmentMessages(input Input) []providers.Message {
	system := `You decompose one already-resolved standalone user request by its semantic work requirements. Do not select an agent and do not use roster availability to make the work look simpler.
Return ONLY JSON: {"workflow_mode":"self|single_owner|multi_role","independent_review_required":true,"reason":"short reason","work_units":[{"id":"unit-id","description":"required work","required_output":"concrete output"}],"dependencies":[{"from":"unit-id","to":"later-unit-id"}],"required_outputs":["final deliverable"]}.
self means one bounded work unit with no specialist ownership requirement.
single_owner means one coherent specialist work unit; the later assignment stage decides whether the current agent or one teammate owns it.
multi_role means multiple work units, dependent outputs, broad research, comparison, recommendation, independent verification, or critique are needed.
Any request requiring independent review must be multi_role. Do not select an agent or create a plan.`
	payload := map[string]any{
		"standalone_request": input.Message,
		"pinned_skills":      input.PinnedSkillsContext,
	}
	raw, _ := json.Marshal(payload)
	return []providers.Message{{Role: "system", Content: system}, {Role: "user", Content: string(raw)}}
}

func ParseWorkAssessment(content string) (WorkAssessment, error) {
	raw, err := normalizeArbiterContent(content)
	if err != nil {
		return WorkAssessment{}, err
	}
	var assessment WorkAssessment
	if err := json.Unmarshal([]byte(raw), &assessment); err != nil {
		return WorkAssessment{}, err
	}
	assessment.WorkflowMode = WorkflowMode(strings.ToLower(strings.TrimSpace(string(assessment.WorkflowMode))))
	assessment.Reason = strings.TrimSpace(assessment.Reason)
	if !validEnum(string(assessment.WorkflowMode), string(WorkflowModeSelf), string(WorkflowModeSingleOwner), string(WorkflowModeMultiRole)) {
		return WorkAssessment{}, fmt.Errorf("invalid assessed workflow_mode %q", assessment.WorkflowMode)
	}
	if assessment.Reason == "" {
		return WorkAssessment{}, fmt.Errorf("assessment reason is required")
	}
	if assessment.IndependentReviewRequired && assessment.WorkflowMode != WorkflowModeMultiRole {
		assessment.WorkflowMode = WorkflowModeMultiRole
	}
	if len(assessment.WorkUnits) == 0 || len(assessment.WorkUnits) > MaxWorkflowSteps {
		return WorkAssessment{}, fmt.Errorf("work_units must contain between 1 and %d items", MaxWorkflowSteps)
	}
	unitIDs := make(map[string]struct{}, len(assessment.WorkUnits))
	for i := range assessment.WorkUnits {
		unit := &assessment.WorkUnits[i]
		unit.ID = strings.TrimSpace(unit.ID)
		unit.Description = strings.TrimSpace(unit.Description)
		unit.RequiredOutput = strings.TrimSpace(unit.RequiredOutput)
		if unit.ID == "" || unit.Description == "" || unit.RequiredOutput == "" {
			return WorkAssessment{}, fmt.Errorf("work unit %d is incomplete", i)
		}
		if _, exists := unitIDs[unit.ID]; exists {
			return WorkAssessment{}, fmt.Errorf("duplicate work unit %q", unit.ID)
		}
		unitIDs[unit.ID] = struct{}{}
	}
	for _, dependency := range assessment.Dependencies {
		if _, ok := unitIDs[strings.TrimSpace(dependency.From)]; !ok {
			return WorkAssessment{}, fmt.Errorf("unknown work dependency source %q", dependency.From)
		}
		if _, ok := unitIDs[strings.TrimSpace(dependency.To)]; !ok {
			return WorkAssessment{}, fmt.Errorf("unknown work dependency target %q", dependency.To)
		}
	}
	assessment.RequiredOutputs = compactSortedStrings(assessment.RequiredOutputs)
	if len(assessment.RequiredOutputs) == 0 {
		return WorkAssessment{}, fmt.Errorf("required_outputs is required")
	}
	return assessment, nil
}

func BuildPlanningMessages(input Input, evidence Evidence, assessment WorkAssessment, reviewRequired bool) []providers.Message {
	system := fmt.Sprintf(`You select canonical owners and, when needed, build a team workflow plan.
An advisory semantic decomposition suggested workflow_mode=%s. An independent evidence-backed shape verifier determined independent_review_required=%t.
The advisory mode is a hint only: if one canonical owner can competently produce the requested outputs, choose self or single_owner even when the decomposition suggested multi_role. For semantic self or single_owner work, choose self when the current agent is the best owner, otherwise choose one canonical teammate. Only a verified independent_review_required=true is a hard constraint that cannot be downgraded — it forces multi_role with a distinct reviewer.
Return ONLY JSON using this schema:
{"workflow_mode":"self|single_owner|multi_role","current_agent_role":"string","task_type":"dev|research|strategy|analytics|content|visual|ops|qa|coordination|other","current_agent_fit":"strong|partial|weak","best_team_owner":"canonical-key-or-empty","best_team_owner_role":"string-or-empty","best_team_fit":"none|partial|strong","specialist_match_found":true,"lead_selected_as_fallback":false,"routing_priority_used":"role_task_match|explicit_user_target|coordination|no_specialist|workflow_unavailable","owner_selection_reason":"short reason","followup_context_used_for_reference_only":true,"workflow_executable":true,"staffing_gaps":[],"decision":"self|team","required_tool":"team_tasks|delegate|","reason":"short reason","plan":null}
For self or single_owner, plan must be null. For multi_role, decision must be "team", required_tool must be "team_tasks", workflow_executable must be true, and plan must replace null with exactly this typed shape:
{"schema_version":1,"goal":"canonical goal","coordinator_agent_id":"canonical lead UUID","coordinator_agent_key":"canonical lead key","final_owner_agent_id":"canonical integrator UUID","final_owner_agent_key":"canonical integrator key","review_status":"none|required|included","terminal_step_id":"terminal-step-id","steps":[{"id":"stable-step-id","title":"short title","instruction":"bounded instruction including required input and output","owner_agent_id":"canonical non-lead UUID","owner_agent_key":"canonical non-lead key","capability_key":"free-text capability","capability_label":"free-text label","workflow_role":"draft|work|critic|integration","required_tools":["known available canonical tool"],"depends_on":["predecessor-step-id"],"required_output":true,"terminal":false}]}
All UUID fields must be copied exactly from the supplied canonical roster. required_output and terminal are JSON booleans, not descriptions. required_tools and depends_on are JSON string arrays, including empty arrays when applicable.
HARD CONSTRAINT: the coordinator (team lead, given as coordinator_id/coordinator_key) coordinates and audits only and must NEVER own an executable step — not a work step, not the review step, and NOT the terminal integration step. Every step owner, including final_owner_agent_id, must come from step_owner_candidates. The coordinator appears in members for context only; picking it for any step makes the whole plan invalid and the work is discarded.
Assign workflow_role for every step. Size and shape the plan to the work and to the roster you are given: split it into as many bounded steps as the request genuinely needs (up to %d steps across up to %d distinct non-lead agents), give each step to the agent whose profile fits that step, and use parallel branches when steps do not depend on each other. Do not collapse genuinely distinct work into one step, and do not invent steps the request does not need.
When independent_review_required=true, choose the critic owner from reviewer_candidates by evaluating the complete profiles and expertise summaries. The critic must review work produced by a DIFFERENT agent, and the critic step must feed the terminal step so its findings cannot be ignored. Any number of steps may sit between the reviewed work and the critic, and between the critic and the terminal step. The agent who produces the reviewed work, the critic, and the agent who owns the terminal integration step may be three different agents. More than one critic is allowed when different aspects need independent review. Do not omit or substitute the critic step.
If the roster genuinely lacks a suitable specialist for a required work unit or lacks a distinct suitable reviewer, do not assign a weak substitute. Return decision="self", workflow_mode="self", workflow_executable=false, plan=null, and staffing_gaps containing the unfilled work-unit IDs and reasons. This reports an execution gap; it does not change the independent semantic assessment.
Use the complete roster descriptions, frontmatter, roles, tools, workload evidence, current request, attachments/context, and advisory pinned skills. Evaluate transferable role competence together with available research/execution tools: a research or analyst profile may investigate a new subject or geography, a strategy profile may synthesize scenarios and recommendations, and a suitable independent analyst/reviewer may critique the evidence. Do not require the profile prose to already name the exact industry, country, regulation, or future period. Do not require structured capabilities, and do not treat unknown structured capabilities as evidence that an agent is unsuitable. Report a staffing gap only when no profile has a transferable role fit or a hard required permission/tool is unavailable, not merely because prior domain wording or credentials are absent. The lead coordinates and does not own executable steps.`, assessment.WorkflowMode, reviewRequired, MaxWorkflowSteps, MaxWorkflowAgents)
	payload := map[string]any{
		"standalone_request":  input.Message,
		"recent_context":      input.RecentContext,
		"pinned_skills":       input.PinnedSkillsContext,
		"current_agent":       input.CurrentAgent,
		"team":                input.Team,
		"team_role":           input.TeamRole,
		"coordinator_id":      input.CoordinatorAgentID,
		"coordinator_key":     input.CoordinatorAgentKey,
		"members":             input.Members,
		"delegates":           input.Delegates,
		"tool_allow":          input.ToolAllow,
		"reviewer_candidates": reviewerCandidateRefs(input),
		// step_owner_candidates is the EXPLICIT allow-list of agents that may own a
		// step. members necessarily includes the coordinator (the planner needs the
		// team's full context), and a model reading a roster that contains the lead
		// will hand the lead the integration step — observed twice in a row live,
		// each time rejected with `step "final-integration" cannot be owned by the
		// team lead coordinator`, discarding an otherwise valid plan. Stating the
		// eligible set positively is far more reliable than expecting the model to
		// subtract the coordinator from members itself.
		"step_owner_candidates": stepOwnerCandidateRefs(input),
		"assessment":            assessment,
		"embedding":             evidence,
	}
	raw, _ := json.Marshal(payload)
	return []providers.Message{{Role: "system", Content: system}, {Role: "user", Content: string(raw)}}
}

// stepOwnerCandidateRefs lists every canonical agent that may own a workflow
// step: team members with a usable identity, minus the coordinator. This mirrors
// exactly what validateWorkflowPlan enforces (canonicalTeamProfile plus the
// lead-exclusion check), so the planner is told the same rule it will be judged by.
func stepOwnerCandidateRefs(input Input) []map[string]string {
	result := make([]map[string]string, 0, len(input.Members))
	for _, profile := range input.Members {
		if profile.AgentID == uuid.Nil || profile.AgentID == input.CoordinatorAgentID {
			continue
		}
		if strings.TrimSpace(profileAgentKey(profile)) == "" {
			continue
		}
		result = append(result, map[string]string{
			"agent_id":  profile.AgentID.String(),
			"agent_key": profileAgentKey(profile),
			"team_role": profile.TeamRole,
		})
	}
	return result
}

func reviewerCandidateRefs(input Input) []map[string]string {
	profiles := append(append([]Profile(nil), input.Members...), input.Delegates...)
	result := make([]map[string]string, 0, len(profiles))
	for _, profile := range profiles {
		if profile.AgentID == uuid.Nil || profile.AgentID == input.CoordinatorAgentID {
			continue
		}
		result = append(result, map[string]string{"agent_id": profile.AgentID.String(), "agent_key": profile.AgentKey})
	}
	return result
}

func critiqueAssignment(ctx context.Context, timeout time.Duration, input Input, assessment WorkAssessment, assignment Result, provider providers.Provider, model string, caps *usagecaps.Service) (PlanCritique, error) {
	payload := map[string]any{
		"standalone_request": input.Message,
		"pinned_skills":      input.PinnedSkillsContext,
		"assessment":         assessment,
		"current_agent":      input.CurrentAgent,
		"coordinator_id":     input.CoordinatorAgentID,
		"coordinator_key":    input.CoordinatorAgentKey,
		"members":            input.Members,
		"delegates":          input.Delegates,
		"assignment":         assignment,
	}
	raw, _ := json.Marshal(payload)
	req := providers.ChatRequest{
		Model:   model,
		Options: map[string]any{providers.OptTemperature: 0.0},
		Messages: []providers.Message{
			{Role: "system", Content: `You independently critique a proposed execution assignment. Check standalone-request coverage, whether self versus teammate ownership is justified by the complete profiles, and for workflows check every step owner, independent review, data flow, required outputs, and terminal convergence. When staffing_gaps is non-empty, independently reassess every canonical candidate from role, description, frontmatter, expertise summary, tools, and permissions. Evaluate transferable role competence: researchers and analysts can investigate unfamiliar domains with available research tools; strategists can synthesize scenarios and recommendations; analysts or reviewers can independently challenge evidence. A profile need not already name the exact subject, country, law, or forecast period. Missing domain wording, credentials not requested by the user, or unknown structured capabilities never prove a staffing gap. Reject the gap when any candidate can reasonably perform the work through research, analysis, strategy, or review, and identify the exact work unit plus canonical agent UUID/key. Accept a gap only for a genuinely missing transferable role or hard permission/tool requirement. Do not authorize identities or rewrite the assignment. Return ONLY JSON: {"valid":true,"issues":["specific issue"]}.`},
			{Role: "user", Content: string(raw)},
		},
	}
	resp, err := callClassifierAttempt(ctx, timeout, provider, req, model, caps, "team-work-assignment-critic")
	if err != nil || resp == nil {
		return PlanCritique{}, err
	}
	content, err := normalizeArbiterContent(resp.Content)
	if err != nil {
		return PlanCritique{}, err
	}
	var critique PlanCritique
	if err := json.Unmarshal([]byte(content), &critique); err != nil {
		return PlanCritique{}, err
	}
	critique.Issues = compactSortedStrings(critique.Issues)
	return critique, nil
}

func revisePlan(ctx context.Context, timeout time.Duration, input Input, evidence Evidence, assessment WorkAssessment, shape ShapeAssessment, provider providers.Provider, model string, caps *usagecaps.Service, planningReq providers.ChatRequest, previous string, issues []string) (Result, error) {
	revisionReq := planningReq
	issueJSON, _ := json.Marshal(issues)
	revisionReq.Messages = append(append([]providers.Message(nil), planningReq.Messages...),
		providers.Message{Role: "assistant", Content: previous},
		providers.Message{Role: "system", Content: "Revise the previous JSON assignment to address these concrete issues: " + string(issueJSON) + `. decision and workflow_mode must agree in every response: workflow_mode="self" requires decision="self", and workflow_mode="single_owner" or "multi_role" requires decision="team". Never pair decision="self" with a non-self workflow_mode — that contradiction invalidates the whole revision and the work is discarded. ` + "You may upgrade an under-classified self or single_owner assessment to multi_role; semantic multi_role and required review cannot be downgraded. Do not introduce staffing_gaps as an escape from repairing owner, reviewer, workflow_role, dependency, tool, or terminal errors. A new staffing gap is allowed only when the previous JSON already reported that same gap and the critique explicitly confirms no canonical candidate is reasonably suitable. Otherwise assign canonical UUID/key pairs from the supplied roster and repair the DAG. If an issue says a step cannot be owned by the team lead coordinator, reassign that step — including the terminal integration step and final_owner_agent_id — to an agent from step_owner_candidates; the coordinator can never own any step. Return one corrected JSON object with no commentary."},
	)
	resp, err := callClassifierAttempt(ctx, timeout, provider, revisionReq, model, caps, "team-work-plan-revision")
	if err != nil || resp == nil {
		return Result{}, err
	}
	revised, err := ParseArbiterResult(resp.Content, input.Mode)
	if err != nil {
		return Result{}, err
	}
	revised = applyAssessmentToPlan(revised, assessment, shape)
	validated, err := validateAssessedResult(input, evidence, shape, revised)
	if err != nil {
		return Result{}, err
	}
	return validated, nil
}

// degradedVerifiedShapeSelfResult fails safe to self while PRESERVING the shape
// the verifier already established. Use it for EVERY degradation that happens
// after shape verification succeeded: the pre-planning staffing check, the
// decomposition stage, and every planner parse/validation/critic failure.
//
// Plain safeSelfResult zeroes the shape fields, so the audit row records an
// empty verified_shape even though the verifier ran and succeeded. That makes a
// late failure indistinguishable from a shape-stage failure and hides which
// shapes actually reach the planner — the exact measurement the audit table
// exists for. degraded_stage alone is not enough: it says where the pipeline
// stopped, not what the request was judged to be.
func degradedVerifiedShapeSelfResult(input Input, evidence Evidence, shape ShapeAssessment, reason string) Result {
	fallback := safeSelfResult(input, evidence, reason)
	fallback.VerifiedWorkShape = shape.WorkShape
	fallback.EffectiveWorkShape = shape.WorkShape
	fallback.EffectiveReviewRequired = shape.IndependentReviewRequired
	fallback.ShapeTraits = append([]ShapeTrait(nil), shape.ShapeTraits...)
	return fallback
}

func degradedShapeSelfResult(input Input, evidence Evidence, result Result, verifiedShape WorkShape, reason string) Result {
	fallback := safeSelfResult(input, evidence, reason)
	fallback.RequestedWorkShape = result.RequestedWorkShape
	fallback.RequestedWorkflowMode = result.RequestedWorkflowMode
	fallback.RequestedReviewRequired = result.RequestedReviewRequired
	fallback.VerifiedWorkShape = verifiedShape
	fallback.EffectiveWorkShape = result.EffectiveWorkShape
	fallback.EffectiveReviewRequired = result.EffectiveReviewRequired
	fallback.ShapeTraits = append([]ShapeTrait(nil), result.ShapeTraits...)
	return fallback
}

func repairClassification(ctx context.Context, timeout time.Duration, input Input, evidence Evidence, provider providers.Provider, model string, caps *usagecaps.Service, baseReq providers.ChatRequest, previous string, validationErr error, requiredShape WorkShape) (Result, error) {
	repairReq := baseReq
	repairReq.Messages = BuildPlannerRepairMessages(input, evidence, previous, validationErr, requiredShape)
	repairedResp, err := callClassifierAttempt(ctx, timeout, provider, repairReq, model, caps, "team-work-planner-repair")
	if err != nil || repairedResp == nil {
		return Result{}, fmt.Errorf("planner repair call failed: %w", err)
	}
	repaired, err := ParseArbiterResult(repairedResp.Content, input.Mode)
	if err != nil {
		return Result{}, fmt.Errorf("planner repair parse failed: %w", err)
	}
	assessment, err := ValidateShapeAssessment(input, ShapeAssessment{WorkShape: repaired.RequestedWorkShape, ShapeTraits: repaired.ShapeTraits, IndependentReviewRequired: repaired.RequestedReviewRequired})
	if err != nil {
		return Result{}, fmt.Errorf("planner repair shape validation failed: %w", err)
	}
	if requiredShape != "" && shapeRank(assessment.WorkShape) < shapeRank(requiredShape) {
		return Result{}, fmt.Errorf("planner repair work_shape %q is weaker than required %q", assessment.WorkShape, requiredShape)
	}
	repaired.EffectiveWorkShape = assessment.WorkShape
	repaired.EffectiveReviewRequired = EffectiveReviewRequired(assessment.WorkShape, assessment.ShapeTraits)
	if executable, reason := workflowRosterExecutability(input, repaired.EffectiveWorkShape, repaired.EffectiveReviewRequired); !executable {
		return Result{}, &workflowExecutabilityError{reason: reason}
	}
	validated, err := validateEffectiveResult(input, evidence, repaired)
	if err != nil {
		return Result{}, fmt.Errorf("planner repair validation failed: %w", err)
	}
	validated.PlannerRepaired = true
	validated.PlannerValidationReason = "accepted repaired canonical multi-role plan"
	return validated, nil
}

func isParseableMultiRoleEnvelope(content string) bool {
	raw, err := normalizeArbiterContent(content)
	if err != nil {
		return false
	}
	var envelope struct {
		WorkflowMode string `json:"workflow_mode"`
	}
	return json.Unmarshal([]byte(raw), &envelope) == nil && strings.EqualFold(strings.TrimSpace(envelope.WorkflowMode), string(WorkflowModeMultiRole))
}

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

// stageTimeouts derives the arbiter-class and planner-class deadlines from an
// optional configured per-stage timeout. Non-positive input keeps the built-in
// defaults. A configured value becomes the arbiter budget and the planner budget
// is scaled by the same default ratio, so the planner keeps its larger share at
// every configured setting instead of being silently clamped down to the arbiter
// value. Callers that are planner-class only (replan) use plannerStageTimeout.
func stageTimeouts(configured time.Duration) (arbiter, planner time.Duration) {
	if configured <= 0 {
		return defaultArbiterTimeout, defaultPlannerTimeout
	}
	return configured, scalePlannerTimeout(configured)
}

// plannerStageTimeout returns the planner-class deadline for a configured
// per-stage timeout, keeping the built-in default when unset.
func plannerStageTimeout(configured time.Duration) time.Duration {
	if configured <= 0 {
		return defaultPlannerTimeout
	}
	return scalePlannerTimeout(configured)
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

func safeSelfResult(input Input, evidence Evidence, reason string) Result {
	return Result{
		Decision: DecisionSelf, DecisionBeforeValidation: DecisionSelf, Mode: input.Mode,
		Reason: reason, ValidatorReason: reason, DegradedWorkflow: true, DegradedReasonCode: reason,
		WorkflowMode: WorkflowModeSelf, RequestedWorkflowMode: WorkflowModeSelf, EffectiveWorkflowMode: WorkflowModeSelf,
		CurrentAgentFit: "partial", BestTeamFit: "none",
		TaskType: classifyTaskType(input.Message, input.RecentContext), RequestKind: requestKindFromTaskType(classifyTaskType(input.Message, input.RecentContext), DecisionSelf),
		EmbeddingAvailable: evidence.Available, EmbeddingReason: evidence.Reason, SelfScore: evidence.SelfScore, CollaborationScore: evidence.CollaborationScore,
	}
}

func validateEffectiveResult(input Input, evidence Evidence, result Result) (Result, error) {
	if result.WorkflowMode != WorkflowModeMultiRole {
		validated := ValidateRoutingDecision(input, evidence, result)
		validated.EffectiveWorkflowMode = validated.WorkflowMode
		return validated, nil
	}
	validated, err := ValidatePlannerResult(input, applyEvidenceToResult(input, evidence, result))
	if err != nil {
		return Result{}, err
	}
	validated.EffectiveWorkflowMode = WorkflowModeMultiRole
	return validated, nil
}

func validateAssessedResult(input Input, evidence Evidence, shape ShapeAssessment, result Result) (Result, error) {
	if len(result.StaffingGaps) > 0 {
		if result.Decision != DecisionSelf || result.WorkflowMode != WorkflowModeSelf || result.WorkflowExecutable || result.Plan != nil {
			return Result{}, fmt.Errorf("staffing gaps require self, workflow_executable=false, and plan=null")
		}
		return forceDegradedSelf(result, "insufficient_canonical_members"), nil
	}
	// Only an evidence-backed independent-review requirement is a hard constraint
	// that cannot be downgraded. The blanket "semantic multi_role cannot be
	// downgraded" rule is intentionally gone: broad research/comparison/recommendation
	// that the decomposition guessed as multi_role may legitimately resolve to a
	// single owner. A verified review requirement still forces the multi_role
	// reviewer chain.
	if shape.IndependentReviewRequired && result.WorkflowMode != WorkflowModeMultiRole {
		return Result{}, fmt.Errorf("evidence-backed independent review cannot be downgraded to %q", result.WorkflowMode)
	}
	// The verified shape is the SOLE authority for the effective review
	// requirement. The planner may not manufacture one (nor suppress a verified
	// one): a multi_role plan whose verified shape did not require review keeps a
	// permitted-but-not-mandatory reviewer, so validateWorkflowPlan does not force
	// the owner->critic->owner chain unless the shape verifier demanded it.
	result.EffectiveReviewRequired = shape.IndependentReviewRequired
	return validateEffectiveResult(input, evidence, result)
}

func applyEvidenceToResult(input Input, evidence Evidence, result Result) Result {
	result.Mode = input.Mode
	result.DecisionBeforeValidation = result.Decision
	result.EmbeddingAvailable = evidence.Available
	result.EmbeddingReason = evidence.Reason
	result.SelfScore = evidence.SelfScore
	result.CollaborationScore = evidence.CollaborationScore
	result.WorkflowExecutable = workflowExecutableFromInput(input)
	return result
}

func arbiterContentPreview(content string) string {
	const max = 240
	trimmed := strings.TrimSpace(content)
	runes := []rune(trimmed)
	if len(runes) <= max {
		return trimmed
	}
	return string(runes[:max]) + "..."
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

type OwnerCandidate struct {
	Profile      Profile
	Capabilities []Capability
	Score        int
	IsLead       bool
}

func BuildRoleCapabilityFallback(input Input, evidence Evidence, _ string) Result {
	return safeSelfResult(input, evidence, "classifier_parse_failed")
}

func ValidateRoutingDecision(input Input, evidence Evidence, result Result) Result {
	before := result.Decision
	executable, executabilityReason := workflowExecutability(input)
	result.DecisionBeforeValidation = before
	result.WorkflowExecutable = executable
	result.EmbeddingAvailable = evidence.Available
	result.EmbeddingReason = evidence.Reason
	result.SelfScore = evidence.SelfScore
	result.CollaborationScore = evidence.CollaborationScore
	if result.Mode == "" || result.Mode == ModeSpawn {
		result.Mode = input.Mode
	}
	if result.TaskType == "" {
		result.TaskType = "other"
	}
	if result.RequestKind == "" {
		result.RequestKind = requestKindFromTaskType(result.TaskType, result.Decision)
	}
	if result.CurrentAgentFit == "" {
		result.CurrentAgentFit = "partial"
	}
	if result.BestTeamFit == "" {
		result.BestTeamFit = result.BetterCollaboratorFit
	}
	if result.BestTeamFit == "" {
		result.BestTeamFit = "none"
	}
	if result.BetterCollaboratorFit == "" {
		result.BetterCollaboratorFit = result.BestTeamFit
	}
	result = normalizeAndValidateBestOwner(input, result)
	if result.Decision == DecisionSelf {
		result.WorkflowMode = WorkflowModeSelf
	} else if result.WorkflowMode == "" {
		result.WorkflowMode = WorkflowModeSingleOwner
	}

	switch {
	case input.Mode == "" || input.Mode == ModeSpawn || !hasCollaborationProfile(input):
		return forceValidatedSelf(result, "no executable team/delegate profile")
	case result.Decision == DecisionTeam && !executable:
		return forceDegradedSelf(result, executabilityReason)
	case result.Decision == DecisionSelf && missionFirstOverrideApplies(input, evidence, result, executable):
		result.Decision = DecisionTeam
		result.WorkflowMode = WorkflowModeSingleOwner
		result.RequiredTool = requiredToolForMode(input.Mode)
		result.WorkflowHint = workflowHintForInput(input)
		if result.ValidatorReason == "" {
			result.ValidatorReason = "mission-first override: current agent is not a strong fit and executable team workflow is available"
		}
	case result.Decision == DecisionTeam:
		// required_tool names a BACKEND CAPABILITY, not a preference the model
		// gets to express. The session mode decides it: a team session routes
		// work through team_tasks, a delegate session through delegate. Trusting
		// the model's label let a mode=team turn come back with "delegate",
		// which authorizes against agent_links rather than the team roster — so
		// the classifier picked a canonical roster owner the delegate tool then
		// refused ("no delegation link"), burning the turn and answering nothing.
		// Normalise the label instead of rejecting the otherwise-valid decision.
		if canonical := requiredToolForMode(input.Mode); canonical != "" && result.RequiredTool != canonical {
			if result.RequiredTool == "" {
				result.ValidatorReason = "filled required tool from session mode"
			} else {
				result.ValidatorReason = fmt.Sprintf("normalised required tool %q to %q for session mode", result.RequiredTool, canonical)
			}
			result.RequiredTool = canonical
		}
		if result.WorkflowHint == "" {
			result.WorkflowHint = workflowHintForInput(input)
		}
	default:
		result.RequiredTool = ""
		result.WorkflowHint = ""
		if result.ValidatorReason == "" {
			result.ValidatorReason = "accepted self decision"
		}
	}
	if result.Decision == DecisionTeam && result.RequiredTool == "" {
		return forceValidatedSelf(result, "team decision missing executable required tool")
	}
	if result.Decision == DecisionTeam {
		owner, ok := canonicalCollaborationOwner(input, result.BestTeamOwner)
		if !ok {
			result.BestTeamOwnerID = uuid.Nil
			return forceDegradedSelf(result, "canonical_owner_unavailable")
		}
		result.BestTeamOwner = profileAgentKey(owner)
		result.BestTeamOwnerID = owner.AgentID
	}
	if result.ValidatorReason == "" {
		result.ValidatorReason = "accepted arbiter decision"
	}
	return result
}

func normalizeAndValidateBestOwner(input Input, result Result) Result {
	selected, selectedCanonical := canonicalCollaborationOwner(input, result.BestTeamOwner)
	if !selectedCanonical {
		return result
	}
	result.BestTeamOwner = profileAgentKey(selected)
	result.BestTeamOwnerID = selected.AgentID
	return result
}

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

func RequiredCapabilitiesForTask(taskType, message, recent string) []Capability {
	switch taskType {
	case "research":
		return []Capability{CapabilityResearch, CapabilityStrategy, CapabilityAnalyticsCritic}
	case "analytics":
		return []Capability{CapabilityAnalyticsCritic}
	case "strategy":
		return []Capability{CapabilityStrategy}
	case "content":
		return []Capability{CapabilityContentLead}
	case "visual":
		return []Capability{CapabilityVisualPrompt}
	case "dev":
		return []Capability{CapabilityTechnical}
	case "qa":
		return []Capability{CapabilityQA, CapabilityLeadCoordinator}
	case "coordination":
		return []Capability{CapabilityLeadCoordinator, CapabilityQA}
	default:
		return nil
	}
}

func NormalizeProfileCapabilities(profile Profile) []Capability {
	var caps []Capability
	if profile.CapabilitiesStatus != DataStatusKnown {
		return caps
	}
	for _, capability := range profile.Capabilities {
		if _, known := canonicalCapabilityKeys[capability.Key]; known {
			caps = append(caps, Capability(capability.Key))
		}
	}
	return caps
}

func FindBestOwnerCandidates(input Input, taskType string) []OwnerCandidate {
	required := RequiredCapabilitiesForTask(taskType, input.Message, input.RecentContext)
	var profiles []Profile
	profiles = append(profiles, input.Members...)
	profiles = append(profiles, input.Delegates...)
	var out []OwnerCandidate
	for _, profile := range profiles {
		caps := NormalizeProfileCapabilities(profile)
		score := capabilityScore(caps, required)
		isLead := strings.EqualFold(strings.TrimSpace(profile.TeamRole), "lead")
		out = append(out, OwnerCandidate{Profile: profile, Capabilities: caps, Score: score, IsLead: isLead})
	}
	slices.SortStableFunc(out, func(a, b OwnerCandidate) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		if a.IsLead != b.IsLead {
			if a.IsLead {
				return 1
			}
			return -1
		}
		return strings.Compare(a.Profile.Name, b.Profile.Name)
	})
	if len(out) > 0 && out[0].Score == 0 {
		return nil
	}
	return out
}

func capabilityScore(caps, required []Capability) int {
	score := 0
	for i, req := range required {
		if hasCapability(caps, req) {
			score += 10 - i
		}
	}
	return score
}

func fitForCapabilities(caps, required []Capability) string {
	score := capabilityScore(caps, required)
	return fitFromScore(score)
}

func fitFromScore(score int) string {
	if score >= 10 {
		return "strong"
	}
	if score > 0 {
		return "partial"
	}
	return "weak"
}

func hasCapability(caps []Capability, cap Capability) bool {
	for _, item := range caps {
		if item == cap {
			return true
		}
	}
	return false
}

func capabilitiesLabel(caps []Capability) string {
	if len(caps) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(caps))
	for _, cap := range caps {
		parts = append(parts, string(cap))
	}
	return strings.Join(parts, ",")
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

func canonicalCollaborationOwner(input Input, raw string) (Profile, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Profile{}, false
	}
	profiles := input.Members
	if input.Mode == ModeDelegate {
		profiles = input.Delegates
	}
	var matched *Profile
	for i := range profiles {
		profile := profiles[i]
		key := profileAgentKey(profile)
		display := firstNonEmpty(profile.DisplayName, profile.Name)
		isMatch := strings.EqualFold(raw, key) || strings.EqualFold(raw, display)
		if profile.AgentID != uuid.Nil && strings.EqualFold(raw, profile.AgentID.String()) {
			isMatch = true
		}
		if !isMatch {
			continue
		}
		if matched != nil && (!strings.EqualFold(profileAgentKey(*matched), key) || matched.AgentID != profile.AgentID) {
			return Profile{}, false
		}
		copy := profile
		matched = &copy
	}
	if matched == nil || strings.TrimSpace(profileAgentKey(*matched)) == "" {
		return Profile{}, false
	}
	return *matched, true
}

func isLeadGeneralist(owner, role string) bool {
	text := strings.ToLower(owner + "\n" + role)
	return containsAny(text, "lead", "coordinator", "assistant", "general", "lead_coordinator")
}

func containsAny(text string, markers ...string) bool {
	for _, marker := range markers {
		if strings.Contains(text, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func workflowNotExecutableReason(input Input) string {
	if input.Mode == ModeTeam {
		role := strings.ToLower(strings.TrimSpace(input.TeamRole))
		if role != "" && role != "lead" && !input.CanAssignTeamTasks {
			if !input.MemberRequestsEnabled {
				return "member lacks team request permission"
			}
		}
	}
	return "workflow not executable"
}

func missionFirstOverrideApplies(input Input, evidence Evidence, result Result, executable bool) bool {
	if !executable || !hasCollaborationProfile(input) {
		return false
	}
	if result.CurrentAgentFit == "strong" {
		return false
	}
	return result.BestTeamFit == "strong"
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

func forceValidatedSelf(result Result, reason string) Result {
	result.Decision = DecisionSelf
	result.WorkflowMode = WorkflowModeSelf
	result.EffectiveWorkflowMode = WorkflowModeSelf
	result.Plan = nil
	result.RequiredTool = ""
	result.WorkflowHint = ""
	result.ValidatorReason = reason
	if strings.TrimSpace(result.Reason) == "" {
		result.Reason = reason
	} else if !strings.Contains(result.Reason, reason) {
		result.Reason = reason + ": " + result.Reason
	}
	return result
}

func forceDegradedSelf(result Result, reason string) Result {
	result = forceValidatedSelf(result, reason)
	result.DegradedWorkflow = true
	result.DegradedReasonCode = reason
	return result
}

func workflowExecutableFromInput(input Input) bool {
	executable, _ := workflowExecutability(input)
	return executable
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

func workflowRosterExecutability(input Input, shape WorkShape, reviewRequired bool) (bool, string) {
	if input.Mode != ModeTeam || shape == "" || shape == WorkShapeAtomic {
		return true, ""
	}

	candidates := make(map[uuid.UUID]Profile, len(input.Members))
	for _, member := range input.Members {
		if member.AgentID == uuid.Nil || member.AgentID == input.CoordinatorAgentID || strings.TrimSpace(profileAgentKey(member)) == "" {
			continue
		}
		candidates[member.AgentID] = member
	}
	if len(candidates) < 2 {
		return false, "insufficient_canonical_members"
	}
	return true, ""
}

func profileHasAvailableTool(profile Profile, required string) bool {
	for _, name := range profile.AvailableTools {
		if strings.EqualFold(strings.TrimSpace(name), required) {
			return true
		}
	}
	return false
}

func workflowDegradationReason(input Input, validationErr, repairErr error) string {
	if executable, reason := workflowExecutability(input); !executable {
		return reason
	}
	var executabilityErr *workflowExecutabilityError
	if errors.As(validationErr, &executabilityErr) || errors.As(repairErr, &executabilityErr) {
		return executabilityErr.reason
	}
	if repairErr != nil {
		return "planner_repair_failed"
	}
	return "planner_validation_failed"
}

func hasCollaborationProfile(input Input) bool {
	switch input.Mode {
	case ModeTeam:
		return strings.TrimSpace(input.Team.Text) != "" || len(input.Members) > 0 || len(input.CollaborationTools) > 0
	case ModeDelegate:
		return len(input.Delegates) > 0 || len(input.CollaborationTools) > 0
	default:
		return false
	}
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

func BuildArbiterMessages(input Input, evidence Evidence) []providers.Message {
	system := `You are a team work classifier.
Return ONLY JSON. Do not answer the user.
Return this exact JSON schema and no other text:
{
	"workflow_mode": "self|single_owner|multi_role",
	"work_shape": "atomic|staged|cross_capability|reviewed_decision",
	"shape_traits": [{"type":"single_bounded_output|multiple_capabilities|sequential_dependency|score_or_rank|recommend_or_select|independent_verification|explicit_critique","source":"current_request|pinned_skill","evidence":"exact excerpt"}],
	"independent_review_required": true,
  "current_agent_role": "string",
  "task_type": "dev|research|strategy|analytics|content|visual|ops|qa|coordination|other",
  "current_agent_fit": "strong|partial|weak",
  "best_team_owner": "agent-key-or-role-or-empty",
  "best_team_owner_role": "agent-role-or-empty",
  "best_team_fit": "none|partial|strong",
  "specialist_match_found": true,
  "lead_selected_as_fallback": false,
  "routing_priority_used": "role_task_match|explicit_user_target|coordination|no_specialist|workflow_unavailable",
  "owner_selection_reason": "short reason",
  "followup_context_used_for_reference_only": true,
  "workflow_executable": true,
  "decision": "self|team",
	"required_tool": "team_tasks|delegate|",
	"reason": "short reason",
	"plan": null
}

For workflow_mode=multi_role, plan must be:
{
  "schema_version": 1,
  "goal": "single canonical goal",
  "coordinator_agent_id": "canonical team lead UUID",
  "coordinator_agent_key": "canonical team lead key",
  "final_owner_agent_id": "canonical final integrator UUID",
  "final_owner_agent_key": "canonical final integrator key",
  "review_status": "none|required|included",
  "terminal_step_id": "step id",
  "steps": [{
    "id": "unique stable id",
    "title": "short title",
    "instruction": "bounded task instruction",
    "owner_agent_id": "canonical roster UUID",
    "owner_agent_key": "canonical roster key",
    "capability_key": "declared capability key",
    "capability_label": "free description",
    "required_tools": ["known available tool names only"],
    "depends_on": ["step ids"],
    "required_output": true,
    "terminal": false
  }]
}

Your priority order is fixed:
1. Identify the current agent, its team, role, mission, and allowed workflow. Read the current agent mission.
2. Identify every available team member/delegate/tool and their role/mission. Read every team, linked agent, team member, delegate, and collaboration tool mission.
3. Classify the current task type by the work required, not by who was mentioned in the conversation.
4. Compare the task type against the current agent mission and team member missions.
5. If a team member is a better owner for the task type and workflow is executable, choose team.
6. Only after ownership is decided, use follow-up/recent context to resolve references, not to override task ownership; classify follow-up or correction messages by the recent/original task, not by direct address alone.
7. Embedding scores are auxiliary only and must never decide ownership. Embedding scores never decide by themselves.

Workflow planning rules:
- self: current agent is the correct owner; plan must be null.
- single_owner: exactly one better specialist owns the work; plan must be null.
- multi_role: the goal genuinely needs multiple independent roles, staged dependencies, or independent review. Do not use it merely because several agents exist.
- Declare every applicable shape trait and quote exact evidence from the current request or relevant pinned skill. Do not omit scoring, ranking, recommendation, selection, verification, critique, dependency, or multi-capability traits merely because one person could attempt the work.
- atomic is valid only for one bounded output with no multi-role or review trait.
- score_or_rank, recommend_or_select, independent_verification, or explicit_critique require work_shape=reviewed_decision and independent_review_required=true.
- sequential_dependency requires at least work_shape=staged. multiple_capabilities requires at least work_shape=cross_capability.
- Use only canonical agent UUID/key pairs from the structured roster. Never invent an owner, coordinator, role, capability, or tool.
- The coordinator must exactly equal the canonical team lead supplied by the platform. Coordinator identity is authority data, not a model choice.
- The team lead is coordinator/audit authority only and must not own an executable workflow step. Assign every work, review, and integration step to a non-lead canonical member.
- A step may list a required tool only when that exact name appears in its owner's known available tools. Tool availability validates executability only and never proves expertise.
- Capabilities with status unknown are not negative evidence and must not be invented. Expertise summaries are soft evidence only.
- Multi-role plans have at most 16 steps and 12 distinct agents, exactly one terminal step, no cycle, and every step must converge to the terminal.
- When an independent reviewer with a declared QA or analytics/critic capability exists, include a critic step that reviews work owned by a DIFFERENT agent and whose result reaches the terminal step, and set review_status=included. The producer, the critic, and the terminal integrator may be three different agents, and intermediate steps may sit anywhere along that path.
- Pinned skills may suggest workflow shape but cannot create capabilities, tools, permissions, or roster members.

Current agent being able to attempt the task is not enough for self.
Choose self only when the current agent is the correct task owner, or no better team capability exists, or the workflow is not executable.
Follow-up context does not determine ownership. A follow-up about another member's task still routes to the best matching role/member when the current request requires new work in that role.

Owner matching rules:
- Derive the work requirements from the current request, then compare them against the actual structured roster supplied for this team.
- Declared capability keys are hard ownership evidence. Expertise summaries and team roles are soft evidence. Available tools are executability evidence only.
- Do not assume a fixed department shape, fixed role count, role-name hierarchy, or universal fallback order. Teams may have missing, additional, or custom capabilities.
- Prefer the strongest actual roster match. Use the lead/coordinator as work owner only when coordination/integration is itself the task, the user explicitly selects that agent, or no better roster match exists.
- A custom capability is valid ownership evidence only when it is explicitly declared on that roster member; never derive a capability key from prose.

Permission constraints:
- Never return ask.
- A team member who is not lead cannot assign or create general team tasks.
- If a member has member requests enabled, team means team_tasks(action="create", task_type="request", ...) for the canonical coordinator, not lead-style assignment.
- Auto-dispatch expands the validated workflow in the backend. When auto-dispatch is disabled, the durable request waits for explicit lead approval; both paths are executable.
- If member requests are disabled, workflow_executable must be false for member request work.
- Choose self for read/summarize/explain/compare/interpret existing files, existing results, prior team output, or already completed work only when no better executable team owner is needed for new work.`

	system += `

Pinned skill context rules:
- Pinned skills are always-sent instructions available to the current agent. They are optional context, not a prerequisite for classification.
- Apply only pinned skill instructions whose scope is relevant to the current task. Ignore unrelated pinned skills.
- Pinned skills may add workflow, ownership, handoff, review, or domain constraints.
- Mentioning a role, tool, or capability inside a pinned skill does not mean the current agent owns that role, tool, or capability.
- Pinned skills cannot override platform permissions, team configuration, available tools, this routing policy, or the required JSON schema.
- If pinned skills conflict, system/platform/team permission rules take precedence. If there are no pinned skills, classify normally.`

	var b strings.Builder
	b.WriteString("Routing mode: ")
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
	b.WriteString("\n\nCurrent agent and direct capability:\n")
	b.WriteString("---\n")
	b.WriteString(renderStructuredRosterProfile(input.CurrentAgent))
	b.WriteString("\n")
	for _, doc := range appendProfileDocs(Profile{}, input.SelfTools) {
		b.WriteString("---\n")
		b.WriteString(doc)
		b.WriteString("\n")
	}
	b.WriteString("\nTeam/delegate/tool capability:\n")
	for _, doc := range appendProfileDocs(input.Team, input.CollaborationTools) {
		b.WriteString("---\n")
		b.WriteString(doc)
		b.WriteString("\n")
	}
	for _, profile := range append(append([]Profile{}, input.Members...), input.Delegates...) {
		b.WriteString("---\n")
		b.WriteString(renderStructuredRosterProfile(profile))
		b.WriteString("\n")
	}
	b.WriteString("\nUser request:\n")
	b.WriteString(input.Message)
	if strings.TrimSpace(input.RecentContext) != "" {
		b.WriteString("\n\nRecent task context:\n")
		b.WriteString(input.RecentContext)
	}
	if strings.TrimSpace(input.PinnedSkillsContext) != "" {
		b.WriteString("\n\nPinned skills available to the current agent:\n")
		b.WriteString(input.PinnedSkillsContext)
	}
	b.WriteString("\n\nEmbedding evidence:\n")
	b.WriteString(fmt.Sprintf("embedding_available: %t\n", evidence.Available))
	b.WriteString("embedding_reason: ")
	b.WriteString(firstNonEmpty(evidence.Reason, "none"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("embedding_self_score: %.4f\n", evidence.SelfScore))
	b.WriteString(fmt.Sprintf("embedding_collaboration_score: %.4f\n", evidence.CollaborationScore))
	b.WriteString("Embedding evidence is only supporting data, not a fallback decision.\n")
	b.WriteString("\nReturn JSON shape:\n")
	b.WriteString(`{"workflow_mode":"self|single_owner|multi_role","work_shape":"atomic|staged|cross_capability|reviewed_decision","shape_traits":[{"type":"single_bounded_output|multiple_capabilities|sequential_dependency|score_or_rank|recommend_or_select|independent_verification|explicit_critique","source":"current_request|pinned_skill","evidence":"exact excerpt"}],"independent_review_required":true,"current_agent_role":"string","task_type":"dev|research|strategy|analytics|content|visual|ops|qa|coordination|other","current_agent_fit":"strong|partial|weak","best_team_owner":"canonical-agent-key-or-empty","best_team_owner_role":"agent-role-or-empty","best_team_fit":"none|partial|strong","specialist_match_found":true,"lead_selected_as_fallback":false,"routing_priority_used":"role_task_match|explicit_user_target|coordination|no_specialist|workflow_unavailable","owner_selection_reason":"short reason","followup_context_used_for_reference_only":true,"workflow_executable":true,"decision":"self|team","required_tool":"team_tasks|delegate|","reason":"short reason","plan":null}`)

	return []providers.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: b.String()},
	}
}

func routingQueryText(input Input) string {
	message := strings.TrimSpace(input.Message)
	recent := strings.TrimSpace(input.RecentContext)
	if recent == "" {
		return message
	}
	if message == "" {
		return recent
	}
	return message + "\n\nRecent task context:\n" + recent
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

// staffingGap accepts either JSON shape a planner model produces for a staffing
// gap: a bare string, or an object like {"id":"unit-2","reason":"no reviewer"}.
// The prompt asks for "the unfilled work-unit IDs and reasons", so an object is a
// reasonable reading of the instruction and models do emit it.
//
// This matters far beyond the gap text itself: staffing_gaps is decoded as part of
// the WHOLE arbiter response, so an object here failed the entire unmarshal and
// killed a validated plan with `cannot unmarshal object into Go struct field
// .staffing_gaps of type string` — even when the array was EMPTY and no gap was
// being claimed at all. A cosmetic disagreement about one field's shape must never
// discard the plan.
//
// Live regression 2026-07-26: a khanh-developer reviewed_decision turn degraded to
// assignment_revision_failed for exactly this reason.
type staffingGap struct {
	value string
}

func (g *staffingGap) UnmarshalJSON(data []byte) error {
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		g.value = strings.TrimSpace(asString)
		return nil
	}
	var asObject struct {
		ID       string `json:"id"`
		UnitID   string `json:"unit_id"`
		WorkUnit string `json:"work_unit"`
		Step     string `json:"step"`
		Reason   string `json:"reason"`
		Detail   string `json:"detail"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal(data, &asObject); err != nil {
		// Neither a string nor an object: keep the gap non-empty so a real gap is
		// never silently dropped, but do not fail the whole response over its shape.
		g.value = strings.TrimSpace(string(data))
		return nil
	}
	id := firstNonEmpty(asObject.ID, asObject.UnitID, asObject.WorkUnit, asObject.Step)
	reason := firstNonEmpty(asObject.Reason, asObject.Detail, asObject.Message)
	switch {
	case id != "" && reason != "":
		g.value = id + ": " + reason
	case id != "":
		g.value = id
	default:
		g.value = reason
	}
	return nil
}

func staffingGapStrings(gaps []staffingGap) []string {
	if len(gaps) == 0 {
		return nil
	}
	out := make([]string, 0, len(gaps))
	for _, gap := range gaps {
		if gap.value != "" {
			out = append(out, gap.value)
		}
	}
	return out
}

func ParseArbiterResult(content string, mode Mode) (Result, error) {
	raw, err := normalizeArbiterContent(content)
	if err != nil {
		return Result{}, err
	}
	var parsed struct {
		CurrentAgentRole       string        `json:"current_agent_role"`
		TaskType               string        `json:"task_type"`
		CurrentAgentFit        string        `json:"current_agent_fit"`
		BestTeamOwner          *string       `json:"best_team_owner"`
		BestTeamOwnerRole      *string       `json:"best_team_owner_role"`
		BestTeamFit            string        `json:"best_team_fit"`
		SpecialistMatchFound   *bool         `json:"specialist_match_found"`
		LeadSelectedAsFallback *bool         `json:"lead_selected_as_fallback"`
		RoutingPriorityUsed    string        `json:"routing_priority_used"`
		OwnerSelectionReason   string        `json:"owner_selection_reason"`
		FollowupContextRefOnly *bool         `json:"followup_context_used_for_reference_only"`
		WorkflowExecutable     *bool         `json:"workflow_executable"`
		Decision               string        `json:"decision"`
		RequiredTool           string        `json:"required_tool"`
		Reason                 string        `json:"reason"`
		WorkflowMode           string        `json:"workflow_mode"`
		WorkShape              string        `json:"work_shape"`
		ShapeTraits            []ShapeTrait  `json:"shape_traits"`
		IndependentReview      *bool         `json:"independent_review_required"`
		Plan                   *WorkflowPlan `json:"plan"`
		StaffingGaps           []staffingGap `json:"staffing_gaps"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return Result{}, err
	}
	currentRole := strings.TrimSpace(parsed.CurrentAgentRole)
	taskType := strings.ToLower(strings.TrimSpace(parsed.TaskType))
	currentFit := strings.ToLower(strings.TrimSpace(parsed.CurrentAgentFit))
	bestTeamOwner := ""
	if parsed.BestTeamOwner != nil {
		bestTeamOwner = normalizeOptionalArbiterValue(*parsed.BestTeamOwner)
	}
	bestTeamOwnerRole := ""
	if parsed.BestTeamOwnerRole != nil {
		bestTeamOwnerRole = normalizeOptionalArbiterValue(*parsed.BestTeamOwnerRole)
	}
	bestTeamFit := strings.ToLower(strings.TrimSpace(parsed.BestTeamFit))
	routingPriority := strings.TrimSpace(parsed.RoutingPriorityUsed)
	ownerReason := strings.TrimSpace(parsed.OwnerSelectionReason)
	decision := Decision(strings.ToLower(strings.TrimSpace(parsed.Decision)))
	workflowMode := WorkflowMode(strings.ToLower(strings.TrimSpace(parsed.WorkflowMode)))
	workShape := WorkShape(strings.ToLower(strings.TrimSpace(parsed.WorkShape)))
	if workflowMode == "" {
		if decision == DecisionTeam {
			workflowMode = WorkflowModeSingleOwner
		} else {
			workflowMode = WorkflowModeSelf
		}
	}
	// `decision` is a LABEL derived from workflow_mode: a mode of single_owner or
	// multi_role IS a team decision, and self IS a self decision. Models routinely
	// get the substance right and the label wrong — live, a revision returned a
	// complete multi_role plan with decision="self" (told not to downgrade the mode,
	// it kept the mode and mislabelled the decision) and the whole assignment was
	// thrown away with `assignment_revision_failed`, degrading a reviewed_decision
	// request to self. Reconcile the label from the mode instead. This is NOT a
	// permission widening: the mode itself came from the model, and every
	// substantive rule below (plan validity, canonical owners, lead exclusion,
	// review chain, executability, best_team_owner presence) still runs and can
	// still reject. An explicit staffing-gap self report is left untouched — it
	// legitimately pairs decision=self with workflow_mode=self.
	if decision == DecisionSelf && workflowMode != WorkflowModeSelf {
		decision = DecisionTeam
	} else if decision == DecisionTeam && workflowMode == WorkflowModeSelf {
		decision = DecisionSelf
	}
	requiredTool := strings.TrimSpace(parsed.RequiredTool)
	reason := strings.TrimSpace(parsed.Reason)
	if currentRole == "" {
		return Result{}, fmt.Errorf("missing current_agent_role")
	}
	if !validEnum(taskType, "dev", "research", "strategy", "analytics", "content", "visual", "ops", "qa", "coordination", "other") {
		return Result{}, fmt.Errorf("invalid task_type %q", parsed.TaskType)
	}
	if !validEnum(currentFit, "strong", "partial", "weak") {
		return Result{}, fmt.Errorf("invalid current_agent_fit %q", parsed.CurrentAgentFit)
	}
	if parsed.BestTeamOwner == nil && workflowMode != WorkflowModeMultiRole {
		return Result{}, fmt.Errorf("missing best_team_owner")
	}
	if parsed.BestTeamOwnerRole == nil && workflowMode != WorkflowModeMultiRole {
		return Result{}, fmt.Errorf("missing best_team_owner_role")
	}
	if !validEnum(bestTeamFit, "none", "partial", "strong") {
		return Result{}, fmt.Errorf("invalid best_team_fit %q", parsed.BestTeamFit)
	}
	if parsed.SpecialistMatchFound == nil {
		return Result{}, fmt.Errorf("missing specialist_match_found")
	}
	if parsed.LeadSelectedAsFallback == nil {
		return Result{}, fmt.Errorf("missing lead_selected_as_fallback")
	}
	if routingPriority == "" {
		return Result{}, fmt.Errorf("missing routing_priority_used")
	}
	if ownerReason == "" {
		return Result{}, fmt.Errorf("missing owner_selection_reason")
	}
	if parsed.FollowupContextRefOnly == nil {
		return Result{}, fmt.Errorf("missing followup_context_used_for_reference_only")
	}
	if parsed.WorkflowExecutable == nil {
		return Result{}, fmt.Errorf("missing workflow_executable")
	}
	if reason == "" {
		return Result{}, fmt.Errorf("missing reason")
	}
	if decision != DecisionSelf && decision != DecisionTeam {
		return Result{}, fmt.Errorf("invalid decision %q", parsed.Decision)
	}
	if !validEnum(string(workflowMode), string(WorkflowModeSelf), string(WorkflowModeSingleOwner), string(WorkflowModeMultiRole)) {
		return Result{}, fmt.Errorf("invalid workflow_mode %q", parsed.WorkflowMode)
	}
	if workShape != "" && !validEnum(string(workShape), string(WorkShapeAtomic), string(WorkShapeStaged), string(WorkShapeCrossCapability), string(WorkShapeReviewedDecision)) {
		return Result{}, fmt.Errorf("invalid work_shape %q", parsed.WorkShape)
	}
	reviewRequired := false
	if parsed.IndependentReview != nil {
		reviewRequired = *parsed.IndependentReview
	}
	if workShape == "" {
		workShape = WorkShapeAtomic
		if workflowMode == WorkflowModeMultiRole {
			workShape = WorkShapeStaged
			if reviewRequired {
				workShape = WorkShapeReviewedDecision
			}
		}
	}
	base := Result{
		Reason:                   reason,
		Mode:                     mode,
		CurrentAgentRole:         currentRole,
		TaskType:                 taskType,
		CurrentAgentFit:          currentFit,
		BestTeamOwner:            bestTeamOwner,
		BestTeamOwnerRole:        bestTeamOwnerRole,
		BestTeamFit:              bestTeamFit,
		SpecialistMatchFound:     *parsed.SpecialistMatchFound,
		LeadSelectedAsFallback:   *parsed.LeadSelectedAsFallback,
		RoutingPriorityUsed:      routingPriority,
		OwnerSelectionReason:     ownerReason,
		FollowupContextReference: *parsed.FollowupContextRefOnly,
		BetterCollaboratorFit:    bestTeamFit,
		WorkflowExecutable:       *parsed.WorkflowExecutable,
		WorkflowMode:             workflowMode,
		RequestedWorkflowMode:    workflowMode,
		RequestedWorkShape:       workShape,
		ShapeTraits:              append([]ShapeTrait(nil), parsed.ShapeTraits...),
		RequestedReviewRequired:  reviewRequired,
		Plan:                     parsed.Plan,
		StaffingGaps:             compactSortedStrings(staffingGapStrings(parsed.StaffingGaps)),
	}
	switch decision {
	case DecisionTeam:
		if workflowMode == WorkflowModeSelf {
			return Result{}, fmt.Errorf("team decision cannot use self workflow_mode")
		}
		if workflowMode != WorkflowModeMultiRole && bestTeamOwner == "" {
			return Result{}, fmt.Errorf("team decision missing best_team_owner")
		}
		if workflowMode != WorkflowModeMultiRole && bestTeamOwnerRole == "" {
			return Result{}, fmt.Errorf("team decision missing best_team_owner_role")
		}
		if workflowMode != WorkflowModeMultiRole && requiredTool == "" {
			return Result{}, fmt.Errorf("team decision missing required_tool")
		}
		if workflowMode != WorkflowModeMultiRole && !*parsed.WorkflowExecutable {
			return Result{}, fmt.Errorf("team decision requires executable workflow")
		}
		if workflowMode != WorkflowModeMultiRole && bestTeamFit == "none" {
			return Result{}, fmt.Errorf("team decision requires team owner fit")
		}
		base.Decision = DecisionTeam
		base.RequiredTool = requiredTool
		base.RequestKind = requestKindFromTaskType(taskType, DecisionTeam)
		return base, nil
	case DecisionSelf:
		if workflowMode != WorkflowModeSelf {
			return Result{}, fmt.Errorf("self decision requires self workflow_mode")
		}
		if (bestTeamOwner == "") != (bestTeamOwnerRole == "") {
			return Result{}, fmt.Errorf("self decision owner and owner role must both be empty or both be set")
		}
		base.Decision = DecisionSelf
		base.RequiredTool = ""
		base.RequestKind = requestKindFromTaskType(taskType, DecisionSelf)
		return base, nil
	}
	return Result{}, fmt.Errorf("invalid decision %q", parsed.Decision)
}

func normalizeOptionalArbiterValue(value string) string {
	trimmed := strings.TrimSpace(value)
	switch strings.ToLower(trimmed) {
	case "none", "null", "n/a", "na":
		return ""
	default:
		return trimmed
	}
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
