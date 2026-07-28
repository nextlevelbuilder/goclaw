package teamworkclassify

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type shapeProviderStep struct {
	content string
	err     error
	delay   time.Duration
}

type shapeSequenceProvider struct {
	steps             []shapeProviderStep
	requests          []providers.ChatRequest
	deadlines         []time.Duration
	intentResolution  *IntentResolution
	intentCritique    *IntentCritique
	intentCritiqueErr error // force an intent-critic transport failure (nil response)
	shapeAssessment   *ShapeAssessment
	shapeErr          error // force a shape-verifier transport failure (nil response)
}

// defaultFakeShape returns a valid atomic, no-review shape whose evidence is the
// current request itself (always literally present, so ValidateShapeAssessment
// accepts it). Tests that exercise a review-driven flow set shapeAssessment
// explicitly with evidence quoted from their own message.
func defaultFakeShape(messages []providers.Message) string {
	return marshalWorkflowTestJSONNoT(ShapeAssessment{
		WorkShape:                 WorkShapeAtomic,
		ShapeTraits:               []ShapeTrait{{Type: ShapeTraitSingleBoundedOutput, Source: ShapeEvidenceCurrentRequest, Evidence: fakeCurrentRequestEvidence(messages)}},
		IndependentReviewRequired: false,
	})
}

