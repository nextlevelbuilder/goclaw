//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
)

// EnsureBuiltinAgents against REAL Postgres.
//
// A unit test cannot cover this, which is the whole reason the file exists. The
// first version of the statement was valid Go: it compiled, passed vet, and passed
// every unit test — then failed at runtime on staging with
//
//	ERROR: inconsistent types deduced for parameter $1 (SQLSTATE 42P08)
//
// because $1 is used both as an inserted value and inside a comparison. The insert
// never happened. The only symptom was one WARN line in a log nobody was reading,
// and the four built-in agents silently did not exist.
func TestEnsureBuiltinAgents(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	newTenant := func(slug string) uuid.UUID {
		id := uuid.Must(uuid.NewV7())
		if _, err := db.Exec(
			`INSERT INTO tenants (id, name, slug, status) VALUES ($1, $2, $3, 'active')`,
			id, slug, slug+"-"+id.String()[:8]); err != nil {
			t.Fatalf("create tenant %s: %v", slug, err)
		}
		t.Cleanup(func() {
			db.Exec("DELETE FROM agents WHERE tenant_id = $1", id)
			db.Exec("DELETE FROM tenants WHERE id = $1", id)
		})
		return id
	}

	tenant := newTenant("builtin-one")

	if _, err := bootstrap.EnsureBuiltinAgents(ctx, db, "/tmp/ws"); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	for _, want := range bootstrap.BuiltinAgents {
		var (
			ownerID, agentType, provider, workspace, prompt string
			locked, isDefault                              bool
			maxIter                                        int
		)
		err := db.QueryRow(`
			SELECT owner_id, agent_type, provider, workspace, system_prompt,
			       is_locked, is_default, max_tool_iterations
			FROM agents WHERE tenant_id = $1 AND agent_key = $2`,
			tenant, want.Key).Scan(&ownerID, &agentType, &provider, &workspace, &prompt,
			&locked, &isDefault, &maxIter)
		if err != nil {
			t.Fatalf("built-in %q was not created: %v", want.Key, err)
		}

		// The three properties that make this a BUILT-IN rather than an ordinary
		// agent. ListAccessible keys off the first two to show it to every member,
		// so breaking either hides these from everyone at once.
		if ownerID != "system" {
			t.Errorf("%s: owner_id = %q, want \"system\" — ListAccessible would not surface it", want.Key, ownerID)
		}
		if agentType != "predefined" {
			t.Errorf("%s: agent_type = %q, want \"predefined\" — ListAccessible would not surface it", want.Key, agentType)
		}
		if !locked {
			t.Errorf("%s: is_locked = false, so one user could edit a prompt shared with their whole tenant", want.Key)
		}

		if isDefault {
			t.Errorf("%s: is_default = true would make a format specialist the tenant fallback", want.Key)
		}
		if prompt != want.SystemPrompt {
			t.Errorf("%s: system_prompt was not stored verbatim", want.Key)
		}
		if maxIter != want.MaxIter {
			t.Errorf("%s: max_tool_iterations = %d, want %d", want.Key, maxIter, want.MaxIter)
		}
		// Separate directories, so a file one built-in writes cannot collide with
		// another's.
		if workspace != "/tmp/ws/"+want.Key {
			t.Errorf("%s: workspace = %q, want a per-agent directory", want.Key, workspace)
		}
		if provider == "" {
			t.Errorf("%s: provider is empty", want.Key)
		}
	}

	// IDEMPOTENCE. The gateway calls this on every start and two can start at
	// once; a duplicate would hit the (tenant_id, agent_key) unique index and take
	// the startup path down with it.
	created, err := bootstrap.EnsureBuiltinAgents(ctx, db, "/tmp/ws")
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if created != 0 {
		t.Errorf("second run created %d rows, want 0", created)
	}

	// A tenant created AFTER the gateway started gets its built-ins on the next
	// run — that is the documented trade for not blocking tenant creation on them.
	later := newTenant("builtin-two")
	if _, err := bootstrap.EnsureBuiltinAgents(ctx, db, "/tmp/ws"); err != nil {
		t.Fatalf("third ensure: %v", err)
	}
	for _, id := range []uuid.UUID{tenant, later} {
		var count int
		if err := db.QueryRow(
			`SELECT count(*) FROM agents WHERE tenant_id = $1 AND owner_id = 'system'`, id).Scan(&count); err != nil {
			t.Fatalf("count: %v", err)
		}
		if count != len(bootstrap.BuiltinAgents) {
			t.Errorf("tenant %s has %d built-ins, want %d", id, count, len(bootstrap.BuiltinAgents))
		}
	}
}
