package memory

import (
	"reflect"
	"testing"
)

func TestExtractLinks(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []Link
	}{
		{
			name: "no wikilinks",
			body: "Plain markdown with [a real link](https://example.com).",
			want: nil,
		},
		{
			name: "basic wikilink",
			body: "See [[Dojo]] for details.",
			want: []Link{{Kind: LinkKindWiki, Target: "Dojo"}},
		},
		{
			name: "wikilink with display alias",
			body: "Built on [[Dojo|the Dojo engine]].",
			want: []Link{{Kind: LinkKindWiki, Target: "Dojo", Display: "the Dojo engine"}},
		},
		{
			name: "wikilink with folder path",
			body: "See [[wiki/projects/Dojo]] in the vault.",
			want: []Link{{Kind: LinkKindWiki, Target: "wiki/projects/Dojo"}},
		},
		{
			name: "wikilink with section anchor",
			body: "Per [[Dojo#Recent Activity]] this quarter.",
			want: []Link{{Kind: LinkKindWiki, Target: "Dojo", Section: "Recent Activity"}},
		},
		{
			name: "embed link",
			body: "Embedded: ![[diagram.png]]",
			want: []Link{{Kind: LinkKindEmbed, Target: "diagram.png"}},
		},
		{
			name: "block reference",
			body: "Quote [[notes#^abc123]].",
			want: []Link{{Kind: LinkKindBlock, Target: "notes", BlockID: "abc123"}},
		},
		{
			name: "multiple links in one body",
			body: "[[A]] mentions [[B|the B page]] and ![[C.png]] embed.",
			want: []Link{
				{Kind: LinkKindWiki, Target: "A"},
				{Kind: LinkKindWiki, Target: "B", Display: "the B page"},
				{Kind: LinkKindEmbed, Target: "C.png"},
			},
		},
		{
			name: "section + display together",
			body: "[[Note#section|alias]]",
			want: []Link{{Kind: LinkKindWiki, Target: "Note", Section: "section", Display: "alias"}},
		},
		{
			name: "trims whitespace inside brackets",
			body: "[[  Spaced Out  | display ]]",
			want: []Link{{Kind: LinkKindWiki, Target: "Spaced Out", Display: "display"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractLinks(tt.body)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("links mismatch\n got: %+v\nwant: %+v", got, tt.want)
			}
		})
	}
}

func TestResolveWikilink(t *testing.T) {
	candidates := []string{
		"memory/wiki/projects/dojo.md",
		"memory/wiki/projects/controller.md",
		"memory/project-updates/dojo/dojo/weekly/2026/q1/2026-01-13.md",
		"memory/voice-sessions/2026-05-04/1500-chill.md",
		"memory/wiki/contributors/dojo.md", // collides with projects/dojo.md by basename
	}
	tests := []struct {
		name   string
		target string
		want   string
		ok     bool
	}{
		// Two candidates have basename "dojo.md":
		//   memory/wiki/projects/dojo.md     (28 chars)
		//   memory/wiki/contributors/dojo.md (32 chars)
		// Shorter path wins (Obsidian's "closest in folder hierarchy"
		// approximation).
		{"basename — picks shortest path", "Dojo", "memory/wiki/projects/dojo.md", true},
		{"path hint with folder", "wiki/projects/dojo", "memory/wiki/projects/dojo.md", true},
		{"basename-only with .md suffix tolerated", "controller", "memory/wiki/projects/controller.md", true},
		{"unresolved target returns false", "Nonexistent", "", false},
		{"folder hint — no match", "wiki/projects/missing", "", false},
		{"empty target", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ResolveWikilink(tt.target, candidates)
			if got != tt.want || ok != tt.ok {
				t.Errorf("ResolveWikilink(%q) = %q, %v; want %q, %v",
					tt.target, got, ok, tt.want, tt.ok)
			}
		})
	}
}
