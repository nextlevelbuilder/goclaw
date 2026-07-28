package teamworkclassify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// BuildPlanConstraint freezes a validated planner result for backend
// execution. The hash is computed from canonical JSON and is never supplied by
// the model.
func BuildPlanConstraint(plan *WorkflowPlan) (*tools.TeamWorkPlanConstraint, error) {
	if plan == nil {
		return nil, fmt.Errorf("workflow plan is required")
	}
	normalizeWorkflowPlan(plan)
	canonical, err := json.Marshal(plan)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical workflow plan: %w", err)
	}
	sum := sha256.Sum256(canonical)
	constraint := &tools.TeamWorkPlanConstraint{
		SchemaVersion:       plan.SchemaVersion,
		Goal:                plan.Goal,
		CoordinatorAgentID:  plan.CoordinatorAgentID,
		CoordinatorAgentKey: plan.CoordinatorAgentKey,
		FinalOwnerAgentID:   plan.FinalOwnerAgentID,
		FinalOwnerAgentKey:  plan.FinalOwnerAgentKey,
		TerminalStepID:      plan.TerminalStepID,
		CanonicalPlan:       canonical,
		PlanHash:            hex.EncodeToString(sum[:]),
		Steps:               make([]tools.TeamWorkPlanStepConstraint, 0, len(plan.Steps)),
	}
	for _, step := range plan.Steps {
		constraint.Steps = append(constraint.Steps, tools.TeamWorkPlanStepConstraint{
			ID:             step.ID,
			Title:          step.Title,
			Instruction:    step.Instruction,
			OwnerAgentID:   step.OwnerAgentID,
			OwnerAgentKey:  step.OwnerAgentKey,
			RequiredTools:  append([]string(nil), step.RequiredTools...),
			DependsOn:      append([]string(nil), step.DependsOn...),
			RequiredOutput: step.RequiredOutput,
			Terminal:       step.Terminal,
		})
	}
	return constraint, nil
}

// RevalidateStoredWorkflow reloads the current roster/tool snapshots and
// validates the persisted plan before delayed approval or recovery expansion.
func RevalidateStoredWorkflow(ctx context.Context, stores ProfileStores, workflow *store.TeamWorkflowData) error {
	if workflow == nil {
		return fmt.Errorf("workflow is required")
	}
	var plan WorkflowPlan
	if err := json.Unmarshal(workflow.CanonicalPlan, &plan); err != nil {
		return fmt.Errorf("decode stored workflow plan: %w", err)
	}
	input := BuildInputFromStores(ctx, stores, BuildInputOptions{
		Mode: ModeTeam, Message: plan.Goal, AgentID: workflow.OriginAgentID,
		TeamID: workflow.TeamID,
	})
	result, err := ValidatePlannerResult(input, Result{
		Mode: ModeTeam, Decision: DecisionTeam, WorkflowMode: WorkflowModeMultiRole,
		RequiredTool: "team_tasks", WorkflowExecutable: true, Plan: &plan,
		EffectiveReviewRequired: strings.EqualFold(strings.TrimSpace(plan.ReviewStatus), "included") || strings.EqualFold(strings.TrimSpace(plan.ReviewStatus), "required"),
	})
	if err != nil {
		return err
	}
	constraint, err := BuildPlanConstraint(result.Plan)
	if err != nil {
		return err
	}
	if constraint.PlanHash != workflow.PlanHash {
		return fmt.Errorf("stored workflow canonical hash changed after revalidation")
	}
	return nil
}

func normalizeWorkflowPlan(plan *WorkflowPlan) {
	plan.Goal = normalizePlanText(plan.Goal)
	plan.CoordinatorAgentKey = strings.TrimSpace(plan.CoordinatorAgentKey)
	plan.FinalOwnerAgentKey = strings.TrimSpace(plan.FinalOwnerAgentKey)
	plan.ReviewStatus = strings.ToLower(strings.TrimSpace(plan.ReviewStatus))
	plan.TerminalStepID = strings.TrimSpace(plan.TerminalStepID)
	for i := range plan.Steps {
		step := &plan.Steps[i]
		step.ID = strings.TrimSpace(step.ID)
		step.Title = normalizePlanText(step.Title)
		step.Instruction = normalizePlanText(step.Instruction)
		step.OwnerAgentKey = strings.TrimSpace(step.OwnerAgentKey)
		step.CapabilityKey = strings.ToLower(strings.TrimSpace(step.CapabilityKey))
		step.WorkflowRole = strings.ToLower(strings.TrimSpace(step.WorkflowRole))
		step.CapabilityLabel = normalizePlanText(step.CapabilityLabel)
		step.RequiredTools = compactSortedStrings(step.RequiredTools)
		step.DependsOn = compactSortedStrings(step.DependsOn)
	}
	slices.SortFunc(plan.Steps, func(a, b WorkflowStep) int { return strings.Compare(a.ID, b.ID) })
}

