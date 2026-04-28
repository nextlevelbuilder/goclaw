package vault

import "testing"

// TestInferOwnerFromPath covers the tenant-wide path parser.
// All patterns now return the FULL relPath (no prefix stripping) so
// enrichment workers can locate files via filepath.Join(workspace, path).
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
		path             string
		wantAgentID      *string
		wantTeamID       *string
		wantScope        string
		wantStrippedPath string
	}{
		// Legacy agents/{key}/... → personal scope, full path preserved
		{
			path:             "agents/my-bot/notes/todo.md",
			wantAgentID:      new("uuid-1"),
			wantScope:        "personal",
			wantStrippedPath: "agents/my-bot/notes/todo.md",
		},
		{
			path:             "agents/my-bot/file.md",
			wantAgentID:      new("uuid-1"),
			wantScope:        "personal",
			wantStrippedPath: "agents/my-bot/file.md",
		},
		// Root-level {agent_key}/... → personal scope (workspace layout)
		{
			path:             "my-bot/telegram/123/report.md",
			wantAgentID:      new("uuid-1"),
			wantScope:        "personal",
			wantStrippedPath: "my-bot/telegram/123/report.md",
		},
		{
			path:             "other-bot/docs/guide.md",
			wantAgentID:      new("uuid-2"),
			wantScope:        "personal",
			wantStrippedPath: "other-bot/docs/guide.md",
		},
		// teams/{uuid}/... → team scope, full path preserved
		{
			path:             "teams/" + validUUID + "/doc.md",
			wantTeamID:       new(validUUID),
			wantScope:        "team",
			wantStrippedPath: "teams/" + validUUID + "/doc.md",
		},
		{
			path:             "teams/" + validUUID + "/deep/nested.md",
			wantTeamID:       new(validUUID),
			wantScope:        "team",
			wantStrippedPath: "teams/" + validUUID + "/deep/nested.md",
		},
		// Root-level file (no slash) → shared
		{
			path:             "README.md",
			wantScope:        "shared",
			wantStrippedPath: "README.md",
		},
		// Nested file not matching any agent key → shared
		{
			path:             "docs/guide.md",
			wantScope:        "shared",
			wantStrippedPath: "docs/guide.md",
		},
		// Unknown agent under agents/ prefix → skip
		{
			path:      "agents/unknown-bot/file.md",
			wantScope: "",
		},
		// Invalid team UUID → skip
		{
			path:      "teams/not-a-uuid/file.md",
			wantScope: "",
		},
		// Valid UUID but not in teamSet → skip
		{
			path:      "teams/11111111-2222-3333-4444-555555555555/file.md",
			wantScope: "",
		},
		// Unknown root folder (not an agent key) → shared
		{
			path:             "telegram/group/file.md",
			wantScope:        "shared",
			wantStrippedPath: "telegram/group/file.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			gotAgentID, gotTeamID, _, gotScope, gotPath := inferOwnerFromPath(tt.path, agentMap, teamSet)

			if gotScope != tt.wantScope {
				t.Errorf("scope = %q, want %q", gotScope, tt.wantScope)
			}
			if tt.wantScope == "" {
				return
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
		{"docs/spec.pdf", "document"},
		{"docs/sheet.xlsx", "document"},
		{"docs/slide.pptx", "document"},
		{"docs/memo.docx", "document"},
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

func TestInferDocType_PathPrefixWinsOverExt(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"memory/foo.md", "memory"},
		{"memory/snapshots/day.md", "memory"},
		{"skills/my-skill/README.md", "skill"},
		{"skills/foo/skill.md", "skill"},
		{"episodic/2024-01-01.json", "episodic"},
		{"path/to/SOUL.md", "context"},
		{"path/to/IDENTITY.md", "context"},
		{"path/to/AGENTS.md", "context"},
		{"notes/daily.md", "note"},
		{"photos/cat.png", "media"},
		{"videos/clip.mp4", "media"},
		{"audio/voice.mp3", "media"},
		{"docs/spec.pdf", "document"},
		{"data/file", "note"},
		{"binaries/tool.exe", "note"},
	}
	for _, c := range cases {
		if got := InferDocType(c.path); got != c.want {
			t.Errorf("InferDocType(%q) = %q; want %q", c.path, got, c.want)
		}
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

//go:fix inline
func strPtr(s string) *string { return new(s) }
