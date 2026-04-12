//go:build integration

package integration

import (
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// --- Context files (agent-level) ---

func TestStoreAgent_ContextFiles_SetGetList(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	as := pg.NewPGAgentStore(db)

	t.Run("set_and_list", func(t *testing.T) {
		for _, f := range []struct{ name, content string }{
			{"SOUL.md", "You are a helpful agent."},
			{"IDENTITY.md", "Name: TestAgent"},
		} {
			if err := as.SetAgentContextFile(ctx, agentID, f.name, f.content); err != nil {
				t.Fatalf("SetAgentContextFile %q: %v", f.name, err)
			}
		}

		files, err := as.GetAgentContextFiles(ctx, agentID)
		if err != nil {
			t.Fatalf("GetAgentContextFiles: %v", err)
		}
		byName := map[string]string{}
		for _, f := range files {
			byName[f.FileName] = f.Content
		}
		if byName["SOUL.md"] != "You are a helpful agent." {
			t.Errorf("SOUL.md content = %q", byName["SOUL.md"])
		}
		if byName["IDENTITY.md"] != "Name: TestAgent" {
			t.Errorf("IDENTITY.md content = %q", byName["IDENTITY.md"])
		}
	})

	t.Run("upsert_overwrite", func(t *testing.T) {
		if err := as.SetAgentContextFile(ctx, agentID, "SOUL.md", "Updated soul content"); err != nil {
			t.Fatalf("SetAgentContextFile overwrite: %v", err)
		}
		files, err := as.GetAgentContextFiles(ctx, agentID)
		if err != nil {
			t.Fatalf("GetAgentContextFiles after overwrite: %v", err)
		}
		for _, f := range files {
			if f.FileName == "SOUL.md" && f.Content != "Updated soul content" {
				t.Errorf("SOUL.md not updated: %q", f.Content)
			}
		}
	})
}

func TestStoreAgent_ContextFiles_TenantIsolation(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantA, agentA := seedTenantAgent(t, db)
	tenantB, agentB := seedTenantAgent(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)
	as := pg.NewPGAgentStore(db)

	// Set a file in tenant A.
	if err := as.SetAgentContextFile(ctxA, agentA, "SECRET.md", "tenant A secret"); err != nil {
		t.Fatalf("SetAgentContextFile A: %v", err)
	}

	// Tenant B listing its agent should see zero files from tenant A.
	filesB, err := as.GetAgentContextFiles(ctxB, agentB)
	if err != nil {
		t.Fatalf("GetAgentContextFiles B: %v", err)
	}
	for _, f := range filesB {
		if f.FileName == "SECRET.md" {
			t.Error("tenant B sees tenant A's context file — isolation broken")
		}
	}
}

// --- Per-user context files ---

func TestStoreAgent_UserContextFiles_SetGetDelete(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	as := pg.NewPGAgentStore(db)
	userID := "ucf-user-" + uuid.New().String()[:8]

	t.Run("set_and_get", func(t *testing.T) {
		if err := as.SetUserContextFile(ctx, agentID, userID, "USER.md", "User profile content"); err != nil {
			t.Fatalf("SetUserContextFile: %v", err)
		}
		files, err := as.GetUserContextFiles(ctx, agentID, userID)
		if err != nil {
			t.Fatalf("GetUserContextFiles: %v", err)
		}
		if len(files) == 0 {
			t.Fatal("expected at least 1 user context file")
		}
		found := false
		for _, f := range files {
			if f.FileName == "USER.md" && f.Content == "User profile content" {
				found = true
			}
		}
		if !found {
			t.Error("USER.md not found in user context files")
		}
	})

	t.Run("delete", func(t *testing.T) {
		if err := as.SetUserContextFile(ctx, agentID, userID, "TEMP.md", "to delete"); err != nil {
			t.Fatalf("SetUserContextFile TEMP: %v", err)
		}
		if err := as.DeleteUserContextFile(ctx, agentID, userID, "TEMP.md"); err != nil {
			t.Fatalf("DeleteUserContextFile: %v", err)
		}
		files, err := as.GetUserContextFiles(ctx, agentID, userID)
		if err != nil {
			t.Fatalf("GetUserContextFiles after delete: %v", err)
		}
		for _, f := range files {
			if f.FileName == "TEMP.md" {
				t.Error("TEMP.md still present after DeleteUserContextFile")
			}
		}
	})

	t.Run("list_by_name_cross_users", func(t *testing.T) {
		user2 := "ucf-user2-" + uuid.New().String()[:8]
		if err := as.SetUserContextFile(ctx, agentID, userID, "SHARED.md", "user1 copy"); err != nil {
			t.Fatalf("SetUserContextFile user1: %v", err)
		}
		if err := as.SetUserContextFile(ctx, agentID, user2, "SHARED.md", "user2 copy"); err != nil {
			t.Fatalf("SetUserContextFile user2: %v", err)
		}
		all, err := as.ListUserContextFilesByName(ctx, agentID, "SHARED.md")
		if err != nil {
			t.Fatalf("ListUserContextFilesByName: %v", err)
		}
		if len(all) < 2 {
			t.Errorf("ListUserContextFilesByName: expected >= 2, got %d", len(all))
		}
	})
}

// --- User profiles ---

func TestStoreAgent_UserProfile_GetOrCreate(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	as := pg.NewPGAgentStore(db)
	userID := "profile-" + uuid.New().String()[:8]

	// First call — insert.
	isNew, ws, err := as.GetOrCreateUserProfile(ctx, agentID, userID, "/workspace", "telegram")
	if err != nil {
		t.Fatalf("GetOrCreateUserProfile: %v", err)
	}
	if !isNew {
		t.Error("first call should be new insert")
	}
	if ws == "" {
		t.Error("workspace should not be empty")
	}

	// Second call — should not be new.
	isNew2, _, err2 := as.GetOrCreateUserProfile(ctx, agentID, userID, "/workspace", "telegram")
	if err2 != nil {
		t.Fatalf("GetOrCreateUserProfile second call: %v", err2)
	}
	if isNew2 {
		t.Error("second call should not be new")
	}
}

func TestStoreAgent_UserProfile_ListInstances(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	as := pg.NewPGAgentStore(db)

	for _, uid := range []string{
		"inst-a-" + uuid.New().String()[:8],
		"inst-b-" + uuid.New().String()[:8],
	} {
		if err := as.EnsureUserProfile(ctx, agentID, uid); err != nil {
			t.Fatalf("EnsureUserProfile %q: %v", uid, err)
		}
	}

	instances, err := as.ListUserInstances(ctx, agentID)
	if err != nil {
		t.Fatalf("ListUserInstances: %v", err)
	}
	if len(instances) < 2 {
		t.Errorf("expected >= 2 instances, got %d", len(instances))
	}
}

// --- User overrides ---

func TestStoreAgent_UserOverride_SetAndGet(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	as := pg.NewPGAgentStore(db)
	userID := "override-" + uuid.New().String()[:8]

	override := &store.UserAgentOverrideData{
		AgentID:  agentID,
		UserID:   userID,
		Provider: "anthropic",
		Model:    "claude-opus-4",
	}
	if err := as.SetUserOverride(ctx, override); err != nil {
		t.Fatalf("SetUserOverride: %v", err)
	}

	got, err := as.GetUserOverride(ctx, agentID, userID)
	if err != nil {
		t.Fatalf("GetUserOverride: %v", err)
	}
	if got == nil {
		t.Fatal("GetUserOverride returned nil")
	}
	if got.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", got.Provider, "anthropic")
	}
	if got.Model != "claude-opus-4" {
		t.Errorf("Model = %q, want %q", got.Model, "claude-opus-4")
	}

	// Unknown user returns nil override (not an error).
	notFound, err2 := as.GetUserOverride(ctx, agentID, "no-such-user")
	if err2 != nil {
		t.Fatalf("GetUserOverride unknown: %v", err2)
	}
	if notFound != nil {
		t.Error("expected nil for unknown user override")
	}
}

