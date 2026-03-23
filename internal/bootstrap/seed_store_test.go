package bootstrap

import (
	"context"
	"maps"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ---- minimal AgentStore stub for seed tests ----

type seedStubStore struct {
	// agent-level files (simulates agent_context_files)
	agentFiles map[string]string // fileName → content
	// per-user files (simulates user_context_files)
	userFiles map[string]string // fileName → content (shared across all users for simplicity)
	// captures what was written per-user: fileName → content
	seededUserFiles map[string]string
}

func newSeedStub() *seedStubStore {
	return &seedStubStore{
		agentFiles:      make(map[string]string),
		userFiles:       make(map[string]string),
		seededUserFiles: make(map[string]string),
	}
}

func (s *seedStubStore) GetAgentContextFiles(_ context.Context, _ uuid.UUID) ([]store.AgentContextFileData, error) {
	var out []store.AgentContextFileData
	for name, content := range s.agentFiles {
		out = append(out, store.AgentContextFileData{FileName: name, Content: content})
	}
	return out, nil
}
func (s *seedStubStore) SetAgentContextFile(_ context.Context, _ uuid.UUID, name, content string) error {
	s.agentFiles[name] = content
	return nil
}
func (s *seedStubStore) GetUserContextFiles(_ context.Context, _ uuid.UUID, _ string) ([]store.UserContextFileData, error) {
	var out []store.UserContextFileData
	for name, content := range s.userFiles {
		out = append(out, store.UserContextFileData{FileName: name, Content: content})
	}
	return out, nil
}
func (s *seedStubStore) SetUserContextFile(_ context.Context, _ uuid.UUID, _, name, content string) error {
	s.seededUserFiles[name] = content
	return nil
}
func (s *seedStubStore) DeleteUserContextFile(_ context.Context, _ uuid.UUID, _, _ string) error {
	return nil
}

// Remaining interface methods — not exercised.
func (s *seedStubStore) Create(_ context.Context, _ *store.AgentData) error { return nil }
func (s *seedStubStore) GetByKey(_ context.Context, _ string) (*store.AgentData, error) {
	return nil, nil
}
func (s *seedStubStore) GetByID(_ context.Context, _ uuid.UUID) (*store.AgentData, error) {
	return nil, nil
}
func (s *seedStubStore) GetByKeys(_ context.Context, _ []string) ([]store.AgentData, error) {
	return nil, nil
}
func (s *seedStubStore) GetByIDs(_ context.Context, _ []uuid.UUID) ([]store.AgentData, error) {
	return nil, nil
}
func (s *seedStubStore) Update(_ context.Context, _ uuid.UUID, _ map[string]any) error   { return nil }
func (s *seedStubStore) Delete(_ context.Context, _ uuid.UUID) error                     { return nil }
func (s *seedStubStore) List(_ context.Context, _ string) ([]store.AgentData, error)     { return nil, nil }
func (s *seedStubStore) GetDefault(_ context.Context) (*store.AgentData, error)          { return nil, nil }
func (s *seedStubStore) ShareAgent(_ context.Context, _ uuid.UUID, _, _, _ string) error { return nil }
func (s *seedStubStore) RevokeShare(_ context.Context, _ uuid.UUID, _ string) error      { return nil }
func (s *seedStubStore) ListShares(_ context.Context, _ uuid.UUID) ([]store.AgentShareData, error) {
	return nil, nil
}
func (s *seedStubStore) CanAccess(_ context.Context, _ uuid.UUID, _ string) (bool, string, error) {
	return true, "admin", nil
}
func (s *seedStubStore) ListAccessible(_ context.Context, _ string) ([]store.AgentData, error) {
	return nil, nil
}
func (s *seedStubStore) GetUserOverride(_ context.Context, _ uuid.UUID, _ string) (*store.UserAgentOverrideData, error) {
	return nil, nil
}
func (s *seedStubStore) SetUserOverride(_ context.Context, _ *store.UserAgentOverrideData) error {
	return nil
}
func (s *seedStubStore) GetOrCreateUserProfile(_ context.Context, _ uuid.UUID, _, _, _ string) (bool, string, error) {
	return false, "", nil
}
func (s *seedStubStore) ListUserInstances(_ context.Context, _ uuid.UUID) ([]store.UserInstanceData, error) {
	return nil, nil
}
func (s *seedStubStore) UpdateUserProfileMetadata(_ context.Context, _ uuid.UUID, _ string, _ map[string]string) error {
	return nil
}
func (s *seedStubStore) EnsureUserProfile(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}

// ---- Tests ----

// TestSeedUserFiles_PredefinedAgent_UsesAgentLevelUserMD is the primary regression test.
// When a predefined agent has wizard-written USER.md in agent_context_files, SeedUserFiles
// must seed that content into user_context_files — NOT the blank embedded template.
func TestSeedUserFiles_PredefinedAgent_UsesAgentLevelUserMD(t *testing.T) {
	as := newSeedStub()
	agentID := uuid.New()
	wizardContent := "# User Profile\nOwner: Alice\nLanguage: English\nNotes: Prefers concise answers"

	// Simulate wizard writing USER.md at agent level via agents.files.set
	as.agentFiles[UserFile] = wizardContent

	seeded, err := SeedUserFiles(context.Background(), as, agentID, "user-alice", store.AgentTypePredefined)
	if err != nil {
		t.Fatalf("SeedUserFiles returned error: %v", err)
	}

	// USER.md must be in the seeded list
	foundUserMD := false
	for _, f := range seeded {
		if f == UserFile {
			foundUserMD = true
		}
	}
	if !foundUserMD {
		t.Errorf("USER.md not in seeded files list: %v", seeded)
	}

	// The seeded USER.md must contain wizard content, not the blank template
	got, ok := as.seededUserFiles[UserFile]
	if !ok {
		t.Fatal("USER.md was not written to user_context_files")
	}
	if got != wizardContent {
		t.Errorf("seeded USER.md content mismatch:\n  want: %q\n  got:  %q", wizardContent, got)
	}
}

// TestSeedUserFiles_PredefinedAgent_FallsBackToTemplateWhenNoAgentLevelUserMD verifies
// that when there is no wizard-written USER.md at agent level, the blank template is used.
func TestSeedUserFiles_PredefinedAgent_FallsBackToTemplateWhenNoAgentLevelUserMD(t *testing.T) {
	as := newSeedStub()
	agentID := uuid.New()
	// No agent-level USER.md — wizard did not write one

	seeded, err := SeedUserFiles(context.Background(), as, agentID, "user-bob", store.AgentTypePredefined)
	if err != nil {
		t.Fatalf("SeedUserFiles returned error: %v", err)
	}

	// USER.md should still be seeded (from embedded template)
	foundUserMD := false
	for _, f := range seeded {
		if f == UserFile {
			foundUserMD = true
		}
	}
	if !foundUserMD {
		t.Errorf("USER.md should be seeded from template when no agent-level file exists: %v", seeded)
	}

	// Content should be the embedded template (non-empty — the template file exists)
	got, ok := as.seededUserFiles[UserFile]
	if !ok {
		t.Fatal("USER.md was not written to user_context_files")
	}
	if got == "" {
		t.Error("seeded USER.md should not be empty (expected embedded template content)")
	}
}

// TestSeedUserFiles_PredefinedAgent_DoesNotOverwriteExistingPerUserContent verifies
// that personalized per-user USER.md written via conversation is never overwritten.
func TestSeedUserFiles_PredefinedAgent_DoesNotOverwriteExistingPerUserContent(t *testing.T) {
	as := newSeedStub()
	agentID := uuid.New()
	personalContent := "# User Profile\nMy customized personal content"

	// Pre-populate per-user USER.md (simulates user who already chatted and personalized)
	as.userFiles[UserFile] = personalContent
	// Also set wizard content at agent level
	as.agentFiles[UserFile] = "wizard content that should NOT override personal content"

	seeded, err := SeedUserFiles(context.Background(), as, agentID, "user-charlie", store.AgentTypePredefined)
	if err != nil {
		t.Fatalf("SeedUserFiles returned error: %v", err)
	}

	// USER.md must NOT be in the seeded list (existing content, should skip)
	for _, f := range seeded {
		if f == UserFile {
			t.Error("USER.md should NOT be re-seeded when per-user content already exists")
		}
	}

	// SetUserContextFile must not have been called for USER.md
	if _, wrote := as.seededUserFiles[UserFile]; wrote {
		t.Error("SetUserContextFile should not be called when per-user USER.md already has content")
	}
}

// TestSeedUserFiles_OpenAgent_UsesEmbeddedTemplate verifies that open agents
// are completely unaffected — they still receive embedded templates per-user.
func TestSeedUserFiles_OpenAgent_UsesEmbeddedTemplate(t *testing.T) {
	as := newSeedStub()
	agentID := uuid.New()
	// Open agents should never check agent_context_files for USER.md

	seeded, err := SeedUserFiles(context.Background(), as, agentID, "user-dave", store.AgentTypeOpen)
	if err != nil {
		t.Fatalf("SeedUserFiles returned error: %v", err)
	}

	// Open agents seed the full set: AGENTS.md, SOUL.md, IDENTITY.md, USER.md, BOOTSTRAP.md
	expectedFiles := map[string]bool{
		AgentsFile: true, SoulFile: true, IdentityFile: true, UserFile: true, BootstrapFile: true,
	}
	for _, f := range seeded {
		delete(expectedFiles, f)
	}
	if len(expectedFiles) > 0 {
		t.Errorf("open agent: missing seeded files: %v", expectedFiles)
	}

	// USER.md must have been written using embedded template (non-empty)
	got, ok := as.seededUserFiles[UserFile]
	if !ok {
		t.Fatal("open agent: USER.md was not written to user_context_files")
	}
	if got == "" {
		t.Error("open agent: seeded USER.md should not be empty")
	}
}

// TestSeedUserFiles_IdempotentOnSecondCall verifies that calling SeedUserFiles
// a second time for the same user does not re-seed already-present files.
func TestSeedUserFiles_IdempotentOnSecondCall(t *testing.T) {
	as := newSeedStub()
	agentID := uuid.New()

	// First call — seeds files
	SeedUserFiles(context.Background(), as, agentID, "user-frank", store.AgentTypePredefined)

	// Simulate what the first call wrote (move seededUserFiles → userFiles)
	maps.Copy(as.userFiles, as.seededUserFiles)
	as.seededUserFiles = make(map[string]string)

	// Second call — must seed nothing (all files already exist)
	seeded, err := SeedUserFiles(context.Background(), as, agentID, "user-frank", store.AgentTypePredefined)
	if err != nil {
		t.Fatalf("second SeedUserFiles returned error: %v", err)
	}
	if len(seeded) != 0 {
		t.Errorf("second call should seed nothing, but seeded: %v", seeded)
	}
	if len(as.seededUserFiles) != 0 {
		t.Errorf("second call should not write any files, but wrote: %v", as.seededUserFiles)
	}
}
