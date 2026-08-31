package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// createDAGSchemaVersion identifies the coordinator-authored DAG plan shape
// persisted on team_workflows.canonical_plan. create_dag is the only writer of
// this schema; the planner's canonical plan uses its own shape.
const createDAGSchemaVersion = 1

// dagTaskInput is one lead-authored node of a create_dag graph. blocked_by holds
// LOCAL ids that reference other nodes in the same batch; they are mapped to real
// task UUIDs only after the whole graph validates.
type dagTaskInput struct {
	ID          string   `json:"id"`
	Subject     string   `json:"subject"`
	Description string   `json:"description"`
	Assignee    string   `json:"assignee"`
	BlockedBy   []string `json:"blocked_by"`
	Reviews     []string `json:"reviews"`
	Terminal    bool     `json:"terminal"`
}

// executeCreateDAG persists a coordinator-authored multi-task DAG as a durable
// team workflow in one atomic step. The lead supplies the whole graph in a single
// tool call; the backend validates it (roster, dependencies, acyclicity, exactly
// one terminal integration task that is the unique reachable sink) and — only on
// success — persists it via CreateWorkflowWithTasks and queues the root tasks for
// post-turn dispatch. Any validation failure returns one clear error and writes
// nothing.
//
// This action is the runtime authoring surface for coordinated (multi_role)
// routing. It deliberately does NOT reuse executeCreate (so the single-owner
// constraint and search-before-create gate never apply) and does NOT invoke any
// intent/critic/planner/replan/finalizer stage — the lead authors the graph and
// the backend settles it deterministically.
func (t *TeamTasksTool) executeCreateDAG(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.ResolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}
	// create_dag is coordinator-only and deliberately does NOT inherit RequireLead's
	// teammate/system channel bypass. A classified coordinated request already
	// executes as the canonical team lead, so the caller's agent ID must BE the lead
	// — no channel can widen that. This server-side check keeps members (and a
	// spoofed system/teammate channel) from authoring a multi-task workflow.
	if agentID != team.LeadAgentID {
		return ErrorResult("create_dag requires the team lead. As a member, create a single request task instead.")
	}
	// create_dag must run inside an accepted classification gate. The gate records
	// the append-only audit row that justified this workflow and puts its ID on the
	// run context; outside that gate there is no audit ID, so we fail closed rather
	// than persist an un-justified workflow. The exact ID is persisted below.
	auditID := TeamWorkClassificationAuditIDFromCtx(ctx)
	if auditID == uuid.Nil {
		return ErrorResult("create_dag can only run from an accepted team-work classification gate (no classification audit id on this run)")
	}
	workflowStore, wfErr := t.workflowStore()
	if wfErr != nil {
		return ErrorResult(wfErr.Error())
	}
	runID := strings.TrimSpace(ToolRunIDFromCtx(ctx))
	if runID == "" {
		return ErrorResult("create_dag requires an active run context; it cannot be used here")
	}

	inputs, parseErr := parseDAGTasks(args)
	if parseErr != nil {
		return ErrorResult(parseErr.Error())
	}
	if len(inputs) < 2 {
		return ErrorResult("create_dag requires at least 2 tasks (one alone is just a normal task — use action=create)")
	}
	if err := validateDAGGraph(inputs); err != nil {
		return ErrorResult(err.Error())
	}

	members, err := t.manager.CachedListMembers(ctx, team.ID, agentID)
	if err != nil {
		return ErrorResult("failed to load team roster: " + err.Error())
	}
	memberIDs := make(map[uuid.UUID]struct{}, len(members))
	for _, m := range members {
		memberIDs[m.AgentID] = struct{}{}
	}

	// Resolve every assignee and assign real UUIDs + owner IDs before touching the
	// store, so a bad assignee fails the whole batch with zero rows persisted.
	idsByLocal := make(map[string]uuid.UUID, len(inputs))
	for _, in := range inputs {
		idsByLocal[in.ID] = store.GenNewID()
	}
	ownerIDs := make([]uuid.UUID, len(inputs))
	for i, in := range inputs {
		key := strings.TrimSpace(in.Assignee)
		if key == "" {
			return ErrorResult(fmt.Sprintf("task %q has no assignee — every DAG task needs one", in.ID))
		}
		ownerID, resolveErr := t.manager.ResolveAgentByKey(ctx, key)
		if resolveErr != nil {
			return ErrorResult(fmt.Sprintf("task %q assignee %q not found: %v", in.ID, key, resolveErr))
		}
		if in.Terminal {
			// The terminal integration task may be owned by the lead or a member.
			if ownerID != team.LeadAgentID {
				if _, ok := memberIDs[ownerID]; !ok {
					return ErrorResult(fmt.Sprintf("task %q assignee %q is not a member of this team", in.ID, key))
				}
			}
		} else {
			// Non-terminal work is dispatched to members; the lead cannot run it
			// (DispatchUnblockedTasks auto-fails non-terminal lead-owned tasks).
			if ownerID == team.LeadAgentID {
				return ErrorResult(fmt.Sprintf("task %q assigns the lead to a non-terminal step — only the terminal integration task may be owned by the lead", in.ID))
			}
			if _, ok := memberIDs[ownerID]; !ok {
				return ErrorResult(fmt.Sprintf("task %q assignee %q is not a member of this team", in.ID, key))
			}
		}
		ownerIDs[i] = ownerID
	}

	if err := validateReviewTasks(inputs, ownerIDs, TeamWorkReviewRequiredFromCtx(ctx)); err != nil {
		return ErrorResult(err.Error())
	}

	workflow := buildDAGWorkflow(ctx, team, agentID, runID, auditID, inputs)
	tasks := buildDAGWorkflowTasks(workflow, inputs, idsByLocal, ownerIDs)

	if err := workflowStore.CreateWorkflowWithTasks(ctx, workflow, tasks); err != nil {
		return ErrorResult("failed to create workflow DAG: " + err.Error())
	}

	// Dispatch after the seal: queue the root (unblocked) work tasks for the
	// post-turn drain, which claims them through the workflow CAS dispatch path.
	queueWorkflowRoots(ctx, team.ID, tasks)

	agentKey := t.manager.AgentKeyFromID(ctx, agentID)
	for i := range tasks {
		task := &tasks[i]
		t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskCreated, BuildTaskEventPayload(
			team.ID.String(), task.ID.String(),
			task.Status,
			"agent", agentKey,
			WithSubject(task.Subject),
			WithContextInfo(ctx),
		))
	}

	return NewResult(fmt.Sprintf("Workflow DAG created (workflow_id=%s, tasks=%d, terminal=%s). The backend will dispatch the unblocked tasks and deliver only the terminal integration result to the requester.",
		workflow.ID, len(tasks), inputs[terminalDAGIndex(inputs)].ID))
}