func normalizePlanText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func BuildPlannerRepairMessages(input Input, evidence Evidence, previous string, validationErr error, requiredShape WorkShape) []providers.Message {
	messages := BuildArbiterMessages(input, evidence)
	shapeInstruction := "Keep the strongest work shape justified by complete traits and evidence."
	if requiredShape != "" {
		shapeInstruction = fmt.Sprintf("The corrected response must use work_shape=%s or a stronger shape and workflow_mode=multi_role with a canonical plan.", requiredShape)
	}
	messages = append(messages,
		providers.Message{Role: "assistant", Content: previous},
		providers.Message{Role: "system", Content: "The previous JSON failed semantic validation: " + validationErr.Error() + ". " + shapeInstruction + " Return one corrected JSON object using the same schema. Include complete shape traits with exact evidence. Keep canonical roster UUID/key pairs, permissions, known tool availability, limits, DAG convergence, terminal ownership, and independent-review rules. Do not add commentary."},
	)
	return messages
}

type DataStatus string

const (
	DataStatusKnown   DataStatus = "known"
	DataStatusUnknown DataStatus = "unknown"
)

type StructuredCapability struct {
	Key   string `json:"key"`
	Label string `json:"label,omitempty"`
}

type WorkflowMode string

const (
	WorkflowModeSelf        WorkflowMode = "self"
	WorkflowModeSingleOwner WorkflowMode = "single_owner"
	WorkflowModeMultiRole   WorkflowMode = "multi_role"
)

const (
	WorkflowPlanSchemaVersion = 1
	MaxWorkflowSteps          = 16
	MaxWorkflowAgents         = 12
)

type WorkflowPlan struct {
	SchemaVersion       int            `json:"schema_version"`
	Goal                string         `json:"goal"`
	CoordinatorAgentID  uuid.UUID      `json:"coordinator_agent_id"`
	CoordinatorAgentKey string         `json:"coordinator_agent_key"`
	FinalOwnerAgentID   uuid.UUID      `json:"final_owner_agent_id"`
	FinalOwnerAgentKey  string         `json:"final_owner_agent_key"`
	ReviewStatus        string         `json:"review_status"`
	TerminalStepID      string         `json:"terminal_step_id"`
	Steps               []WorkflowStep `json:"steps"`
}

type WorkflowStep struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Instruction     string    `json:"instruction"`
	OwnerAgentID    uuid.UUID `json:"owner_agent_id"`
	OwnerAgentKey   string    `json:"owner_agent_key"`
	CapabilityKey   string    `json:"capability_key"`
	CapabilityLabel string    `json:"capability_label,omitempty"`
	WorkflowRole    string    `json:"workflow_role,omitempty"`
	RequiredTools   []string  `json:"required_tools"`
	DependsOn       []string  `json:"depends_on"`
	RequiredOutput  bool      `json:"required_output"`
	Terminal        bool      `json:"terminal"`
}

func ValidatePlannerResult(input Input, result Result) (Result, error) {
	switch result.WorkflowMode {
	case "", WorkflowModeSelf, WorkflowModeSingleOwner:
		return result, nil
	case WorkflowModeMultiRole:
		if result.Decision != DecisionTeam {
			return result, fmt.Errorf("multi_role requires team decision")
		}
		if input.Mode != ModeTeam {
			return result, fmt.Errorf("multi_role is only supported for team mode")
		}
		if executable, reason := workflowExecutability(input); !executable {
			return result, fmt.Errorf("multi_role workflow is not executable: %s", reason)
		}
		if strings.TrimSpace(result.RequiredTool) != "team_tasks" {
			return result, fmt.Errorf("multi_role requires team_tasks")
		}
		if result.Plan == nil {
			return result, fmt.Errorf("multi_role requires plan")
		}
		if err := validateWorkflowPlan(input, result.Plan, result.EffectiveReviewRequired); err != nil {
			return result, err
		}
		result.BestTeamOwner = result.Plan.FinalOwnerAgentKey
		result.BestTeamOwnerID = result.Plan.FinalOwnerAgentID
		result.RequiredTool = "team_tasks"
		return result, nil
	default:
		return result, fmt.Errorf("invalid workflow_mode %q", result.WorkflowMode)
	}
}

