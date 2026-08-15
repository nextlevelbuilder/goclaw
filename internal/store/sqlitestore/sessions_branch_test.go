//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"errors"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestBranchSessionPersistsCopyAndMetadata(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	sessionStore := NewSQLiteSessionStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	sourceKey := "agent:test-agent:direct:source-branch-test"
	branchKey := "agent:test-agent:branch:copy-branch-test"

	source := sessionStore.GetOrCreate(ctx, sourceKey)
	source.Summary = "source summary"
	source.Metadata = map[string]string{"source": "yes"}
	sessionStore.SetHistory(ctx, sourceKey, []providers.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
	})
	if err := sessionStore.Save(ctx, sourceKey); err != nil {
		t.Fatalf("Save source: %v", err)
	}

	branch, copied, err := sessionStore.BranchSession(ctx, sourceKey, store.SessionBranchOpts{
		NewKey:    branchKey,
		UpToIndex: 1,
		Label:     "branch label",
		Metadata:  map[string]string{"requested_by": "test"},
	})
	if err != nil {
		t.Fatalf("BranchSession: %v", err)
	}
	if copied != 1 {
		t.Fatalf("copied = %d, want 1", copied)
	}
	if branch.Key != branchKey || branch.Channel != "branch" || branch.Label != "branch label" {
		t.Fatalf("branch metadata = key:%q channel:%q label:%q", branch.Key, branch.Channel, branch.Label)
	}
	if len(branch.Messages) != 1 || branch.Messages[0].Content != "first" {
		t.Fatalf("branch messages = %+v", branch.Messages)
	}
	if branch.Metadata["branched_from"] != sourceKey || branch.Metadata["branched_from_index"] != "1" {
		t.Fatalf("branch provenance metadata = %+v", branch.Metadata)
	}
	if branch.Metadata["source"] != "yes" || branch.Metadata["requested_by"] != "test" {
		t.Fatalf("branch merged metadata = %+v", branch.Metadata)
	}

	reloaded := NewSQLiteSessionStore(db).Get(ctx, branchKey)
	if reloaded == nil {
		t.Fatal("reloaded branch = nil")
	}
	if len(reloaded.Messages) != 1 || reloaded.Messages[0].Content != "first" {
		t.Fatalf("reloaded messages = %+v", reloaded.Messages)
	}
	if reloaded.Metadata["branched_from"] != sourceKey {
		t.Fatalf("reloaded metadata = %+v", reloaded.Metadata)
	}
}

func TestBranchSessionRejectsExistingTargetWithoutOverwrite(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	sourceKey := "agent:test-agent:direct:source-conflict-test"
	branchKey := "agent:test-agent:branch:conflict-test"

	firstStore := NewSQLiteSessionStore(db)
	firstStore.GetOrCreate(ctx, sourceKey)
	firstStore.SetHistory(ctx, sourceKey, []providers.Message{{Role: "user", Content: "first"}})
	if err := firstStore.Save(ctx, sourceKey); err != nil {
		t.Fatalf("Save source: %v", err)
	}
	if _, _, err := firstStore.BranchSession(ctx, sourceKey, store.SessionBranchOpts{
		NewKey:    branchKey,
		UpToIndex: 1,
		Label:     "first label",
	}); err != nil {
		t.Fatalf("BranchSession first: %v", err)
	}

	secondStore := NewSQLiteSessionStore(db)
	_, _, err := secondStore.BranchSession(ctx, sourceKey, store.SessionBranchOpts{
		NewKey:    branchKey,
		UpToIndex: 1,
		Label:     "second label",
	})
	if !errors.Is(err, store.ErrSessionAlreadyExists) {
		t.Fatalf("second BranchSession err = %v, want ErrSessionAlreadyExists", err)
	}

	reloaded := NewSQLiteSessionStore(db).Get(ctx, branchKey)
	if reloaded == nil {
		t.Fatal("reloaded branch = nil")
	}
	if reloaded.Label != "first label" {
		t.Fatalf("label = %q, want first label", reloaded.Label)
	}
}
