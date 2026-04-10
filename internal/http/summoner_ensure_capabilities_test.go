package http

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// backfillStubStore is a minimal AgentStore stub for ensureBackfillFiles tests.
// Only GetAgentContextFiles and SetAgentContextFile are exercised.
type backfillStubStore struct {
	store.AgentStore // embed to satisfy interface; unused methods panic
	files            []store.AgentContextFileData
	setCalls         atomic.Int32
	setFiles         []string
}

func (s *backfillStubStore) GetAgentContextFiles(_ context.Context, _ uuid.UUID) ([]store.AgentContextFileData, error) {
	return s.files, nil
}
func (s *backfillStubStore) SetAgentContextFile(_ context.Context, _ uuid.UUID, fileName, _ string) error {
	s.setCalls.Add(1)
	s.setFiles = append(s.setFiles, fileName)
	return nil
}

func TestEnsureBackfillFiles_SeedsBothWhenMissing(t *testing.T) {
	stub := &backfillStubStore{
		files: []store.AgentContextFileData{
			{FileName: "SOUL.md", Content: "style info"},
		},
	}
	s := &AgentSummoner{agents: stub}

	s.ensureBackfillFiles(context.Background(), uuid.New())

	if n := stub.setCalls.Load(); n != 2 {
		t.Fatalf("expected 2 SetAgentContextFile calls, got %d", n)
	}
	want := map[string]bool{bootstrap.UserPredefinedFile: true, bootstrap.CapabilitiesFile: true}
	for _, f := range stub.setFiles {
		if !want[f] {
			t.Errorf("unexpected file seeded: %s", f)
		}
		delete(want, f)
	}
	for f := range want {
		t.Errorf("expected file not seeded: %s", f)
	}
}

func TestEnsureBackfillFiles_SkipsWhenAllExist(t *testing.T) {
	stub := &backfillStubStore{
		files: []store.AgentContextFileData{
			{FileName: "SOUL.md", Content: "style info"},
			{FileName: bootstrap.UserPredefinedFile, Content: "predefined rules"},
			{FileName: bootstrap.CapabilitiesFile, Content: "existing capabilities"},
		},
	}
	s := &AgentSummoner{agents: stub}

	s.ensureBackfillFiles(context.Background(), uuid.New())

	if n := stub.setCalls.Load(); n != 0 {
		t.Fatalf("expected 0 SetAgentContextFile calls (all exist), got %d", n)
	}
}

func TestEnsureBackfillFiles_SeedsOnlyMissing(t *testing.T) {
	stub := &backfillStubStore{
		files: []store.AgentContextFileData{
			{FileName: bootstrap.UserPredefinedFile, Content: "predefined rules"},
		},
	}
	s := &AgentSummoner{agents: stub}

	s.ensureBackfillFiles(context.Background(), uuid.New())

	if n := stub.setCalls.Load(); n != 1 {
		t.Fatalf("expected 1 SetAgentContextFile call, got %d", n)
	}
	if stub.setFiles[0] != bootstrap.CapabilitiesFile {
		t.Fatalf("expected %q seeded, got %q", bootstrap.CapabilitiesFile, stub.setFiles[0])
	}
}
