package vault

import "testing"

// TestInferOwnerFromPath covers the new tenant-wide path parser.
func TestInferOwnerFromPath(t *testing.T) {
	agentMap := map[string]string{
		"my-bot":    "uuid-1",
		"other-bot": "uuid-2",
	}
	validUUID := "550e8400-e29b-41d4-a716-446655440000"
	teamSet := map[string]bool{
		validUUID: true,
	}

	tests := []struct {
		path            string
		wantAgentID     *string
		wantTeamID      *string
		wantScope       string
		wantStrippedPath string
	}{
		// agents/{key}/... → personal scope, strip prefix
		{
			path:            "agents/my-bot/notes/todo.md",
			wantAgentID:     strPtr("uuid-1"),
			wantScope:       "personal",
			wantStrippedPath: "notes/todo.md",
		},
		// agents/{key} with no trailing path → personal, empty stripped path
		{
			path:            "agents/my-bot/file.md",
			wantAgentID:     strPtr("uuid-1"),
			wantScope:       "personal",
			wantStrippedPath: "file.md",
		},
		// teams/{uuid}/... → team scope, strip prefix
		{
			path:            "teams/" + validUUID + "/doc.md",
			wantTeamID:      strPtr(validUUID),
			wantScope:       "team",
			wantStrippedPath: "doc.md",
		},
		// teams/{uuid}/deep/nested → team scope
		{
			path:            "teams/" + validUUID + "/deep/nested.md",
			wantTeamID:      strPtr(validUUID),
			wantScope:       "team",
			wantStrippedPath: "deep/nested.md",
		},
		// root-level file → shared scope, path unchanged
		{
			path:            "README.md",
			wantScope:       "shared",
			wantStrippedPath: "README.md",
		},
		// nested file not under agents/ or teams/ → shared
		{
			path:            "docs/guide.md",
			wantScope:       "shared",
			wantStrippedPath: "docs/guide.md",
		},
		// unknown agent → skip (scope="")
		{
			path:      "agents/unknown-bot/file.md",
			wantScope: "",
		},
		// invalid team UUID → skip
		{
			path:      "teams/not-a-uuid/file.md",
			wantScope: "",
		},
		// valid UUID but not in teamSet → skip
		{
			path:      "teams/11111111-2222-3333-4444-555555555555/file.md",
			wantScope: "",
		},
		// malformed agents path (no trailing file) is still an unknown agent key check
		{
			path:      "agents/unknown/",
			wantScope: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			gotAgentID, gotTeamID, gotScope, gotPath := inferOwnerFromPath(tt.path, agentMap, teamSet)

			if gotScope != tt.wantScope {
				t.Errorf("scope = %q, want %q", gotScope, tt.wantScope)
			}
			if tt.wantScope == "" {
				return // skip is signaled; remaining fields don't matter
			}
			if tt.wantStrippedPath != "" && gotPath != tt.wantStrippedPath {
				t.Errorf("strippedPath = %q, want %q", gotPath, tt.wantStrippedPath)
			}
			if tt.wantAgentID != nil {
				if gotAgentID == nil || *gotAgentID != *tt.wantAgentID {
					t.Errorf("agentID = %v, want %q", gotAgentID, *tt.wantAgentID)
				}
			} else if gotAgentID != nil {
				t.Errorf("agentID = %v, want nil", gotAgentID)
			}
			if tt.wantTeamID != nil {
				if gotTeamID == nil || *gotTeamID != *tt.wantTeamID {
					t.Errorf("teamID = %v, want %q", gotTeamID, *tt.wantTeamID)
				}
			} else if gotTeamID != nil {
				t.Errorf("teamID = %v, want nil", gotTeamID)
			}
		})
	}
}

func TestInferVaultDocType(t *testing.T) {
	tests := []struct {
		path    string
		docType string
	}{
		{"screenshot.png", "media"},
		{"photo.jpg", "media"},
		{"video.mp4", "media"},
		{"audio.mp3", "media"},
		{"notes/meeting.md", "note"},
		{"report.txt", "note"},
		{"web-fetch/page.html", "note"},
		{"skills/my-skill/SKILL.md", "skill"},
		{"deep/soul.md", "context"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := InferDocType(tt.path)
			if got != tt.docType {
				t.Errorf("InferDocType(%q) = %q, want %q", tt.path, got, tt.docType)
			}
		})
	}
}

func TestInferTitle(t *testing.T) {
	tests := []struct {
		path  string
		title string
	}{
		{"report.md", "report"},
		{"notes/meeting-notes.txt", "meeting-notes"},
		{"deep/nested/file.png", "file"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := InferTitle(tt.path)
			if got != tt.title {
				t.Errorf("InferTitle(%q) = %q, want %q", tt.path, got, tt.title)
			}
		})
	}
}

func strPtr(s string) *string { return &s }
