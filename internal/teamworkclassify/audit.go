package teamworkclassify

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ClassificationAuditInput carries the non-Result request metadata an audit
// record needs. OriginalMessage is hashed, never stored raw; PlanHash is the
// already-frozen canonical hash from the gate (empty for self/single_owner).
type ClassificationAuditInput struct {
	Ingress            string
	RunID              string
	SessionKey         string
	AgentID            *uuid.UUID
	OriginalMessage    string
	PlanHash           string
	ClassifierProvider string
	ClassifierModel    string
}

// BuildClassificationAudit maps a classifier Result plus request metadata into
// an append-only audit record for over-selection / degradation measurement. It
// hashes the original and resolved requests (never storing raw text), records
// the verified work shape and trait TYPES (dropping each trait's quoted
// evidence, which is raw user content), the requested vs effective staffing
// mode, the effective independent-review requirement, the selected
// owner/coordinator, the frozen plan hash, and the degraded stage/reason code.
// It persists no prompts, provider payloads, or credentials.
func BuildClassificationAudit(in ClassificationAuditInput, result Result) *store.TeamWorkClassificationAudit {
	audit := &store.TeamWorkClassificationAudit{
		Ingress:            in.Ingress,
		RunID:              in.RunID,
		SessionKey:         in.SessionKey,
		AgentID:            in.AgentID,
		OriginalHash:       hashRequest(in.OriginalMessage),
		ResolvedHash:       hashRequest(result.StandaloneRequest),
		VerifiedShape:      string(result.VerifiedWorkShape),
		Traits:             marshalAuditTraits(result.ShapeTraits),
		RequestedMode:      normalizeAuditMode(result.RequestedWorkflowMode),
		EffectiveMode:      normalizeAuditMode(result.EffectiveWorkflowMode),
		IndependentReview:  result.EffectiveReviewRequired,
		PlanHash:           in.PlanHash,
		ClassifierProvider: in.ClassifierProvider,
		ClassifierModel:    in.ClassifierModel,
	}
	audit.StageStatuses, audit.DegradedStage, audit.DegradedReason = auditStageStatuses(result)
	setAuditOwners(audit, result)
	return audit
}

// hashRequest returns the hex sha256 of a request, or "" when empty, so the
// audit links original↔resolved without duplicating raw message text.
func hashRequest(msg string) string {
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	return hex.EncodeToString(sum[:])
}

// normalizeAuditMode maps a WorkflowMode onto the store's audit mode enum,
// returning "" for anything outside the CHECK-constrained set.
func normalizeAuditMode(mode WorkflowMode) string {
	switch mode {
	case WorkflowModeSelf:
		return store.TeamWorkModeSelf
	case WorkflowModeSingleOwner:
		return store.TeamWorkModeSingleOwner
	case WorkflowModeMultiRole:
		return store.TeamWorkModeMultiRole
	default:
		return ""
	}
}

// marshalAuditTraits serializes only each trait's type and evidence source. The
// quoted evidence string is intentionally dropped: it is a literal excerpt of
// the user's message, so persisting it would store raw prompt content.
func marshalAuditTraits(traits []ShapeTrait) json.RawMessage {
	if len(traits) == 0 {
		return nil
	}
	type auditTrait struct {
		Type   string `json:"type"`
		Source string `json:"source"`
	}
	out := make([]auditTrait, 0, len(traits))
	for _, t := range traits {
		out = append(out, auditTrait{Type: string(t.Type), Source: string(t.Source)})
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return raw
}

// setAuditOwners records the selected owner and coordinator. A multi_role plan
// carries both (final owner + coordinator); a single_owner decision carries the
// best team owner; self carries neither.
func setAuditOwners(audit *store.TeamWorkClassificationAudit, result Result) {
	if result.Plan != nil {
		if result.Plan.CoordinatorAgentID != uuid.Nil {
			id := result.Plan.CoordinatorAgentID
			audit.CoordinatorAgentID = &id
		}
		if result.Plan.FinalOwnerAgentID != uuid.Nil {
			id := result.Plan.FinalOwnerAgentID
			audit.SelectedOwnerAgentID = &id
		}
		return
	}
	if result.BestTeamOwnerID != uuid.Nil {
		id := result.BestTeamOwnerID
		audit.SelectedOwnerAgentID = &id
	}
}

// auditStageStatuses summarizes the classification outcome for the JSON
// stage_statuses column and derives the degraded stage/reason. Result does not
// track a PASS per stage, so an accepted run reports {"outcome":"accepted"} and
// a degraded run reports the failed stage plus its bounded reason code — enough
// to measure degradation rate by stage without persisting raw content.
func auditStageStatuses(result Result) (json.RawMessage, string, string) {
	if !result.DegradedWorkflow {
		raw, _ := json.Marshal(map[string]string{"outcome": "accepted"})
		return raw, "", ""
	}
	stage := degradedStageFromReason(result.DegradedReasonCode)
	status := map[string]string{
		"outcome":     "degraded",
		"reason_code": result.DegradedReasonCode,
	}
	if stage != "" {
		status["failed_stage"] = stage
	}
	raw, _ := json.Marshal(status)
	return raw, stage, result.DegradedReasonCode
}

// degradedStageFromReason maps a degraded reason code onto the pipeline stage
// that produced it, so audits can be grouped by failing stage. Codes come from
// intentFailureReason / shapeFailureReason / callFailureReason and the
// stage-specific literals raised in ClassifyWithLLM.
func degradedStageFromReason(code string) string {
	switch {
	case strings.HasPrefix(code, "intent_resolver"):
		return "intent_resolver"
	case strings.HasPrefix(code, "intent_critic"), strings.HasPrefix(code, "intent_clarification"):
		return "intent_critic"
	case strings.HasPrefix(code, "shape_verifier"):
		return "shape_verifier"
	case strings.HasPrefix(code, "classifier"):
		return "work_assessment"
	case strings.HasPrefix(code, "planner"):
		return "planning"
	case strings.HasPrefix(code, "assignment"):
		return "assignment_critic"
	case code == "insufficient_canonical_members":
		return "planning"
	default:
		return ""
	}
}
