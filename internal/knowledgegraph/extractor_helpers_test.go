package knowledgegraph

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// --- mockProvider implements providers.Provider for unit tests (no network) ---

type mockProvider struct {
	response providers.ChatResponse
	err      error
}

func (m *mockProvider) Chat(_ context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	r := m.response
	return &r, nil
}

func (m *mockProvider) ChatStream(_ context.Context, _ providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	r := m.response
	return &r, nil
}

func (m *mockProvider) DefaultModel() string { return "mock-model" }
func (m *mockProvider) Name() string         { return "mock" }

// --- NewExtractor ---

func TestNewExtractor_DefaultConfidence(t *testing.T) {
	e := NewExtractor(&mockProvider{}, "model", 0)
	if e.minConfidence != 0.75 {
		t.Errorf("expected default minConfidence=0.75, got %f", e.minConfidence)
	}
}

func TestNewExtractor_CustomConfidence(t *testing.T) {
	e := NewExtractor(&mockProvider{}, "model", 0.9)
	if e.minConfidence != 0.9 {
		t.Errorf("expected minConfidence=0.9, got %f", e.minConfidence)
	}
}

func TestNewExtractor_NegativeConfidence(t *testing.T) {
	e := NewExtractor(&mockProvider{}, "model", -1)
	if e.minConfidence != 0.75 {
		t.Errorf("expected default minConfidence=0.75 for negative input, got %f", e.minConfidence)
	}
}

// --- splitChunks ---

func TestSplitChunks_ShortText(t *testing.T) {
	text := "short text"
	chunks := splitChunks(text, 100)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != text {
		t.Errorf("chunk mismatch: got %q", chunks[0])
	}
}

func TestSplitChunks_ExactLimit(t *testing.T) {
	text := strings.Repeat("a", 100)
	chunks := splitChunks(text, 100)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for exact-limit text, got %d", len(chunks))
	}
}

func TestSplitChunks_LongTextSplitsAtParagraph(t *testing.T) {
	// Two paragraphs each >50 chars; max=100 → should split.
	para1 := strings.Repeat("a", 60)
	para2 := strings.Repeat("b", 60)
	text := para1 + "\n\n" + para2

	chunks := splitChunks(text, 100)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks for text > maxChars, got %d", len(chunks))
	}
	for i, ch := range chunks {
		if strings.TrimSpace(ch) == "" {
			t.Errorf("chunk %d is empty", i)
		}
	}
}

func TestSplitChunks_NoParagraphBreak(t *testing.T) {
	// No \n\n → hard cut at maxChars boundary.
	text := strings.Repeat("x", 200)
	chunks := splitChunks(text, 100)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}
	// Verify no data loss.
	var total int
	for _, ch := range chunks {
		total += len(ch)
	}
	if total != len(text) {
		t.Errorf("chunks total length %d != original %d", total, len(text))
	}
}

func TestSplitChunks_Empty(t *testing.T) {
	chunks := splitChunks("", 100)
	// len("") <= maxChars → single (empty) chunk.
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for empty string, got %d", len(chunks))
	}
}

func TestSplitChunks_MultipleChunks(t *testing.T) {
	// 5 paragraphs of 40 chars each; max=50 → multiple chunks.
	var paras []string
	for i := 0; i < 5; i++ {
		paras = append(paras, strings.Repeat(fmt.Sprintf("%d", i), 40))
	}
	text := strings.Join(paras, "\n\n")
	chunks := splitChunks(text, 50)
	if len(chunks) < 3 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
}

// --- mergeResults ---

func TestMergeResults_Empty(t *testing.T) {
	result := mergeResults(&ExtractionResult{}, &ExtractionResult{})
	if len(result.Entities) != 0 || len(result.Relations) != 0 {
		t.Error("merging two empty results should produce empty result")
	}
}

