package pg

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestPGRecordTeamWorkClassificationAuditRoundTrips proves one audit row is
// persisted tenant-scoped from context, that ID/CreatedAt/schema_version are
// populated on the passed record, and that the JSONB trait/stage columns
// round-trip. This is the PG twin of the SQLite parity test (plan §4.2: "Audit
// persist trước schedule").
func TestPGRecordTeamWorkClassificationAuditRoundTrips(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, agentID := seedTenantAndAgent(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM team_work_classification_audits WHERE tenant_id=$1", tenantID) })

	ctx := store.WithTenantID(context.Background(), tenantID)
	s := NewPGTeamStore(db)

	audit := &store.TeamWorkClassificationAudit{
		Ingress:              store.TeamWorkIngressWS,
		RunID:                "workflow-step:wf:1:tok",
		SessionKey:           "agent:lead:ws:direct:user",
		AgentID:              &agentID,
		OriginalHash:         "0011",
		ResolvedHash:         "2233",
		VerifiedShape:        "cross_capability",
		Traits:               json.RawMessage(`["multiple_capabilities","explicit_critique"]`),
		RequestedMode:        store.TeamWorkModeMultiRole,
		EffectiveMode:        store.TeamWorkModeMultiRole,
		IndependentReview:    true,
		SelectedOwnerAgentID: &agentID,
		CoordinatorAgentID:   &agentID,
		PlanHash:             "abcd",
		StageStatuses:        json.RawMessage(`{"shape":"ok","planning":"ok"}`),
		DegradedStage:        "",
		DegradedReason:       "",
		ClassifierProvider:   "test",
		ClassifierModel:      "test-model",
	}

	if err := s.RecordTeamWorkClassificationAudit(ctx, audit); err != nil {
		t.Fatalf("record audit: %v", err)
	}
	if audit.ID == uuid.Nil {
		t.Fatal("audit ID must be populated after insert")
	}
	if audit.CreatedAt.IsZero() {
		t.Fatal("audit CreatedAt must be populated after insert")
	}
	if audit.AuditSchemaVersion != store.TeamWorkClassificationAuditSchemaVersion {
		t.Fatalf("schema version = %d, want %d", audit.AuditSchemaVersion, store.TeamWorkClassificationAuditSchemaVersion)
	}

	var (
		gotTenant                uuid.UUID
		gotIngress, gotReqMode   string
		gotEffMode, gotShape     string
		gotIndepReview           bool
		gotTraits, gotStages     []byte
		gotProvider, gotModel    string
		gotSchemaVersion         int
		gotAgent, gotOwner, gotC *uuid.UUID
	)
	err := db.QueryRowContext(ctx, `SELECT tenant_id, ingress, requested_mode, effective_mode, verified_shape,
		independent_review, traits, stage_statuses, classifier_provider, classifier_model, schema_version,
		agent_id, selected_owner_agent_id, coordinator_agent_id
		FROM team_work_classification_audits WHERE id=$1`, audit.ID).Scan(
		&gotTenant, &gotIngress, &gotReqMode, &gotEffMode, &gotShape,
		&gotIndepReview, &gotTraits, &gotStages, &gotProvider, &gotModel, &gotSchemaVersion,
		&gotAgent, &gotOwner, &gotC)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}

	if gotTenant != tenantID {
		t.Fatalf("tenant_id = %s, want %s (must be scoped from context)", gotTenant, tenantID)
	}
	if gotIngress != store.TeamWorkIngressWS {
		t.Fatalf("ingress = %q, want %q", gotIngress, store.TeamWorkIngressWS)
	}
	if gotReqMode != store.TeamWorkModeMultiRole || gotEffMode != store.TeamWorkModeMultiRole {
		t.Fatalf("modes = (%q,%q), want (multi_role,multi_role)", gotReqMode, gotEffMode)
	}
	if gotShape != "cross_capability" {
		t.Fatalf("verified_shape = %q, want cross_capability", gotShape)
	}
	if !gotIndepReview {
		t.Fatal("independent_review must round-trip as true")
	}
	if gotProvider != "test" || gotModel != "test-model" {
		t.Fatalf("provider/model = (%q,%q)", gotProvider, gotModel)
	}
	if gotSchemaVersion != store.TeamWorkClassificationAuditSchemaVersion {
		t.Fatalf("persisted schema_version = %d", gotSchemaVersion)
	}
	if gotAgent == nil || *gotAgent != agentID || gotOwner == nil || *gotOwner != agentID || gotC == nil || *gotC != agentID {
		t.Fatal("nullable agent FKs must round-trip to the seeded agent")
	}

	var traits []string
	if err := json.Unmarshal(gotTraits, &traits); err != nil {
		t.Fatalf("unmarshal traits: %v", err)
	}
	if len(traits) != 2 || traits[0] != "multiple_capabilities" || traits[1] != "explicit_critique" {
		t.Fatalf("traits round-trip = %v", traits)
	}
	var stages map[string]string
	if err := json.Unmarshal(gotStages, &stages); err != nil {
		t.Fatalf("unmarshal stage_statuses: %v", err)
	}
	if stages["shape"] != "ok" || stages["planning"] != "ok" {
		t.Fatalf("stage_statuses round-trip = %v", stages)
	}
}

// TestPGRecordTeamWorkClassificationAuditTenantGuards proves the write refuses a
// missing tenant and a cross-tenant audit, and defaults the JSON columns to
// valid empty containers when the caller leaves them unset (NOT NULL columns).
func TestPGRecordTeamWorkClassificationAuditTenantGuards(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, _ := seedTenantAndAgent(t, db)
	t.Cleanup(func() { db.Exec("DELETE FROM team_work_classification_audits WHERE tenant_id=$1", tenantID) })
	s := NewPGTeamStore(db)

	// No tenant in context → rejected, no row written.
	if err := s.RecordTeamWorkClassificationAudit(context.Background(), &store.TeamWorkClassificationAudit{Ingress: store.TeamWorkIngressSystem}); err == nil {
		t.Fatal("expected error when tenant missing from context")
	}

	// Cross-tenant audit → rejected.
	ctx := store.WithTenantID(context.Background(), tenantID)
	other := uuid.Must(uuid.NewV7())
	if err := s.RecordTeamWorkClassificationAudit(ctx, &store.TeamWorkClassificationAudit{TenantID: other, Ingress: store.TeamWorkIngressInbound}); err == nil {
		t.Fatal("expected error on cross-tenant audit")
	}

	// Unset JSON columns default to valid empty containers.
	audit := &store.TeamWorkClassificationAudit{Ingress: store.TeamWorkIngressInbound}
	if err := s.RecordTeamWorkClassificationAudit(ctx, audit); err != nil {
		t.Fatalf("record minimal audit: %v", err)
	}
	var traits, stages []byte
	if err := db.QueryRowContext(ctx, `SELECT traits, stage_statuses FROM team_work_classification_audits WHERE id=$1`, audit.ID).Scan(&traits, &stages); err != nil {
		t.Fatalf("query minimal audit: %v", err)
	}
	if string(traits) != "[]" {
		t.Fatalf("default traits = %q, want []", traits)
	}
	if string(stages) != "{}" {
		t.Fatalf("default stage_statuses = %q, want {}", stages)
	}
}