// reconcileTerminalAndRoles repairs the two bookkeeping fields a planner model
// most often contradicts itself on, using the plan's own declared intent rather
// than rejecting the plan.
//
// terminal_step_id is the SINGLE source of truth for which step is terminal. A
// model that names the terminal step correctly but forgets to flip that step's
// `terminal` boolean has stated its intent unambiguously; the two fields encode
// the same fact, so requiring the model to agree with itself buys nothing and
// throws away an otherwise valid plan. workflow_role follows on the same axis:
// the terminal step is the integration step, and no other step can be.
//
// INVARIANT: this is a no-op on any plan that would already have validated.
// Previous validation required exactly one terminal step and required it to be
// the one terminal_step_id names, and required `terminal == (role ==
// "integration")` — so every rewrite below is already the plan's own value.
// That matters because WorkflowRole and Terminal are inside the canonical plan
// JSON that BuildPlanConstraint hashes: any change to a stored plan would make
// RevalidateStoredWorkflow fail with "canonical hash changed". An EMPTY role is
// left empty for the same reason — validateWorkflowRole accepts it for
// schema-v1 back-compat, so filling it in would rewrite stored v1 hashes.
//
// This repairs ONLY self-contradiction about bookkeeping. Every substantive rule
// still applies afterwards and can still reject the plan: owner canonicality,
// lead exclusion, DAG acyclicity and convergence, the independent-review chain,
// tool availability, and step limits. If terminal_step_id is missing or names no
// step, nothing is touched and validation reports it — inventing a terminal step
// would be guessing at intent rather than reading it.
//
// Live regression: a khanh-developer multi_role plan was rejected with
// `step "step-6-integration" terminal state conflicts with workflow_role
// "integration"` while terminal_step_id already said "step-6-integration".
func reconcileTerminalAndRoles(plan *WorkflowPlan) {
	declared := strings.TrimSpace(plan.TerminalStepID)
	if declared == "" {
		return
	}
	var terminal *WorkflowStep
	for i := range plan.Steps {
		if strings.TrimSpace(plan.Steps[i].ID) == declared {
			terminal = &plan.Steps[i]
			break
		}
	}
	if terminal == nil {
		return
	}
	for i := range plan.Steps {
		step := &plan.Steps[i]
		isTerminal := step == terminal
		step.Terminal = isTerminal
		role := strings.ToLower(strings.TrimSpace(step.WorkflowRole))
		if role == "" {
			continue
		}
		if isTerminal {
			// A terminal step labelled with a work role is the same
			// bookkeeping slip read from the other side. "critic" is NOT
			// repaired: a review step declared last is a substantive claim
			// about the plan's shape (a review with nothing integrating it),
			// and the review-chain validator must be the one to reject it.
			if role == "draft" || role == "work" {
				step.WorkflowRole = "integration"
			}
			continue
		}
		// Only the terminal step may carry the integration role. A non-terminal
		// step left holding it gets the role its DAG position implies: an entry
		// step drafts, a dependent step works. A "critic" role is never inferred —
		// that is a substantive assignment the review-chain validator must judge on
		// what the model actually declared.
		if role == "integration" {
			if len(step.DependsOn) == 0 {
				step.WorkflowRole = "draft"
			} else {
				step.WorkflowRole = "work"
			}
		}
	}
}