func parseDAGTasks(args map[string]any) ([]dagTaskInput, error) {
	raw, ok := args["tasks"].([]any)
	if !ok {
		return nil, fmt.Errorf("create_dag requires a tasks array")
	}
	inputs := make([]dagTaskInput, 0, len(raw))
	for i, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tasks[%d] must be an object", i)
		}
		// Every field is required and read with its expected type. Deliberately no
		// fmt.Sprint coercion: a missing field must fail the batch, not stringify to
		// "<nil>" and sail through validation.
		id, err := requiredDAGString(obj, "id")
		if err != nil {
			return nil, fmt.Errorf("tasks[%d]: %v", i, err)
		}
		subject, err := requiredDAGString(obj, "subject")
		if err != nil {
			return nil, fmt.Errorf("task %q: %v", id, err)
		}
		description, err := requiredDAGString(obj, "description")
		if err != nil {
			return nil, fmt.Errorf("task %q: %v", id, err)
		}
		assignee, err := requiredDAGString(obj, "assignee")
		if err != nil {
			return nil, fmt.Errorf("task %q: %v", id, err)
		}
		blockedBy, err := requiredDAGBlockedBy(obj)
		if err != nil {
			return nil, fmt.Errorf("task %q: %v", id, err)
		}
		terminal, err := requiredDAGBool(obj, "terminal")
		if err != nil {
			return nil, fmt.Errorf("task %q: %v", id, err)
		}
		reviews, err := optionalDAGStringArray(obj, "reviews")
		if err != nil {
			return nil, fmt.Errorf("task %q: %v", id, err)
		}
		inputs = append(inputs, dagTaskInput{
			ID:          id,
			Subject:     subject,
			Description: description,
			Assignee:    assignee,
			BlockedBy:   blockedBy,
			Reviews:     reviews,
			Terminal:    terminal,
		})
	}
	return inputs, nil
}

