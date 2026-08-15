//go:build sqlite || sqliteonly

package integration

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// seedSQLiteAgent inserts an agent into an existing tenant in the SQLite test DB.
func seedSQLiteAgent(t *testing.T, stores *store.Stores, tenantID string) string {
	t.Helper()
	agentID := uuid.Must(uuid.NewV7()).String()
	key := "sqla-" + agentID[:8]
	stores.DB.Exec(
		`INSERT INTO agents (id, agent_key, display_name, status, tenant_id, owner_id, model, provider)
		 VALUES (?, ?, 'B', 'active', ?, 'owner', 'gpt-4o', 'openai')`,
		agentID, key, tenantID)
	return agentID
}

// seedSQLiteTeam creates a team with two members in the SQLite test DB.
// Returns teamID and the member agent's ID (ownerID is agentA).
func seedSQLiteTeam(t *testing.T, stores *store.Stores, tenantID, ownerAgentID string) (teamID, memberAgentID string) {
	t.Helper()
	teamID = uuid.Must(uuid.NewV7()).String()
	memberAgentID = uuid.Must(uuid.NewV7()).String()
	memberKey := "sqlm-" + memberAgentID[:8]

	stores.DB.Exec(
		`INSERT INTO agents (id, agent_key, display_name, status, tenant_id, owner_id, model, provider)
		 VALUES (?, ?, 'M', 'active', ?, 'owner', 'gpt-4o', 'openai')`,
		memberAgentID, memberKey, tenantID)

	stores.DB.Exec(
		`INSERT INTO agent_teams (id, tenant_id, name, lead_agent_id, status, settings, created_by)
		 VALUES (?, ?, ?, ?, 'active', '{"version":2}', 'test')`,
		teamID, tenantID, "test-team-"+teamID[:8], ownerAgentID)

	for _, m := range []struct{ id, role string }{{ownerAgentID, "lead"}, {memberAgentID, "member"}} {
		stores.DB.Exec(
			`INSERT INTO agent_team_members (team_id, agent_id, tenant_id, role) VALUES (?, ?, ?, ?)`,
			teamID, m.id, tenantID, m.role)
	}
	return teamID, memberAgentID
}