func validateWorkflowPlan(input Input, plan *WorkflowPlan, reviewRequired bool) error {
	if plan.SchemaVersion != WorkflowPlanSchemaVersion {
		return fmt.Errorf("unsupported workflow plan schema_version %d", plan.SchemaVersion)
	}
	reconcileTerminalAndRoles(plan)
	if strings.TrimSpace(plan.Goal) == "" {
		return fmt.Errorf("workflow goal is required")
	}
	if input.CoordinatorAgentID == uuid.Nil || strings.TrimSpace(input.CoordinatorAgentKey) == "" {
		return fmt.Errorf("canonical team coordinator is unavailable")
	}
	if plan.CoordinatorAgentID != input.CoordinatorAgentID || !strings.EqualFold(strings.TrimSpace(plan.CoordinatorAgentKey), input.CoordinatorAgentKey) {
		return fmt.Errorf("workflow coordinator does not match canonical team lead")
	}
	coordinator, ok := canonicalTeamProfile(input, plan.CoordinatorAgentID, plan.CoordinatorAgentKey)
	if !ok || coordinator.AgentID != input.CoordinatorAgentID {
		return fmt.Errorf("workflow coordinator is not the canonical team lead")
	}
	if len(plan.Steps) < 2 || len(plan.Steps) > MaxWorkflowSteps {
		return fmt.Errorf("workflow steps must be between 2 and %d", MaxWorkflowSteps)
	}
	if !validEnum(strings.ToLower(strings.TrimSpace(plan.ReviewStatus)), "none", "required", "included") {
		return fmt.Errorf("invalid review_status %q", plan.ReviewStatus)
	}

	steps := make(map[string]*WorkflowStep, len(plan.Steps))
	owners := make(map[uuid.UUID]struct{})
	terminalCount := 0
	for i := range plan.Steps {
		step := &plan.Steps[i]
		step.ID = strings.TrimSpace(step.ID)
		if step.ID == "" || strings.TrimSpace(step.Title) == "" || strings.TrimSpace(step.Instruction) == "" {
			return fmt.Errorf("workflow step %d is missing id, title, or instruction", i)
		}
		if _, exists := steps[step.ID]; exists {
			return fmt.Errorf("duplicate workflow step %q", step.ID)
		}
		owner, ok := canonicalTeamProfile(input, step.OwnerAgentID, step.OwnerAgentKey)
		if !ok {
			return fmt.Errorf("step %q owner is not a canonical team member", step.ID)
		}
		if owner.AgentID == input.CoordinatorAgentID {
			return fmt.Errorf("step %q cannot be owned by the team lead coordinator", step.ID)
		}
		step.OwnerAgentID = owner.AgentID
		step.OwnerAgentKey = profileAgentKey(owner)
		owners[owner.AgentID] = struct{}{}
		if len(owners) > MaxWorkflowAgents {
			return fmt.Errorf("workflow exceeds %d distinct agents", MaxWorkflowAgents)
		}
		if err := validateStepCapability(owner, step); err != nil {
			return fmt.Errorf("step %q: %w", step.ID, err)
		}
		if err := validateWorkflowRole(step); err != nil {
			return err
		}
		if err := validateStepTools(owner, step.RequiredTools); err != nil {
			return fmt.Errorf("step %q: %w", step.ID, err)
		}
		step.RequiredTools = compactSortedStrings(step.RequiredTools)
		step.DependsOn = compactSortedStrings(step.DependsOn)
		if step.Terminal {
			terminalCount++
		}
		steps[step.ID] = step
	}
	if len(owners) < 2 {
		return fmt.Errorf("multi_role workflow requires at least two distinct step owners")
	}
	if terminalCount != 1 {
		return fmt.Errorf("workflow requires exactly one terminal step")
	}
	terminal := steps[strings.TrimSpace(plan.TerminalStepID)]
	if terminal == nil || !terminal.Terminal {
		return fmt.Errorf("terminal_step_id must reference the terminal step")
	}
	finalOwner, ok := canonicalTeamProfile(input, plan.FinalOwnerAgentID, plan.FinalOwnerAgentKey)
	if !ok {
		return fmt.Errorf("final owner is not a canonical team member")
	}
	plan.FinalOwnerAgentID = finalOwner.AgentID
	plan.FinalOwnerAgentKey = profileAgentKey(finalOwner)
	if terminal.OwnerAgentID != finalOwner.AgentID {
		return fmt.Errorf("terminal step must be owned by final owner")
	}

	dependents := make(map[string][]string, len(steps))
	indegree := make(map[string]int, len(steps))
	for id := range steps {
		indegree[id] = 0
	}
	for _, step := range plan.Steps {
		for _, dependency := range step.DependsOn {
			if dependency == step.ID {
				return fmt.Errorf("step %q cannot depend on itself", step.ID)
			}
			if steps[dependency] == nil {
				return fmt.Errorf("step %q has unknown dependency %q", step.ID, dependency)
			}
			dependents[dependency] = append(dependents[dependency], step.ID)
			indegree[step.ID]++
		}
	}
	queue := make([]string, 0, len(steps))
	for id, degree := range indegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}
	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range dependents[id] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if visited != len(steps) {
		return fmt.Errorf("workflow contains a dependency cycle")
	}
	for id := range steps {
		if id != terminal.ID && !hasWorkflowPath(id, terminal.ID, dependents) {
			return fmt.Errorf("step %q does not converge to terminal", id)
		}
	}
	if err := validateIndependentReview(input, plan, steps, dependents, reviewRequired); err != nil {
		return err
	}
	return nil
}

