package cmd

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// stubProjectStore implements ProjectStore for testing.
type stubProjectStore struct {
	store.ProjectStore // embed to satisfy interface; panics on unimplemented methods
	project            *store.Project
	chatErr            error
	overrides          map[string]map[string]string
	overridesErr       error
}

func (s *stubProjectStore) GetProjectByChatID(_ context.Context, _, _ string) (*store.Project, error) {
	return s.project, s.chatErr
}

func (s *stubProjectStore) GetMCPOverridesMap(_ context.Context, _ uuid.UUID) (map[string]map[string]string, error) {
	return s.overrides, s.overridesErr
}

func TestResolveProjectOverrides(t *testing.T) {
	projectID := uuid.Must(uuid.NewV7())

	tests := []struct {
		name          string
		store         store.ProjectStore
		channelType   string
		chatID        string
		wantProjectID string
		wantOverrides int
	}{
		{
			name:          "nil store returns empty (backward compat)",
			store:         nil,
			channelType:   "telegram",
			chatID:        "-100123",
			wantProjectID: "",
		},
		{
			name:          "empty channel type returns empty",
			store:         &stubProjectStore{},
			channelType:   "",
			chatID:        "-100123",
			wantProjectID: "",
		},
		{
			name:          "no project bound returns empty",
			store:         &stubProjectStore{project: nil},
			channelType:   "telegram",
			chatID:        "-100123",
			wantProjectID: "",
		},
		{
			name: "project found with overrides",
			store: &stubProjectStore{
				project: &store.Project{
					BaseModel: store.BaseModel{ID: projectID},
					Slug:      "my-project",
				},
				overrides: map[string]map[string]string{
					"github": {"GITHUB_REPO": "org/repo"},
				},
			},
			channelType:   "telegram",
			chatID:        "-100456",
			wantProjectID: projectID.String(),
			wantOverrides: 1,
		},
		{
			name: "project found without overrides",
			store: &stubProjectStore{
				project: &store.Project{
					BaseModel: store.BaseModel{ID: projectID},
					Slug:      "bare-project",
				},
			},
			channelType:   "discord",
			chatID:        "guild123",
			wantProjectID: projectID.String(),
			wantOverrides: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotOverrides := resolveProjectOverrides(context.Background(), tt.store, tt.channelType, tt.chatID)
			if gotID != tt.wantProjectID {
				t.Errorf("projectID = %q, want %q", gotID, tt.wantProjectID)
			}
			if len(gotOverrides) != tt.wantOverrides {
				t.Errorf("overrides count = %d, want %d", len(gotOverrides), tt.wantOverrides)
			}
		})
	}
}
