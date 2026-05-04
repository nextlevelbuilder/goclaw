package memory

import (
	"reflect"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantMeta Metadata
		wantBody string
	}{
		{
			name: "no frontmatter — body is full input",
			input: `# A regular note

Some content here.`,
			wantMeta: Metadata{},
			wantBody: "# A regular note\n\nSome content here.",
		},
		{
			name: "well-known fields",
			input: `---
title: Dojo
type: project
updated: "2026-04-09"
tags:
  - project
  - dojo
  - katana
aliases:
  - Dojo Engine
sources:
  - project-updates/dojo/dojo/weekly/2026/q1/
---
# Dojo

Body content.`,
			wantMeta: Metadata{
				Title:   "Dojo",
				Type:    "project",
				Updated: "2026-04-09",
				Tags:    []string{"project", "dojo", "katana"},
				Aliases: []string{"Dojo Engine"},
				Sources: []string{"project-updates/dojo/dojo/weekly/2026/q1/"},
			},
			wantBody: "# Dojo\n\nBody content.",
		},
		{
			name: "extra fields land in Extra map",
			input: `---
title: Person
type: contributor
github: glihm
discord_id: "123456"
---
Body`,
			wantMeta: Metadata{
				Title: "Person",
				Type:  "contributor",
				Extra: map[string]any{
					"github":     "glihm",
					"discord_id": "123456",
				},
			},
			wantBody: "Body",
		},
		{
			name: "single-string tag tolerated",
			input: `---
title: Quick note
tags: voice
---
content`,
			wantMeta: Metadata{
				Title: "Quick note",
				Tags:  []string{"voice"},
			},
			wantBody: "content",
		},
		{
			name: "no closing delimiter — treat as no frontmatter",
			input: `---
title: bad
content with no close`,
			wantMeta: Metadata{},
			wantBody: `---
title: bad
content with no close`,
		},
		{
			name: "malformed YAML — body still extracted, no metadata",
			input: `---
title: ok
tags: [unclosed
---
body here`,
			wantMeta: Metadata{},
			wantBody: "body here",
		},
		{
			name: "empty frontmatter block",
			input: `---
---
just body`,
			wantMeta: Metadata{},
			wantBody: "just body",
		},
		{
			name:     "leading BOM tolerated",
			input:    "\ufeff---\ntitle: BOM\n---\nbody",
			wantMeta: Metadata{Title: "BOM"},
			wantBody: "body",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMeta, gotBody := ParseFrontmatter(tt.input)
			if !reflect.DeepEqual(gotMeta, tt.wantMeta) {
				t.Errorf("metadata mismatch\n got: %+v\nwant: %+v", gotMeta, tt.wantMeta)
			}
			if gotBody != tt.wantBody {
				t.Errorf("body mismatch\n got: %q\nwant: %q", gotBody, tt.wantBody)
			}
		})
	}
}

func TestMetadataHasContent(t *testing.T) {
	if (Metadata{}).HasContent() {
		t.Error("zero Metadata reports HasContent")
	}
	if !(Metadata{Title: "x"}).HasContent() {
		t.Error("Title alone should report HasContent")
	}
	if !(Metadata{Tags: []string{"a"}}).HasContent() {
		t.Error("Tags alone should report HasContent")
	}
	if !(Metadata{Extra: map[string]any{"x": 1}}).HasContent() {
		t.Error("Extra alone should report HasContent")
	}
}