func (p *shapeSequenceProvider) Chat(ctx context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	p.requests = append(p.requests, req)
	if deadline, ok := ctx.Deadline(); ok {
		p.deadlines = append(p.deadlines, time.Until(deadline))
	} else {
		p.deadlines = append(p.deadlines, 0)
	}
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "Resolve the current user message into a complete standalone request") {
		if p.intentResolution != nil {
			return &providers.ChatResponse{Content: marshalWorkflowTestJSONNoT(p.intentResolution)}, nil
		}
		var payload struct {
			CurrentUserMessage string `json:"current_user_message"`
		}
		_ = json.Unmarshal([]byte(req.Messages[len(req.Messages)-1].Content), &payload)
		return &providers.ChatResponse{Content: marshalWorkflowTestJSONNoT(IntentResolution{StandaloneRequest: payload.CurrentUserMessage, Relation: IntentRelationNew, UserIntent: "execute request"})}, nil
	}
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "Independently verify that the draft standalone request") {
		if p.intentCritiqueErr != nil {
			return nil, p.intentCritiqueErr
		}
		if p.intentCritique != nil {
			return &providers.ChatResponse{Content: marshalWorkflowTestJSONNoT(p.intentCritique)}, nil
		}
		return &providers.ChatResponse{Content: `{"valid":true,"issues":[],"corrected_resolution":null}`}, nil
	}
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "You independently verify the semantic work shape") {
		if p.shapeErr != nil {
			return nil, p.shapeErr
		}
		if p.shapeAssessment != nil {
			return &providers.ChatResponse{Content: marshalWorkflowTestJSONNoT(p.shapeAssessment)}, nil
		}
		return &providers.ChatResponse{Content: defaultFakeShape(req.Messages)}, nil
	}
	if len(p.steps) == 0 {
		return nil, errors.New("unexpected model call")
	}
	step := p.steps[0]
	p.steps = p.steps[1:]
	if step.delay > 0 {
		timer := time.NewTimer(step.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	if step.err != nil {
		return nil, step.err
	}
	return &providers.ChatResponse{Content: step.content}, nil
}

func TestIntentCriticCorrectsDroppedContinuationScope(t *testing.T) {
	input := plannerTestInput()
	input.Message = "Thế còn nửa đầu năm 2027 thì sao?"
	corrected := &IntentResolution{
		StandaloneRequest:     "Nghiên cứu toàn cảnh tài chính Việt Nam nửa đầu năm 2027 với cùng phạm vi và độ sâu như báo cáo cuối năm 2026.",
		Relation:              IntentRelationContinuation,
		UserIntent:            "continue the prior research",
		InheritedScope:        []string{"toàn cảnh tài chính Việt Nam", "kiểm chứng nguồn"},
		RequestedDeliverables: []string{"báo cáo nghiên cứu"},
	}
	provider := &shapeSequenceProvider{
		intentResolution: &IntentResolution{StandaloneRequest: "Nói về năm 2027.", Relation: IntentRelationNew, UserIntent: "talk about 2027"},
		intentCritique:   &IntentCritique{Valid: false, Issues: []string{"dropped prior scope"}, Correction: corrected},
		steps: []shapeProviderStep{
			{content: assessmentJSON(t, WorkflowModeMultiRole, true)},
			{content: planningJSON(t, input, WorkflowModeMultiRole, validPlannerTestPlan(input))},
			{content: marshalWorkflowTestJSON(t, PlanCritique{Valid: true})},
		},
	}
	result := ClassifyWithLLM(context.Background(), input, provider, "workflow-model", nil)
	if result.StandaloneRequest != corrected.StandaloneRequest || result.IntentRelation != IntentRelationContinuation {
		t.Fatalf("intent critic correction was not used: %+v", result)
	}
}

// An intent-critic TRANSPORT/nil failure (not an explicit rejection) must fail
// safe to a degraded self and stop before work assessment: the resolved intent
// could not be independently verified, so the run must not proceed to a decision
// that might escalate to Team Work. (Plan §1.3.4 — intent critic errors fail safe.)
func TestIntentCriticTransportFailureFailsSafe(t *testing.T) {
	input := plannerTestInput()
	provider := &shapeSequenceProvider{
		intentResolution:  &IntentResolution{StandaloneRequest: input.Message, Relation: IntentRelationNew, UserIntent: "execute request"},
		intentCritiqueErr: errors.New("intent critic unavailable"),
	}
	result := ClassifyWithLLM(context.Background(), input, provider, "workflow-model", nil)
	if result.Decision != DecisionSelf || !result.DegradedWorkflow {
		t.Fatalf("intent-critic transport failure must degrade to self: %+v", result)
	}
	if result.DegradedReasonCode != "intent_critic_transport_failed" {
		t.Fatalf("degraded reason = %q, want intent_critic_transport_failed", result.DegradedReasonCode)
	}
	// Must stop after resolver + intent critic — no shape/assessment/planning calls.
	if len(provider.requests) != 2 {
		t.Fatalf("intent-critic failure must stop after resolver+critic, calls=%d", len(provider.requests))
	}
}

// The independent shape verifier is a REQUIRED live stage: a transport/nil
// failure must fail safe to a degraded self and stop before work assessment. The
// verified shape is the sole authority for the review requirement, so the run
// must not proceed without it. (Plan §1.3.4 — shape verifier errors fail safe.)
func TestShapeVerifierTransportFailureFailsSafe(t *testing.T) {
	input := plannerTestInput()
	provider := &shapeSequenceProvider{shapeErr: errors.New("shape verifier unavailable")}
	result := ClassifyWithLLM(context.Background(), input, provider, "workflow-model", nil)
	if result.Decision != DecisionSelf || !result.DegradedWorkflow {
		t.Fatalf("shape-verifier transport failure must degrade to self: %+v", result)
	}
	if result.DegradedReasonCode != "shape_verifier_transport_failed" {
		t.Fatalf("degraded reason = %q, want shape_verifier_transport_failed", result.DegradedReasonCode)
	}
	// Must stop after resolver + intent critic + shape verify — no assessment/planning.
	if len(provider.requests) != 3 {
		t.Fatalf("shape failure must stop after resolver+critic+shape, calls=%d", len(provider.requests))
	}
}

func TestIntentClarificationNeverMutatesTeamWork(t *testing.T) {
	input := plannerTestInput()
	provider := &shapeSequenceProvider{intentResolution: &IntentResolution{
		StandaloneRequest:  "Clarify which market and time period should be researched.",
		Relation:           IntentRelationRefinement,
		UserIntent:         "request research with unresolved scope",
		Ambiguities:        []string{"market", "time period"},
		NeedsClarification: true,
	}}
	result := ClassifyWithLLM(context.Background(), input, provider, "workflow-model", nil)
	if result.Decision != DecisionSelf || !result.DegradedWorkflow || result.DegradedReasonCode != "intent_clarification_required" || result.RequiredTool != "" {
		t.Fatalf("clarification result can mutate Team Work: %+v", result)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("clarification should stop after resolver and intent critic, calls=%d", len(provider.requests))
	}
}

func TestAssignmentCriticCanUpgradeUnderclassifiedSelf(t *testing.T) {
	input := plannerTestInput()
	provider := &shapeSequenceProvider{steps: []shapeProviderStep{
		{content: assessmentJSON(t, WorkflowModeSelf, false)},
		{content: planningJSON(t, input, WorkflowModeSelf, nil)},
		{content: marshalWorkflowTestJSON(t, PlanCritique{Valid: false, Issues: []string{"request requires multiple dependent owners"}})},
		{content: planningJSON(t, input, WorkflowModeMultiRole, validPlannerTestPlan(input))},
	}}
	result := ClassifyWithLLM(context.Background(), input, provider, "workflow-model", nil)
	if result.EffectiveWorkflowMode != WorkflowModeMultiRole || result.Plan == nil || !result.PlannerRepaired {
		t.Fatalf("critic could not upgrade under-classified self: %+v", result)
	}
}

func TestFollowupIsResolvedBeforeWorkAssessment(t *testing.T) {
	input := plannerTestInput()
	input.Message = "Em ơi còn nửa đầu năm 2027 thì sao?"
	input.RecentContext = "user: Nghiên cứu toàn cảnh tài chính Việt Nam cuối năm 2026\nassistant: Báo cáo gồm vĩ mô, lãi suất, tỷ giá, chứng khoán, trái phiếu, bất động sản, vàng và ba kịch bản."
	standalone := "Nghiên cứu toàn cảnh thị trường tài chính Việt Nam nửa đầu năm 2027, tiếp nối báo cáo cuối năm 2026 với cùng phạm vi, độ sâu, kiểm chứng nguồn và phân tích theo kịch bản."
	provider := &shapeSequenceProvider{
		intentResolution: &IntentResolution{
			StandaloneRequest:     standalone,
			Relation:              IntentRelationContinuation,
			UserIntent:            "extend the prior full financial-market research to the next period",
			InheritedScope:        []string{"vĩ mô", "lãi suất", "tỷ giá", "chứng khoán", "trái phiếu", "bất động sản", "vàng", "kịch bản"},
			RequestedDeliverables: []string{"báo cáo nghiên cứu có kiểm chứng nguồn"},
		},
		steps: []shapeProviderStep{
			{content: assessmentJSON(t, WorkflowModeMultiRole, true)},
			{content: planningJSON(t, input, WorkflowModeMultiRole, validPlannerTestPlan(input))},
			{content: marshalWorkflowTestJSON(t, PlanCritique{Valid: true})},
		},
	}
	result := ClassifyWithLLM(context.Background(), input, provider, "workflow-model", nil)
	if result.StandaloneRequest != standalone || result.IntentRelation != IntentRelationContinuation {
		t.Fatalf("resolved intent = %+v", result)
	}
	if result.EffectiveWorkflowMode != WorkflowModeMultiRole || result.Plan == nil {
		t.Fatalf("follow-up was downgraded after resolution: %+v", result)
	}
	assessmentPayload := provider.requests[3].Messages[1].Content
	if !strings.Contains(assessmentPayload, standalone) || strings.Contains(assessmentPayload, `"members"`) {
		t.Fatalf("semantic assessment did not receive only the standalone task: %s", assessmentPayload)
	}
}

func marshalWorkflowTestJSONNoT(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func (*shapeSequenceProvider) ChatStream(context.Context, providers.ChatRequest, func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return nil, errors.New("streaming is not supported")
}

func (*shapeSequenceProvider) DefaultModel() string { return "workflow-model" }
func (*shapeSequenceProvider) Name() string         { return "workflow-sequence" }

func assessmentJSON(t *testing.T, mode WorkflowMode, review bool) string {
	t.Helper()
	assessment := WorkAssessment{
		WorkflowMode: mode, IndependentReviewRequired: review, Reason: "assessed current request",
		WorkUnits:       []WorkUnit{{ID: "produce", Description: "produce the requested result", RequiredOutput: "requested result"}},
		RequiredOutputs: []string{"requested result"},
	}
	if mode == WorkflowModeMultiRole {
		assessment.WorkUnits = []WorkUnit{
			{ID: "draft", Description: "produce the primary analysis", RequiredOutput: "draft analysis"},
			{ID: "review", Description: "review independently", RequiredOutput: "independent critique"},
			{ID: "integrate", Description: "integrate the reviewed result", RequiredOutput: "final result"},
		}
		assessment.Dependencies = []WorkDependency{{From: "draft", To: "review"}, {From: "review", To: "integrate"}}
		assessment.RequiredOutputs = []string{"draft analysis", "independent critique", "final result"}
	}
	return marshalWorkflowTestJSON(t, assessment)
}

func planningJSON(t *testing.T, input Input, mode WorkflowMode, plan *WorkflowPlan) string {
	t.Helper()
	decision := "team"
	requiredTool := "team_tasks"
	owner := input.Members[1].AgentKey
	ownerRole := "specialist"
	bestFit := "strong"
	specialist := true
	if mode == WorkflowModeSelf {
		decision = "self"
		requiredTool = ""
		owner = ""
		ownerRole = ""
		bestFit = "none"
		specialist = false
	}
	payload := map[string]any{
		"workflow_mode": mode, "current_agent_role": input.TeamRole, "task_type": "analytics", "current_agent_fit": "partial",
		"best_team_owner": owner, "best_team_owner_role": ownerRole, "best_team_fit": bestFit,
		"specialist_match_found": specialist, "lead_selected_as_fallback": false, "routing_priority_used": "role_task_match",
		"owner_selection_reason": "canonical roster fit", "followup_context_used_for_reference_only": true,
		"workflow_executable": true, "decision": decision, "required_tool": requiredTool, "reason": "planned from assessment", "plan": plan,
	}
	return marshalWorkflowTestJSON(t, payload)
}

func marshalWorkflowTestJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestMultiCallWorkflowResolvesAndCritiquesSelfAssignment(t *testing.T) {
	input := plannerTestInput()
	provider := &shapeSequenceProvider{steps: []shapeProviderStep{
		{content: assessmentJSON(t, WorkflowModeSelf, false)},
		{content: planningJSON(t, input, WorkflowModeSelf, nil)},
		{content: marshalWorkflowTestJSON(t, PlanCritique{Valid: true})},
	}}
	result := ClassifyWithLLM(context.Background(), input, provider, "workflow-model", nil)
	if result.Decision != DecisionSelf || result.DegradedWorkflow || result.EffectiveWorkflowMode != WorkflowModeSelf {
		t.Fatalf("self result = %+v", result)
	}
	if len(provider.requests) != 6 {
		t.Fatalf("calls = %d, want intent resolver + intent critic + shape + assessment + assignment + critic", len(provider.requests))
	}
}

func TestMultiCallWorkflowSelectsSingleCanonicalOwner(t *testing.T) {
	input := plannerTestInput()
	provider := &shapeSequenceProvider{steps: []shapeProviderStep{
		{content: assessmentJSON(t, WorkflowModeSingleOwner, false)},
		{content: planningJSON(t, input, WorkflowModeSingleOwner, nil)},
		{content: marshalWorkflowTestJSON(t, PlanCritique{Valid: true})},
	}}
	result := ClassifyWithLLM(context.Background(), input, provider, "workflow-model", nil)
	if result.Decision != DecisionTeam || result.WorkflowMode != WorkflowModeSingleOwner || result.BestTeamOwnerID != input.Members[1].AgentID {
		t.Fatalf("single-owner result = %+v", result)
	}
	if len(provider.requests) != 6 {
		t.Fatalf("calls = %d, want intent resolver + intent critic + shape + assessment + owner selection + critic", len(provider.requests))
	}
}

func TestMultiCallWorkflowAcceptsReviewedPlanWithoutStructuredCapabilities(t *testing.T) {
	input := plannerTestInput()
	input.Message = "score the options, recommend one, and independently critique the recommendation"
	for i := range input.Members {
		input.Members[i].Capabilities = nil
		input.Members[i].CapabilitiesStatus = DataStatusUnknown
	}
	provider := &shapeSequenceProvider{
		shapeAssessment: &ShapeAssessment{
			WorkShape:                 WorkShapeReviewedDecision,
			ShapeTraits:               []ShapeTrait{{Type: ShapeTraitExplicitCritique, Source: ShapeEvidenceCurrentRequest, Evidence: "independently critique the recommendation"}},
			IndependentReviewRequired: true,
		},
		steps: []shapeProviderStep{
			{content: assessmentJSON(t, WorkflowModeMultiRole, true)},
			{content: planningJSON(t, input, WorkflowModeMultiRole, validPlannerTestPlan(input))},
			{content: marshalWorkflowTestJSON(t, PlanCritique{Valid: true})},
		},
	}
	result := ClassifyWithLLM(context.Background(), input, provider, "workflow-model", nil)
	if result.Decision != DecisionTeam || result.EffectiveWorkflowMode != WorkflowModeMultiRole || result.Plan == nil || result.DegradedWorkflow {
		t.Fatalf("multi-role result = %+v", result)
	}
	if !result.EffectiveReviewRequired {
		t.Fatalf("verified explicit-critique shape must require review: %+v", result)
	}
	if len(provider.requests) != 6 {
		t.Fatalf("calls = %d, want intent resolver + intent critic + shape + assessment + planning + critic", len(provider.requests))
	}
}

func TestMultiCallWorkflowRevisesCriticIssues(t *testing.T) {
	input := plannerTestInput()
	draft := validPlannerTestPlan(input)
	revised := validPlannerTestPlan(input)
	revised.Goal = "revised reviewed output"
	provider := &shapeSequenceProvider{steps: []shapeProviderStep{
		{content: assessmentJSON(t, WorkflowModeMultiRole, true)},
		{content: planningJSON(t, input, WorkflowModeMultiRole, draft)},
		{content: marshalWorkflowTestJSON(t, PlanCritique{Valid: false, Issues: []string{"clarify the final deliverable"}})},
		{content: planningJSON(t, input, WorkflowModeMultiRole, revised)},
	}}
	result := ClassifyWithLLM(context.Background(), input, provider, "workflow-model", nil)
	if result.Plan == nil || result.Plan.Goal != revised.Goal || !result.PlannerRepaired {
		t.Fatalf("revised result = %+v", result)
	}
	if len(provider.requests) != 7 {
		t.Fatalf("calls = %d, want intent resolver + intent critic + shape + assessment + planning + critic + revision", len(provider.requests))
	}
}

// A TEAM decision whose independent assignment critic cannot run (transport/
// timeout/nil/parse) must FAIL SAFE to a degraded self — never orchestrate an
// un-critiqued plan. This is the assignment-critic fail-open gap: the plan is
// backend-valid, but without the independent critique the run must not schedule
// Team Work. (Plan §1.3.4: intent/shape/assignment critic errors all fail safe.)
func TestMultiCallWorkflowFailsSafeWhenAssignmentCriticFails(t *testing.T) {
	input := plannerTestInput()
	provider := &shapeSequenceProvider{steps: []shapeProviderStep{
		{content: assessmentJSON(t, WorkflowModeMultiRole, true)},
		{content: planningJSON(t, input, WorkflowModeMultiRole, validPlannerTestPlan(input))},
		{err: errors.New("critic unavailable")},
	}}
	result := ClassifyWithLLM(context.Background(), input, provider, "workflow-model", nil)
	if result.Decision != DecisionSelf || !result.DegradedWorkflow {
		t.Fatalf("assignment-critic failure must degrade a team decision to self: %+v", result)
	}
	if result.Plan != nil || result.EffectiveWorkflowMode != WorkflowModeSelf || result.RequiredTool != "" {
		t.Fatalf("degraded self must carry no plan/required tool: %+v", result)
	}
	if result.DegradedReasonCode != "assignment_critic_transport_failed" {
		t.Fatalf("degraded reason = %q, want assignment_critic_transport_failed", result.DegradedReasonCode)
	}
}

// The self counterpart: when the decision already resolved to self, an
// assignment-critic failure keeps it self but STILL marks the run degraded. A
// required verification stage that could not run is a degradation event no matter
// what it was verifying; recording it as "accepted" would understate the
// degradation rate the audit exists to measure (§1.3.7). The decision stays self
// (no plan, no required tool) so nothing orchestrates, and the audit projection
// attributes the degradation to the assignment_critic stage.
func TestMultiCallWorkflowSelfSurvivesAssignmentCriticFailure(t *testing.T) {
	input := plannerTestInput()
	provider := &shapeSequenceProvider{steps: []shapeProviderStep{
		{content: assessmentJSON(t, WorkflowModeSelf, false)},
		{content: planningJSON(t, input, WorkflowModeSelf, nil)},
		{err: errors.New("critic unavailable")},
	}}
	result := ClassifyWithLLM(context.Background(), input, provider, "workflow-model", nil)
	if result.Decision != DecisionSelf || !result.DegradedWorkflow {
		t.Fatalf("self decision must stay self AND be marked degraded on critic failure: %+v", result)
	}
	if result.EffectiveWorkflowMode != WorkflowModeSelf || result.Plan != nil || result.RequiredTool != "" {
		t.Fatalf("degraded self must carry no plan/required tool: %+v", result)
	}
	if result.DegradedReasonCode != "assignment_critic_transport_failed" {
		t.Fatalf("degraded reason = %q, want assignment_critic_transport_failed", result.DegradedReasonCode)
	}
	// The audit projection must attribute this degradation to the assignment_critic
	// stage — not silently drop it as accepted — so over-selection/degradation
	// measurement counts a stage that could not run.
	audit := BuildClassificationAudit(ClassificationAuditInput{Ingress: store.TeamWorkIngressWS}, result)
	if audit.DegradedStage != "assignment_critic" {
		t.Fatalf("audit degraded stage = %q, want assignment_critic", audit.DegradedStage)
	}
	if audit.DegradedReason != "assignment_critic_transport_failed" {
		t.Fatalf("audit degraded reason = %q, want assignment_critic_transport_failed", audit.DegradedReason)
	}
}

func TestAssessmentTimeoutUsesIndependentChildContext(t *testing.T) {
	input := plannerTestInput()
	input.Timeout = 20 * time.Millisecond
	provider := &shapeSequenceProvider{steps: []shapeProviderStep{{delay: 100 * time.Millisecond}}}
	parent := context.Background()
	result := ClassifyWithLLM(parent, input, provider, "workflow-model", nil)
	if result.Decision != DecisionSelf || result.DegradedReasonCode != "classifier_timeout" {
		t.Fatalf("timeout result = %+v", result)
	}
	if parent.Err() != nil {
		t.Fatalf("child timeout cancelled parent: %v", parent.Err())
	}
}
