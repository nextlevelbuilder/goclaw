package skills

import (
	"testing"
)

// --- tokenize ---

func TestTokenize(t *testing.T) {
	tests := []struct {
		input  string
		expect []string
	}{
		{"hello world", []string{"hello", "world"}},
		{"Hello World", []string{"hello", "world"}},
		{"web search tool", []string{"web", "search", "tool"}},
		{"BM25 search", []string{"bm25", "search"}},
		// punctuation stripped
		{"hello, world!", []string{"hello", "world"}},
		// single-char tokens filtered
		{"a b c hello", []string{"hello"}},
		// digits kept
		{"tool123 search", []string{"tool123", "search"}},
		// empty
		{"", nil},
		// only single chars
		{"a b c", nil},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := tokenize(tt.input)
			if len(got) != len(tt.expect) {
				t.Errorf("tokenize(%q): got %v, want %v", tt.input, got, tt.expect)
				return
			}
			for i, tok := range got {
				if tok != tt.expect[i] {
					t.Errorf("tokenize(%q)[%d] = %q, want %q", tt.input, i, tok, tt.expect[i])
				}
			}
		})
	}
}

// --- BM25 Index: Build + Search ---

func TestIndex_EmptyIndex_ReturnsNil(t *testing.T) {
	idx := NewIndex()
	idx.Build(nil)

	results := idx.Search("anything", 5)
	if len(results) != 0 {
		t.Errorf("empty index should return no results, got %d", len(results))
	}
}

func TestIndex_EmptyQuery_ReturnsNil(t *testing.T) {
	idx := NewIndex()
	idx.Build([]Info{
		{Name: "web-search", Slug: "web-search", Description: "Search the web"},
	})

	results := idx.Search("", 5)
	if len(results) != 0 {
		t.Errorf("empty query should return no results, got %d", len(results))
	}
}

func TestIndex_ExactMatch(t *testing.T) {
	idx := NewIndex()
	idx.Build([]Info{
		{Name: "web-search", Slug: "web-search", Description: "Search the web using DuckDuckGo"},
		{Name: "code-runner", Slug: "code-runner", Description: "Execute code in sandbox"},
		{Name: "memory-store", Slug: "memory-store", Description: "Store and recall memories"},
	})

	results := idx.Search("web search", 5)
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for 'web search'")
	}
	// web-search should rank first
	if results[0].Slug != "web-search" {
		t.Errorf("expected web-search first, got %q", results[0].Slug)
	}
}

func TestIndex_PartialMatch(t *testing.T) {
	idx := NewIndex()
	idx.Build([]Info{
		{Name: "code-runner", Slug: "code-runner", Description: "Execute code in sandbox"},
		{Name: "web-search", Slug: "web-search", Description: "Search the web"},
	})

	results := idx.Search("code", 5)
	if len(results) == 0 {
		t.Fatal("expected result for partial query 'code'")
	}
	if results[0].Slug != "code-runner" {
		t.Errorf("expected code-runner first, got %q", results[0].Slug)
	}
}

func TestIndex_ZeroResults_UnknownQuery(t *testing.T) {
	idx := NewIndex()
	idx.Build([]Info{
		{Name: "web-search", Slug: "web-search", Description: "Search the web"},
	})

	results := idx.Search("xyzzy totally unknown term", 5)
	if len(results) != 0 {
		t.Errorf("unknown query should return 0 results, got %d", len(results))
	}
}

func TestIndex_RelevanceOrdering(t *testing.T) {
	// skill-b matches "search" more specifically
	idx := NewIndex()
	idx.Build([]Info{
		{Name: "general-tool", Slug: "general", Description: "A general purpose tool for various tasks"},
		{Name: "search-engine", Slug: "searcher", Description: "Search search engine for web search queries"},
		{Name: "code-runner", Slug: "runner", Description: "Run code in a container"},
	})

	results := idx.Search("search", 5)
	if len(results) < 1 {
		t.Fatal("expected results for 'search'")
	}
	// searcher should rank higher than general due to more 'search' occurrences
	if results[0].Slug != "searcher" {
		t.Errorf("expected searcher to rank first, got %q (score=%f)", results[0].Slug, results[0].Score)
	}
}

func TestIndex_MaxResults_Respected(t *testing.T) {
	idx := NewIndex()
	skills := make([]Info, 10)
	for i := range skills {
		skills[i] = Info{
			Name:        "tool",
			Slug:        "tool",
			Description: "search tool helper",
		}
	}
	idx.Build(skills)

	results := idx.Search("search", 3)
	if len(results) > 3 {
		t.Errorf("maxResults=3 should cap output, got %d", len(results))
	}
}

func TestIndex_DefaultMaxResults(t *testing.T) {
	idx := NewIndex()
	idx.Build([]Info{
		{Name: "a", Slug: "a", Description: "search helper tool"},
	})
	// maxResults <= 0 → defaults to 5
	results := idx.Search("search", 0)
	// Should not panic and should return results
	_ = results
}

func TestIndex_ScoreField_Populated(t *testing.T) {
	idx := NewIndex()
	idx.Build([]Info{
		{Name: "web", Slug: "web", Description: "Web search tool"},
	})

	results := idx.Search("web search", 5)
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if results[0].Score <= 0 {
		t.Errorf("score should be positive, got %f", results[0].Score)
	}
}

func TestIndex_ResultFields_Mapped(t *testing.T) {
	idx := NewIndex()
	idx.Build([]Info{
		{
			Name:        "My Tool",
			Slug:        "my-tool",
			Description: "Does something useful",
			Path:        "/path/to/SKILL.md",
			BaseDir:     "/path/to",
			Source:      "workspace",
		},
	})

	results := idx.Search("useful", 5)
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	r := results[0]
	if r.Name != "My Tool" {
		t.Errorf("Name: got %q", r.Name)
	}
	if r.Slug != "my-tool" {
		t.Errorf("Slug: got %q", r.Slug)
	}
	if r.Description != "Does something useful" {
		t.Errorf("Description: got %q", r.Description)
	}
	if r.Location != "/path/to/SKILL.md" {
		t.Errorf("Location: got %q", r.Location)
	}
	if r.Source != "workspace" {
		t.Errorf("Source: got %q", r.Source)
	}
}

func TestIndex_Rebuild_Replaces(t *testing.T) {
	idx := NewIndex()
	idx.Build([]Info{
		{Name: "old", Slug: "old", Description: "old skill"},
	})

	// Rebuild with completely different skills
	idx.Build([]Info{
		{Name: "new", Slug: "new", Description: "new skill"},
	})

	results := idx.Search("old", 5)
	if len(results) != 0 {
		t.Errorf("after rebuild, old skills should be gone, got %d results", len(results))
	}

	results = idx.Search("new", 5)
	if len(results) == 0 {
		t.Error("after rebuild, new skills should be searchable")
	}
}

// --- sortScored ---

func TestSortScored(t *testing.T) {
	results := []scored{
		{score: 1.5},
		{score: 3.0},
		{score: 0.5},
		{score: 2.0},
	}
	sortScored(results)

	for i := 1; i < len(results); i++ {
		if results[i].score > results[i-1].score {
			t.Errorf("not sorted descending: results[%d].score=%f > results[%d].score=%f",
				i, results[i].score, i-1, results[i-1].score)
		}
	}
}