func canonicalTeamProfile(input Input, id uuid.UUID, key string) (Profile, bool) {
	key = strings.TrimSpace(key)
	for _, profile := range input.Members {
		if id != uuid.Nil && profile.AgentID == id {
			if key == "" || strings.EqualFold(key, profileAgentKey(profile)) {
				return profile, true
			}
		}
		if id == uuid.Nil && key != "" && strings.EqualFold(key, profileAgentKey(profile)) {
			return profile, true
		}
	}
	return Profile{}, false
}

func validateStepCapability(owner Profile, step *WorkflowStep) error {
	key := strings.ToLower(strings.TrimSpace(step.CapabilityKey))
	if key == "" {
		step.CapabilityKey = "general"
		return nil
	}
	step.CapabilityKey = key
	return nil
}

// workflowRoleSynonyms maps labels the planner reaches for onto the canonical
// enum. workflow_role is BOOKKEEPING — the substantive rules (owner canonicality,
// lead exclusion, DAG convergence, the review chain, tools) read the graph and
// the profiles, not this label. Rejecting a structurally sound plan over the
// wording of a tag threw the whole plan away and degraded the turn to self: live,
// a plan whose review step was tagged "review" instead of "critic" was lost with
// `assignment_revision_failed`. Normalising is safe because the resulting role
// still has to pass the terminal/role agreement check below.
var workflowRoleSynonyms = map[string]string{
	"review":      "critic",
	"reviewer":    "critic",
	"critique":    "critic",
	"qa":          "critic",
	"verify":      "critic",
	"validation":  "critic",
	"integrate":   "integration",
	"integrator":  "integration",
	"synthesis":   "integration",
	"synthesize":  "integration",
	"final":       "integration",
	"drafting":    "draft",
	"author":      "draft",
	"research":    "work",
	"analysis":    "work",
	"execute":     "work",
	"execution":   "work",
	"implement":   "work",
	"development": "work",
}

func validateWorkflowRole(step *WorkflowStep) error {
	role := strings.ToLower(strings.TrimSpace(step.WorkflowRole))
	step.WorkflowRole = role
	if role == "" {
		return nil // Backward compatibility for persisted schema-v1 plans.
	}
	if canonical, ok := workflowRoleSynonyms[role]; ok {
		role = canonical
		step.WorkflowRole = canonical
	}
	if !validEnum(role, "draft", "work", "critic", "integration") {
		return fmt.Errorf("step %q has invalid workflow_role %q", step.ID, role)
	}
	if step.Terminal != (role == "integration") {
		return fmt.Errorf("step %q terminal state conflicts with workflow_role %q", step.ID, role)
	}
	return nil
}