// requiredDAGString reads a required string field, rejecting a missing key or a
// non-string/blank value. Returning an error (rather than coercing with
// fmt.Sprint) is what makes a dropped field fail the whole batch closed.
func requiredDAGString(obj map[string]any, key string) (string, error) {
	raw, present := obj[key]
	if !present {
		return "", fmt.Errorf("missing required %q", key)
	}
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%q must be a string", key)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("%q must not be empty", key)
	}
	return s, nil
}

// requiredDAGBool reads the required terminal flag. It accepts a real boolean
// only — a missing field or a string like "true" fails the batch so a terminal
// can never be dropped by accident.
func requiredDAGBool(obj map[string]any, key string) (bool, error) {
	raw, present := obj[key]
	if !present {
		return false, fmt.Errorf("missing required %q", key)
	}
	b, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf("%q must be a boolean", key)
	}
	return b, nil
}

// requiredDAGBlockedBy reads the required dependency list. blocked_by is a typed
// field: the key MUST be present and its value MUST be an array. An empty array
// means "root task" (no dependencies). A missing key, null, a string, or any
// non-array value fails the batch — a malformed field is never silently treated
// as an empty dependency list. Each entry must be a non-empty string id.
func requiredDAGBlockedBy(obj map[string]any) ([]string, error) {
	raw, present := obj["blocked_by"]
	if !present {
		return nil, fmt.Errorf("missing required \"blocked_by\" (use [] for a root task)")
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("blocked_by must be an array of task ids ([] for a root task)")
	}
	out := make([]string, 0, len(arr))
	for _, dep := range arr {
		s, ok := dep.(string)
		if !ok {
			return nil, fmt.Errorf("blocked_by entries must be strings")
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, fmt.Errorf("blocked_by contains an empty dependency id")
		}
		out = append(out, s)
	}
	return out, nil
}

// optionalDAGStringArray reads an optional string-array field (reviews). Absent
// returns an empty slice; present-but-non-array or non-string entries fail the
// batch so a malformed reviews list is never silently treated as empty.
func optionalDAGStringArray(obj map[string]any, key string) ([]string, error) {
	raw, present := obj[key]
	if !present || raw == nil {
		return nil, nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%q must be an array of task ids", key)
	}
	out := make([]string, 0, len(arr))
	for _, entry := range arr {
		s, ok := entry.(string)
		if !ok {
			return nil, fmt.Errorf("%q entries must be strings", key)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, fmt.Errorf("%q contains an empty id", key)
		}
		out = append(out, s)
	}
	return out, nil
}

