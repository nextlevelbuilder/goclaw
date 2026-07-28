package teamworkclassify

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// TestBuildClassificationAuditMapsMultiRolePlan proves an accepted multi_role
// result maps its verified shape, modes, review flag, frozen plan hash and the
// plan's coordinator/final owner into the audit — and that raw text never
// leaks: hashes are stored (not the messages), and trait evidence is dropped.
func TestBuildClassificationAuditMapsMultiRolePlan(t *testing.T) {
	coord, owner, agent := uuid.New(), uuid.New(), uuid.New()
	original := "  compare these three vendors and have someone independently verify the pick  "
	resolved := "Compare vendors A/B/C on cost and reliability, then independently verify the recommendation."

	result := Result{
		StandaloneRequest:       resolved,
		VerifiedWorkShape:       WorkShapeReviewedDecision,
		RequestedWorkflowMode:   WorkflowModeMultiRole,
		EffectiveWorkflowMode:   WorkflowModeMultiRole,
		EffectiveReviewRequired: true,
		ShapeTraits: []ShapeTrait{
			{Type: ShapeTraitMultipleCapabilities, Source: ShapeEvidenceCurrentRequest, Evidence: "compare these three vendors"},
			{Type: ShapeTraitIndependentVerification, Source: ShapeEvidenceCurrentRequest, Evidence: "independently verify the pick"},
		},
		Plan: &WorkflowPlan{CoordinatorAgentID: coord, FinalOwnerAgentID: owner},
	}

	audit := BuildClassificationAudit(ClassificationAuditInput{
		Ingress:            store.TeamWorkIngressWS,
		RunID:              "workflow-step:wf:1:tok",
		SessionKey:         "agent:lead:ws:direct:user",
		AgentID:            &agent,
		OriginalMessage:    original,
		PlanHash:           "planhashabc",
		ClassifierProvider: "test",
		ClassifierModel:    "test-model",
	}, result)

	if audit.VerifiedShape != string(WorkShapeReviewedDecision) {
		t.Fatalf("verified shape = %q", audit.VerifiedShape)
	}
	if audit.RequestedMode != store.TeamWorkModeMultiRole || audit.EffectiveMode != store.TeamWorkModeMultiRole {
		t.Fatalf("modes = (%q,%q)", audit.RequestedMode, audit.EffectiveMode)
	}
	if !audit.IndependentReview {
		t.Fatal("independent review must be recorded true")
	}
	if audit.PlanHash != "planhashabc" {
		t.Fatalf("plan hash = %q", audit.PlanHash)
	}
	if audit.CoordinatorAgentID == nil || *audit.CoordinatorAgentID != coord {
		t.Fatal("coordinator must map from plan")
	}
	if audit.SelectedOwnerAgentID == nil || *audit.SelectedOwnerAgentID != owner {
		t.Fatal("selected owner must map from plan final owner")
	}

	// No-raw guarantee: original/resolved are stored as trimmed sha256 hashes.
	if audit.OriginalHash != hashOf(strings.TrimSpace(original)) {
		t.Fatalf("original hash mismatch: %q", audit.OriginalHash)
	}
	if audit.ResolvedHash != hashOf(resolved) {
		t.Fatalf("resolved hash mismatch: %q", audit.ResolvedHash)
	}
	if audit.OriginalHash == audit.ResolvedHash {
		t.Fatal("original and resolved hashes must differ for distinct messages")
	}

	// Trait evidence (a literal excerpt of the user's message) must be dropped;
	// only type + source survive.
	rawTraits := string(audit.Traits)
	if strings.Contains(rawTraits, "compare these three vendors") || strings.Contains(rawTraits, "independently verify") {
		t.Fatalf("trait evidence (raw user content) leaked into audit: %s", rawTraits)
	}
	var traits []map[string]string
	if err := json.Unmarshal(audit.Traits, &traits); err != nil {
		t.Fatalf("unmarshal traits: %v", err)
	}
	if len(traits) != 2 {
		t.Fatalf("trait count = %d", len(traits))
	}
	if traits[0]["type"] != string(ShapeTraitMultipleCapabilities) || traits[0]["source"] != string(ShapeEvidenceCurrentRequest) {
		t.Fatalf("trait[0] = %v", traits[0])
	}
	if _, ok := traits[0]["evidence"]; ok {
		t.Fatal("trait must not serialize an evidence field")
	}

	// Accepted (non-degraded) run: no degraded stage/reason, outcome accepted.
	if audit.DegradedStage != "" || audit.DegradedReason != "" {
		t.Fatalf("accepted run must not be degraded: stage=%q reason=%q", audit.DegradedStage, audit.DegradedReason)
	}
	var stages map[string]string
	if err := json.Unmarshal(audit.StageStatuses, &stages); err != nil {
		t.Fatalf("unmarshal stage_statuses: %v", err)
	}
	if stages["outcome"] != "accepted" {
		t.Fatalf("stage outcome = %q, want accepted", stages["outcome"])
	}
}

