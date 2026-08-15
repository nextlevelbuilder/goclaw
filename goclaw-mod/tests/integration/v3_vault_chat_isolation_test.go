//go:build integration

package integration

import (
	"context"
	"sort"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// vaultSearchPathsWithChat runs a search bound to a specific chat scope and
// returns sorted result paths. Used by cross-chat isolation assertions.
func vaultSearchPathsWithChat(t *testing.T, vs store.VaultStore, ctx context.Context, tenantID, agentID string, teamID *string, chatID string, isolated bool) []string {
	t.Helper()
	var chatPtr *string
	if chatID != "" {
		chatPtr = &chatID
	}
	results, err := vs.Search(ctx, store.VaultSearchOptions{
		TenantID:     tenantID,
		AgentID:      agentID,
		TeamID:       teamID,
		ChatID:       chatPtr,
		TeamIsolated: isolated,
		Query:        "note",
		MaxResults:   100,
	})
	if err != nil {
		t.Fatalf("Search(chat=%s isolated=%v): %v", chatID, isolated, err)
	}
	paths := make([]string, 0, len(results))
	for _, r := range results {
		paths = append(paths, r.Document.Path)
	}
	sort.Strings(paths)
	return paths
}

// upsertChatDoc inserts a team-scoped vault doc tagged with a chat_id.
func upsertChatDoc(t *testing.T, vs store.VaultStore, ctx context.Context, tenantID, teamID uuid.UUID, chatID, path string) {
	t.Helper()
	var chatPtr *string
	if chatID != "" {
		cid := chatID
		chatPtr = &cid
	}
	teamStr := teamID.String()
	doc := &store.VaultDocument{
		TenantID:    tenantID.String(),
		TeamID:      &teamStr,
		ChatID:      chatPtr,
		Scope:       "team",
		Path:        path,
		Title:       "note-" + path,
		DocType:     "note",
		ContentHash: "h-" + path,
		Summary:     "note summary for " + path,
	}
	if err := vs.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("UpsertDocument(%s chat=%s): %v", path, chatID, err)
	}
}

// TestVaultChatIDIsolation_Isolated verifies that isolated-team vault search
// excludes cross-chat docs while still returning team-wide (chat_id IS NULL)
// docs and same-chat docs.
func TestVaultChatIDIsolation_Isolated(t *testing.T) {
	db := testDB(t)
	vs := newVaultStore(db)
	tenantA, _, agentA, _ := seedTwoTenants(t, db)
	ctx := tenantCtx(tenantA)
	teamID, _ := seedTeam(t, db, tenantA, agentA)

	// Three docs for the same team: chatA-only, chatB-only, team-wide (NULL).
	upsertChatDoc(t, vs, ctx, tenantA, teamID, "chatA", "teams/t/chatA/noteA.md")
	upsertChatDoc(t, vs, ctx, tenantA, teamID, "chatB", "teams/t/chatB/noteB.md")
	upsertChatDoc(t, vs, ctx, tenantA, teamID, "", "teams/t/shared-note.md")

	teamStr := teamID.String()

	// Agent in chatA with isolated scope: sees chatA + team-wide, NOT chatB.
	gotA := vaultSearchPathsWithChat(t, vs, ctx, tenantA.String(), agentA.String(), &teamStr, "chatA", true)
	wantA := []string{"teams/t/chatA/noteA.md", "teams/t/shared-note.md"}
	assertEqualPaths(t, "isolated chatA", wantA, gotA)

	// Agent in chatB with isolated scope: sees chatB + team-wide, NOT chatA.
	gotB := vaultSearchPathsWithChat(t, vs, ctx, tenantA.String(), agentA.String(), &teamStr, "chatB", true)
	wantB := []string{"teams/t/chatB/noteB.md", "teams/t/shared-note.md"}
	assertEqualPaths(t, "isolated chatB", wantB, gotB)
}

// TestVaultChatIDIsolation_SharedUnfiltered verifies that when TeamIsolated
// is false (shared workspace) the chat_id filter is skipped entirely — agents
// see every team doc regardless of origin chat. This is the pre-chat_id
// behavior and must not regress for shared teams.
func TestVaultChatIDIsolation_SharedUnfiltered(t *testing.T) {
	db := testDB(t)
	vs := newVaultStore(db)
	tenantA, _, agentA, _ := seedTwoTenants(t, db)
	ctx := tenantCtx(tenantA)
	teamID, _ := seedTeam(t, db, tenantA, agentA)

	upsertChatDoc(t, vs, ctx, tenantA, teamID, "chatA", "teams/t/chatA/noteA.md")
	upsertChatDoc(t, vs, ctx, tenantA, teamID, "chatB", "teams/t/chatB/noteB.md")
	upsertChatDoc(t, vs, ctx, tenantA, teamID, "", "teams/t/shared-note.md")

	teamStr := teamID.String()
	got := vaultSearchPathsWithChat(t, vs, ctx, tenantA.String(), agentA.String(), &teamStr, "chatA", false)
	want := []string{"teams/t/chatA/noteA.md", "teams/t/chatB/noteB.md", "teams/t/shared-note.md"}
	assertEqualPaths(t, "shared mode (no filter)", want, got)
}

func assertEqualPaths(t *testing.T, label string, want, got []string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s: paths mismatch\n want: %v\n  got: %v", label, want, got)
	}
	for i := range want {
		if want[i] != got[i] {
			t.Fatalf("%s: paths mismatch\n want: %v\n  got: %v", label, want, got)
		}
	}
}