func TestMergeResults_DeduplicatesEntitiesByExternalID(t *testing.T) {
	a := &ExtractionResult{
		Entities: []store.Entity{
			{ExternalID: "alice", Name: "Alice", Confidence: 0.8},
			{ExternalID: "bob", Name: "Bob", Confidence: 0.7},
		},
	}
	b := &ExtractionResult{
		Entities: []store.Entity{
			// Same external_id as alice but higher confidence — should win.
			{ExternalID: "alice", Name: "Alice Updated", Confidence: 0.95},
			{ExternalID: "carol", Name: "Carol", Confidence: 0.9},
		},
	}

	result := mergeResults(a, b)
	entityMap := make(map[string]store.Entity)
	for _, e := range result.Entities {
		entityMap[e.ExternalID] = e
	}

	if len(entityMap) != 3 {
		t.Fatalf("expected 3 unique entities, got %d", len(result.Entities))
	}
	if entityMap["alice"].Confidence != 0.95 {
		t.Errorf("alice: expected confidence 0.95 (higher wins), got %f", entityMap["alice"].Confidence)
	}
	if _, ok := entityMap["bob"]; !ok {
		t.Error("bob should be in merged result")
	}
	if _, ok := entityMap["carol"]; !ok {
		t.Error("carol should be in merged result")
	}
}

func TestMergeResults_KeepsHigherConfidence(t *testing.T) {
	a := &ExtractionResult{
		Entities: []store.Entity{{ExternalID: "alice", Confidence: 0.95}},
	}
	b := &ExtractionResult{
		Entities: []store.Entity{{ExternalID: "alice", Confidence: 0.5}},
	}

	result := mergeResults(a, b)
	if len(result.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(result.Entities))
	}
	if result.Entities[0].Confidence != 0.95 {
		t.Errorf("expected higher confidence to be kept, got %f", result.Entities[0].Confidence)
	}
}

func TestMergeResults_DeduplicatesRelations(t *testing.T) {
	rel := store.Relation{SourceEntityID: "alice", RelationType: "knows", TargetEntityID: "bob", Confidence: 0.8}
	relHigher := store.Relation{SourceEntityID: "alice", RelationType: "knows", TargetEntityID: "bob", Confidence: 0.95}
	unrelated := store.Relation{SourceEntityID: "carol", RelationType: "works_at", TargetEntityID: "acme", Confidence: 0.9}

	result := mergeResults(
		&ExtractionResult{Relations: []store.Relation{rel}},
		&ExtractionResult{Relations: []store.Relation{relHigher, unrelated}},
	)

	if len(result.Relations) != 2 {
		t.Fatalf("expected 2 unique relations, got %d", len(result.Relations))
	}
	for _, r := range result.Relations {
		if r.SourceEntityID == "alice" && r.RelationType == "knows" && r.Confidence != 0.95 {
			t.Errorf("alice→knows→bob: expected 0.95, got %f", r.Confidence)
		}
	}
}

func TestMergeResults_OneSideEmpty(t *testing.T) {
	a := &ExtractionResult{
		Entities: []store.Entity{{ExternalID: "alice", Confidence: 0.9}},
	}
	result := mergeResults(a, &ExtractionResult{})
	if len(result.Entities) != 1 {
		t.Errorf("expected 1 entity from non-empty side, got %d", len(result.Entities))
	}
}

// --- Extract with mock provider ---

func TestExtract_ShortText_Success(t *testing.T) {
	respJSON := `{"entities":[{"external_id":"alice","name":"Alice","entity_type":"person","confidence":0.9}],"relations":[]}`
	e := NewExtractor(&mockProvider{response: providers.ChatResponse{Content: respJSON, FinishReason: "stop"}}, "m", 0.8)

	result, err := e.Extract(context.Background(), "Alice works at Acme.")
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(result.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(result.Entities))
	}
	if result.Entities[0].ExternalID != "alice" {
		t.Errorf("expected external_id 'alice', got %q", result.Entities[0].ExternalID)
	}
}

func TestExtract_FiltersLowConfidence(t *testing.T) {
	respJSON := `{"entities":[
		{"external_id":"high","name":"High","entity_type":"person","confidence":0.9},
		{"external_id":"low","name":"Low","entity_type":"person","confidence":0.5}
	],"relations":[]}`
	e := NewExtractor(&mockProvider{response: providers.ChatResponse{Content: respJSON, FinishReason: "stop"}}, "m", 0.8)

	result, err := e.Extract(context.Background(), "text")
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(result.Entities) != 1 || result.Entities[0].ExternalID != "high" {
		t.Errorf("expected 1 high-confidence entity, got %d: %+v", len(result.Entities), result.Entities)
	}
}

