//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// sqliteAuditFixture builds a tenant + agent on a fresh isolated DB and returns
// the store, a tenant-scoped ctx and the agent ID for the nullable FKs.
func sqliteAuditFixture(t *testing.T) (*SQLiteTeamStore, context.Context, uuid.UUID, uuid.UUID) {
	t.Helper()
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	tenantID, agentID := uuid.New(), uuid.New()
	if _, err := db.Exec(`INSERT INTO tenants(id,name,slug,status,settings) VALUES(?,?,?,'active','{}')`, tenantID, "Tenant", "tenant-"+tenantID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agents(id,agent_key,owner_id,provider,model,tenant_id) VALUES(?,?,?,'openai','test',?)`, agentID, "lead", "owner", tenantID); err != nil {
		t.Fatal(err)
	}
	return NewSQLiteTeamStore(db), store.WithTenantID(context.Background(), tenantID), tenantID, agentID
}

// TestSQLiteRecordTeamWorkClassificationAuditRoundTrips is the SQLite twin of
// the PG parity test: one audit row persists tenant-scoped, ID/CreatedAt/schema
// version populate on the record, and the JSON trait/stage columns + the
// boolean independent_review round-trip.
func TestSQLiteRecordTeamWorkClassificationAuditRoundTrips(t *testing.T) {
	s, ctx, tenantID, agentID := sqliteAuditFixture(t)

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
		gotTenant              string
		gotIngress             string
		gotReqMode, gotEffMode string
		gotShape               string
		gotIndepReview         int
		gotTraits, gotStages   string
		gotProvider, gotModel  string
		gotSchemaVersion       int
		gotAgent, gotOwner     *string
		gotCoord               *string
	)
	err := s.db.QueryRowContext(ctx, `SELECT tenant_id, ingress, requested_mode, effective_mode, verified_shape,
		independent_review, traits, stage_statuses, classifier_provider, classifier_model, schema_version,
		agent_id, selected_owner_agent_id, coordinator_agent_id
		FROM team_work_classification_audits WHERE id=?`, audit.ID).Scan(
		&gotTenant, &gotIngress, &gotReqMode, &gotEffMode, &gotShape,
		&gotIndepReview, &gotTraits, &gotStages, &gotProvider, &gotModel, &gotSchemaVersion,
		&gotAgent, &gotOwner, &gotCoord)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}

	if gotTenant != tenantID.String() {
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
	if gotIndepReview != 1 {
		t.Fatalf("independent_review = %d, want 1", gotIndepReview)
	}
	if gotProvider != "test" || gotModel != "test-model" {
		t.Fatalf("provider/model = (%q,%q)", gotProvider, gotModel)
	}
	if gotSchemaVersion != store.TeamWorkClassificationAuditSchemaVersion {
		t.Fatalf("persisted schema_version = %d", gotSchemaVersion)
	}
	if gotAgent == nil || *gotAgent != agentID.String() ||
		gotOwner == nil || *gotOwner != agentID.String() ||
		gotCoord == nil || *gotCoord != agentID.String() {
		t.Fatal("nullable agent FKs must round-trip to the seeded agent")
	}

	var traits []string
	if err := json.Unmarshal([]byte(gotTraits), &traits); err != nil {
		t.Fatalf("unmarshal traits: %v", err)
	}
	if len(traits) != 2 || traits[0] != "multiple_capabilities" || traits[1] != "explicit_critique" {
		t.Fatalf("traits round-trip = %v", traits)
	}
	var stages map[string]string
	if err := json.Unmarshal([]byte(gotStages), &stages); err != nil {
		t.Fatalf("unmarshal stage_statuses: %v", err)
	}
	if stages["shape"] != "ok" || stages["planning"] != "ok" {
		t.Fatalf("stage_statuses round-trip = %v", stages)
	}
}

// TestSQLiteRecordTeamWorkClassificationAuditTenantGuards mirrors the PG guard
// test: missing tenant and cross-tenant audits are rejected, and unset JSON
// columns default to valid empty containers.
func TestSQLiteRecordTeamWorkClassificationAuditTenantGuards(t *testing.T) {
	s, ctx, _, _ := sqliteAuditFixture(t)

	if err := s.RecordTeamWorkClassificationAudit(context.Background(), &store.TeamWorkClassificationAudit{Ingress: store.TeamWorkIngressSystem}); err == nil {
		t.Fatal("expected error when tenant missing from context")
	}

	other := uuid.New()
	if err := s.RecordTeamWorkClassificationAudit(ctx, &store.TeamWorkClassificationAudit{TenantID: other, Ingress: store.TeamWorkIngressInbound}); err == nil {
		t.Fatal("expected error on cross-tenant audit")
	}

	audit := &store.TeamWorkClassificationAudit{Ingress: store.TeamWorkIngressInbound}
	if err := s.RecordTeamWorkClassificationAudit(ctx, audit); err != nil {
		t.Fatalf("record minimal audit: %v", err)
	}
	var traits, stages string
	if err := s.db.QueryRowContext(ctx, `SELECT traits, stage_statuses FROM team_work_classification_audits WHERE id=?`, audit.ID).Scan(&traits, &stages); err != nil {
		t.Fatalf("query minimal audit: %v", err)
	}
	if traits != "[]" {
		t.Fatalf("default traits = %q, want []", traits)
	}
	if stages != "{}" {
		t.Fatalf("default stage_statuses = %q, want {}", stages)
	}
}
