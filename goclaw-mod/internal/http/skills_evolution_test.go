package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestApplySkillSuggestionPatchCreatesNewReferenceFile(t *testing.T) {
	handler, skillStore, ctx, root := newTestUploadHandler(t)
	evolution := &skillEvolutionStoreStub{}
	handler.SetEvolutionStore(evolution, nil)

	currentDir := filepath.Join(root, "skills-store", "reference-skill", "1")
	if err := os.MkdirAll(currentDir, 0755); err != nil {
		t.Fatalf("mkdir current skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(currentDir, "SKILL.md"), []byte(skillMarkdown("Reference Skill", "reference-skill")), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	skillID := skillStore.seedCustomSkill("reference-skill", currentDir, "active", nil)
	content := "Use the documented query syntax.\n"
	patch, err := json.Marshal(skillDraftPatch{Content: &content})
	if err != nil {
		t.Fatalf("marshal draft patch: %v", err)
	}
	suggestion := &store.SkillImprovementSuggestion{
		ID:             uuid.New(),
		SkillID:        skillID,
		TargetFile:     "references/troubleshooting.md",
		DraftPatch:     patch,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		SkillSlug:      "reference-skill",
		Status:         store.SkillSuggestionStatusApproved,
		SuggestionType: "skill_reference_add",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/skills/"+skillID.String()+"/evolution/suggestions/"+suggestion.ID.String()+"/apply", nil).WithContext(ctx)

	applied, err := handler.applySkillSuggestionPatch(req, skillID, suggestion)
	if err != nil {
		t.Fatalf("apply suggestion: %v", err)
	}
	if applied.Status != store.SkillSuggestionStatusApplied {
		t.Fatalf("status = %q, want applied", applied.Status)
	}

	newReference := filepath.Join(root, "skills-store", "reference-skill", "2", "references", "troubleshooting.md")
	got, err := os.ReadFile(newReference)
	if err != nil {
		t.Fatalf("read created reference: %v", err)
	}
	if string(got) != content {
		t.Fatalf("created reference = %q, want %q", got, content)
	}
	if len(evolution.versions) != 1 || evolution.versions[0].Version != 2 {
		t.Fatalf("versions = %+v, want one version 2", evolution.versions)
	}
}

func TestApplySkillSuggestionPatchKeepsActiveFilesWhenVersionRecordFails(t *testing.T) {
	handler, skillStore, ctx, root := newTestUploadHandler(t)
	evolution := &skillEvolutionStoreStub{createVersionErr: errors.New("version store unavailable")}
	handler.SetEvolutionStore(evolution, nil)

	currentDir := filepath.Join(root, "skills-store", "failure-skill", "1")
	if err := os.MkdirAll(currentDir, 0755); err != nil {
		t.Fatalf("mkdir current skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(currentDir, "SKILL.md"), []byte(skillMarkdown("Failure Skill", "failure-skill")), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	skillID := skillStore.seedCustomSkill("failure-skill", currentDir, "active", nil)
	content := "Keep active files when metadata recording fails.\n"
	patch, err := json.Marshal(skillDraftPatch{Content: &content})
	if err != nil {
		t.Fatalf("marshal draft patch: %v", err)
	}
	suggestion := &store.SkillImprovementSuggestion{
		ID:             uuid.New(),
		SkillID:        skillID,
		TargetFile:     "references/recovery.md",
		DraftPatch:     patch,
		Status:         store.SkillSuggestionStatusApproved,
		SuggestionType: "skill_reference_add",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/skills/"+skillID.String()+"/evolution/suggestions/"+suggestion.ID.String()+"/apply", nil).WithContext(ctx)

	_, err = handler.applySkillSuggestionPatch(req, skillID, suggestion)
	if err == nil || !strings.Contains(err.Error(), "record skill version") {
		t.Fatalf("error = %v, want record skill version failure", err)
	}

	newDir := filepath.Join(root, "skills-store", "failure-skill", "2")
	if _, err := os.Stat(newDir); err != nil {
		t.Fatalf("new active skill dir missing after metadata failure: %v", err)
	}
	created, err := os.ReadFile(filepath.Join(newDir, "references", "recovery.md"))
	if err != nil {
		t.Fatalf("read preserved reference: %v", err)
	}
	if string(created) != content {
		t.Fatalf("preserved reference = %q, want %q", created, content)
	}
	info, ok := skillStore.GetSkillByID(ctx, skillID)
	if !ok {
		t.Fatal("skill not found after apply failure")
	}
	if info.BaseDir != newDir || info.Version != 2 {
		t.Fatalf("skill active target = (%q, v%d), want (%q, v2)", info.BaseDir, info.Version, newDir)
	}
}

func TestSkillEvolutionMutationRouteRejectsNonTenantAdmin(t *testing.T) {
	handler, skillStore, _, root := newTestUploadHandler(t)
	tenantID := uuid.New()
	skillID := skillStore.seedCustomSkillForTenant(tenantID, "evolution-skill", filepath.Join(root, "tenants", tenantID.String(), "skills-store", "evolution-skill", "1"), "active", nil)
	tenants := newMockTenantStore()
	tenants.addTenant(tenantID, "tenant-a")
	tenants.setUserRole(tenantID, "scope-admin", store.TenantRoleMember)
	handler.tenantStore = tenants
	evolution := &skillEvolutionStoreStub{}
	handler.SetEvolutionStore(evolution, nil)
	rawKey := "evolution-admin"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(rawKey): {
			ID:       uuid.New(),
			Scopes:   []string{"operator.admin"},
			TenantID: tenantID,
			OwnerID:  "scope-admin",
		},
	})

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPatch, "/v1/skills/"+skillID.String()+"/evolution", strings.NewReader(`{"enabled":true}`))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403", w.Code, w.Body.String())
	}
	if evolution.settingsUpdates != 0 {
		t.Fatalf("settings updates = %d, want 0", evolution.settingsUpdates)
	}
}

func TestHandleSuggestionStatusDoesNotUpdateDifferentSkillSuggestion(t *testing.T) {
	handler, skillStore, ctx, root := newTestUploadHandler(t)
	requestSkillID := skillStore.seedCustomSkill("request-skill", filepath.Join(root, "skills-store", "request-skill", "1"), "active", nil)
	ownerSkillID := skillStore.seedCustomSkill("owner-skill", filepath.Join(root, "skills-store", "owner-skill", "1"), "active", nil)
	suggestionID := uuid.New()
	evolution := &skillEvolutionStoreStub{
		suggestions: map[uuid.UUID]*store.SkillImprovementSuggestion{
			suggestionID: {
				ID:             suggestionID,
				SkillID:        ownerSkillID,
				Status:         store.SkillSuggestionStatusPending,
				SuggestionType: "skill_reference_add",
			},
		},
	}
	handler.SetEvolutionStore(evolution, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/skills/"+requestSkillID.String()+"/evolution/suggestions/"+suggestionID.String()+"/approve", nil).WithContext(ctx)
	req.SetPathValue("id", requestSkillID.String())
	req.SetPathValue("suggestionID", suggestionID.String())
	w := httptest.NewRecorder()

	handler.handleApproveSkillSuggestion(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s, want 404", w.Code, w.Body.String())
	}
	if evolution.statusUpdates != 0 {
		t.Fatalf("status updates = %d, want 0", evolution.statusUpdates)
	}
	if got := evolution.suggestions[suggestionID].Status; got != store.SkillSuggestionStatusPending {
		t.Fatalf("suggestion status = %q, want pending", got)
	}
}

type skillEvolutionStoreStub struct {
	versions         []store.SkillVersion
	suggestions      map[uuid.UUID]*store.SkillImprovementSuggestion
	statusUpdates    int
	settingsUpdates  int
	createVersionErr error
	markAppliedErr   error
}

func (s *skillEvolutionStoreStub) GetSettings(context.Context, uuid.UUID) (*store.SkillEvolutionSettings, error) {
	return nil, nil
}

func (s *skillEvolutionStoreStub) UpsertSettings(context.Context, store.SkillEvolutionSettings) (*store.SkillEvolutionSettings, error) {
	s.settingsUpdates++
	return &store.SkillEvolutionSettings{}, nil
}

func (s *skillEvolutionStoreStub) RecordUsage(context.Context, store.SkillUsageMetric) error {
	return nil
}

func (s *skillEvolutionStoreStub) AggregateUsage(context.Context, uuid.UUID, *time.Time) (*store.SkillUsageStats, error) {
	return nil, nil
}

func (s *skillEvolutionStoreStub) ListUsage(context.Context, uuid.UUID, int) ([]store.SkillUsageMetric, error) {
	return nil, nil
}

func (s *skillEvolutionStoreStub) CreateSuggestion(context.Context, store.SkillImprovementSuggestion) (*store.SkillImprovementSuggestion, error) {
	return nil, nil
}

func (s *skillEvolutionStoreStub) ListSuggestions(context.Context, uuid.UUID, string, int) ([]store.SkillImprovementSuggestion, error) {
	return nil, nil
}

func (s *skillEvolutionStoreStub) GetSuggestion(_ context.Context, id uuid.UUID) (*store.SkillImprovementSuggestion, error) {
	if s.suggestions == nil {
		return nil, nil
	}
	sg, ok := s.suggestions[id]
	if !ok {
		return nil, nil
	}
	copied := *sg
	return &copied, nil
}

func (s *skillEvolutionStoreStub) UpdateSuggestionStatus(_ context.Context, id uuid.UUID, status, actorType, actorID string) (*store.SkillImprovementSuggestion, error) {
	s.statusUpdates++
	if s.suggestions == nil {
		return nil, fmt.Errorf("suggestion not found")
	}
	sg, ok := s.suggestions[id]
	if !ok {
		return nil, fmt.Errorf("suggestion not found")
	}
	sg.Status = status
	sg.ReviewedByActorType = actorType
	sg.ReviewedByActorID = actorID
	now := time.Now()
	sg.ReviewedAt = &now
	return sg, nil
}

func (s *skillEvolutionStoreStub) MarkSuggestionApplied(_ context.Context, id uuid.UUID, version int, actorType, actorID string) (*store.SkillImprovementSuggestion, error) {
	if s.markAppliedErr != nil {
		return nil, s.markAppliedErr
	}
	return &store.SkillImprovementSuggestion{
		ID:                  id,
		Status:              store.SkillSuggestionStatusApplied,
		ReviewedByActorType: actorType,
		ReviewedByActorID:   actorID,
		AppliedVersion:      &version,
	}, nil
}

func (s *skillEvolutionStoreStub) CreateSkillVersion(_ context.Context, version store.SkillVersion) (*store.SkillVersion, error) {
	if s.createVersionErr != nil {
		return nil, s.createVersionErr
	}
	s.versions = append(s.versions, version)
	return &version, nil
}

func (s *skillEvolutionStoreStub) ListSkillVersions(context.Context, uuid.UUID, int) ([]store.SkillVersion, error) {
	return s.versions, nil
}

func (s *skillEvolutionStoreStub) GetSkillVersion(context.Context, uuid.UUID, int) (*store.SkillVersion, error) {
	return nil, nil
}
