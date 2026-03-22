package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// stubProjectStore implements store.ProjectStore for testing.
type stubProjectStore struct {
	store.ProjectStore // embed interface — panics on unimplemented methods

	project   *store.Project
	overrides map[string]map[string]string
	chatErr   error
	overErr   error
}

func (s *stubProjectStore) GetProjectByChatID(_ context.Context, _, _ string) (*store.Project, error) {
	return s.project, s.chatErr
}

func (s *stubProjectStore) GetMCPOverridesMap(_ context.Context, _ uuid.UUID) (map[string]map[string]string, error) {
	return s.overrides, s.overErr
}

func TestResolveProjectOverrides(t *testing.T) {
	testProjectID := uuid.New()

	tests := []struct {
		name          string
		store         store.ProjectStore
		channelType   string
		chatID        string
		wantProjectID string
		wantOverrides map[string]map[string]string
	}{
		{
			name:          "nil store returns empty (backward compat)",
			store:         nil,
			channelType:   "telegram",
			chatID:        "-100123",
			wantProjectID: "",
			wantOverrides: nil,
		},
		{
			name:          "empty channelType returns empty",
			store:         &stubProjectStore{},
			channelType:   "",
			chatID:        "-100123",
			wantProjectID: "",
			wantOverrides: nil,
		},
		{
			name:          "empty chatID returns empty",
			store:         &stubProjectStore{},
			channelType:   "telegram",
			chatID:        "",
			wantProjectID: "",
			wantOverrides: nil,
		},
		{
			name: "no project found returns empty (not an error)",
			store: &stubProjectStore{
				project: nil,
				chatErr: nil,
			},
			channelType:   "telegram",
			chatID:        "-100999",
			wantProjectID: "",
			wantOverrides: nil,
		},
		{
			name: "project found with overrides",
			store: &stubProjectStore{
				project: &store.Project{
					BaseModel: store.BaseModel{ID: testProjectID},
					Slug:      "xpos",
				},
				overrides: map[string]map[string]string{
					"gitlab":    {"GITLAB_PROJECT_PATH": "duhd/xpos"},
					"atlassian": {"JIRA_PROJECT_KEY": "XPOS"},
				},
			},
			channelType:   "telegram",
			chatID:        "-100123",
			wantProjectID: testProjectID.String(),
			wantOverrides: map[string]map[string]string{
				"gitlab":    {"GITLAB_PROJECT_PATH": "duhd/xpos"},
				"atlassian": {"JIRA_PROJECT_KEY": "XPOS"},
			},
		},
		{
			name: "project found but no overrides configured",
			store: &stubProjectStore{
				project: &store.Project{
					BaseModel: store.BaseModel{ID: testProjectID},
					Slug:      "empty-proj",
				},
				overrides: map[string]map[string]string{},
			},
			channelType:   "telegram",
			chatID:        "-100456",
			wantProjectID: testProjectID.String(),
			wantOverrides: map[string]map[string]string{},
		},
		{
			name: "DB error on project lookup — graceful degradation",
			store: &stubProjectStore{
				chatErr: errors.New("connection refused"),
			},
			channelType:   "telegram",
			chatID:        "-100123",
			wantProjectID: "",
			wantOverrides: nil,
		},
		{
			name: "project found but overrides query fails — returns projectID only",
			store: &stubProjectStore{
				project: &store.Project{
					BaseModel: store.BaseModel{ID: testProjectID},
					Slug:      "xpos",
				},
				overErr: errors.New("timeout"),
			},
			channelType:   "telegram",
			chatID:        "-100123",
			wantProjectID: testProjectID.String(),
			wantOverrides: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			gotID, gotOverrides := resolveProjectOverrides(ctx, tt.store, tt.channelType, tt.chatID)

			if gotID != tt.wantProjectID {
				t.Errorf("projectID: got %q, want %q", gotID, tt.wantProjectID)
			}
			if tt.wantOverrides == nil {
				if gotOverrides != nil {
					t.Errorf("overrides: got %v, want nil", gotOverrides)
				}
				return
			}
			if len(gotOverrides) != len(tt.wantOverrides) {
				t.Errorf("overrides len: got %d, want %d", len(gotOverrides), len(tt.wantOverrides))
				return
			}
			for server, wantEnv := range tt.wantOverrides {
				gotEnv, ok := gotOverrides[server]
				if !ok {
					t.Errorf("missing server %q in overrides", server)
					continue
				}
				for k, wantV := range wantEnv {
					if gotV := gotEnv[k]; gotV != wantV {
						t.Errorf("overrides[%q][%q]: got %q, want %q", server, k, gotV, wantV)
					}
				}
			}
		})
	}
}
