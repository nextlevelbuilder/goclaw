package mcp

import (
	"math"
	"strings"
	"unicode"
)

// MCPToolSearchResult is a single result from a BM25 search over MCP tools.
type MCPToolSearchResult struct {
	RegisteredName string  `json:"name"`
	OriginalName   string  `json:"original_name"`
	ServerName     string  `json:"server"`
	Description    string  `json:"description"`
	Score          float64 `json:"-"`
}

// toolDoc is an internal document for BM25 scoring.
type toolDoc struct {
	registeredName string
	originalName   string
	serverName     string
	description    string
	tokens         []string
}

// mcpBM25Index is a minimal BM25 index for MCP tool search.
type mcpBM25Index struct {
	docs  []toolDoc
	df    map[string]int
	avgDL float64
	k1    float64
	b     float64
}

func newMCPBM25Index() *mcpBM25Index {
	return &mcpBM25Index{
		df: make(map[string]int),
		k1: 1.2,
		b:  0.75,
	}
}

// build indexes a list of BridgeTools for BM25 search.
func (idx *mcpBM25Index) build(tools []*BridgeTool) {
	idx.docs = make([]toolDoc, 0, len(tools))
	idx.df = make(map[string]int)

	totalTokens := 0

	for _, bt := range tools {
		// Include server name + original tool name + description for broad matching
		searchText := bt.serverName + " " + bt.toolName + " " + bt.description
		tokens := tokenizeMCP(searchText)

		idx.docs = append(idx.docs, toolDoc{
			registeredName: bt.registeredName,
			originalName:   bt.toolName,
			serverName:     bt.serverName,
			description:    bt.description,
			tokens:         tokens,
		})

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
}

// search performs a BM25 search over indexed MCP tools.
func (idx *mcpBM25Index) search(query string, maxResults int) []MCPToolSearchResult {
	if maxResults <= 0 {
		maxResults = 5
	}

	queryTokens := tokenizeMCP(query)
	if len(queryTokens) == 0 || len(idx.docs) == 0 {
		return nil
	}

	N := float64(len(idx.docs))

	type scored struct {
		doc   toolDoc
		score float64
	}

	var results []scored

	for _, doc := range idx.docs {
		score := 0.0
		dl := float64(len(doc.tokens))

		tf := make(map[string]int)
		for _, t := range doc.tokens {
			tf[t]++
		}

		for _, qt := range queryTokens {
			termFreq := float64(tf[qt])
			if termFreq == 0 {
				continue
			}

			dfTerm := float64(idx.df[qt])
			idf := math.Log((N-dfTerm+0.5)/(dfTerm+0.5) + 1)

			numerator := termFreq * (idx.k1 + 1)
			denominator := termFreq + idx.k1*(1-idx.b+idx.b*dl/idx.avgDL)
			score += idf * numerator / denominator
		}

		if score > 0 {
			results = append(results, scored{doc: doc, score: score})
		}
	}

	// Sort by score descending (insertion sort, small N)
	for i := 1; i < len(results); i++ {
		key := results[i]
		j := i - 1
		for j >= 0 && results[j].score < key.score {
			results[j+1] = results[j]
			j--
		}
		results[j+1] = key
	}

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	out := make([]MCPToolSearchResult, len(results))
	for i, r := range results {
		out[i] = MCPToolSearchResult{
			RegisteredName: r.doc.registeredName,
			OriginalName:   r.doc.originalName,
			ServerName:     r.doc.serverName,
			Description:    r.doc.description,
			Score:          r.score,
		}
	}
	return out
}

// docCount returns the number of indexed documents.
func (idx *mcpBM25Index) docCount() int { return len(idx.docs) }

// tokenizeMCP splits text into lowercase tokens, removing punctuation.
// Mirrors skills.tokenize() (unexported, so duplicated here).
func tokenizeMCP(text string) []string {
	lower := strings.ToLower(text)

	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return ' '
	}, lower)

	fields := strings.Fields(cleaned)

	var tokens []string
	for _, f := range fields {
		if len(f) > 1 {
			tokens = append(tokens, f)
		}
	}
	return tokens
}
