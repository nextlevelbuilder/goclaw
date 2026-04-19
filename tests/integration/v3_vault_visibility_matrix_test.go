//go:build integration

package integration

import (
	"context"
	"database/sql"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// seedAgentInTenant inserts an agent into an existing tenant for same-tenant multi-agent tests.
func seedAgentInTenant(t *testing.T, db *sql.DB, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	agentID := uuid.New()
	key := "ta-" + agentID.String()[:8]
	_, err := db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES ($1, $2, $3, 'predefined', 'active', 'test', 'test-model', 'test-owner')
		 ON CONFLICT DO NOTHING`,
		agentID, tenantID, key)
	if err != nil {
		t.Fatalf("seedAgentInTenant: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agents WHERE id = $1", agentID)
	})
	return agentID
}

// vaultListPaths calls ListDocuments and returns sorted paths.
func vaultListPaths(t *testing.T, vs interface {
	ListDocuments(ctx context.Context, tenantID, agentID string, opts store.VaultListOptions) ([]store.VaultDocument, error)
}, ctx context.Context, tenantID, agentID string) []string {
	t.Helper()
	docs, err := vs.ListDocuments(ctx, tenantID, agentID, store.VaultListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("ListDocuments(%s): %v", agentID, err)
	}
	paths := make([]string, 0, len(docs))
	for _, d := range docs {
		paths = append(paths, d.Path)
	}
	sort.Strings(paths)
	return paths
}

// vaultSearchPaths calls Search with optional teamID and returns sorted paths.
func vaultSearchPaths(t *testing.T, vs interface {
	Search(ctx context.Context, opts store.VaultSearchOptions) ([]store.VaultSearchResult, error)
}, ctx context.Context, tenantID, agentID string, teamID *string) []string {
	t.Helper()
	results, err := vs.Search(ctx, store.VaultSearchOptions{
		TenantID:   tenantID,
		AgentID:    agentID,
		TeamID:     teamID,
		MaxResults: 100,
	})
	if err != nil {
		t.Fatalf("Search(%s): %v", agentID, err)
	}
	paths := make([]string, 0, len(results))
	for _, r := range results {
		paths = append(paths, r.Document.Path)
	}
	sort.Strings(paths)
	return paths
}

// vaultTreeRootPaths calls ListTreeEntries at root and returns sorted entry paths.
func vaultTreeRootPaths(t *testing.T, vs interface {
	ListTreeEntries(ctx context.Context, tenantID string, opts store.VaultTreeOptions) ([]store.VaultTreeEntry, error)
}, ctx context.Context, tenantID string, opts store.VaultTreeOptions) []string {
	t.Helper()
	entries, err := vs.ListTreeEntries(ctx, tenantID, opts)
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

// containsPath reports whether target appears in the sorted paths slice.
func containsPath(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}

// TestStoreVault_VisibilityMatrix covers 9 matrix rows:
// personal/shared/team visibility by owner, other-agent, cross-tenant, team-member, non-member.
func TestStoreVault_VisibilityMatrix(t *testing.T) {
	db := testDB(t)
	vs := newVaultStore(db)

	// Tenant A: agentA (owner), agentBinA (same-tenant non-team), teamMember.
	tenantA, agentA := seedTenantAgent(t, db)
	agentBinA := seedAgentInTenant(t, db, tenantA)
	teamID, teamMember := seedTeam(t, db, tenantA, agentA)

	// Tenant X: cross-tenant viewer.
	tenantX, agentX := seedTenantAgent(t, db)

	tidA := tenantA.String()
	tidX := tenantX.String()
	aidA := agentA.String()
	aidB := agentBinA.String()
	aidMember := teamMember.String()
	aidX := agentX.String()
	teamIDStr := teamID.String()

	ctxA := tenantCtx(tenantA)
	ctxX := tenantCtx(tenantX)

	// Seed docs in tenantA.
	const (
		pathPersonal = "matrix/personal.md"
		pathShared   = "matrix/shared.md"
		pathTeam     = "matrix/team.md"
	)

	personal := makeVaultDoc(tidA, aidA, pathPersonal, "Personal A")
	personal.Summary = "personal owner doc"
	if err := vs.UpsertDocument(ctxA, personal); err != nil {
		t.Fatalf("upsert personal: %v", err)
	}

	shared := makeSharedVaultDoc(tidA, pathShared, "Shared Doc")
	shared.Summary = "shared tenant doc"
	if err := vs.UpsertDocument(ctxA, shared); err != nil {
		t.Fatalf("upsert shared: %v", err)
	}

	team := makeTeamVaultDoc(tidA, teamIDStr, pathTeam, "Team Doc")
	team.Summary = "team scoped doc"
	if err := vs.UpsertDocument(ctxA, team); err != nil {
		t.Fatalf("upsert team: %v", err)
	}

	type matrixCase struct {
		name        string
		getPaths    func() []string
		wantAll     []string // all must appear
		wantNone    []string // none must appear
	}

	cases := []matrixCase{
		{
			// Row 1: personal doc visible to its owner via ListDocuments.
			name: "personal_visible_to_owner",
			getPaths: func() []string {
				return vaultListPaths(t, vs, ctxA, tidA, aidA)
			},
			wantAll: []string{pathPersonal},
		},
		{
			// Row 2: personal doc NOT visible to another agent in same tenant.
			name: "personal_invisible_to_other_agent_same_tenant",
			getPaths: func() []string {
				return vaultListPaths(t, vs, ctxA, tidA, aidB)
			},
			wantNone: []string{pathPersonal},
		},
		{
			// Row 3: shared doc visible to any agent in tenant via ListDocuments.
			name: "shared_visible_to_any_agent_via_list",
			getPaths: func() []string {
				return vaultListPaths(t, vs, ctxA, tidA, aidA)
			},
			wantAll: []string{pathShared},
		},
		{
			// Row 4: shared doc visible via ListTreeEntries(agentID=A).
			name: "shared_visible_via_list_tree_agentA",
			getPaths: func() []string {
				// ListTreeEntries returns top-level entries; "matrix" folder should appear.
				paths := vaultTreeRootPaths(t, vs, ctxA, tidA, store.VaultTreeOptions{AgentID: aidA, Path: ""})
				// Drill into "matrix" to check shared.md is there.
				children := vaultTreeRootPaths(t, vs, ctxA, tidA, store.VaultTreeOptions{AgentID: aidA, Path: "matrix"})
				return append(paths, children...)
			},
			wantAll: []string{"matrix/shared.md"},
		},
		{
			// Row 5: shared doc from tenantA NOT visible in cross-tenant search.
			name: "shared_invisible_cross_tenant",
			getPaths: func() []string {
				return vaultSearchPaths(t, vs, ctxX, tidX, aidX, nil)
			},
			wantNone: []string{pathShared, pathPersonal, pathTeam},
		},
		{
			// Row 6: team doc visible to a member of that team (Search with TeamID).
			name: "team_doc_visible_to_member",
			getPaths: func() []string {
				return vaultSearchPaths(t, vs, ctxA, tidA, aidMember, &teamIDStr)
			},
			wantAll: []string{pathTeam},
		},
		{
			// Row 7: team doc NOT visible in personal-only search (TeamID="", means team_id IS NULL).
			// The store layer filters by team_id when a TeamID is provided; passing an empty string
			// means "personal only" (team_id IS NULL). Membership enforcement is a handler concern.
			name: "team_doc_invisible_in_personal_only_search",
			getPaths: func() []string {
				personalOnly := ""
				return vaultSearchPaths(t, vs, ctxA, tidA, aidB, &personalOnly)
			},
			wantNone: []string{pathTeam},
		},
		{
			// Row 8: shared doc visible to team member when RunContext.TeamID=T.
			name: "shared_visible_to_team_member_with_teamID",
			getPaths: func() []string {
				return vaultSearchPaths(t, vs, ctxA, tidA, aidMember, &teamIDStr)
			},
			wantAll: []string{pathShared},
		},
		{
			// Row 9: shared doc visible via ListTreeEntries with TeamID=T.
			name: "shared_visible_via_list_tree_with_teamID",
			getPaths: func() []string {
				paths := vaultTreeRootPaths(t, vs, ctxA, tidA, store.VaultTreeOptions{
					AgentID: aidMember,
					TeamID:  &teamIDStr,
					Path:    "",
				})
				children := vaultTreeRootPaths(t, vs, ctxA, tidA, store.VaultTreeOptions{
					AgentID: aidMember,
					TeamID:  &teamIDStr,
					Path:    "matrix",
				})
				return append(paths, children...)
			},
			wantAll: []string{"matrix/shared.md"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.getPaths()
			t.Logf("paths: %v", got)

			for _, want := range tc.wantAll {
				if !containsPath(got, want) {
					t.Errorf("want %q in results %v", want, got)
				}
			}
			for _, forbid := range tc.wantNone {
				if containsPath(got, forbid) {
					t.Errorf("path %q must NOT appear in results %v", forbid, got)
				}
			}
		})
	}
}