// --- Open vs Predefined agent type ---

func TestStoreAgent_AgentType_OpenVsPredefined(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	as := pg.NewPGAgentStore(db)

	cases := []struct {
		agentType string
		ownerID   string
	}{
		{"open", "type-user-" + uuid.New().String()[:8]},
		{"predefined", "type-user-" + uuid.New().String()[:8]},
	}

	for _, tc := range cases {
		a := newTestAgent(tenantID, uuid.New().String()[:8])
		a.AgentType = tc.agentType
		a.OwnerID = tc.ownerID
		if err := as.Create(ctx, &a); err != nil {
			t.Fatalf("Create %s agent: %v", tc.agentType, err)
		}
		t.Cleanup(func() { db.Exec("DELETE FROM agents WHERE id = $1", a.ID) })

		got, err := as.GetByID(ctx, a.ID)
		if err != nil {
			t.Fatalf("GetByID %s: %v", tc.agentType, err)
		}
		if got.AgentType != tc.agentType {
			t.Errorf("AgentType = %q, want %q", got.AgentType, tc.agentType)
		}
	}
}

// TestStoreAgent_DuplicateKey_Errors verifies that inserting two agents
// with the same agent_key in the same tenant errors.
func TestStoreAgent_DuplicateKey_Errors(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	as := pg.NewPGAgentStore(db)

	sharedKey := "dup-key-" + uuid.New().String()[:8]
	a1 := newTestAgent(tenantID, "")
	a1.AgentKey = sharedKey
	if err := as.Create(ctx, &a1); err != nil {
		t.Fatalf("Create first agent: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agents WHERE id = $1", a1.ID) })

	a2 := newTestAgent(tenantID, "")
	a2.AgentKey = sharedKey
	err := as.Create(ctx, &a2)
	if err == nil {
		t.Error("expected error on duplicate agent_key, got nil")
		db.Exec("DELETE FROM agents WHERE id = $1", a2.ID)
	}
}