// validateDAGGraph enforces the coordinator DAG invariants on the lead-authored
// graph before anything is persisted: unique local ids, dependencies that resolve
// within the batch with no self-blocks, acyclicity, exactly one terminal task,
// and a terminal that is the unique sink reachable from every other task.
func validateDAGGraph(inputs []dagTaskInput) error {
	indexByID := make(map[string]int, len(inputs))
	for i, in := range inputs {
		if _, dup := indexByID[in.ID]; dup {
			return fmt.Errorf("duplicate task id %q — every task needs a unique local id", in.ID)
		}
		indexByID[in.ID] = i
	}
	// dependents[id] = tasks that list id in blocked_by. A task with no dependents
	// is a sink.
	dependents := make(map[string][]string, len(inputs))
	indegree := make([]int, len(inputs))
	for i, in := range inputs {
		seen := make(map[string]bool, len(in.BlockedBy))
		for _, dep := range in.BlockedBy {
			if dep == in.ID {
				return fmt.Errorf("task %q blocks on itself", in.ID)
			}
			if _, ok := indexByID[dep]; !ok {
				return fmt.Errorf("task %q blocked_by references unknown task %q (dependencies must be ids within this same batch)", in.ID, dep)
			}
			if seen[dep] {
				return fmt.Errorf("task %q lists %q twice in blocked_by", in.ID, dep)
			}
			seen[dep] = true
			dependents[dep] = append(dependents[dep], in.ID)
			indegree[i]++
		}
	}
	// Kahn's algorithm: a DAG processes every node; a leftover node means a cycle.
	queue := make([]int, 0, len(inputs))
	for i, deg := range indegree {
		if deg == 0 {
			queue = append(queue, i)
		}
	}
	processed := 0
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		processed++
		for _, child := range dependents[inputs[n].ID] {
			ci := indexByID[child]
			indegree[ci]--
			if indegree[ci] == 0 {
				queue = append(queue, ci)
			}
		}
	}
	if processed != len(inputs) {
		return fmt.Errorf("the task graph has a dependency cycle — remove it so every task can complete")
	}
	// Exactly one terminal, and it must be the unique sink (the only task nothing
	// depends on). If any non-terminal has no dependents it is a dead end, and the
	// terminal would not be the single deliverable result. terminalDAGIndex returns
	// -1 for none and -2 for more than one; report each accurately rather than
	// conflating them.
	switch terminalIdx := terminalDAGIndex(inputs); {
	case terminalIdx == dagTerminalMultiple:
		return fmt.Errorf("exactly one task must have terminal=true (the integration task); found more than one")
	case terminalIdx == dagTerminalNone:
		return fmt.Errorf("exactly one task must have terminal=true (the integration task); none was marked")
	default:
		sinks := 0
		for _, in := range inputs {
			if len(dependents[in.ID]) == 0 {
				sinks++
				if in.ID != inputs[terminalIdx].ID {
					return fmt.Errorf("task %q has no dependent and is not the terminal task — every task must lead to the single terminal integration task", in.ID)
				}
			}
		}
		if sinks != 1 {
			return fmt.Errorf("the terminal task must be the unique endpoint of the graph; found %d tasks with no dependents", sinks)
		}
	}
	return nil
}

const (
	// dagTerminalNone is returned by terminalDAGIndex when no task is terminal.
	dagTerminalNone = -1
	// dagTerminalMultiple is returned by terminalDAGIndex when more than one task
	// is terminal.
	dagTerminalMultiple = -2
)

func terminalDAGIndex(inputs []dagTaskInput) int {
	idx := dagTerminalNone
	for i, in := range inputs {
		if in.Terminal {
			if idx >= 0 {
				return dagTerminalMultiple // more than one terminal
			}
			idx = i
		}
	}
	return idx
}

