//go:build integration

package integration

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestStaleRun_RecoverStaleRuns_MarksFailed verifies that PGRunStore.RecoverStaleRuns
// transitions runs with an expired heartbeat to failed (with the canonical
// "run stalled" error and a completed_at stamp) while leaving freshly-heartbeated
// runs untouched. Exercises the real store against the real PostgreSQL migration.
func TestStaleRun_RecoverStaleRuns_MarksFailed(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)

	staleRunID := "run-stale-" + uuid.New().String()[:8]
	freshRunID := "run-fresh-" + uuid.New().String()[:8]

	// A stale run: heartbeat expired about an hour ago, still "running".
	stale := &store.AgentRun{
		ID:          uuid.New(),
		TenantID:    tenantID,
		RunID:       staleRunID,
		SessionKey:  "sess-stale-" + uuid.New().String()[:8],
		AgentID:     &agentID,
		Status:      store.AgentRunStatusRunning,
		Attempt:     1,
		HeartbeatAt: time.Now().Add(-1 * time.Hour),
		StartedAt:   time.Now().Add(-2 * time.Hour),
	}

	// A fresh run: heartbeat current, also "running" — must be left untouched.
	fresh := &store.AgentRun{
		ID:          uuid.New(),
		TenantID:    tenantID,
		RunID:       freshRunID,
		SessionKey:  "sess-fresh-" + uuid.New().String()[:8],
		AgentID:     &agentID,
		Status:      store.AgentRunStatusRunning,
		Attempt:     1,
		HeartbeatAt: time.Now(),
		StartedAt:   time.Now().Add(-5 * time.Minute),
	}

	// seedTenantAgent cleanup does NOT delete agent_runs; add our own cleanup.
	insertRun(t, db, stale)
	insertRun(t, db, fresh)
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_runs WHERE tenant_id = $1", tenantID)
	})

	st := pg.NewPGRunStore(db)

	// RecoverStaleRuns is cross-tenant (startup + periodic): run it with a
	// cross-tenant context so it sees every tenant's stale runs.
	count, err := st.RecoverStaleRuns(crossTenantCtx(), 30*time.Minute)
	if err != nil {
		t.Fatalf("RecoverStaleRuns: %v", err)
	}
	if count != 1 {
		t.Fatalf("RecoverStaleRuns count = %d, want 1 (only the stale run)", count)
	}

	staleRow, err := st.GetRun(crossTenantCtx(), staleRunID)
	if err != nil {
		t.Fatalf("GetRun(stale): %v", err)
	}
	if staleRow.Status != store.AgentRunStatusFailed {
		t.Errorf("stale run status = %q, want %q", staleRow.Status, store.AgentRunStatusFailed)
	}
	if !strings.Contains(staleRow.Error, "run stalled") {
		t.Errorf("stale run error = %q, want it to contain %q", staleRow.Error, "run stalled")
	}
	if staleRow.CompletedAt == nil {
		t.Error("stale run completed_at is nil, want it stamped")
	}

	freshRow, err := st.GetRun(crossTenantCtx(), freshRunID)
	if err != nil {
		t.Fatalf("GetRun(fresh): %v", err)
	}
	if freshRow.Status != store.AgentRunStatusRunning {
		t.Errorf("fresh run status = %q, want %q (untouched)", freshRow.Status, store.AgentRunStatusRunning)
	}
	if freshRow.Error != "" {
		t.Errorf("fresh run error = %q, want empty", freshRow.Error)
	}
	if freshRow.CompletedAt != nil {
		t.Error("fresh run completed_at set, want nil")
	}
}

// insertRun inserts an agent_runs row directly (raw SQL). seedTenantAgent's
// cleanup does not delete agent_runs, so callers add their own cleanup.
func insertRun(t *testing.T, db *sql.DB, run *store.AgentRun) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO agent_runs
		 (id, tenant_id, run_id, session_key, agent_id, status, attempt, heartbeat_at, started_at, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, '{}'::jsonb)`,
		run.ID, run.TenantID, run.RunID, run.SessionKey, run.AgentID, run.Status,
		run.Attempt, run.HeartbeatAt, run.StartedAt)
	if err != nil {
		t.Fatalf("insert agent_runs %s: %v", run.RunID, err)
	}
}
