package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type staleEmbeddingSearcher struct {
	results []store.SkillSearchResult
}

func (s *staleEmbeddingSearcher) SearchByEmbedding(_ context.Context, _ []float32, _ int) ([]store.SkillSearchResult, error) {
	return s.results, nil
}
func (s *staleEmbeddingSearcher) SetEmbeddingProvider(store.EmbeddingProvider) {}
func (s *staleEmbeddingSearcher) BackfillSkillEmbeddings(context.Context) (int, error) {
	return 0, nil
}

type staleEmbeddingProvider struct{}

func (staleEmbeddingProvider) Name() string  { return "fake" }
func (staleEmbeddingProvider) Model() string { return "fake" }
func (staleEmbeddingProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}

// A skill row can outlive its files on disk. The vector-search branch takes the
// path straight from the DB row, so skill_search must not advertise a SKILL.md
// location that does not exist (the agent would then fail on read_file).
func TestSkillSearchTool_HybridSkipsSkillWithMissingFiles(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "skills-store", "goclaw-add-acp-provider", "3", "SKILL.md")
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("precondition: %s must not exist (err=%v)", missing, err)
	}

	loader := skills.NewLoader("", "", "")
	tool := NewSkillSearchTool(loader)
	tool.SetEmbeddingSearcher(&staleEmbeddingSearcher{results: []store.SkillSearchResult{{
		Name:        "goclaw-add-acp-provider",
		Slug:        "goclaw-add-acp-provider",
		Description: "add an ACP provider",
		Path:        missing,
		Score:       0.9,
	}}}, staleEmbeddingProvider{})

	res := tool.Execute(context.Background(), map[string]any{"query": "acp provider"})
	if res == nil {
		t.Fatal("Execute returned nil result")
	}
	if strings.Contains(res.ForLLM, missing) || strings.Contains(res.ForLLM, "goclaw-add-acp-provider") {
		t.Fatalf("skill_search advertised a non-existent SKILL.md location %q: %s", missing, res.ForLLM)
	}
}