// TestStoreAgent_ListByStatus verifies agents can be filtered by status via List.
func TestStoreAgent_List_MultipleOwners(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	as := pg.NewPGAgentStore(db)

	ownerA := "multi-owner-A-" + uuid.New().String()[:8]
	ownerB := "multi-owner-B-" + uuid.New().String()[:8]
	var idsA, idsB []uuid.UUID

	for i := 0; i < 2; i++ {
		a := newTestAgent(tenantID, uuid.New().String()[:8])
		a.OwnerID = ownerA
		if err := as.Create(ctx, &a); err != nil {
			t.Fatalf("Create ownerA[%d]: %v", i, err)
		}
		idsA = append(idsA, a.ID)
	}
	for i := 0; i < 3; i++ {
		a := newTestAgent(tenantID, uuid.New().String()[:8])
		a.OwnerID = ownerB
		if err := as.Create(ctx, &a); err != nil {
			t.Fatalf("Create ownerB[%d]: %v", i, err)
		}
		idsB = append(idsB, a.ID)
	}
	t.Cleanup(func() {
		for _, id := range append(idsA, idsB...) {
			db.Exec("DELETE FROM agents WHERE id = $1", id)
		}
	})

	listA, err := as.List(ctx, ownerA)
	if err != nil {
		t.Fatalf("List ownerA: %v", err)
	}
	if countInList(listA, idsA) != 2 {
		t.Errorf("ownerA list: expected 2 own agents, got %d", countInList(listA, idsA))
	}
	if countInList(listA, idsB) != 0 {
		t.Error("ownerA list contains ownerB agents — isolation broken")
	}

	listB, err := as.List(ctx, ownerB)
	if err != nil {
		t.Fatalf("List ownerB: %v", err)
	}
	if countInList(listB, idsB) != 3 {
		t.Errorf("ownerB list: expected 3 own agents, got %d", countInList(listB, idsB))
	}
}