// validateReviewTasks enforces explicit review-task integrity; a task is a review task iff it declares a non-empty reviews list.
func validateReviewTasks(inputs []dagTaskInput, ownerIDs []uuid.UUID, reviewRequired bool) error {
	indexByID := make(map[string]int, len(inputs))
	dependents := make(map[string][]string, len(inputs))
	for i, in := range inputs {
		indexByID[in.ID] = i
	}
	for _, in := range inputs {
		for _, dep := range in.BlockedBy {
			dependents[dep] = append(dependents[dep], in.ID)
		}
	}
	terminalIdx := terminalDAGIndex(inputs)
	hasReviewTask := false
	for i, in := range inputs {
		if len(in.Reviews) == 0 {
			continue
		}
		hasReviewTask = true
		if in.Terminal {
			return fmt.Errorf("task %q is terminal but declares reviews — a critic cannot be the integration task", in.ID)
		}
		seen := make(map[string]struct{}, len(in.Reviews))
		for _, reviewed := range in.Reviews {
			if _, dup := seen[reviewed]; dup {
				return fmt.Errorf("task %q reviews %q more than once", in.ID, reviewed)
			}
			seen[reviewed] = struct{}{}
			if reviewed == in.ID {
				return fmt.Errorf("task %q cannot review itself", in.ID)
			}
			j, ok := indexByID[reviewed]
			if !ok || inputs[j].Terminal {
				return fmt.Errorf("task %q reviews %q which is not a non-terminal work task in this batch", in.ID, reviewed)
			}
			if ownerIDs[i] == ownerIDs[j] {
				return fmt.Errorf("task %q reviews %q but both are owned by %q — an independent reviewer cannot be the author of the work it reviews", in.ID, reviewed, inputs[j].Assignee)
			}
			if !hasDAGPath(reviewed, in.ID, dependents) {
				return fmt.Errorf("task %q reviews %q but %q does not precede it in the graph — add a blocked_by chain from the reviewed task to the review task", in.ID, reviewed, reviewed)
			}
		}
		if terminalIdx < 0 || !hasDAGPath(in.ID, inputs[terminalIdx].ID, dependents) {
			return fmt.Errorf("review task %q does not reach the terminal integration task — the review result must flow to the integrator", in.ID)
		}
	}
	if !reviewRequired && hasReviewTask {
		return fmt.Errorf("review_required is false but a review task was declared — remove the reviews list unless the system classified this request as requiring independent review")
	}
	if reviewRequired && !hasReviewTask {
		return fmt.Errorf("review_required was set but no task declares a reviews list — add a critic task that reviews at least one producer")
	}
	return nil
}

// hasDAGPath reports whether target is reachable from start along blocked_by edges (dependents map producer -> downstream tasks).
func hasDAGPath(start, target string, dependents map[string][]string) bool {
	visited := map[string]struct{}{start: {}}
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == target {
			return true
		}
		for _, next := range dependents[cur] {
			if _, seen := visited[next]; !seen {
				visited[next] = struct{}{}
				queue = append(queue, next)
			}
		}
	}
	return false
}

