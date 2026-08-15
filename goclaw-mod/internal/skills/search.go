package skills

import (
	"math"
	"strings"
	"sync"
	"unicode"
)

// SkillSearchResult is a single result from a skill search.
type SkillSearchResult struct {
	Name        string  `json:"name"`
	Slug        string  `json:"slug"` // directory name (unique identifier, used for access filtering)
	Description string  `json:"description"`
	Location    string  `json:"location"` // absolute path to SKILL.md
	BaseDir     string  `json:"baseDir"`  // skill directory (for {baseDir} references)
	Source      string  `json:"source"`   // "workspace", "global", "builtin", "managed"
	Score       float64 `json:"score"`
}

// skillDoc is an internal representation of a skill document for BM25 scoring.
type skillDoc struct {
	info   Info
	tokens []string // pre-tokenized search text (lowercased)
}

// scored pairs a document with its BM25 relevance score.
type scored struct {
	doc   skillDoc
	score float64
}

// SkillEmbedder computes embeddings for skill search queries.
// When set on Index, Search() uses hybrid BM25+vector scoring.
// When nil, falls back to BM25-only (current default behavior).
type SkillEmbedder interface {
	// EmbedQuery returns a vector embedding for the search query.
	EmbedQuery(query string) ([]float32, error)
	// EmbedSkills pre-computes embeddings for all skill descriptions.
	// Called during Index.Build(). Returns map[slug]→embedding.
	EmbedSkills(skills []Info) (map[string][]float32, error)
}

// Index is an in-memory BM25 index for skill search.
type Index struct {
	mu    sync.RWMutex
	docs  []skillDoc
	df    map[string]int // document frequency: term → number of docs containing it
	avgDL float64        // average document length (in tokens)
	k1    float64        // BM25 term frequency saturation (default 1.2)
	b     float64        // BM25 length normalization (default 0.75)

	// Optional: embedding-based hybrid search (nil = BM25 only).
	embedder   SkillEmbedder
	embeddings map[string][]float32 // slug → embedding vector
}

// NewIndex creates a new empty skill search index.
func NewIndex() *Index {
	return &Index{
		df: make(map[string]int),
		k1: 1.2,
		b:  0.75,
	}
}

// SetEmbedder enables hybrid BM25+vector search.
// Must be called before Build(). Nil disables vector scoring (BM25 only).
func (idx *Index) SetEmbedder(e SkillEmbedder) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.embedder = e
}

// Build indexes a list of skills for BM25 search.
// If embedder is set, also pre-computes skill embeddings for hybrid search.
// Call this at startup and whenever the skill set changes.
func (idx *Index) Build(skills []Info) {
	idx.mu.Lock()

	idx.docs = make([]skillDoc, 0, len(skills))
	idx.df = make(map[string]int)

	totalTokens := 0

	for _, s := range skills {
		// Build searchable text from name + description
		searchText := s.Name + " " + s.Description
		tokens := tokenize(searchText)

		idx.docs = append(idx.docs, skillDoc{
			info:   s,
			tokens: tokens,
		})

		// Count document frequency (unique terms per document)
		seen := make(map[string]bool)
		for _, t := range tokens {
			if !seen[t] {
				idx.df[t]++
				seen[t] = true
			}
		}

		totalTokens += len(tokens)
	}

	if len(idx.docs) > 0 {
		idx.avgDL = float64(totalTokens) / float64(len(idx.docs))
	}

	// Capture embedder ref before releasing lock — EmbedSkills is an external API call
	// that can take seconds; holding the lock would block all Search() callers.
	embedder := idx.embedder
	idx.mu.Unlock()

	// Pre-compute embeddings if embedder is available (best-effort).
	if embedder != nil {
		embs, err := embedder.EmbedSkills(skills)
		if err == nil && len(embs) > 0 {
			idx.mu.Lock()
			idx.embeddings = embs
			idx.mu.Unlock()
		}
	}
}

// Search performs a BM25 search over the indexed skills.
// Returns up to maxResults results sorted by relevance score (highest first).
func (idx *Index) Search(query string, maxResults int) []SkillSearchResult {
	if maxResults <= 0 {
		maxResults = 5
	}

	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if len(idx.docs) == 0 {
		return nil
	}

	N := float64(len(idx.docs))

	var results []scored

	for _, doc := range idx.docs {
		score := 0.0
		dl := float64(len(doc.tokens))

		// Count term frequencies in this document
		tf := make(map[string]int)
		for _, t := range doc.tokens {
			tf[t]++
		}

		for _, qt := range queryTokens {
			termFreq := float64(tf[qt])
			if termFreq == 0 {
				continue
			}

			// IDF: log((N - df + 0.5) / (df + 0.5) + 1)
			dfTerm := float64(idx.df[qt])
			idf := math.Log((N-dfTerm+0.5)/(dfTerm+0.5) + 1)

			// BM25: IDF * tf * (k1+1) / (tf + k1 * (1 - b + b * dl/avgdl))
			numerator := termFreq * (idx.k1 + 1)
			denominator := termFreq + idx.k1*(1-idx.b+idx.b*dl/idx.avgDL)
			score += idf * numerator / denominator
		}

		if score > 0 {
			results = append(results, scored{doc: doc, score: score})
		}
	}

	// Sort by score descending
	sortScored(results)

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	out := make([]SkillSearchResult, len(results))
	for i, r := range results {
		out[i] = SkillSearchResult{
			Name:        r.doc.info.Name,
			Slug:        r.doc.info.Slug,
			Description: r.doc.info.Description,
			Location:    r.doc.info.Path,
			BaseDir:     r.doc.info.BaseDir,
			Source:      r.doc.info.Source,
			Score:       r.score,
		}
	}

	return out
}

// tokenize splits text into lowercase tokens, removing punctuation.
func tokenize(text string) []string {
	lower := strings.ToLower(text)

	// Replace non-alphanumeric with spaces
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return ' '
	}, lower)

	fields := strings.Fields(cleaned)

	// Filter out very short tokens (1 char)
	var tokens []string
	for _, f := range fields {
		if len(f) > 1 {
			tokens = append(tokens, f)
		}
	}
	return tokens
}

// sortScored sorts scored results by score descending (simple insertion sort for small N).
func sortScored(results []scored) {
	for i := 1; i < len(results); i++ {
		key := results[i]
		j := i - 1
		for j >= 0 && results[j].score < key.score {
			results[j+1] = results[j]
			j--
		}
		results[j+1] = key
	}
}