// validateStepTools enforces a positive-presence policy on a step's declared
// required tools: each must actually appear in the owner's available-tool
// snapshot. Unknown ABSENCE is not availability — a required tool that the
// snapshot cannot confirm (nil list, failed roster load, or a tool from a source
// that could not be enumerated) rejects the plan and fails safe to self. Positive
// evidence still passes when an UNRELATED tool source is unknown, because
// builtins and static-server tools remain listed even when a dynamic MCP server
// flips the owner's status to unknown.
func validateStepTools(owner Profile, required []string) error {
	if len(required) == 0 {
		return nil
	}
	available := make(map[string]struct{}, len(owner.AvailableTools))
	for _, name := range owner.AvailableTools {
		available[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	for _, name := range required {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			return fmt.Errorf("required_tools contains an empty name")
		}
		if _, ok := available[name]; !ok {
			return fmt.Errorf("required tool %q is not known available", name)
		}
	}
	return nil
}

// validateIndependentReview enforces what "independent review" actually means,
// not one particular team shape:
//
//  1. some non-terminal step is under review,
//  2. the reviewer is NOT the agent who produced that work, and
//  3. the review result reaches the terminal step, so it cannot be ignored.
//
// It deliberately does NOT require the reviewed step to be owned by the final
// owner. That older rule forced every reviewed workflow into
// drafter==integrator, which rejected the ordinary shape of a team split into
// specialists: A produces, B reviews, C integrates. Independence is a property
// of author-vs-reviewer, not of who happens to integrate at the end.
//
// The critic still may not be the final owner. Otherwise the integrator becomes
// their own reviewer, and because isReviewerStep can infer reviewer intent from
// the OWNER's profile capability alone, a plan with no declared critic at all
// would satisfy a hard review requirement just because the integrator happens to
// be QA-capable. A roster that can staff a reviewer at all can always express the
// alternative (let the producer integrate), so this costs no legitimate shape.
//
// Reachability rather than a direct edge is also deliberate: arbitrarily many
// intermediate steps may sit between the reviewed work and the critic, and
// between the critic and the terminal step, so wide/deep plans (up to
// MaxWorkflowSteps / MaxWorkflowAgents) stay expressible.
func validateIndependentReview(input Input, plan *WorkflowPlan, steps map[string]*WorkflowStep, dependents map[string][]string, reviewRequired bool) error {
	if !reviewRequired {
		return nil
	}
	profiles := make(map[uuid.UUID]Profile, len(input.Members)+len(input.Delegates)+1)
	profiles[input.CurrentAgent.AgentID] = input.CurrentAgent
	for _, profile := range input.Members {
		profiles[profile.AgentID] = profile
	}
	for _, profile := range input.Delegates {
		profiles[profile.AgentID] = profile
	}
	var criticFound bool
	var reviewerDiagnostics []string
	for i := range plan.Steps {
		step := &plan.Steps[i]
		// The critic may not be the final owner (see the fail-open note above) and a
		// terminal step reviews nothing upstream of itself.
		if step.OwnerAgentID == plan.FinalOwnerAgentID || step.Terminal || !isReviewerStep(*step, profiles[step.OwnerAgentID]) {
			continue
		}
		// Something upstream must actually be under review, and its author must not
		// be the reviewer. Reachability (not a direct edge) is deliberate: any number
		// of intermediate steps may sit between the reviewed work and the critic.
		reviewsOthersWork := false
		for _, upstream := range steps {
			if upstream.ID == step.ID || upstream.Terminal {
				continue
			}
			if upstream.OwnerAgentID != step.OwnerAgentID && hasWorkflowPath(upstream.ID, step.ID, dependents) {
				reviewsOthersWork = true
				break
			}
		}
		reachesTerminal := hasWorkflowPath(step.ID, plan.TerminalStepID, dependents)
		if reviewsOthersWork && reachesTerminal {
			criticFound = true
			break
		}
		reviewerDiagnostics = append(reviewerDiagnostics, fmt.Sprintf("%s(owner=%s,reviews_others_work=%t,reaches_terminal=%t)", step.ID, step.OwnerAgentKey, reviewsOthersWork, reachesTerminal))
	}
	if !criticFound {
		return fmt.Errorf("workflow requires a critic step that reviews work owned by a different agent and whose result reaches the terminal step; final_owner=%s reviewers=[%s]", plan.FinalOwnerAgentKey, strings.Join(reviewerDiagnostics, ","))
	}
	plan.ReviewStatus = "included"
	return nil
}

func isReviewerStep(step WorkflowStep, owner Profile) bool {
	if strings.EqualFold(strings.TrimSpace(step.WorkflowRole), "critic") {
		return true
	}
	key := strings.ToLower(strings.TrimSpace(step.CapabilityKey))
	if key == string(CapabilityQA) || key == string(CapabilityAnalyticsCritic) {
		return true
	}
	return profileHasStructuredCapability(owner, string(CapabilityQA)) || profileHasStructuredCapability(owner, string(CapabilityAnalyticsCritic))
}

func hasWorkflowPath(from, target string, dependents map[string][]string) bool {
	seen := map[string]bool{from: true}
	queue := []string{from}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range dependents[current] {
			if next == target {
				return true
			}
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return false
}

func profileHasStructuredCapability(profile Profile, key string) bool {
	for _, capability := range profile.Capabilities {
		if strings.EqualFold(capability.Key, key) {
			return true
		}
	}
	return false
}

func compactSortedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}