// TestBuildClassificationAuditMapsSingleOwner proves a single_owner routing
// decision (no plan) records the best team owner as the selected owner and no
// coordinator, with an empty plan hash.
func TestBuildClassificationAuditMapsSingleOwner(t *testing.T) {
	owner := uuid.New()
	result := Result{
		StandaloneRequest:     "draft the Q3 report",
		VerifiedWorkShape:     WorkShapeAtomic,
		RequestedWorkflowMode: WorkflowModeSingleOwner,
		EffectiveWorkflowMode: WorkflowModeSingleOwner,
		BestTeamOwnerID:       owner,
	}
	audit := BuildClassificationAudit(ClassificationAuditInput{Ingress: store.TeamWorkIngressInbound}, result)

	if audit.EffectiveMode != store.TeamWorkModeSingleOwner {
		t.Fatalf("effective mode = %q", audit.EffectiveMode)
	}
	if audit.SelectedOwnerAgentID == nil || *audit.SelectedOwnerAgentID != owner {
		t.Fatal("single_owner must record best team owner")
	}
	if audit.CoordinatorAgentID != nil {
		t.Fatal("single_owner must not record a coordinator")
	}
	if audit.PlanHash != "" {
		t.Fatalf("single_owner plan hash = %q, want empty", audit.PlanHash)
	}
	if audit.IndependentReview {
		t.Fatal("single_owner without review must record independent_review=false")
	}
}

// TestBuildClassificationAuditDegradedStages proves a degraded (fail-safe) self
// result records the failing stage derived from the reason code, and that empty
// messages hash to empty strings.
func TestBuildClassificationAuditDegradedStages(t *testing.T) {
	cases := []struct {
		reason    string
		wantStage string
	}{
		{"intent_resolver_timeout", "intent_resolver"},
		{"intent_critic_rejected", "intent_critic"},
		{"intent_clarification_required", "intent_critic"},
		{"shape_verifier_parse_failed", "shape_verifier"},
		{"classifier_parse_failed", "work_assessment"},
		{"planner_validation_failed", "planning"},
		{"assignment_revision_failed", "assignment_critic"},
		{"insufficient_canonical_members", "planning"},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			result := Result{
				DegradedWorkflow:      true,
				DegradedReasonCode:    tc.reason,
				WorkflowMode:          WorkflowModeSelf,
				RequestedWorkflowMode: WorkflowModeSelf,
				EffectiveWorkflowMode: WorkflowModeSelf,
			}
			audit := BuildClassificationAudit(ClassificationAuditInput{Ingress: store.TeamWorkIngressWS}, result)
			if audit.DegradedStage != tc.wantStage {
				t.Fatalf("reason %q → stage %q, want %q", tc.reason, audit.DegradedStage, tc.wantStage)
			}
			if audit.DegradedReason != tc.reason {
				t.Fatalf("degraded reason = %q", audit.DegradedReason)
			}
			var stages map[string]string
			if err := json.Unmarshal(audit.StageStatuses, &stages); err != nil {
				t.Fatalf("unmarshal stage_statuses: %v", err)
			}
			if stages["outcome"] != "degraded" || stages["reason_code"] != tc.reason {
				t.Fatalf("stage_statuses = %v", stages)
			}
			if audit.EffectiveMode != store.TeamWorkModeSelf {
				t.Fatalf("degraded effective mode = %q, want self", audit.EffectiveMode)
			}
			// Empty messages hash to empty strings (no spurious hash of "").
			if audit.OriginalHash != "" || audit.ResolvedHash != "" {
				t.Fatalf("empty messages must hash to empty: (%q,%q)", audit.OriginalHash, audit.ResolvedHash)
			}
		})
	}
}