// buildDAGWorkflow assembles the durable workflow row for a coordinator DAG. The
// canonical plan is the normalized lead-authored graph and plan_hash is its
// sha256 — both required by prepareWorkflow and used so finalization/recovery
// never depend on the classifier context that created the workflow. The auditID is
// the exact classification audit row that justified this workflow (guaranteed
// non-Nil by the caller's fail-closed gate) and is persisted verbatim.
func buildDAGWorkflow(ctx context.Context, team *store.TeamData, leadID uuid.UUID, runID string, auditID uuid.UUID, inputs []dagTaskInput) *store.TeamWorkflowData {
	canonical, _ := json.Marshal(inputs)
	sum := sha256.Sum256(canonical)
	audit := auditID
	// OriginRouting carries the coordinator_dag marker AND the requester locale so
	// a failed/cancelled DAG's deterministic summary renders in the requester's
	// language (workflowLocale reads the locale key here before falling back to
	// Vietnamese-detection on the canonical plan — which is a flat task array for
	// DAGs and cannot yield prose). Downstream consumers of MetaOriginRouting read
	// the routing string verbatim, so adding a key is backward compatible.
	routing := map[string]any{
		"coordinator_dag": true,
		"locale":          store.LocaleFromContext(ctx),
	}
	originRouting, _ := json.Marshal(routing)
	workflow := &store.TeamWorkflowData{
		TeamID:                team.ID,
		Status:                store.TeamWorkflowStatusRunning,
		CanonicalPlan:         canonical,
		SchemaVersion:         createDAGSchemaVersion,
		PlanHash:              hex.EncodeToString(sum[:]),
		CoordinatorAgentID:    team.LeadAgentID,
		CoordinatorAgentKey:   team.LeadAgentKey,
		OriginAgentID:         leadID,
		OriginAgentKey:        team.LeadAgentKey,
		OriginRunID:           runID,
		OriginSessionKey:      ToolSessionKeyFromCtx(ctx),
		OriginChannel:         ToolChannelFromCtx(ctx),
		OriginChatID:          ToolChatIDFromCtx(ctx),
		OriginPeerKind:        ToolPeerKindFromCtx(ctx),
		OriginLocalKey:        ToolLocalKeyFromCtx(ctx),
		OriginUserID:          store.UserIDFromContext(ctx),
		OriginSenderID:        store.SenderIDFromContext(ctx),
		OriginRole:            store.RoleFromContext(ctx),
		OriginRouting:         originRouting,
		ClassificationAuditID: &audit,
	}
	return workflow
}

func buildDAGWorkflowTasks(workflow *store.TeamWorkflowData, inputs []dagTaskInput, idsByLocal map[string]uuid.UUID, ownerIDs []uuid.UUID) []store.TeamTaskData {
	tasks := make([]store.TeamTaskData, 0, len(inputs))
	for i, in := range inputs {
		blockedBy := make([]uuid.UUID, 0, len(in.BlockedBy))
		for _, dep := range in.BlockedBy {
			blockedBy = append(blockedBy, idsByLocal[dep])
		}
		status := store.TeamTaskStatusPending
		if len(blockedBy) > 0 {
			status = store.TeamTaskStatusBlocked
		}
		ownerID := ownerIDs[i]
		task := store.TeamTaskData{
			BaseModel:        store.BaseModel{ID: idsByLocal[in.ID]},
			TeamID:           workflow.TeamID,
			Subject:          in.Subject,
			Description:      in.Description,
			Status:           status,
			OwnerAgentID:     &ownerID,
			BlockedBy:        blockedBy,
			TaskType:         "general",
			CreatedByAgentID: &workflow.OriginAgentID,
			UserID:           workflow.OriginUserID,
			Channel:          workflow.OriginChannel,
			ChatID:           workflow.OriginChatID,
			WorkflowStepID:   in.ID,
			WorkflowKind:     store.TeamWorkflowTaskKindWork,
			WorkflowTerminal: in.Terminal,
			Metadata:         dagTaskMetadata(workflow, blockedBy, in.Reviews),
		}
		tasks = append(tasks, task)
	}
	return tasks
}

// dagTaskMetadata builds the persisted per-task metadata; reviews is stored only when the task is an explicit critic.
func dagTaskMetadata(workflow *store.TeamWorkflowData, blockedBy []uuid.UUID, reviews []string) map[string]any {
	metadata := map[string]any{
		TaskMetaOriginSession: workflow.OriginSessionKey,
		TaskMetaPeerKind:      workflow.OriginPeerKind,
		TaskMetaLocalKey:      workflow.OriginLocalKey,
		"origin_sender_id":    workflow.OriginSenderID,
		"origin_role":         workflow.OriginRole,
		TaskMetaOriginRouting: string(workflow.OriginRouting),
	}
	if len(blockedBy) > 0 {
		ids := make([]string, len(blockedBy))
		for i, id := range blockedBy {
			ids[i] = id.String()
		}
		metadata["original_blocked_by"] = ids
	}
	if len(reviews) > 0 {
		metadata["reviews"] = append([]string{}, reviews...)
	}
	return metadata
}