// sqliteListPaths returns sorted paths from Vault.ListDocuments.
func sqliteListPaths(t *testing.T, stores *store.Stores, ctx context.Context, tenantID, agentID string) []string {
	t.Helper()
	docs, err := stores.Vault.ListDocuments(ctx, tenantID, agentID, store.VaultListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	paths := make([]string, 0, len(docs))
	for _, d := range docs {
		paths = append(paths, d.Path)
	}
	sort.Strings(paths)
	return paths
}

// sqliteSearchPaths returns sorted paths from Vault.Search.
// query must be non-empty; SQLite Search returns nil on empty query.
func sqliteSearchPaths(t *testing.T, stores *store.Stores, ctx context.Context, tenantID, agentID, query string, teamID *string) []string {
	t.Helper()
	if query == "" {
		t.Fatal("sqliteSearchPaths: query must be non-empty for SQLite LIKE search")
	}
	results, err := stores.Vault.Search(ctx, store.VaultSearchOptions{
		Query:      query,
		TenantID:   tenantID,
		AgentID:    agentID,
		TeamID:     teamID,
		MaxResults: 100,
	})
	if err != nil {
		t.Fatalf("Search(%q): %v", query, err)
	}
	paths := make([]string, 0, len(results))
	for _, r := range results {
		paths = append(paths, r.Document.Path)
	}
	sort.Strings(paths)
	return paths
}

// sqliteTreePaths returns sorted paths from Vault.ListTreeEntries at the given root.
func sqliteTreePaths(t *testing.T, stores *store.Stores, ctx context.Context, tenantID string, opts store.VaultTreeOptions) []string {
	t.Helper()
	entries, err := stores.Vault.ListTreeEntries(ctx, tenantID, opts)
	if err != nil {
		t.Fatalf("ListTreeEntries: %v", err)
	}
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	sort.Strings(paths)
	return paths
}

// sqliteContainsPath reports whether target is in paths.
func sqliteContainsPath(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}

// TestSQLiteVaultStore_VisibilityMatrix mirrors a subset of the PG matrix for SQLite.
// SQLite uses LIKE-based search (no FTS), so Search results may be narrower.
func TestSQLiteVaultStore_VisibilityMatrix(t *testing.T) {
	stores := newSQLiteTestStores(t)

	tenantID := uuid.Must(uuid.NewV7()).String()
	agentA := uuid.Must(uuid.NewV7()).String()

	// Seed tenant + agentA.
	stores.DB.Exec(`INSERT INTO tenants (id, name, slug, status) VALUES (?, 'TenantA', 'ta', 'active')`, tenantID)
	stores.DB.Exec(
		`INSERT INTO agents (id, agent_key, display_name, status, tenant_id, owner_id, model, provider)
		 VALUES (?, 'matrix-a', 'A', 'active', ?, 'owner', 'gpt-4o', 'openai')`,
		agentA, tenantID)

	agentB := seedSQLiteAgent(t, stores, tenantID)
	teamID, teamMember := seedSQLiteTeam(t, stores, tenantID, agentA)

	ctx := store.WithTenantID(context.Background(), uuid.MustParse(tenantID))

	const (
		pathPersonal = "sqmatrix/personal.md"
		pathShared   = "sqmatrix/shared.md"
		pathTeam     = "sqmatrix/team.md"
	)

	// Seed personal doc (agentA-owned).
	if err := stores.Vault.UpsertDocument(ctx, &store.VaultDocument{
		TenantID: tenantID, AgentID: &agentA, Scope: "personal",
		Path: pathPersonal, Title: "Personal A", DocType: "note", ContentHash: "sq-pa1",
		Summary: "personal sqlite doc",
	}); err != nil {
		t.Fatalf("upsert personal: %v", err)
	}

	// Seed shared doc (agent_id=NULL).
	if err := stores.Vault.UpsertDocument(ctx, &store.VaultDocument{
		TenantID: tenantID, AgentID: nil, Scope: "shared",
		Path: pathShared, Title: "Shared Doc", DocType: "note", ContentHash: "sq-sh1",
		Summary: "shared sqlite doc",
	}); err != nil {
		t.Fatalf("upsert shared: %v", err)
	}

	// Seed team doc (team_id set, agent_id=NULL).
	if err := stores.Vault.UpsertDocument(ctx, &store.VaultDocument{
		TenantID: tenantID, AgentID: nil, TeamID: &teamID, Scope: "team",
		Path: pathTeam, Title: "Team Doc", DocType: "note", ContentHash: "sq-tm1",
		Summary: "team sqlite doc",
	}); err != nil {
		t.Fatalf("upsert team: %v", err)
	}

	type matrixCase struct {
		name     string
		getPaths func() []string
		wantAll  []string
		wantNone []string
	}

	cases := []matrixCase{
		{
			name: "personal_visible_to_owner_via_list",
			getPaths: func() []string {
				return sqliteListPaths(t, stores, ctx, tenantID, agentA)
			},
			wantAll: []string{pathPersonal},
		},
		{
			name: "personal_invisible_to_other_agent_same_tenant",
			getPaths: func() []string {
				return sqliteListPaths(t, stores, ctx, tenantID, agentB)
			},
			wantNone: []string{pathPersonal},
		},
		{
			name: "shared_visible_to_owner_via_list",
			getPaths: func() []string {
				return sqliteListPaths(t, stores, ctx, tenantID, agentA)
			},
			wantAll: []string{pathShared},
		},
		{
			name: "shared_visible_to_other_agent_via_list",
			getPaths: func() []string {
				return sqliteListPaths(t, stores, ctx, tenantID, agentB)
			},
			wantAll: []string{pathShared},
		},
		{
			name: "shared_visible_via_list_tree_agentA",
			getPaths: func() []string {
				root := sqliteTreePaths(t, stores, ctx, tenantID, store.VaultTreeOptions{AgentID: agentA, Path: ""})
				children := sqliteTreePaths(t, stores, ctx, tenantID, store.VaultTreeOptions{AgentID: agentA, Path: "sqmatrix"})
				return append(root, children...)
			},
			wantAll: []string{"sqmatrix/shared.md"},
		},
		{
			name: "team_doc_visible_to_member_via_search",
			getPaths: func() []string {
				return sqliteSearchPaths(t, stores, ctx, tenantID, teamMember, "sqmatrix", &teamID)
			},
			wantAll: []string{pathTeam},
		},
		{
			// personal-only search (TeamID="") must not return team-scoped docs.
			name: "team_doc_invisible_in_personal_only_search",
			getPaths: func() []string {
				personalOnly := ""
				return sqliteSearchPaths(t, stores, ctx, tenantID, agentB, "sqmatrix", &personalOnly)
			},
			wantNone: []string{pathTeam},
		},
		{
			name: "shared_visible_to_team_member_with_teamID_via_search",
			getPaths: func() []string {
				return sqliteSearchPaths(t, stores, ctx, tenantID, teamMember, "sqmatrix", &teamID)
			},
			wantAll: []string{pathShared},
		},
		{
			name: "shared_visible_via_list_tree_with_teamID",
			getPaths: func() []string {
				root := sqliteTreePaths(t, stores, ctx, tenantID, store.VaultTreeOptions{AgentID: teamMember, TeamID: &teamID, Path: ""})
				children := sqliteTreePaths(t, stores, ctx, tenantID, store.VaultTreeOptions{AgentID: teamMember, TeamID: &teamID, Path: "sqmatrix"})
				return append(root, children...)
			},
			wantAll: []string{"sqmatrix/shared.md"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.getPaths()
			t.Logf("paths: %v", got)
			for _, want := range tc.wantAll {
				if !sqliteContainsPath(got, want) {
					t.Errorf("want %q in results %v", want, got)
				}
			}
			for _, forbid := range tc.wantNone {
				if sqliteContainsPath(got, forbid) {
					t.Errorf("path %q must NOT appear in results %v", forbid, got)
				}
			}
		})
	}
}

// TestSQLiteVaultStore_ScopeCheckTrigger verifies the SQLite BEFORE INSERT trigger
// (trg_vault_docs_scope_consistency_ins) fires RAISE(ABORT) for invalid scope/ownership
// combinations and accepts valid ones.
func TestSQLiteVaultStore_ScopeCheckTrigger(t *testing.T) {
	stores := newSQLiteTestStores(t)

	tenantID := uuid.Must(uuid.NewV7()).String()
	agentID := uuid.Must(uuid.NewV7()).String()
	suffix := uuid.Must(uuid.NewV7()).String()[:8]

	stores.DB.Exec(`INSERT INTO tenants (id, name, slug, status) VALUES (?, 'T', 'sc', 'active')`, tenantID)
	stores.DB.Exec(
		`INSERT INTO agents (id, agent_key, display_name, status, tenant_id, owner_id, model, provider)
		 VALUES (?, 'sc-agt', 'A', 'active', ?, 'owner', 'gpt-4o', 'openai')`,
		agentID, tenantID)

	cases := []struct {
		name    string
		query   string
		args    []any
		wantErr bool
	}{
		{
			name: "reject_personal_null_agent_id",
			query: `INSERT INTO vault_documents (id, tenant_id, scope, path, title, doc_type, content_hash)
				    VALUES (?, ?, 'personal', ?, 'bad', 'note', 'h1')`,
			args:    []any{uuid.Must(uuid.NewV7()).String(), tenantID, "sc/bad-personal-" + suffix},
			wantErr: true,
		},
		{
			name: "reject_team_null_team_id",
			query: `INSERT INTO vault_documents (id, tenant_id, scope, path, title, doc_type, content_hash)
				    VALUES (?, ?, 'team', ?, 'bad', 'note', 'h2')`,
			args:    []any{uuid.Must(uuid.NewV7()).String(), tenantID, "sc/bad-team-" + suffix},
			wantErr: true,
		},
		{
			name: "reject_shared_with_agent_id",
			query: `INSERT INTO vault_documents (id, tenant_id, agent_id, scope, path, title, doc_type, content_hash)
				    VALUES (?, ?, ?, 'shared', ?, 'bad', 'note', 'h3')`,
			args:    []any{uuid.Must(uuid.NewV7()).String(), tenantID, agentID, "sc/bad-shared-" + suffix},
			wantErr: true,
		},
		{
			name: "accept_custom_scope_null_agent_id",
			query: `INSERT INTO vault_documents (id, tenant_id, scope, path, title, doc_type, content_hash)
				    VALUES (?, ?, 'custom', ?, 'ok', 'note', 'h4')`,
			args:    []any{uuid.Must(uuid.NewV7()).String(), tenantID, "sc/ok-custom-" + suffix},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := stores.DB.Exec(tc.query, tc.args...)
			if tc.wantErr {
				if err == nil {
					t.Error("expected scope_consistency trigger error, got nil")
					return
				}
				if !strings.Contains(err.Error(), "scope_consistency") {
					t.Errorf("expected error containing 'scope_consistency', got: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			}
		})
	}
}