func TestExtract_NormalizesFields(t *testing.T) {
	respJSON := `{"entities":[{"external_id":"  ALICE  ","name":"  Alice  ","entity_type":"  PERSON  ","confidence":0.9}],"relations":[]}`
	e := NewExtractor(&mockProvider{response: providers.ChatResponse{Content: respJSON, FinishReason: "stop"}}, "m", 0.8)

	result, err := e.Extract(context.Background(), "text")
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(result.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(result.Entities))
	}
	ent := result.Entities[0]
	if ent.ExternalID != "alice" {
		t.Errorf("external_id not lowercased/trimmed: %q", ent.ExternalID)
	}
	if ent.Name != "Alice" {
		t.Errorf("name not trimmed: %q", ent.Name)
	}
	if ent.EntityType != "person" {
		t.Errorf("entity_type not lowercased: %q", ent.EntityType)
	}
}

func TestExtract_ProviderError(t *testing.T) {
	e := NewExtractor(&mockProvider{err: fmt.Errorf("connection refused")}, "m", 0.8)
	_, err := e.Extract(context.Background(), "text")
	if err == nil {
		t.Fatal("expected error when provider fails, got nil")
	}
}

func TestExtract_InvalidJSON(t *testing.T) {
	e := NewExtractor(&mockProvider{response: providers.ChatResponse{Content: "not json at all", FinishReason: "stop"}}, "m", 0.8)
	_, err := e.Extract(context.Background(), "text")
	if err == nil {
		t.Fatal("expected error for invalid JSON response, got nil")
	}
}

func TestExtract_CodeBlockStripped(t *testing.T) {
	respJSON := "```json\n{\"entities\":[{\"external_id\":\"alice\",\"name\":\"Alice\",\"entity_type\":\"person\",\"confidence\":0.9}],\"relations\":[]}\n```"
	e := NewExtractor(&mockProvider{response: providers.ChatResponse{Content: respJSON, FinishReason: "stop"}}, "m", 0.8)

	result, err := e.Extract(context.Background(), "Alice.")
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(result.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(result.Entities))
	}
}

func TestExtract_RelationsNormalized(t *testing.T) {
	respJSON := `{"entities":[],"relations":[
		{"source_entity_id":"  ALICE  ","relation_type":"  KNOWS  ","target_entity_id":"  BOB  ","confidence":0.9}
	]}`
	e := NewExtractor(&mockProvider{response: providers.ChatResponse{Content: respJSON, FinishReason: "stop"}}, "m", 0.8)

	result, err := e.Extract(context.Background(), "text")
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(result.Relations) != 1 {
		t.Fatalf("expected 1 relation, got %d", len(result.Relations))
	}
	rel := result.Relations[0]
	if rel.SourceEntityID != "alice" {
		t.Errorf("source_entity_id not normalized: %q", rel.SourceEntityID)
	}
	if rel.RelationType != "knows" {
		t.Errorf("relation_type not normalized: %q", rel.RelationType)
	}
	if rel.TargetEntityID != "bob" {
		t.Errorf("target_entity_id not normalized: %q", rel.TargetEntityID)
	}
}

func TestExtract_LongText_SplitsIntoChunks(t *testing.T) {
	// Build text longer than maxChunkChars (12000) with paragraph breaks.
	para := strings.Repeat("word ", 200) // ~1000 chars per para
	var paras []string
	for i := 0; i < 15; i++ {
		paras = append(paras, para)
	}
	longText := strings.Join(paras, "\n\n") // ~15000+ chars

	callCount := 0
	respJSON := `{"entities":[{"external_id":"e1","name":"E1","entity_type":"person","confidence":0.9}],"relations":[]}`
	mock := &countingMockProvider{
		response: providers.ChatResponse{Content: respJSON, FinishReason: "stop"},
		onCall:   func() { callCount++ },
	}
	e := NewExtractor(mock, "m", 0.8)

	result, err := e.Extract(context.Background(), longText)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if callCount < 2 {
		t.Errorf("expected multiple LLM calls for long text, got %d", callCount)
	}
	// All chunks return same external_id → merged to 1 entity.
	if len(result.Entities) != 1 {
		t.Errorf("expected 1 deduplicated entity, got %d", len(result.Entities))
	}
}

// countingMockProvider counts Chat calls for the long-text split test.
type countingMockProvider struct {
	response providers.ChatResponse
	err      error
	onCall   func()
}

func (m *countingMockProvider) Chat(_ context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	if m.onCall != nil {
		m.onCall()
	}
	if m.err != nil {
		return nil, m.err
	}
	r := m.response
	return &r, nil
}

func (m *countingMockProvider) ChatStream(_ context.Context, _ providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	r := m.response
	return &r, nil
}

func (m *countingMockProvider) DefaultModel() string { return "counting-mock" }
func (m *countingMockProvider) Name() string         { return "counting-mock" }
