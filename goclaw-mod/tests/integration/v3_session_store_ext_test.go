//go:build integration

package integration

import (
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestStoreSession_ListPaged_Pagination verifies limit/offset pagination
// and cross-tenant isolation in ListPaged.
func TestStoreSession_ListPaged_Pagination(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ss := pg.NewPGSessionStore(db)

	// Create 5 sessions for this tenant.
	var keys []string
	for i := 0; i < 5; i++ {
		k := "agent:" + agentID.String() + ":paged:" + uuid.New().String()[:8]
		ss.GetOrCreate(ctx, k)
		if err := ss.Save(ctx, k); err != nil {
			t.Fatalf("Save [%d]: %v", i, err)
		}
		keys = append(keys, k)
	}
	t.Cleanup(func() {
		for _, k := range keys {
			db.Exec("DELETE FROM sessions WHERE session_key = $1 AND tenant_id = $2", k, tenantID)
		}
	})

	t.Run("limit=2 offset=0", func(t *testing.T) {
		result := ss.ListPaged(ctx, store.SessionListOpts{Limit: 2, Offset: 0})
		if result.Total < 5 {
			t.Errorf("Total = %d, want >= 5", result.Total)
		}
		if len(result.Sessions) != 2 {
			t.Errorf("Sessions len = %d, want 2", len(result.Sessions))
		}
	})

	t.Run("limit=3 offset=2", func(t *testing.T) {
		result := ss.ListPaged(ctx, store.SessionListOpts{Limit: 3, Offset: 2})
		if result.Total < 5 {
			t.Errorf("Total = %d, want >= 5", result.Total)
		}
		// At least 1 result — depends on how many total exist; if exactly 5, expect 3.
		if len(result.Sessions) == 0 {
			t.Error("expected at least 1 session on page 2")
		}
	})

	t.Run("tenant_isolation", func(t *testing.T) {
		tenantB, _ := seedTenantAgent(t, db)
		ctxB := tenantCtx(tenantB)
		// Create 1 session for tenant B.
		kb := "agent:" + agentID.String() + ":iso:" + uuid.New().String()[:8]
		ss.GetOrCreate(ctxB, kb)
		_ = ss.Save(ctxB, kb)
		t.Cleanup(func() {
			db.Exec("DELETE FROM sessions WHERE session_key = $1 AND tenant_id = $2", kb, tenantB)
		})

		// Tenant A querying with its own context must not see tenant B's session.
		resultA := ss.ListPaged(ctx, store.SessionListOpts{Limit: 100, Offset: 0})
		for _, s := range resultA.Sessions {
			if s.Key == kb {
				t.Error("tenant A sees tenant B session — isolation broken")
			}
		}
	})
}

// TestStoreSession_ListPaged_Filter_UserID verifies user_id scoping.
func TestStoreSession_ListPaged_Filter_UserID(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ss := pg.NewPGSessionStore(db)

	prefix := "agent:" + agentID.String() + ":uid-"
	suffix1 := uuid.New().String()[:8]
	suffix2 := uuid.New().String()[:8]
	k1 := prefix + suffix1
	k2 := prefix + suffix2

	// Create two sessions with distinct userIDs stored via metadata.
	s1 := ss.GetOrCreate(ctx, k1)
	s1.UserID = "filter-user-A"
	_ = ss.Save(ctx, k1)

	s2 := ss.GetOrCreate(ctx, k2)
	s2.UserID = "filter-user-B"
	_ = ss.Save(ctx, k2)

	t.Cleanup(func() {
		db.Exec("DELETE FROM sessions WHERE session_key = $1 AND tenant_id = $2", k1, tenantID)
		db.Exec("DELETE FROM sessions WHERE session_key = $1 AND tenant_id = $2", k2, tenantID)
	})

	result := ss.ListPaged(ctx, store.SessionListOpts{
		UserID: "filter-user-A",
		Limit:  50,
	})
	for _, s := range result.Sessions {
		if s.Key == k2 {
			t.Error("filter by user_id should not return session of a different user")
		}
	}
}

// TestStoreSession_List_Simple verifies the simple List() path.
func TestStoreSession_List_Simple(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ss := pg.NewPGSessionStore(db)
	pg.InitSqlx(db)

	k := "agent:" + agentID.String() + ":listtest:" + uuid.New().String()[:8]
	ss.GetOrCreate(ctx, k)
	_ = ss.Save(ctx, k)
	t.Cleanup(func() {
		db.Exec("DELETE FROM sessions WHERE session_key = $1 AND tenant_id = $2", k, tenantID)
	})

	// List with agentID filter should include the created session.
	infos := ss.List(ctx, agentID.String())
	found := false
	for _, s := range infos {
		if s.Key == k {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("List(agentID) did not return key %q", k)
	}
}

// TestStoreSession_GetOrCreate_LoadsFromDB verifies that GetOrCreate
// returns DB data on a cache miss (new store instance).
func TestStoreSession_GetOrCreate_LoadsFromDB(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ss1 := pg.NewPGSessionStore(db)

	k := "test-reload-" + uuid.New().String()[:8]
	data := ss1.GetOrCreate(ctx, k)
	data.Label = "reload-label"
	_ = ss1.Save(ctx, k)
	t.Cleanup(func() {
		db.Exec("DELETE FROM sessions WHERE session_key = $1 AND tenant_id = $2", k, tenantID)
	})

	// New store instance — no cache. GetOrCreate must reload from DB.
	ss2 := pg.NewPGSessionStore(db)
	got := ss2.GetOrCreate(ctx, k)
	if got == nil {
		t.Fatal("GetOrCreate on new store returned nil")
	}
	if got.Key != k {
		t.Errorf("Key = %q, want %q", got.Key, k)
	}
}

// TestStoreSession_SetGetLabel_Persisted verifies label persists through Save/load cycle.
func TestStoreSession_SetGetLabel_Persisted(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ss := pg.NewPGSessionStore(db)

	k := "persist-label-" + uuid.New().String()[:8]
	ss.GetOrCreate(ctx, k)
	ss.SetLabel(ctx, k, "my-persisted-label")
	_ = ss.Save(ctx, k)
	t.Cleanup(func() {
		db.Exec("DELETE FROM sessions WHERE session_key = $1 AND tenant_id = $2", k, tenantID)
	})

	// Re-load from DB.
	ss2 := pg.NewPGSessionStore(db)
	got := ss2.GetOrCreate(ctx, k)
	if got == nil {
		t.Fatal("reload returned nil")
	}
	if got.Label != "my-persisted-label" {
		t.Errorf("Label after reload = %q, want %q", got.Label, "my-persisted-label")
	}
}

// TestStoreSession_AddMessage_Ordering verifies that messages are stored in insertion order.
func TestStoreSession_AddMessage_Ordering(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ss := pg.NewPGSessionStore(db)

	k := "order-" + uuid.New().String()[:8]
	ss.GetOrCreate(ctx, k)
	t.Cleanup(func() {
		db.Exec("DELETE FROM sessions WHERE session_key = $1 AND tenant_id = $2", k, tenantID)
	})

	contents := []string{"first", "second", "third", "fourth", "fifth"}
	for _, c := range contents {
		ss.AddMessage(ctx, k, providers.Message{Role: "user", Content: c})
	}
	_ = ss.Save(ctx, k)

	// Reload and verify order.
	ss2 := pg.NewPGSessionStore(db)
	ss2.GetOrCreate(ctx, k) // triggers DB load
	hist := ss2.GetHistory(ctx, k)
	if len(hist) != len(contents) {
		t.Fatalf("message count = %d, want %d", len(hist), len(contents))
	}
	for i, c := range contents {
		if hist[i].Content != c {
			t.Errorf("msg[%d] = %q, want %q", i, hist[i].Content, c)
		}
	}
}

// TestStoreSession_ListPagedRich_TenantIsolation verifies ListPagedRich respects tenant.
func TestStoreSession_ListPagedRich_TenantIsolation(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantA, agentA := seedTenantAgent(t, db)
	tenantB, _ := seedTenantAgent(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)
	ss := pg.NewPGSessionStore(db)

	kA := "agent:" + agentA.String() + ":rich:" + uuid.New().String()[:8]
	ss.GetOrCreate(ctxA, kA)
	_ = ss.Save(ctxA, kA)
	t.Cleanup(func() {
		db.Exec("DELETE FROM sessions WHERE session_key = $1 AND tenant_id = $2", kA, tenantA)
	})

	resultB := ss.ListPagedRich(ctxB, store.SessionListOpts{Limit: 100})
	for _, s := range resultB.Sessions {
		if s.Key == kA {
			t.Error("tenant B sees tenant A rich session — isolation broken")
		}
	}

	resultA := ss.ListPagedRich(ctxA, store.SessionListOpts{Limit: 100})
	found := false
	for _, s := range resultA.Sessions {
		if s.Key == kA {
			found = true
		}
	}
	if !found {
		t.Errorf("tenant A cannot see its own rich session %q", kA)
	}
}
